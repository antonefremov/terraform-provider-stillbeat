package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/antonefremov/terraform-provider-stillbeat/internal/client"
)

var (
	_ resource.Resource                   = &checkResource{}
	_ resource.ResourceWithConfigure      = &checkResource{}
	_ resource.ResourceWithImportState    = &checkResource{}
	_ resource.ResourceWithValidateConfig = &checkResource{}
)

// NewCheckResource is the resource factory registered with the provider.
func NewCheckResource() resource.Resource { return &checkResource{} }

type checkResource struct {
	client *client.Client
}

// scheduleModel maps the nested `schedule` block.
type scheduleModel struct {
	Kind     types.String  `tfsdk:"kind"`
	Interval durationValue `tfsdk:"interval"`
	CronExpr types.String  `tfsdk:"cron_expr"`
	TZ       types.String  `tfsdk:"tz"`
}

// checkModel maps the stillbeat_check resource state.
type checkModel struct {
	ID           types.String  `tfsdk:"id"`
	Name         types.String  `tfsdk:"name"`
	Schedule     scheduleModel `tfsdk:"schedule"`
	Grace        durationValue `tfsdk:"grace"`
	Channels     types.Set     `tfsdk:"channels"`
	FlapCooldown durationValue `tfsdk:"flap_cooldown"`
	NagInterval  durationValue `tfsdk:"nag_interval"`
	MaxRun       durationValue `tfsdk:"max_run"`
	MaxRunMode   types.String  `tfsdk:"max_run_mode"`
	Paused       types.Bool    `tfsdk:"paused"`
	Status       types.String  `tfsdk:"status"`
	PingURL      types.String  `tfsdk:"ping_url"`
}

func (r *checkResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_check"
}

func (r *checkResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A monitored cron/heartbeat check. The job pings `ping_url` on success; a missed schedule + grace triggers alerts.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Server-generated check UUID.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-readable check name.",
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 200)},
			},
			"schedule": schema.SingleNestedAttribute{
				Required:            true,
				MarkdownDescription: "When the check is expected to ping.",
				Attributes: map[string]schema.Attribute{
					"kind": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "`interval` (fixed period) or `cron` (cron expression).",
						Validators:          []validator.String{stringvalidator.OneOf("interval", "cron")},
					},
					"interval": schema.StringAttribute{
						Optional:            true,
						CustomType:          durationType{},
						MarkdownDescription: "Expected period between pings, as a Go duration (e.g. `\"1h\"`). Required when `kind = interval`.",
					},
					"cron_expr": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Cron expression (5-field). Required when `kind = cron`.",
					},
					"tz": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "IANA timezone, e.g. `\"UTC\"` or `\"Europe/Berlin\"`.",
					},
				},
			},
			"grace": schema.StringAttribute{
				Optional:            true,
				CustomType:          durationType{},
				MarkdownDescription: "Grace period after a missed deadline before alerting, as a Go duration (e.g. `\"5m\"`).",
			},
			"channels": schema.SetAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Alert channels, e.g. `email:you@example.com`, `webhook:https://...`, `telegram:<chat_id>`.",
			},
			"flap_cooldown": schema.StringAttribute{
				Optional:            true,
				CustomType:          durationType{},
				MarkdownDescription: "Suppress a down alert that self-resolves within this window (Go duration).",
			},
			"nag_interval": schema.StringAttribute{
				Optional:            true,
				CustomType:          durationType{},
				MarkdownDescription: "Re-alert cadence while a check stays down (Go duration). Omit to disable nagging.",
			},
			"max_run": schema.StringAttribute{
				Optional:            true,
				CustomType:          durationType{},
				MarkdownDescription: "Run-too-long threshold between a `/start` and its finish (Go duration).",
			},
			"max_run_mode": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "`hung` (sweeper flips down if unfinished) or `late` (warn on a slow finish). Defaults to `hung` when `max_run` is set.",
				Validators:          []validator.String{stringvalidator.OneOf("hung", "late")},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"paused": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "When true, the check is paused (not monitored). Defaults to `false`.",
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Current status: `new`, `up`, `down`, or `paused`.",
			},
			"ping_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The URL the job pings on success. Wire this into your cron/CI.",
			},
		},
	}
}

func (r *checkResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("expected *client.Client, got %T", req.ProviderData))
		return
	}
	r.client = c
}

