# Veridian fork of Sablier — HashiCorp Nomad provider

This is Veridian's fork of [sablierapp/sablier](https://github.com/sablierapp/sablier)
with an **added HashiCorp Nomad provider** (`pkg/provider/nomad/`). It lets us do
**scale-to-zero on-demand** on our Nomad cluster: a job's task group sits at
`count = 0` until traffic arrives, Sablier scales it up, serves a loading page
while it boots, then scales it back down after an inactivity window — exactly the
way the upstream Kubernetes provider scales a Deployment 0↔1.

Everything else (API, reverse-proxy integrations, loading strategies, themes) is
unchanged from upstream. See `README.md` for the upstream documentation; this
file only covers what is Veridian-specific.

> Do **not** edit `README.md` (upstream). Veridian notes live here.

---

## Status

- Nomad provider **written** (`pkg/provider/nomad/`), unit-tested, `go vet`-clean.
- Spike **validated** against the cluster (integration test gated behind
  `SABLIER_NOMAD_INTEGRATION=1`).
- **Image published** to `ghcr.io/christ-roy/veridian-sablier` (tags `nomad` and
  the short commit SHA).
- **Upstream PR still to do** — this provider answers the long-standing requests
  in [sablierapp/sablier#217](https://github.com/sablierapp/sablier/issues/217)
  and [#266](https://github.com/sablierapp/sablier/issues/266). The fork is kept
  clean (full git history, upstream `README.md` untouched) so the branch
  `feat/nomad-provider` can be turned into that PR later.

---

## Build

### Binary

```sh
export PATH="$HOME/.local/go/bin:/usr/local/go/bin:$PATH"   # Go 1.26+
CGO_ENABLED=0 go build -o sablier ./cmd/sablier
./sablier version
```

The binary is fully static (`CGO_ENABLED=0`). The upstream loading themes
(`pkg/theme/embedded/*.html`) are compiled in via `go:embed`, so the binary is
self-contained.

### Docker image

Multi-stage `Dockerfile` at the repo root (build on `golang:1.26-alpine`,
ship on `gcr.io/distroless/static`):

```sh
docker build -t ghcr.io/christ-roy/veridian-sablier:nomad .
docker run --rm ghcr.io/christ-roy/veridian-sablier:nomad version
```

`ENTRYPOINT` is `/sablier`, default `CMD` is `["start"]`, port **10000** is
exposed.

> The **Veridian custom loading theme is mounted at RUNTIME** by the Nomad job
> (e.g. a `template`/host mount into `/etc/sablier/themes`), **not** baked into
> the image. Do not `COPY` it in the Dockerfile.

Publish (CI or manual):

```sh
echo "$GHCR_PAT" | docker login ghcr.io -u "$GHCR_USER" --password-stdin
docker tag ghcr.io/christ-roy/veridian-sablier:nomad ghcr.io/christ-roy/veridian-sablier:$(git rev-parse --short HEAD)
docker push ghcr.io/christ-roy/veridian-sablier:nomad
docker push ghcr.io/christ-roy/veridian-sablier:$(git rev-parse --short HEAD)
```

---

## Tests

```sh
export PATH="$HOME/.local/go/bin:/usr/local/go/bin:$PATH"

go build ./...                        # compiles everything
go test ./pkg/provider/nomad/...      # the Nomad provider unit tests
go vet  ./pkg/provider/nomad/...
go test -short ./...                  # full short battery
```

The Nomad provider has one **integration test** that talks to a real cluster.
It is skipped unless you opt in:

```sh
export SABLIER_NOMAD_INTEGRATION=1
export NOMAD_ADDR=...        # e.g. http://100.108.136.89:4646
export NOMAD_TOKEN=...
go test -run TestIntegration ./pkg/provider/nomad/...
```

---

## Using the Nomad provider

### Flags

| Flag | Env / default | Meaning |
|------|---------------|---------|
| `--provider.name=nomad` | default `docker` | Select the Nomad provider. |
| `--provider.nomad.namespace` | `NOMAD_NAMESPACE`, then Nomad's `default` | Namespace the watched jobs live in. |
| `--provider.nomad.delimiter` | `@` | Separator between job ID, task group, and replica count in an instance name. `@` is invalid in Nomad job/group IDs, so it never collides. |

Connection settings (address, ACL token, region, TLS) are **not** Sablier flags —
they are read from the standard Nomad environment through the Nomad API's
`DefaultConfig`, exactly like the `nomad` CLI:

```sh
export NOMAD_ADDR=http://100.108.136.89:4646
export NOMAD_TOKEN=<acl-token>
# optional: NOMAD_NAMESPACE, NOMAD_REGION, NOMAD_CACERT, NOMAD_CLIENT_CERT, ...

sablier start \
  --provider.name=nomad \
  --provider.nomad.namespace=default
```

### Instance naming convention

A Sablier *instance* maps to **one task group** of a Nomad job. Instance names
(used by the reverse-proxy middleware / API) are:

```
jobID                     # single-group job — bare job ID
jobID@group               # multi-group job — group must be named
jobID@group@replicas      # start at an explicit replica count
```

- `@` is the default delimiter (override with `--provider.nomad.delimiter`).
- `replicas` is optional; when omitted the `sablier.active.replicas` meta
  (default `1`) is used.
- Examples: `whoami`, `whoami@web`, `whoami@web@2`.

### Opt-in from the Nomad job

A job opts into Sablier by setting **`sablier.enable = "true"` in its job meta**.
Discovery (`InstanceList`) only lists jobs carrying this flag; each task group of
an enabled job becomes an instance. Group-level meta overrides job-level meta.

```hcl
job "whoami" {
  meta {
    "sablier.enable" = "true"
    # optional:
    # "sablier.group"           = "default"
    # "sablier.active.replicas" = "1"
  }

  group "web" {
    count = 0            # scale-to-zero: Sablier scales this up on demand
    # ...
  }
}
```

Relevant meta keys (shared with the other providers, read from job meta merged
with group meta):

- `sablier.enable` — `"true"` to make the job discoverable.
- `sablier.group` — logical Sablier group(s) the instance belongs to.
- `sablier.active.replicas` — replica count to scale up to (default `1`).

---

## Layout of the Nomad provider

`pkg/provider/nomad/`:

- `nomad.go` — provider construction, namespace/delimiter, meta merging, group resolution.
- `parse_name.go` — `jobID@group@replicas` parsing and canonical instance naming.
- `instance_list.go` — discovery of `sablier.enable=true` jobs and their groups.
- `instance_start.go` / `instance_stop.go` — scale a task group up / to zero.
- `instance_inspect.go` — report an instance's ready/not-ready status.
- `instance_events.go` — transition events (created/started/stopped/removed).
- `*_test.go` — unit tests; `nomad_integration_test.go` — opt-in live test.
