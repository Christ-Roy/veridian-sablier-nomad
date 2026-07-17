package nomad

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/sablierapp/sablier/pkg/provider"
	"github.com/sablierapp/sablier/pkg/sablier"
)

// enabledTarget is a Sablier-enabled task group discovered from the cluster,
// resolved from the merged job+group meta (the same source of truth used by
// InstanceInspect and InstanceEvents).
type enabledTarget struct {
	name   string
	groups []string
}

// InstanceList discovers the Sablier-enabled task groups in the cluster.
//
// Discovery reads the full job spec (job list → per-job Info) and evaluates the
// MERGED job+group meta of every task group, exactly like InstanceInspect and
// InstanceEvents. This matters for two reasons:
//
//   - A target may opt in at the task-group level (sablier.enable on the group
//     meta). The job-list stub only carries job-level meta, so a group-level
//     opt-in is invisible to a stub-only scan — the event stream would add such
//     an instance (it reads the full job) while reconciliation, reading only the
//     stub, would drop it again ("removed ... reason=reconciliation").
//   - A scaled-to-zero / "dead" job is still registered and still appears in the
//     job list; its spec (and thus its meta and task groups) is fully readable.
//     Deriving instances from the spec — not from the running-allocation summary —
//     keeps sleeping targets in the list so Sablier can wake them on demand.
//
// Each enabled task group of an enabled job becomes one instance; single-group
// jobs use the bare job ID as their name, multi-group jobs use
// "<jobID><delim><group>".
func (p *Provider) InstanceList(ctx context.Context, _ provider.InstanceListOptions) ([]sablier.InstanceConfiguration, error) {
	targets, err := p.listEnabledTargets(ctx)
	if err != nil {
		return nil, err
	}

	instances := make([]sablier.InstanceConfiguration, 0, len(targets))
	for _, t := range targets {
		instances = append(instances, sablier.InstanceConfiguration{
			Name:    t.name,
			Groups:  t.groups,
			Enabled: "true",
		})
	}

	return instances, nil
}

// InstanceGroups maps each Sablier group to the instances that belong to it.
func (p *Provider) InstanceGroups(ctx context.Context) (map[string][]string, error) {
	targets, err := p.listEnabledTargets(ctx)
	if err != nil {
		return nil, err
	}

	groups := make(map[string][]string)
	for _, t := range targets {
		for _, group := range t.groups {
			groups[group] = append(groups[group], t.name)
		}
	}

	return groups, nil
}

// listEnabledTargets returns every task group in the cluster whose merged
// (job + group) meta opts into Sablier, regardless of its current replica count
// or status. It lists the jobs, then reads each job's full spec so that
// group-level sablier.* meta is honored and scaled-to-zero / dead jobs are
// still reported (they remain registered and readable until purged).
//
// This costs one extra Info call per job compared to a stub-only scan; on the
// small job counts Sablier reconciles every 30s that is negligible, and it is
// the price of discovery being consistent with the inspect/event paths.
func (p *Provider) listEnabledTargets(ctx context.Context) ([]enabledTarget, error) {
	stubs, _, err := p.Client.Jobs().List(p.queryOptions(ctx))
	if err != nil {
		return nil, fmt.Errorf("cannot list nomad jobs: %w", err)
	}

	var targets []enabledTarget
	for _, stub := range stubs {
		job, _, err := p.Client.Jobs().Info(stub.ID, p.queryOptions(ctx))
		if err != nil {
			// A job can be purged between the list and the inspect; skip it
			// rather than failing the whole reconciliation.
			p.l.WarnContext(ctx, "cannot inspect nomad job during discovery",
				slog.String("job", stub.ID),
				slog.Any("error", err),
			)
			continue
		}

		single := len(job.TaskGroups) == 1
		for _, tg := range job.TaskGroups {
			meta := mergeMeta(job, tg)
			if meta[sablier.LabelEnable] != "true" {
				continue
			}
			targets = append(targets, enabledTarget{
				name:   JobInstanceName(jobIDOf(job), groupNameOf(tg), single, ParseOptions{Delimiter: p.delimiter}),
				groups: sablier.ParseGroups(meta[sablier.LabelGroup]),
			})
		}
	}

	return targets, nil
}
