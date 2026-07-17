package nomad

import (
	"os"
	"testing"

	nomadapi "github.com/hashicorp/nomad/api"
	"github.com/neilotoole/slogt"

	"github.com/sablierapp/sablier/pkg/config"
	"github.com/sablierapp/sablier/pkg/provider"
)

// TestIntegration_ListInstances exercises the provider against a real Nomad
// cluster. It is skipped unless SABLIER_NOMAD_INTEGRATION=1 and the standard
// NOMAD_ADDR / NOMAD_TOKEN environment variables are set, so it never runs in
// CI (which has no Nomad). It always compiles, keeping the integration path
// from bit-rotting.
//
// A full end-to-end scale-to-zero test would use testcontainers to boot a
// single-node Nomad agent; that image is not available in this environment, so
// the integration surface is intentionally a smoke test against an operator's
// own cluster.
func TestIntegration_ListInstances(t *testing.T) {
	if os.Getenv("SABLIER_NOMAD_INTEGRATION") != "1" {
		t.Skip("set SABLIER_NOMAD_INTEGRATION=1 and NOMAD_ADDR/NOMAD_TOKEN to run")
	}

	cfg := nomadapi.DefaultConfig()
	client, err := nomadapi.NewClient(cfg)
	if err != nil {
		t.Fatalf("cannot build nomad client: %v", err)
	}

	p, err := New(t.Context(), client, slogt.New(t), config.Nomad{Delimiter: "@"})
	if err != nil {
		t.Fatalf("cannot connect to nomad: %v", err)
	}

	instances, err := p.InstanceList(t.Context(), provider.InstanceListOptions{All: true})
	if err != nil {
		t.Fatalf("InstanceList failed: %v", err)
	}
	t.Logf("discovered %d sablier-enabled instances", len(instances))

	groups, err := p.InstanceGroups(t.Context())
	if err != nil {
		t.Fatalf("InstanceGroups failed: %v", err)
	}
	t.Logf("discovered %d groups", len(groups))
}
