# Architecture reference

This document describes how Redline is put together: the control plane
and data plane split, the sandbox boundary between them, and the
scheduling model that places work onto GPU hardware. It assumes the
threat model and trust boundaries in [`SECURITY.md`](../SECURITY.md);
this document is about how the system is built, that one is about why.

## Control plane / data plane separation

Redline draws one hard line through the whole system: the **control
plane** decides what should run and where; the **data plane** is where
tenant model code actually executes. Nothing in the data plane is
trusted to make a decision that affects another tenant, and nothing in
the control plane runs tenant-supplied code.

**Control plane** (`internal/api`, `internal/app`, `internal/auth`,
`internal/db`, `internal/scheduler`, `internal/ingest`, `internal/policy`,
Postgres, Redis):

- Terminates every client connection - the OpenAI-compatible
  `/v1/chat/completions` and `/v1/models` surface, and the session-authed
  `/v1/dashboard/*` surface the Next.js frontend under `web/` calls.
- Owns identity and authorization: password login and JWT sessions
  (`internal/auth`, `internal/httpmw`), Argon2id-hashed API keys, and
  scope-gated admin actions (`httpmw.RequireScope`).
- Owns placement decisions: which node and GPU a deployment lands on
  (`internal/scheduler`), never the placement's execution.
- Owns durable state: Postgres for everything that must survive a
  restart (users, models, deployments, telemetry rollups, benchmark
  history), Redis for everything that is fast, ephemeral, or
  time-windowed (rate limits, idle tracking, GPU binding, abuse signal
  counters, durable job queues in `internal/queue`).
- Never executes a byte of a tenant's model weights or inference code.

**Data plane** (Nomad-scheduled allocations, gVisor-sandboxed
containers, the vLLM/SGLang inference engine):

- Runs exactly one thing: one model, one precision, one allocation,
  inside the sandbox boundary described below.
- Speaks back to the control plane only through two narrow channels: an
  OpenAI-compatible HTTP surface the control plane's `internal/inference`
  backends call into, and a Prometheus `/metrics` endpoint
  (`internal/metrics/vllm_scraper.go`) the control plane scrapes.
- Has no credentials for, and no network path to, the control plane's
  own database, Redis, or object store beyond what `internal/netsec`'s
  egress firewall explicitly allows (the artifact cache, for pulling its
  own weights - nothing else).

A request's path through both planes: a client calls
`POST /v1/chat/completions` with an API key → `internal/httpmw.APIKeyAuth`
and `internal/policy.AbuseGuardMiddleware` run entirely in the control
plane → `internal/api.ChatCompletionsHandler` forwards to an
`internal/inference.Backend` implementation, which is the one narrow
bridge into the data plane → the vLLM/SGLang engine inside its sandboxed
allocation generates tokens and streams them back over that same bridge
→ the control plane relays them to the client as SSE, recording TTFT/TPOT
telemetry (`internal/telemetry`) as it goes. At no point does the client,
or the model's own generated output, get a network path into anything
else the control plane owns.

## The sandbox boundary

Every data-plane allocation runs under gVisor (`runsc`) rather than the
host kernel - see `internal/sandbox` and `SECURITY.md`'s threat model for
the full detail. Architecturally, the boundary shows up in three places:

1. **The scheduler never places two tenants' allocations to share a
   GPU.** `internal/scheduler/gpubinding.go`'s Redis-backed atomic claim
   refuses any placement that would time-slice a device, and zeroes VRAM
   between tenants before the device becomes claimable again.
2. **The Nomad job spec is the boundary's concrete expression.**
   `internal/nomadclient/jobspec.go` sets the sandboxed runtime, a
   read-only root filesystem, dropped capabilities, and scoped
   tmpfs/shm mounts on every inference task it builds - there is no
   code path that constructs an inference job without them.
3. **Egress from inside the sandbox is default-deny.**
   `internal/netsec` builds an nftables ruleset that drops everything
   except the artifact cache, explicitly including the
   `169.254.169.254` link-local metadata range - the path a compromised
   container would otherwise use to reach cloud credential-issuing
   endpoints.

The sandbox boundary is why the control plane can safely treat a
worker node as permanently untrusted (see `SECURITY.md`'s zero-trust
posture and node repaving policy below) rather than needing to detect
compromise and remediate in place.

## Scheduling model

Placement is a pure function of declared resource needs against known
node capacity - `internal/scheduler.SelectNode` and
`internal/quant.SelectPlacement` - not a bin-packing heuristic layered on
after the fact:

1. **Ingestion determines the footprint before scheduling ever runs.**
   `internal/ingest.Orchestrator` resolves a model's architecture,
   parameter count, and per-precision VRAM footprint
   (`internal/ingest.ComputeVRAMFootprint`) from its safetensors header
   and config, durably, once - so the scheduler is never guessing at
   how much a model needs.
2. **Precision selection happens before node selection.**
   `internal/quant.SelectPrecision` picks FP16, FP8, or INT4 based on
   whether the node's GPU generation actually supports FP8 compute
   (`internal/quant.DetectFP8Support`) and whether the footprint fits -
   never optimistically scheduling a precision the hardware can't run.
3. **A deployment's Nomad job is dispatched through a degraded-mode
   layer, not directly.** `internal/scheduler.DegradedModeDispatcher`
   submits to Nomad when it's reachable and durably queues the
   deployment for retry when it isn't, rather than failing the request -
   see the Nomad-unreachable section of the runbook.
4. **Once running, an allocation's health is measured, not assumed.**
   `internal/resilience.Watchdog` tracks per-request token cadence
   independently of Nomad's own allocation status, since "the container
   is running" and "the container is making progress" are different
   claims - a stuck generation is recovered (cancelled and requeued)
   without waiting for Nomad to notice anything wrong.
5. **Idle and cost accounting close the loop.**
   `internal/scheduler.IdleTracker` and `internal/api.SessionCostHandler`
   turn wall-clock allocation time into both an automatic-termination
   countdown and an accumulated dollar cost, so a deployment's resource
   claim has a visible, bounded lifetime rather than running until
   someone remembers to stop it.

## Where to look

| Concern | Package |
|---|---|
| HTTP surface, request validation, SSE streaming | `internal/api` |
| Dependency wiring, process lifecycle | `internal/app` |
| Auth: sessions, API keys, password hashing | `internal/auth`, `internal/httpmw` |
| Durable state: users, models, deployments, telemetry | `internal/db` |
| Model ingestion: manifest, header, checksum, orchestration | `internal/ingest` |
| Artifact cache: upload, quarantine, dedup, retry | `internal/cache` |
| GPU binding, placement, idle tracking, retry policy, Nomad degradation | `internal/scheduler` |
| Nomad job construction and API client | `internal/nomadclient` |
| Sandbox boundary and its own test suite | `internal/sandbox` |
| Egress firewall, metadata-endpoint blocking | `internal/netsec` |
| Inference backend abstraction (vLLM/SGLang), engine arg rendering | `internal/inference` |
| Precision and placement decisions | `internal/quant` |
| Live telemetry: metrics, hub, WebSocket push | `internal/metrics`, `internal/telemetry` |
| Stuck-request detection and recovery | `internal/resilience` |
| Terms-of-service, suspension, abuse detection | `internal/policy` |
| Benchmark suite and scoring | `internal/bench` |
| Cost attribution | `internal/billing` |
| Durable job queue (ingestion, retries) | `internal/queue` |
| Frontend: dashboard, chat playground, telemetry charts | `web/` |
