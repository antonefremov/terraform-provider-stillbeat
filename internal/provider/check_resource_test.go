package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccCheckResource covers the resource lifecycle end-to-end against a live
// API: create, import, and update. Requires TF_ACC=1 + STILLBEAT_API_KEY.
func TestAccCheckResource(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create
				Config: testAccCheckResourceConfig(name, "5m"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("stillbeat_check.test", "name", name),
					resource.TestCheckResourceAttr("stillbeat_check.test", "grace", "5m"),
					resource.TestCheckResourceAttr("stillbeat_check.test", "schedule.kind", "interval"),
					resource.TestCheckResourceAttr("stillbeat_check.test", "paused", "false"),
					resource.TestCheckResourceAttrSet("stillbeat_check.test", "id"),
					resource.TestCheckResourceAttrSet("stillbeat_check.test", "ping_url"),
				),
			},
			{ // import
				ResourceName:      "stillbeat_check.test",
				ImportState:       true,
				ImportStateVerify: true,
				// Duration attributes use a semantic-equality type: state keeps the
				// user's spelling ("5m") while a fresh import reads the API's
				// canonical form ("5m0s"). ImportStateVerify compares raw strings,
				// so these two spellings are ignored here (they ARE semantically
				// equal, which the no-drift plan proves).
				ImportStateVerifyIgnore: []string{"grace", "schedule.interval"},
			},
			{ // update grace
				Config: testAccCheckResourceConfig(name, "10m"),
				Check:  resource.TestCheckResourceAttr("stillbeat_check.test", "grace", "10m"),
			},
		},
	})
}

// TestAccCheckDataSource verifies the data source reads a check created by the
// resource in the same config.
func TestAccCheckDataSource(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-ds")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccCheckDataSourceConfig(name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.stillbeat_check.by_id", "name", name),
					resource.TestCheckResourceAttrPair("data.stillbeat_check.by_id", "id", "stillbeat_check.seed", "id"),
					resource.TestCheckResourceAttrPair("data.stillbeat_check.by_id", "ping_url", "stillbeat_check.seed", "ping_url"),
				),
			},
		},
	})
}

func testAccCheckResourceConfig(name, grace string) string {
	return fmt.Sprintf(`
resource "stillbeat_check" "test" {
  name = %[1]q
  schedule = {
    kind     = "interval"
    interval = "15m"
    tz       = "UTC"
  }
  grace = %[2]q
}
`, name, grace)
}

func testAccCheckDataSourceConfig(name string) string {
	return fmt.Sprintf(`
resource "stillbeat_check" "seed" {
  name = %[1]q
  schedule = {
    kind     = "interval"
    interval = "15m"
    tz       = "UTC"
  }
}

data "stillbeat_check" "by_id" {
  id = stillbeat_check.seed.id
}
`, name)
}
