package nomad

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	nomadapi "github.com/hashicorp/nomad/api"

	"github.com/sablierapp/sablier/pkg/provider"
	"github.com/sablierapp/sablier/pkg/sablier"
)

// fakeNomad serves the two endpoints the discovery path uses: the job list and
// per-job Info. It lets us exercise InstanceList/InstanceGroups without a live
// cluster, the way a mocked Docker/Swarm client does for those providers.
func fakeNomad(t *testing.T, stubs []*nomadapi.JobListStub, jobs map[string]*nomadapi.Job) *Provider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/jobs":
			_ = json.NewEncoder(w).Encode(stubs)
		case len(r.URL.Path) > len("/v1/job/") && r.URL.Path[:len("/v1/job/")] == "/v1/job/":
			id := r.URL.Path[len("/v1/job/"):]
			job, ok := jobs[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(job)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := nomadapi.DefaultConfig()
	cfg.Address = srv.URL
	client, err := nomadapi.NewClient(cfg)
	if err != nil {
		t.Fatalf("cannot build nomad client: %v", err)
	}
	return &Provider{Client: client, delimiter: "@", l: slog.Default()}
}

// TestInstanceList_ReturnsScaledToZeroGroupMeta is the regression test for the
// scale-to-zero bug: a single-group job scaled to count 0 (Status "dead") whose
// sablier.enable lives on the TASK GROUP meta must still be listed, so a
// reconciliation does not evict a sleeping target that Sablier must be able to
// wake.
func TestInstanceList_ReturnsScaledToZeroGroupMeta(t *testing.T) {
	const jobID = "cms-staging"

	stubs := []*nomadapi.JobListStub{{
		ID:     jobID,
		Name:   jobID,
		Status: "dead", // scaled to zero, no running allocations
		JobSummary: &nomadapi.JobSummary{
			JobID: jobID,
			Summary: map[string]nomadapi.TaskGroupSummary{
				"cms": {Running: 0, Complete: 3},
			},
		},
	}}
	jobs := map[string]*nomadapi.Job{
		jobID: {
			ID:   strptr(jobID),
			Name: strptr(jobID),
			Stop: boolptr(false),
			TaskGroups: []*nomadapi.TaskGroup{{
				Name:  strptr("cms"),
				Count: intptr(0), // scaled to zero
				Meta: map[string]string{
					// Opt-in declared on the GROUP, not the job — invisible to a
					// job-list-stub-only scan.
					sablier.LabelEnable: "true",
					sablier.LabelGroup:  "staging",
				},
			}},
		},
	}

	p := fakeNomad(t, stubs, jobs)

	instances, err := p.InstanceList(t.Context(), provider.InstanceListOptions{})
	if err != nil {
		t.Fatalf("InstanceList: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected the scaled-to-zero job to be listed, got %d instances: %+v", len(instances), instances)
	}
	got := instances[0]
	if got.Name != jobID {
		t.Errorf("instance name = %q, want %q", got.Name, jobID)
	}
	if got.Enabled != "true" || !got.IsEnabled() {
		t.Errorf("instance should be enabled, got Enabled=%q", got.Enabled)
	}
	if len(got.Groups) != 1 || got.Groups[0] != "staging" {
		t.Errorf("groups = %v, want [staging]", got.Groups)
	}

	groups, err := p.InstanceGroups(t.Context())
	if err != nil {
		t.Fatalf("InstanceGroups: %v", err)
	}
	if members := groups["staging"]; len(members) != 1 || members[0] != jobID {
		t.Errorf("groups[staging] = %v, want [%s]", members, jobID)
	}
}

// TestInstanceList_JobLevelMetaMultiGroup covers the job-level opt-in and the
// multi-group naming (jobID@group), and confirms a non-enabled group of the
// same job is not listed.
func TestInstanceList_JobLevelMetaMultiGroup(t *testing.T) {
	const jobID = "stack"

	stubs := []*nomadapi.JobListStub{{ID: jobID, Name: jobID, Status: "running"}}
	jobs := map[string]*nomadapi.Job{
		jobID: {
			ID:   strptr(jobID),
			Name: strptr(jobID),
			Stop: boolptr(false),
			// Enable at the job level; it merges into every group.
			Meta: map[string]string{sablier.LabelEnable: "true"},
			TaskGroups: []*nomadapi.TaskGroup{
				{Name: strptr("web"), Count: intptr(1)},
				{Name: strptr("worker"), Count: intptr(0)}, // sleeping group still listed
			},
		},
	}

	p := fakeNomad(t, stubs, jobs)

	instances, err := p.InstanceList(t.Context(), provider.InstanceListOptions{})
	if err != nil {
		t.Fatalf("InstanceList: %v", err)
	}
	names := map[string]bool{}
	for _, in := range instances {
		names[in.Name] = true
	}
	if !names["stack@web"] || !names["stack@worker"] {
		t.Errorf("expected both stack@web and stack@worker, got %v", names)
	}
}

// TestInstanceList_SkipsDisabled makes sure a job with no sablier opt-in is
// ignored rather than listed.
func TestInstanceList_SkipsDisabled(t *testing.T) {
	const jobID = "plain"
	stubs := []*nomadapi.JobListStub{{ID: jobID, Name: jobID, Status: "running"}}
	jobs := map[string]*nomadapi.Job{
		jobID: {
			ID:         strptr(jobID),
			Name:       strptr(jobID),
			Stop:       boolptr(false),
			TaskGroups: []*nomadapi.TaskGroup{{Name: strptr("app"), Count: intptr(1)}},
		},
	}

	p := fakeNomad(t, stubs, jobs)

	instances, err := p.InstanceList(t.Context(), provider.InstanceListOptions{})
	if err != nil {
		t.Fatalf("InstanceList: %v", err)
	}
	if len(instances) != 0 {
		t.Fatalf("a non-opted-in job must not be listed, got %+v", instances)
	}
}
