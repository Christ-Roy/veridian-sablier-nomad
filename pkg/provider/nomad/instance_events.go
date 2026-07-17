package nomad

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"time"

	nomadapi "github.com/hashicorp/nomad/api"

	"github.com/sablierapp/sablier/pkg/provider"
	"github.com/sablierapp/sablier/pkg/sablier"
)

// eventJobDeregistered is the Nomad event type emitted on the Job topic when a
// job is stopped or purged.
const eventJobDeregistered = "JobDeregistered"

// InstanceEvents streams task-group lifecycle changes derived from the Nomad
// Job topic. Nomad does not emit a dedicated "scaled to zero" event, so the
// stream tracks each task group's desired replica count and reports a
// transition whenever it crosses zero (started on 0→N, stopped on N→0). A job
// deregistration reports the group as removed (and stopped).
//
// The stream reconnects indefinitely with linear backoff; the tracked state is
// preserved across reconnects so a redial does not replay spurious "created"
// events. As with the other providers, consumers pair it with periodic
// reconciliation for anything missed while disconnected. Err is part of the
// contract but is never sent: the stream recovers from every transient failure.
func (p *Provider) InstanceEvents(ctx context.Context, opts provider.InstanceEventsOptions) sablier.InstanceEventStream {
	return p.instanceEvents(ctx, opts, linearBackoff)
}

// instanceEvents is InstanceEvents with an injectable backoff so tests can run
// without waiting.
func (p *Provider) instanceEvents(ctx context.Context, opts provider.InstanceEventsOptions, backoff func(attempt int) time.Duration) sablier.InstanceEventStream {
	wantStopped := len(opts.Types) == 0 || slices.Contains(opts.Types, provider.InstanceEventStopped)
	wantStarted := len(opts.Types) == 0 || slices.Contains(opts.Types, provider.InstanceEventStarted)
	wantCreated := len(opts.Types) == 0 || slices.Contains(opts.Types, provider.InstanceEventCreated)
	wantRemoved := len(opts.Types) == 0 || slices.Contains(opts.Types, provider.InstanceEventRemoved)

	eventsC := make(chan sablier.InstanceEvent)
	errC := make(chan error, 1)

	// counts tracks the last-known desired replica count per instance name.
	// Presence in the map means the instance has been seen at least once.
	counts := make(map[string]int)

	go func() {
		defer close(eventsC)
		defer close(errC)

		for attempt := 0; ; attempt++ {
			if attempt > 0 {
				d := backoff(attempt)
				p.l.WarnContext(ctx, "reconnecting nomad event stream", "attempt", attempt, "backoff", d)
				select {
				case <-time.After(d):
				case <-ctx.Done():
					return
				}
			}

			ch, err := p.Client.EventStream().Stream(ctx, map[nomadapi.Topic][]string{nomadapi.TopicJob: {"*"}}, 0, p.queryOptions(ctx))
			if err != nil {
				p.l.ErrorContext(ctx, "cannot open nomad event stream", slog.Any("error", err))
				continue
			}

			if reconnect := p.consumeEvents(ctx, ch, eventsC, counts, wantStopped, wantStarted, wantCreated, wantRemoved); !reconnect {
				return
			}
		}
	}()

	return sablier.InstanceEventStream{Events: eventsC, Err: errC}
}

// consumeEvents drains a single event channel. It returns true when the channel
// ended and the caller should redial, or false when ctx was cancelled.
func (p *Provider) consumeEvents(
	ctx context.Context,
	ch <-chan *nomadapi.Events,
	out chan<- sablier.InstanceEvent,
	counts map[string]int,
	wantStopped, wantStarted, wantCreated, wantRemoved bool,
) (reconnect bool) {
	for {
		select {
		case <-ctx.Done():
			return false
		case events, ok := <-ch:
			if !ok {
				p.l.WarnContext(ctx, "nomad event stream closed")
				return true
			}
			if events.Err != nil {
				if errors.Is(events.Err, io.EOF) || errors.Is(events.Err, context.Canceled) {
					return true
				}
				p.l.ErrorContext(ctx, "nomad event stream error", slog.Any("error", events.Err))
				return true
			}
			for i := range events.Events {
				for _, ev := range p.eventsFor(ctx, &events.Events[i], counts, wantStopped, wantStarted, wantCreated, wantRemoved) {
					select {
					case out <- ev:
					case <-ctx.Done():
						return false
					}
				}
			}
		}
	}
}

