package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/antonefremov/terraform-provider-alwaysbeat/internal/client"
)

var (
	_ datasource.DataSource              = &checkDataSource{}
	_ datasource.DataSourceWithConfigure = &checkDataSource{}
)

// NewCheckDataSource is the data source factory registered with the provider.
func NewCheckDataSource() datasource.DataSource { return &checkDataSource{} }

type checkDataSource struct {
	client *client.Client
}

func (d *checkDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_check"
}

func (d *checkDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up an existing check by its ID — e.g. to read the `ping_url` of a check managed elsewhere.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The check's UUID.",
			},
			"name": schema.StringAttribute{Computed: true, MarkdownDescription: "Check name."},
			"schedule": schema.SingleNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The check's schedule.",
				Attributes: map[string]schema.Attribute{
					"kind":      schema.StringAttribute{Computed: true, MarkdownDescription: "`interval` or `cron`."},
					"interval":  schema.StringAttribute{Computed: true, CustomType: durationType{}, MarkdownDescription: "Interval (Go duration), when kind is `interval`."},
					"cron_expr": schema.StringAttribute{Computed: true, MarkdownDescription: "Cron expression, when kind is `cron`."},
					"tz":        schema.StringAttribute{Computed: true, MarkdownDescription: "IANA timezone."},
				},
			},
			"grace":         schema.StringAttribute{Computed: true, CustomType: durationType{}, MarkdownDescription: "Grace period (Go duration)."},
			"channels":      schema.SetAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Alert channels."},
			"flap_cooldown": schema.StringAttribute{Computed: true, CustomType: durationType{}, MarkdownDescription: "Flap-cooldown window (Go duration)."},
			"nag_interval":  schema.StringAttribute{Computed: true, CustomType: durationType{}, MarkdownDescription: "Nag cadence while down (Go duration)."},
			"max_run":       schema.StringAttribute{Computed: true, CustomType: durationType{}, MarkdownDescription: "Run-too-long threshold (Go duration)."},
			"max_run_mode":  schema.StringAttribute{Computed: true, MarkdownDescription: "`hung` or `late`."},
			"paused":        schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the check is paused."},
			"status":        schema.StringAttribute{Computed: true, MarkdownDescription: "Current status: `new`, `up`, `down`, or `paused`."},
			"ping_url":      schema.StringAttribute{Computed: true, MarkdownDescription: "The URL the job pings on success."},
		},
	}
}

func (d *checkDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("expected *client.Client, got %T", req.ProviderData))
		return
	}
	d.client = c
}

func (d *checkDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	// Read ONLY the id from config: every other attribute is Computed and thus
	// null at config time, which can't be decoded into the non-nullable nested
	// structs (e.g. scheduleModel). We rebuild the rest from the API below.
	var id types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	c, err := d.client.GetCheck(ctx, id.ValueString())
	if err != nil {
		if isNotFound(err) {
			resp.Diagnostics.AddError("Check not found",
				fmt.Sprintf("no check with id %q", id.ValueString()))
			return
		}
		resp.Diagnostics.AddError("Read check failed", err.Error())
		return
	}

	channels, diags := types.SetValueFrom(ctx, types.StringType, c.Channels)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	model := checkModel{
		ID:           types.StringValue(c.CheckID),
		Name:         types.StringValue(c.Name),
		Grace:        secondsToDuration(c.GraceS),
		Channels:     channels,
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
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}