// ValidateConfig enforces the kind/field pairing at plan time — a friendlier
// error than the API's 400 on a later apply. The API remains the source of
// truth (cron syntax, tz, durations); this just catches the common mismatch.
func (r *checkResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data checkModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Skip when kind is unknown (e.g. driven by an unresolved variable).
	if data.Schedule.Kind.IsUnknown() || data.Schedule.Kind.IsNull() {
		return
	}
	switch data.Schedule.Kind.ValueString() {
	case "interval":
		if data.Schedule.Interval.IsNull() {
			resp.Diagnostics.AddAttributeError(
				path.Root("schedule").AtName("interval"),
				"Missing interval",
				`schedule.interval is required when schedule.kind is "interval".`)
		}
	case "cron":
		if data.Schedule.CronExpr.IsNull() {
			resp.Diagnostics.AddAttributeError(
				path.Root("schedule").AtName("cron_expr"),
				"Missing cron_expr",
				`schedule.cron_expr is required when schedule.kind is "cron".`)
		}
	}
}

func (r *checkResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan checkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, diags := r.toInput(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	check, err := r.client.CreateCheck(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Create check failed", err.Error())
		return
	}
	// paused defaults to false; if the user asked for paused, apply it now.
	if plan.Paused.ValueBool() {
		check, err = r.client.SetPaused(ctx, check.CheckID, true)
		if err != nil {
			resp.Diagnostics.AddError("Pause check failed", err.Error())
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, r.toModel(check, plan))...)
}

func (r *checkResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state checkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	check, err := r.client.GetCheck(ctx, state.ID.ValueString())
	if err != nil {
		if isNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read check failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, r.toModel(check, state))...)
}

func (r *checkResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state checkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, diags := r.toInput(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	check, err := r.client.UpdateCheck(ctx, id, in)
	if err != nil {
		resp.Diagnostics.AddError("Update check failed", err.Error())
		return
	}
	if plan.Paused.ValueBool() != state.Paused.ValueBool() {
		check, err = r.client.SetPaused(ctx, id, plan.Paused.ValueBool())
		if err != nil {
			resp.Diagnostics.AddError("Pause/resume failed", err.Error())
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, r.toModel(check, plan))...)
}

func (r *checkResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state checkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteCheck(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Delete check failed", err.Error())
	}
}

func (r *checkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// toInput builds the API create/update body from the plan.
func (r *checkResource) toInput(ctx context.Context, m checkModel) (client.CheckInput, diag.Diagnostics) {
	var diags diag.Diagnostics
	in := client.CheckInput{
		Name: m.Name.ValueString(),
		Schedule: client.Schedule{
			Kind:     m.Schedule.Kind.ValueString(),
			Interval: m.Schedule.Interval.ValueString(),
			CronExpr: m.Schedule.CronExpr.ValueString(),
			TZ:       m.Schedule.TZ.ValueString(),
		},
		Grace:        m.Grace.ValueString(),
		FlapCooldown: m.FlapCooldown.ValueString(),
		NagInterval:  m.NagInterval.ValueString(),
		MaxRun:       m.MaxRun.ValueString(),
		MaxRunMode:   m.MaxRunMode.ValueString(),
	}
	if !m.Channels.IsNull() && !m.Channels.IsUnknown() {
		diags.Append(m.Channels.ElementsAs(ctx, &in.Channels, false)...)
	}
	return in, diags
}

// toModel maps an API check into resource state. prior carries the user's
// configured values so optional fields that the API echoes can keep the
// caller's intent (channels ordering, paused) rather than drift.
func (r *checkResource) toModel(c *client.Check, prior checkModel) checkModel {
	m := checkModel{
		ID:           types.StringValue(c.CheckID),
		Name:         types.StringValue(c.Name),
		Grace:        secondsToDuration(c.GraceS),
		FlapCooldown: secondsToDuration(c.FlapCooldownS),
		NagInterval:  secondsToDuration(c.NagIntervalS),
		MaxRun:       secondsToDuration(c.MaxRunS),
		MaxRunMode:   optString(c.MaxRunMode),
		Paused:       types.BoolValue(c.Status == "paused"),
		Status:       types.StringValue(c.Status),
		PingURL:      types.StringValue(c.PingURL),
		Schedule: scheduleModel{
			Kind:     types.StringValue(c.ScheduleKind),
			Interval: secondsToDuration(c.IntervalS),
			CronExpr: optString(c.CronExpr),
			TZ:       types.StringValue(c.TZ),
		},
		Channels: prior.Channels, // set semantics — preserve the configured set
	}
	return m
}

func optString(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

func isNotFound(err error) bool {
	return errors.Is(err, client.ErrNotFound)
}
