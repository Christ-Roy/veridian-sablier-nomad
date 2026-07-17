package nomad

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/sablierapp/sablier/pkg/sablier"
)

// InstanceStop scales the targeted task group down to its idle replica count
// (zero by default). Resource-throttling scale mode (idle CPU/memory) is not
// supported on Nomad — only the replica count is adjusted — so a non-zero
// sablier.idle.replicas keeps that many allocations running while any
// sablier.idle.cpu/memory labels are ignored.
func (p *Provider) InstanceStop(ctx context.Context, name string) (err error) {
	ctx, span := p.tracer.Start(ctx, "nomad.instance.stop",
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
	count := 0
	if sc.Idle.Replicas >= 1 {
		count = int(sc.Idle.Replicas)
	}

	span.SetAttributes(
		attribute.String("job", jobIDOf(job)),
		attribute.String("group", groupNameOf(tg)),
		attribute.Int("replicas", count),
	)
	p.l.DebugContext(ctx, "scaling nomad group down",
		slog.String("job", jobIDOf(job)),
		slog.String("group", groupNameOf(tg)),
		slog.Int("replicas", count),
	)

	target := count
	_, _, err = p.Client.Jobs().Scale(parsed.JobID, groupNameOf(tg), &target, "sablier: scale to idle", false, nil, p.writeOptions(ctx))
	return err
}
