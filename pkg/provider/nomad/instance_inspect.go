package nomad

import (
	"context"

	nomadapi "github.com/hashicorp/nomad/api"

	"github.com/sablierapp/sablier/pkg/sablier"
)

// InstanceInspect reports the current state of a Nomad task group. Readiness is
// derived from the number of running allocations versus the desired count: an
// instance is Ready once at least the desired number of allocations are
// running, Starting while it is scaling up, and Stopped when its desired count
// is zero (or the job is stopped).
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

	info := sablier.InstanceInfo{
		Name:            parsed.Original,
		CurrentReplicas: int32(running),
		DesiredReplicas: int32(desired),
		Status:          instanceStatus(desired, running),
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
// pure so the readiness rule is unit-tested without a live cluster.
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

func (p *Provider) jobNamespace(job *nomadapi.Job) string {
	if job.Namespace != nil && *job.Namespace != "" {
		return *job.Namespace
	}
	return p.namespace
}
