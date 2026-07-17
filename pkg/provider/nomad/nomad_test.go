package nomad

import (
	"log/slog"
	"testing"

	nomadapi "github.com/hashicorp/nomad/api"

	"github.com/sablierapp/sablier/pkg/config"
)

func TestNew_ConnectionError(t *testing.T) {
	// Point at a port that refuses connections so New fails fast on the leader
	// probe instead of hanging.
	cfg := nomadapi.DefaultConfig()
	cfg.Address = "http://127.0.0.1:1"
	client, err := nomadapi.NewClient(cfg)
	if err != nil {
		t.Fatalf("cannot build client: %v", err)
	}

	if _, err := New(t.Context(), client, slog.Default(), config.Nomad{}); err == nil {
		t.Fatal("expected a connection error against an unreachable nomad")
	}
}

func TestQueryOptions(t *testing.T) {
	p := &Provider{namespace: "prod"}
	q := p.queryOptions(t.Context())
	if q.Namespace != "prod" {
		t.Errorf("query namespace=%q want prod", q.Namespace)
	}

	p = &Provider{namespace: ""}
	q = p.queryOptions(t.Context())
	if q.Namespace != "" {
		t.Errorf("empty namespace should not be forced, got %q", q.Namespace)
	}
}

func TestWriteOptions(t *testing.T) {
	p := &Provider{namespace: "prod"}
	w := p.writeOptions(t.Context())
	if w.Namespace != "prod" {
		t.Errorf("write namespace=%q want prod", w.Namespace)
	}
}
