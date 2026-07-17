// Package nomad implements the Sablier provider for HashiCorp Nomad. It scales
// a job's task group between zero and a target replica count on demand, the
// same way the Kubernetes provider scales a Deployment/StatefulSet 0↔1.
//
// A Sablier instance maps to a single task group of a Nomad job. Instances are
// named "<jobID>", "<jobID><delim><group>", or "<jobID><delim><group><delim><replicas>"
// (see ParseName); the default delimiter is "@", which is invalid in Nomad job
// and group identifiers and therefore never collides with a bare job ID.
//
// Connection settings (address, ACL token, region, TLS, namespace) are read
// from the standard Nomad environment variables through the Nomad API's
// DefaultConfig, so NOMAD_ADDR and NOMAD_TOKEN behave exactly as they do for
// the nomad CLI.
package nomad

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

	nomadapi "github.com/hashicorp/nomad/api"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	providerConfig "github.com/sablierapp/sablier/pkg/config"
	"github.com/sablierapp/sablier/pkg/sablier"
)

// Interface guard
var _ sablier.Provider = (*Provider)(nil)

// Provider manages Nomad workloads on behalf of Sablier.
type Provider struct {
	Client    *nomadapi.Client
	namespace string
	delimiter string
	l         *slog.Logger
	tracer    trace.Tracer
}

// New builds a Nomad provider from an already-configured Nomad API client. The
// caller is expected to construct the client from nomadapi.DefaultConfig() so
// that the standard NOMAD_* environment variables are honored. New verifies
// connectivity by querying the cluster leader before returning.
func New(ctx context.Context, client *nomadapi.Client, logger *slog.Logger, config providerConfig.Nomad) (*Provider, error) {
	logger = logger.With(slog.String("provider", "nomad"))

	delimiter := config.Delimiter
	if delimiter == "" {
		delimiter = "@"
	}

	leader, err := client.Status().Leader()
	if err != nil {
		return nil, fmt.Errorf("cannot connect to nomad: %w", err)
	}

	logger.InfoContext(ctx, "connection established with nomad",
		slog.String("leader", leader),
		slog.String("namespace", config.Namespace),
		slog.String("delimiter", delimiter),
	)

	return &Provider{
		Client:    client,
		namespace: config.Namespace,
		delimiter: delimiter,
		l:         logger,
		tracer:    otel.Tracer("github.com/sablierapp/sablier/pkg/provider/nomad"),
	}, nil
}

// queryOptions returns read options scoped to the provider namespace and the
// request context.
func (p *Provider) queryOptions(ctx context.Context) *nomadapi.QueryOptions {
	q := &nomadapi.QueryOptions{}
	if p.namespace != "" {
		q.Namespace = p.namespace
	}
	return q.WithContext(ctx)
}

// writeOptions returns write options scoped to the provider namespace and the
// request context.
func (p *Provider) writeOptions(ctx context.Context) *nomadapi.WriteOptions {
	w := &nomadapi.WriteOptions{}
	if p.namespace != "" {
		w.Namespace = p.namespace
	}
	return w.WithContext(ctx)
}

// workload resolves the task group targeted by a parsed instance name. When the
// name omits the group, the job must have exactly one task group; otherwise the
// caller must disambiguate. It returns the job, the resolved task group, and the
// merged sablier.* configuration (job meta overlaid by group meta).
func (p *Provider) workload(ctx context.Context, parsed ParsedName) (*nomadapi.Job, *nomadapi.TaskGroup, map[string]string, error) {
	job, _, err := p.Client.Jobs().Info(parsed.JobID, p.queryOptions(ctx))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("cannot inspect nomad job %q: %w", parsed.JobID, err)
	}

	tg, err := selectGroup(job, parsed.Group)
	if err != nil {
		return nil, nil, nil, err
	}

	return job, tg, mergeMeta(job, tg), nil
}

// selectGroup returns the requested task group of job. An empty group name is
// only valid for single-group jobs.
func selectGroup(job *nomadapi.Job, group string) (*nomadapi.TaskGroup, error) {
	if len(job.TaskGroups) == 0 {
		return nil, fmt.Errorf("nomad job %q has no task groups", jobIDOf(job))
	}

	if group == "" {
		if len(job.TaskGroups) != 1 {
			names := make([]string, 0, len(job.TaskGroups))
			for _, tg := range job.TaskGroups {
				names = append(names, groupNameOf(tg))
			}
			return nil, fmt.Errorf("nomad job %q has %d task groups %v; specify one in the instance name", jobIDOf(job), len(job.TaskGroups), names)
		}
		return job.TaskGroups[0], nil
	}

	for _, tg := range job.TaskGroups {
		if groupNameOf(tg) == group {
			return tg, nil
		}
	}
	return nil, fmt.Errorf("nomad job %q has no task group %q", jobIDOf(job), group)
}

// mergeMeta merges a job's meta with the target task group's meta, with the
// group taking precedence. This is where the sablier.* configuration is read
// from, mirroring how the Kubernetes provider merges labels and annotations.
func mergeMeta(job *nomadapi.Job, tg *nomadapi.TaskGroup) map[string]string {
	merged := make(map[string]string, len(job.Meta)+len(tg.Meta))
	maps.Copy(merged, job.Meta)
	maps.Copy(merged, tg.Meta)
	return merged
}

// groupImage returns the image of the first task in the group that declares one
// in its driver config, or "" when none does. Only string image values are
// reported (Docker/Podman drivers); other drivers are ignored.
func groupImage(tg *nomadapi.TaskGroup) string {
	for _, task := range tg.Tasks {
		if img, ok := task.Config["image"].(string); ok && img != "" {
			return img
		}
	}
	return ""
}

func jobIDOf(job *nomadapi.Job) string {
	if job.ID != nil {
		return *job.ID
	}
	if job.Name != nil {
		return *job.Name
	}
	return ""
}

func groupNameOf(tg *nomadapi.TaskGroup) string {
	if tg.Name != nil {
		return *tg.Name
	}
	return ""
}

// groupCount returns the desired replica count of a task group, defaulting to 1
// when the count is unset (Nomad's own default).
func groupCount(tg *nomadapi.TaskGroup) int {
	if tg.Count != nil {
		return *tg.Count
	}
	return 1
}
