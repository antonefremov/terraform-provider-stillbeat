package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories registers the in-process provider under the
// "stillbeat" name for acceptance tests.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"stillbeat": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccPreCheck asserts the environment needed for acceptance tests.
//
// These tests run against a REAL Stillbeat API and create/destroy real checks —
// point STILLBEAT_ENDPOINT at staging, never prod. They only run when TF_ACC is
// set (resource.Test skips otherwise), so plain `go test` stays offline.
func testAccPreCheck(t *testing.T) {
	if os.Getenv("STILLBEAT_API_KEY") == "" {
		t.Fatal("STILLBEAT_API_KEY must be set for TF_ACC acceptance tests")
	}
}
