package nomad

import (
	"testing"

	nomadapi "github.com/hashicorp/nomad/api"

	"github.com/sablierapp/sablier/pkg/sablier"
)

func strptr(s string) *string { return &s }
func intptr(i int) *int       { return &i }
func boolptr(b bool) *bool    { return &b }

func TestInstanceStatus(t *testing.T) {
	tests := []struct {
		desired, running int
		want             sablier.InstanceStatus
	}{
		{desired: 0, running: 0, want: sablier.InstanceStatusStopped},
		{desired: 0, running: 3, want: sablier.InstanceStatusStopped}, // draining
		{desired: 1, running: 0, want: sablier.InstanceStatusStarting},
		{desired: 2, running: 1, want: sablier.InstanceStatusStarting},
		{desired: 1, running: 1, want: sablier.InstanceStatusReady},
		{desired: 2, running: 3, want: sablier.InstanceStatusReady}, // over-provisioned
	}
	for _, tt := range tests {
		if got := instanceStatus(tt.desired, tt.running); got != tt.want {
			t.Errorf("instanceStatus(%d,%d)=%q want %q", tt.desired, tt.running, got, tt.want)
		}
	}
}

func TestStatusForCount(t *testing.T) {
	if got := statusForCount(0); got != sablier.InstanceStatusStopped {
		t.Errorf("statusForCount(0)=%q", got)
	}
	if got := statusForCount(2); got != sablier.InstanceStatusStarting {
		t.Errorf("statusForCount(2)=%q", got)
	}
}

func TestGroupCount(t *testing.T) {
	if got := groupCount(&nomadapi.TaskGroup{}); got != 1 {
		t.Errorf("unset count should default to 1, got %d", got)
	}
	if got := groupCount(&nomadapi.TaskGroup{Count: intptr(0)}); got != 0 {
		t.Errorf("count 0 should be honored, got %d", got)
	}
	if got := groupCount(&nomadapi.TaskGroup{Count: intptr(5)}); got != 5 {
		t.Errorf("count 5, got %d", got)
	}
}

func TestMergeMeta(t *testing.T) {
	job := &nomadapi.Job{Meta: map[string]string{
		sablier.LabelEnable: "true",
		sablier.LabelGroup:  "job-level",
	}}
	tg := &nomadapi.TaskGroup{Meta: map[string]string{
		sablier.LabelGroup: "group-level", // overrides job meta
	}}
	merged := mergeMeta(job, tg)
	if merged[sablier.LabelEnable] != "true" {
		t.Errorf("job meta should survive merge: %+v", merged)
	}
	if merged[sablier.LabelGroup] != "group-level" {
		t.Errorf("group meta should take precedence: %+v", merged)
	}
}

func TestGroupImage(t *testing.T) {
	tg := &nomadapi.TaskGroup{Tasks: []*nomadapi.Task{
		{Driver: "exec", Config: map[string]any{}},
		{Driver: "docker", Config: map[string]any{"image": "traefik/whoami:latest"}},
	}}
	if got := groupImage(tg); got != "traefik/whoami:latest" {
		t.Errorf("groupImage=%q", got)
	}
	if got := groupImage(&nomadapi.TaskGroup{}); got != "" {
		t.Errorf("empty group should yield no image, got %q", got)
	}
}

func TestSelectGroup(t *testing.T) {
	single := &nomadapi.Job{
		ID:         strptr("whoami"),
		TaskGroups: []*nomadapi.TaskGroup{{Name: strptr("web")}},
	}
	multi := &nomadapi.Job{
		ID: strptr("stack"),
		TaskGroups: []*nomadapi.TaskGroup{
			{Name: strptr("web")},
			{Name: strptr("worker")},
		},
	}

	// Empty group on a single-group job resolves automatically.
	tg, err := selectGroup(single, "")
	if err != nil || groupNameOf(tg) != "web" {
		t.Fatalf("single-group auto-resolve failed: tg=%v err=%v", tg, err)
	}

	// Empty group on a multi-group job is ambiguous.
	if _, err := selectGroup(multi, ""); err == nil {
		t.Error("expected ambiguity error for multi-group job with empty group")
	}

	// Explicit group is resolved by name.
	tg, err = selectGroup(multi, "worker")
	if err != nil || groupNameOf(tg) != "worker" {
		t.Fatalf("explicit group resolve failed: tg=%v err=%v", tg, err)
	}

	// Unknown group errors.
	if _, err := selectGroup(multi, "nope"); err == nil {
		t.Error("expected error for unknown group")
	}
}

func TestJobNamespace(t *testing.T) {
	p := &Provider{namespace: "fallback"}
	if got := p.jobNamespace(&nomadapi.Job{Namespace: strptr("prod")}); got != "prod" {
		t.Errorf("job namespace should win, got %q", got)
	}
	if got := p.jobNamespace(&nomadapi.Job{}); got != "fallback" {
		t.Errorf("should fall back to provider namespace, got %q", got)
	}
}
