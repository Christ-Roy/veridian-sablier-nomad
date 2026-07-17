package nomad

import (
	"context"
	"fmt"
	"slices"

	nomadapi "github.com/hashicorp/nomad/api"

	"github.com/sablierapp/sablier/pkg/provider"
	"github.com/sablierapp/sablier/pkg/sablier"
)

// InstanceList discovers the Sablier-enabled task groups in the cluster.
// Discovery reads the job-level meta (returned inline by the jobs list with
// Fields.Meta), so a job opts in by setting sablier.enable=true in its job meta.
// Each task group of an enabled job becomes one instance; single-group jobs use
// the bare job ID as their name, multi-group jobs use "<jobID><delim><group>".
func (p *Provider) InstanceList(ctx context.Context, _ provider.InstanceListOptions) ([]sablier.InstanceConfiguration, error) {
	stubs, err := p.listEnabledJobs(ctx)
	if err != nil {
		return nil, err
	}

	instances := make([]sablier.InstanceConfiguration, 0, len(stubs))
	for _, stub := range stubs {
		groupNames := groupNamesOf(stub)
		single := len(groupNames) == 1
		enabled := stub.Meta[sablier.LabelEnable]
		groups := sablier.ParseGroups(stub.Meta[sablier.LabelGroup])
		for _, g := range groupNames {
			instances = append(instances, sablier.InstanceConfiguration{
				Name:    JobInstanceName(stub.ID, g, single, ParseOptions{Delimiter: p.delimiter}),
				Groups:  groups,
				Enabled: enabled,
			})
		}
	}

	return instances, nil
}

// InstanceGroups maps each Sablier group to the instances that belong to it.
func (p *Provider) InstanceGroups(ctx context.Context) (map[string][]string, error) {
	stubs, err := p.listEnabledJobs(ctx)
	if err != nil {
		return nil, err
	}

	groups := make(map[string][]string)
	for _, stub := range stubs {
		groupNames := groupNamesOf(stub)
		single := len(groupNames) == 1
		instanceGroups := sablier.ParseGroups(stub.Meta[sablier.LabelGroup])
		for _, g := range groupNames {
			name := JobInstanceName(stub.ID, g, single, ParseOptions{Delimiter: p.delimiter})
			for _, group := range instanceGroups {
				groups[group] = append(groups[group], name)
			}
		}
	}

	return groups, nil
}

// listEnabledJobs returns the job stubs (with meta populated) that carry
// sablier.enable=true at the job level.
func (p *Provider) listEnabledJobs(ctx context.Context) ([]*nomadapi.JobListStub, error) {
	stubs, _, err := p.Client.Jobs().ListOptions(
		&nomadapi.JobListOptions{Fields: &nomadapi.JobListFields{Meta: true}},
		p.queryOptions(ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("cannot list nomad jobs: %w", err)
	}

	enabled := make([]*nomadapi.JobListStub, 0, len(stubs))
	for _, stub := range stubs {
		if stub.Meta[sablier.LabelEnable] == "true" {
			enabled = append(enabled, stub)
		}
	}
	return enabled, nil
}

// groupNamesOf returns the task group names of a job stub, sorted for
// deterministic output. They are sourced from the job summary carried by the
// list stub, so no extra per-job call is needed.
func groupNamesOf(stub *nomadapi.JobListStub) []string {
	if stub.JobSummary == nil {
		return nil
	}
	names := make([]string, 0, len(stub.JobSummary.Summary))
	for g := range stub.JobSummary.Summary {
		names = append(names, g)
	}
	slices.Sort(names)
	return names
}