// eventsFor turns a single Nomad Job-topic event into the Sablier instance
// events it implies, updating the tracked counts as a side effect.
func (p *Provider) eventsFor(
	ctx context.Context,
	ev *nomadapi.Event,
	counts map[string]int,
	wantStopped, wantStarted, wantCreated, wantRemoved bool,
) []sablier.InstanceEvent {
	if ev.Topic != nomadapi.TopicJob {
		return nil
	}

	if ev.Type == eventJobDeregistered {
		job, _, err := ev.DeregisteredJob()
		if err != nil || job == nil {
			return nil
		}
		return p.removalEvents(job, counts, wantStopped, wantRemoved)
	}

	job, err := ev.Job()
	if err != nil || job == nil {
		p.l.WarnContext(ctx, "cannot decode nomad job event", slog.Any("error", err))
		return nil
	}
	return p.transitionEvents(job, counts, wantStopped, wantStarted, wantCreated)
}

// transitionEvents computes the created/started/stopped events implied by the
// current desired counts of a job's task groups.
func (p *Provider) transitionEvents(job *nomadapi.Job, counts map[string]int, wantStopped, wantStarted, wantCreated bool) []sablier.InstanceEvent {
	single := len(job.TaskGroups) == 1
	var out []sablier.InstanceEvent
	for _, tg := range job.TaskGroups {
		name := JobInstanceName(jobIDOf(job), groupNameOf(tg), single, ParseOptions{Delimiter: p.delimiter})
		cur := groupCount(tg)
		if job.Stop != nil && *job.Stop {
			cur = 0
		}
		prev, seen := counts[name]
		counts[name] = cur

		for _, t := range classifyTransition(prev, seen, cur) {
			switch t {
			case provider.InstanceEventCreated:
				if wantCreated {
					out = append(out, sablier.InstanceEvent{Type: t, Info: p.eventInfo(job, tg, name, statusForCount(cur))})
				}
			case provider.InstanceEventStarted:
				if wantStarted {
					out = append(out, sablier.InstanceEvent{Type: t, Info: p.eventInfo(job, tg, name, sablier.InstanceStatusStarting)})
				}
			case provider.InstanceEventStopped:
				if wantStopped {
					out = append(out, sablier.InstanceEvent{Type: t, Info: p.eventInfo(job, tg, name, sablier.InstanceStatusStopped)})
				}
			}
		}
	}
	return out
}

// removalEvents reports a deregistered job's task groups as removed (and, when
// requested, stopped), and forgets their tracked state.
func (p *Provider) removalEvents(job *nomadapi.Job, counts map[string]int, wantStopped, wantRemoved bool) []sablier.InstanceEvent {
	single := len(job.TaskGroups) == 1
	var out []sablier.InstanceEvent
	for _, tg := range job.TaskGroups {
		name := JobInstanceName(jobIDOf(job), groupNameOf(tg), single, ParseOptions{Delimiter: p.delimiter})
		delete(counts, name)
		if wantRemoved {
			out = append(out, sablier.InstanceEvent{Type: provider.InstanceEventRemoved, Info: p.eventInfo(job, tg, name, sablier.InstanceStatusStopped)})
		}
		if wantStopped {
			out = append(out, sablier.InstanceEvent{Type: provider.InstanceEventStopped, Info: p.eventInfo(job, tg, name, sablier.InstanceStatusStopped)})
		}
	}
	return out
}

// classifyTransition reports the lifecycle transitions implied by a change in a
// task group's desired replica count. It is pure so the zero-crossing rule can
// be unit-tested without a live cluster.
func classifyTransition(prev int, seen bool, cur int) []provider.InstanceEventType {
	if !seen {
		return []provider.InstanceEventType{provider.InstanceEventCreated}
	}
	switch {
	case prev == 0 && cur > 0:
		return []provider.InstanceEventType{provider.InstanceEventStarted}
	case prev > 0 && cur == 0:
		return []provider.InstanceEventType{provider.InstanceEventStopped}
	default:
		return nil
	}
}

// statusForCount reports the coarse status implied by a desired count alone
// (the running count is not carried by job events).
func statusForCount(desired int) sablier.InstanceStatus {
	if desired <= 0 {
		return sablier.InstanceStatusStopped
	}
	return sablier.InstanceStatusStarting
}

// eventInfo builds the InstanceInfo carried by an event from the job payload,
// without an extra inspect round-trip.
func (p *Provider) eventInfo(job *nomadapi.Job, tg *nomadapi.TaskGroup, name string, status sablier.InstanceStatus) sablier.InstanceInfo {
	meta := mergeMeta(job, tg)
	info := sablier.InstanceInfo{
		Name:   name,
		Status: status,
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
	return info
}

// linearBackoff waits one more second per attempt, capped at 30 seconds,
// matching the Docker-family providers' reconnect policy.
func linearBackoff(attempt int) time.Duration {
	return min(time.Duration(attempt)*time.Second, 30*time.Second)
}
