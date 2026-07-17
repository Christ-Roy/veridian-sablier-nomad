package nomad

import (
	"log/slog"
	"testing"

	nomadapi "github.com/hashicorp/nomad/api"

	"github.com/sablierapp/sablier/pkg/provider"
	"github.com/sablierapp/sablier/pkg/sablier"
)

func TestClassifyTransition(t *testing.T) {
	tests := []struct {
		name string
		prev int
		seen bool
		cur  int
		want []provider.InstanceEventType
	}{
		{name: "first sight yields created", seen: false, cur: 0, want: []provider.InstanceEventType{provider.InstanceEventCreated}},
		{name: "first sight running yields created", seen: false, cur: 3, want: []provider.InstanceEventType{provider.InstanceEventCreated}},
		{name: "scale from zero yields started", seen: true, prev: 0, cur: 2, want: []provider.InstanceEventType{provider.InstanceEventStarted}},
		{name: "scale to zero yields stopped", seen: true, prev: 2, cur: 0, want: []provider.InstanceEventType{provider.InstanceEventStopped}},
		{name: "no crossing yields nothing", seen: true, prev: 2, cur: 3, want: nil},
		{name: "stay zero yields nothing", seen: true, prev: 0, cur: 0, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyTransition(tt.prev, tt.seen, tt.cur)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v want %v", got, tt.want)
				}
			}
		})
	}
}

func testProvider() *Provider {
	return &Provider{delimiter: "@", namespace: "default", l: slog.Default()}
}

func singleGroupJob(count int) *nomadapi.Job {
	return &nomadapi.Job{
		ID:   strptr("whoami"),
		Meta: map[string]string{sablier.LabelEnable: "true"},
		TaskGroups: []*nomadapi.TaskGroup{
			{Name: strptr("web"), Count: intptr(count)},
		},
	}
}

func TestTransitionEvents_Created(t *testing.T) {
	p := testProvider()
	counts := map[string]int{}

	events := p.transitionEvents(singleGroupJob(0), counts, true, true, true)
	if len(events) != 1 || events[0].Type != provider.InstanceEventCreated {
		t.Fatalf("expected one created event, got %+v", events)
	}
	if events[0].Info.Name != "whoami" {
		t.Errorf("single-group job should use bare name, got %q", events[0].Info.Name)
	}
	if events[0].Info.Provider != sablier.ProviderNomad {
		t.Errorf("provider not tagged: %q", events[0].Info.Provider)
	}
	if counts["whoami"] != 0 {
		t.Errorf("count not tracked: %v", counts)
	}
}

func TestTransitionEvents_StartedAndStopped(t *testing.T) {
	p := testProvider()
	counts := map[string]int{"whoami": 0} // already seen, idle

	started := p.transitionEvents(singleGroupJob(2), counts, true, true, true)
	if len(started) != 1 || started[0].Type != provider.InstanceEventStarted {
		t.Fatalf("expected started, got %+v", started)
	}
	if started[0].Info.Status != sablier.InstanceStatusStarting {
		t.Errorf("started status should be starting, got %q", started[0].Info.Status)
	}

	stopped := p.transitionEvents(singleGroupJob(0), counts, true, true, true)
	if len(stopped) != 1 || stopped[0].Type != provider.InstanceEventStopped {
		t.Fatalf("expected stopped, got %+v", stopped)
	}
}

func TestTransitionEvents_StopFlagForcesZero(t *testing.T) {
	p := testProvider()
	counts := map[string]int{"whoami": 3}

	job := singleGroupJob(3)
	job.Stop = boolptr(true) // job stopped even though group count is 3

	events := p.transitionEvents(job, counts, true, true, true)
	if len(events) != 1 || events[0].Type != provider.InstanceEventStopped {
		t.Fatalf("expected stopped due to job.Stop, got %+v", events)
	}
}

func TestTransitionEvents_TypeFiltering(t *testing.T) {
	p := testProvider()
	counts := map[string]int{"whoami": 0}

	// Only interested in stopped events: a start transition yields nothing.
	events := p.transitionEvents(singleGroupJob(2), counts, true /*stopped*/, false /*started*/, false /*created*/)
	if len(events) != 0 {
		t.Fatalf("started event should be filtered out, got %+v", events)
	}
	// But the count is still tracked so a later stop is detected.
	if counts["whoami"] != 2 {
		t.Errorf("count should update even when event filtered: %v", counts)
	}
}

func TestRemovalEvents(t *testing.T) {
	p := testProvider()
	counts := map[string]int{"whoami": 2}

	events := p.removalEvents(singleGroupJob(2), counts, true /*stopped*/, true /*removed*/)
	if len(events) != 2 {
		t.Fatalf("expected removed+stopped, got %+v", events)
	}
	if events[0].Type != provider.InstanceEventRemoved || events[1].Type != provider.InstanceEventStopped {
		t.Errorf("unexpected event order/types: %+v", events)
	}
	if _, ok := counts["whoami"]; ok {
		t.Errorf("removed instance should be forgotten: %v", counts)
	}
}

func TestTransitionEvents_MultiGroupNaming(t *testing.T) {
	p := testProvider()
	counts := map[string]int{}

	job := &nomadapi.Job{
		ID:   strptr("stack"),
		Meta: map[string]string{sablier.LabelEnable: "true"},
		TaskGroups: []*nomadapi.TaskGroup{
			{Name: strptr("web"), Count: intptr(0)},
			{Name: strptr("worker"), Count: intptr(0)},
		},
	}

	events := p.transitionEvents(job, counts, true, true, true)
	if len(events) != 2 {
		t.Fatalf("expected one created per group, got %+v", events)
	}
	names := map[string]bool{events[0].Info.Name: true, events[1].Info.Name: true}
	if !names["stack@web"] || !names["stack@worker"] {
		t.Errorf("multi-group names should be qualified, got %v", names)
	}
}

func TestLinearBackoff(t *testing.T) {
	if linearBackoff(0) != 0 {
		t.Error("attempt 0 should have no backoff")
	}
	if linearBackoff(100) != 30*1e9 { // 30s cap in nanoseconds
		t.Errorf("backoff should cap at 30s, got %v", linearBackoff(100))
	}
}
