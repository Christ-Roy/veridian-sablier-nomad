package sabliercmd

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	nomadapi "github.com/hashicorp/nomad/api"

	"github.com/sablierapp/sablier/pkg/config"
)

func TestSetupProviderInvalidConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if _, err := setupProvider(context.Background(), logger, config.Provider{Name: ""}); err == nil {
		t.Fatal("expected an error for an invalid provider configuration")
	}
}

func TestNewNomadAPIConfigPreservesUnixSocketTransport(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "nomad.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on unix socket: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/status/leader" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode("127.0.0.1:4647")
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	t.Setenv("NOMAD_ADDR", "unix://"+socketPath)

	cfg := newNomadAPIConfig(config.Nomad{Namespace: "default"})

	if cfg.Address != "unix://"+socketPath {
		t.Fatalf("address=%q, want unix socket", cfg.Address)
	}
	if cfg.Namespace != "default" {
		t.Fatalf("namespace=%q, want default", cfg.Namespace)
	}
	if cfg.HttpClient != nil {
		t.Fatal("unix socket transport must be configured by nomadapi.NewClient")
	}

	client, err := nomadapi.NewClient(cfg)
	if err != nil {
		t.Fatalf("create nomad client: %v", err)
	}
	leader, err := client.Status().Leader()
	if err != nil {
		t.Fatalf("query leader over unix socket: %v", err)
	}
	if leader != "127.0.0.1:4647" {
		t.Fatalf("leader=%q, want 127.0.0.1:4647", leader)
	}
}

func TestNewNomadAPIConfigInstrumentsHTTPTransport(t *testing.T) {
	t.Setenv("NOMAD_ADDR", "http://127.0.0.1:4646")

	cfg := newNomadAPIConfig(config.Nomad{})

	if cfg.HttpClient == nil || cfg.HttpClient.Transport == nil {
		t.Fatal("HTTP transport must be instrumented")
	}
}
