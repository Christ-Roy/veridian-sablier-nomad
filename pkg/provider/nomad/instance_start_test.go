package nomad

import (
	"testing"

	"github.com/sablierapp/sablier/pkg/sablier"
)

func TestActiveReplicas(t *testing.T) {
	tests := []struct {
		name   string
		meta   map[string]string
		parsed ParsedName
		want   int
	}{
		{
			name:   "default is one",
			meta:   map[string]string{},
			parsed: ParsedName{},
			want:   1,
		},
		{
			name:   "name replicas honored when no label",
			meta:   map[string]string{},
			parsed: ParsedName{Replicas: 3},
			want:   3,
		},
		{
			name:   "label wins over name replicas",
			meta:   map[string]string{sablier.LabelActiveReplicas: "2"},
			parsed: ParsedName{Replicas: 5},
			want:   2,
		},
		{
			name:   "name replicas of one falls back to label default",
			meta:   map[string]string{},
			parsed: ParsedName{Replicas: 1},
			want:   1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := sablier.ScaleConfigFromLabels(tt.meta)
			if got := activeReplicas(sc, tt.meta, tt.parsed); got != tt.want {
				t.Errorf("activeReplicas=%d want %d", got, tt.want)
			}
		})
	}
}

func TestInstanceDependencies(t *testing.T) {
	p := &Provider{}
	deps, err := p.InstanceDependencies(t.Context(), "whoami")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deps != nil {
		t.Errorf("nomad provider reports no dependencies, got %v", deps)
	}
}
