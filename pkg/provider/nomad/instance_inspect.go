package nomad

import (
	"context"
	"fmt"
	"log/slog"

	nomadapi "github.com/hashicorp/nomad/api"

	"github.com/sablierapp/sablier/pkg/sablier"
)

// checkStatusSuccess is the Nomad service-check status reported for a passing
// check (mirrors structs.CheckSuccess). Sablier treats an allocation as healthy
// only when every one of its checks reports this value.
const checkStatusSuccess = "success"

// InstanceInspect reports the current state of a Nomad task group. Readiness is
// gated on the REAL health of the app, not merely on the container running: an
// instance is Ready once at least the desired number of allocations are running
// AND every Nomad service check of those allocations reports "success". While
// allocations are running but their checks are still pending/failing the
// instance is Starting (so Sablier keeps the waiting page up until the app
// actually serves — this closes the cold-start window where a half-booted app
// serves its SPA index.html as a catch-all for asset requests, which the
// browser then refuses to execute under X-Content-Type-Options: nosniff).
//
// Targets that expose no service check at all fall back to the running-count
// rule so a checkless job is never held in Starting forever.
func (p *Provider) InstanceInspect(ctx context.Context, name string) (sablier.InstanceInfo, error) {
	parsed, err := ParseName(name, ParseOptions{Delimiter: p.delimiter})
	if err != nil {
		return sablier.InstanceInfo{}, err
	}

	job, tg, meta, err := p.workload(ctx, parsed)
	if err != nil {
		return sablier.InstanceInfo{}, err
	}

	desired := groupCount(tg)
	// A stopped (or fully drained) job has no running allocations regardless of
	// the group's declared count.
	if job.Stop != nil && *job.Stop {
		desired = 0
	}

	running := 0
	summary, _, err := p.Client.Jobs().Summary(parsed.JobID, p.queryOptions(ctx))
	if err != nil {
		return sablier.InstanceInfo{}, err
	}
	if tgs, ok := summary.Summary[groupNameOf(tg)]; ok {
		running = tgs.Running
	}

	// Health-gate: count how many running allocations are fully healthy. Only
	// probe when the job is meant to run (desired > 0) — a sleeping target has
	// no allocations to check and must report Stopped without extra API calls.
	status := sablier.InstanceStatusStopped
	if desired > 0 {
		healthy, checksSeen, herr := p.groupHealth(ctx, parsed.JobID, groupNameOf(tg))
		if herr != nil {
			return sablier.InstanceInfo{}, herr
		}
		status = instanceStatusFromHealth(desired, running, healthy, checksSeen)
	}

	info := sablier.InstanceInfo{
		Name:            parsed.Original,
		CurrentReplicas: int32(running),
		DesiredReplicas: int32(desired),
		Status:          status,
	}

	sablier.PopulateEnabledAndGroup(&info, meta)

	info.Provider = sablier.ProviderNomad
	info.Nomad = &sablier.NomadJobInfo{
		Namespace: p.jobNamespace(job),
		JobID:     jobIDOf(job),
		Group:     groupNameOf(tg),
		Image:     groupImage(tg),
		Meta:      meta,
	}

	return info, nil
}

// instanceStatus maps a desired/running pair to a Sablier status. It is kept
// pure so the readiness rule is unit-tested without a live cluster. It is the
// fallback rule used when no health check is available to gate on.
func instanceStatus(desired, running int) sablier.InstanceStatus {
	switch {
	case desired <= 0:
		return sablier.InstanceStatusStopped
	case running >= desired:
		return sablier.InstanceStatusReady
	default:
		return sablier.InstanceStatusStarting
	}
}

// instanceStatusFromHealth maps a desired/running/healthy triple to a Sablier
// status, gating Ready on the app's real health. It is pure so the readiness
// rule is unit-tested without a live cluster.
//
//   - desired <= 0                       → Stopped
//   - no check exposed (checksSeen false) → fall back to the running-count rule
//   - healthy >= desired                  → Ready
//   - otherwise                           → Starting
func instanceStatusFromHealth(desired, running, healthy int, checksSeen bool) sablier.InstanceStatus {
	if desired <= 0 {
		return sablier.InstanceStatusStopped
	}
	if !checksSeen {
		// No health signal at all: don't hold the target hostage in Starting,
		// fall back to "container running is good enough".
		return instanceStatus(desired, running)
	}
	if healthy >= desired {
		return sablier.InstanceStatusReady
	}
	return sablier.InstanceStatusStarting
}

// groupHealth inspects the running allocations of a task group and reports how
// many are fully healthy (every Nomad service check reporting "success"), plus
// whether any running allocation exposed at least one check at all. The latter
// lets the caller fall back to a running-only readiness rule for checkless
// targets instead of holding them in Starting forever.
//
// A per-allocation Checks() error (e.g. the owning client is momentarily
// unreachable) is logged and treated as "no health signal for that allocation"
// rather than failing the whole inspect: a transient client blip must not wedge
// the wake path.
func (p *Provider) groupHealth(ctx context.Context, jobID, group string) (healthy int, checksSeen bool, err error) {
	stubs, _, err := p.Client.Jobs().Allocations(jobID, false, p.queryOptions(ctx))
	if err != nil {
		return 0, false, fmt.Errorf("cannot list allocations for nomad job %q: %w", jobID, err)
	}

	for _, a := range stubs {
		if a.TaskGroup != group {
			continue
		}
		if a.DesiredStatus != nomadapi.AllocDesiredStatusRun || a.ClientStatus != nomadapi.AllocClientStatusRunning {
			continue
		}

		checks, cerr := p.Client.Allocations().Checks(a.ID, p.queryOptions(ctx))
		if cerr != nil {
			p.l.WarnContext(ctx, "cannot read allocation checks; treating as no health signal",
				slog.String("job", jobID),
				slog.String("group", group),
				slog.String("alloc", a.ID),
				slog.Any("error", cerr),
			)
			continue
		}
		if len(checks) == 0 {
			continue
		}
		checksSeen = true
		if allChecksHealthy(checks) {
			healthy++
		}
	}

	return healthy, checksSeen, nil
}

// allChecksHealthy reports whether every check in the set is passing. An empty
// set is not healthy (the caller filters those out via checksSeen).
func allChecksHealthy(checks nomadapi.AllocCheckStatuses) bool {
	if len(checks) == 0 {
		return false
	}
	for _, c := range checks {
		if c.Status != checkStatusSuccess {
			return false
		}
	}
	return true
}

func (p *Provider) jobNamespace(job *nomadapi.Job) string {
	if job.Namespace != nil && *job.Namespace != "" {
		return *job.Namespace
	}
	return p.namespace
}
