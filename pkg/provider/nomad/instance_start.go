package nomad

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/sablierapp/sablier/pkg/sablier"
)

// InstanceDependencies reports the direct dependencies of an instance. Nomad
// expresses ordering within a job (task lifecycle, group ordering) rather than
// as cross-instance Sablier dependencies, so the provider reports none.
func (p *Provider) InstanceDependencies(_ context.Context, _ string) ([]sablier.InstanceDependency, error) {
	return nil, nil
}

// activeReplicas resolves the replica count used when starting an instance.
// The sablier.active.replicas label stays authoritative when present. Without
// it, an explicit count from the name (jobID@group@replicas) is honored, so
// "whoami@web@2" scales to 2 as documented instead of falling back to 1.
func activeReplicas(sc sablier.ScaleConfig, meta map[string]string, parsed ParsedName) int {
	if _, hasLabel := meta[sablier.LabelActiveReplicas]; hasLabel {
		return int(sc.Active.Replicas)
	}
	if parsed.Replicas > 1 {
		return parsed.Replicas
	}
	return int(sc.Active.Replicas)
}

// InstanceStart scales the targeted task group up to its active replica count.
func (p *Provider) InstanceStart(ctx context.Context, name string) (err error) {
	ctx, span := p.tracer.Start(ctx, "nomad.instance.start",
		trace.WithAttributes(attribute.String("instance", name)))
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	parsed, err := ParseName(name, ParseOptions{Delimiter: p.delimiter})
	if err != nil {
		return err
	}

	job, tg, meta, err := p.workload(ctx, parsed)
	if err != nil {
		return err
	}

	sc := sablier.ScaleConfigFromLabels(meta)
	replicas := activeReplicas(sc, meta, parsed)

	span.SetAttributes(
		attribute.String("job", jobIDOf(job)),
		attribute.String("group", groupNameOf(tg)),
		attribute.Int("replicas", replicas),
	)
	p.l.DebugContext(ctx, "scaling nomad group up",
		slog.String("job", jobIDOf(job)),
		slog.String("group", groupNameOf(tg)),
		slog.Int("replicas", replicas),
	)

	count := replicas
	_, _, err = p.Client.Jobs().Scale(parsed.JobID, groupNameOf(tg), &count, "sablier: scale up on demand", false, nil, p.writeOptions(ctx))
	return err
}
