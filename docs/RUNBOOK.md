# Operator runbook

Procedures for running Redline in production: node repaving, and
incident response for the failure modes the system is specifically
designed to survive or contain. Pair this with
[`ARCHITECTURE.md`](ARCHITECTURE.md) for how the pieces referenced below
fit together, and [`SECURITY.md`](../SECURITY.md) for the threat model
behind them.

## Node repaving

Worker nodes are reimaged from a known-good base on a fixed cadence, and
immediately, out of cadence, on any of the triggers in `SECURITY.md`'s
node repaving policy: a suspected sandbox escape, an nvproxy driver
mismatch, or three or more sandbox-boundary allocation failures on one
node within a rolling window.

**Procedure:**

1. Cordon the node in Nomad so no new allocation is placed on it
   (`nomad node drain -enable <node-id>`), which also begins draining
   any live allocation - `DrainPeriod` in `internal/nomadclient/jobspec.go`
   gives in-flight requests time to finish before the kill signal.
2. Confirm the drain completed: `nomad node status <node-id>` shows no
   running allocations.
3. Reimage from the known-good base image. Do not patch the existing
   disk in place - the point of repaving is that nothing from the prior
   state survives, including anything a sandbox escape might have left
   behind outside the sandbox itself.
4. Re-provision (see `deploy/provisioning/`) and re-join the node to the
   Nomad cluster.
5. Verify before uncordoning: `runsc` is the configured Docker runtime,
   the nftables egress ruleset from `internal/netsec` is applied, and
   the node's NVIDIA driver version is one nvproxy has a signature for
   (`runsc nvproxy list-supported-drivers`) - an unsupported-driver
   fallback is itself a repaving trigger, so a freshly repaved node must
   not immediately need repaving again.
6. Uncordon: `nomad node drain -disable <node-id>`.

The control plane never routes traffic to a node based on the node's own
self-reported health; readiness is gated on what each allocation's own
container reports. A repaved node earns traffic the same way any node
does - by passing its allocations' own readiness checks - not by an
operator marking it trusted.

## Incident response

### Suspected sandbox escape

1. Cordon and drain the affected node immediately (see Node repaving,
   step 1) - do not wait for a scheduled maintenance window.
2. Preserve the node's state for investigation before reimaging: a
   repaved node's prior disk state is gone, so if there is any doubt
   about whether this is a real escape versus a false alarm, snapshot
   or otherwise capture the node before step 3 of the repaving procedure.
3. Check whether the same signature (syscall pattern, container image,
   tenant) appears on other nodes; a real sandbox escape technique is
   not usually node-specific.
4. Repave per the procedure above once investigation is done.
5. Treat this as the highest-severity incident class Redline has: the
   entire multi-tenant threat model in `SECURITY.md` rests on the
   sandbox boundary holding. `internal/sandbox/syscall_interception_test.go`
   is the automated check that this boundary still holds on every CI
   run (`sandbox-escape-tests` in `.github/workflows/ci.yml`); if an
   escape is confirmed, that suite's assumptions need re-examination
   before any node is trusted with tenant workloads again.

### Nomad control plane unreachable

Expected, handled behavior, not a page-worthy outage by itself -
`internal/scheduler.DegradedModeDispatcher` durably queues new
deployments for retry rather than failing them, and existing ready
allocations keep serving inference traffic untouched, since the request
path never calls into Nomad at request time.

1. Confirm the degraded state: `GET /v1/dashboard/scheduler/status`
   reports `degraded: true`.
2. Confirm existing traffic is unaffected: chat completions against
   already-running deployments should show no elevated error rate.
3. Investigate Nomad reachability (server health, network partition,
   ACL/token expiry) using Nomad's own tooling - this is an
   infrastructure problem outside Redline's control plane.
4. Once Nomad is reachable again, queued deployments need a drain of
   the `deployment-retry` queue (`internal/queue`) - either the
   dispatcher's own retry path or a manual replay from the queue's
   dead-letter list if attempts were exhausted while Nomad was down.
5. This only becomes page-worthy if the outage persists long enough
   that queued deployments represent a real capacity gap, or if
   `deployment-retry`'s dead-letter list is accumulating.

### Stuck inference allocation

Expected, handled behavior for the common case -
`internal/resilience.Watchdog` and `Reaper` detect a request that has
stopped producing tokens mid-stream and automatically cancel and requeue
it (see `internal/resilience` for the watchdog and reaper recovery mechanisms).

1. This is invisible to an operator in the normal case: the client sees
   its request fail and, if the caller retries, a fresh allocation
   serves it.
2. Escalate only if the same allocation or node produces repeated stuck
   requests - that pattern points at a node-level problem (driver,
   thermal, GPU ECC error) rather than an isolated hung generation, and
   is worth treating as a repaving trigger even though it isn't one of
   the three formal triggers in the policy above.

### Corrupted artifact download

Expected, handled behavior - `internal/ingest.RetryingDownload` retries
a checksum mismatch with backoff, and `internal/cache.Client.Quarantine`
moves the corrupted object aside (rather than silently deleting it) for
inspection before purging it from its normal location.

1. If ingestion for a model repeatedly fails after exhausting retries,
   inspect the quarantined object at `quarantine/<original-key>` in the
   artifact bucket before assuming the upstream source itself is bad -
   a mismatch that reproduces identically across attempts against a
   healthy source usually means the artifact cache's own storage, not
   the network path, is where the corruption is happening.
2. A single transient mismatch that succeeds on retry needs no operator
   action; the quarantined copy from the failed attempt can be deleted
   once the retry succeeds.

### Abuse escalation / account suspension

`internal/policy.AbuseDetector` and `Escalator` handle the common case
automatically: a flagged account has its rate limit cut after a few
violations, and is suspended outright - keys revoked, live deployments
terminated - if the pattern continues.

1. To suspend an account manually (outside the automatic escalation
   path - e.g. a confirmed ToS violation reported through another
   channel), call `POST /v1/admin/users/{id}/suspend` with an API key
   carrying the `admin` scope.
2. Suspension is immediate and does not wait on Nomad: the account is
   marked suspended and its keys revoked first, so authentication stops
   working from the very next request even if the subsequent Nomad
   `StopJob` calls fail. Check the response's `stop_job_failure_count`;
   a nonzero value means some allocations may still be running and need
   manual cleanup once Nomad is reachable.
3. A suspension is not currently reversible through the API - reversing
   one is a direct database operation (clearing `users.suspended_at`)
   until a corresponding unsuspend endpoint exists.

## A note on the egress firewall's design

`internal/netsec`'s default-deny egress policy explicitly blocks the
`169.254.169.254` link-local metadata range from every sandboxed
container - not merely the tenant-facing network, the metadata range
specifically. This followed a wave of publicly disclosed
credential-harvesting incidents in mid-2026 affecting multiple AI
hosting and model-serving platforms, where a compromised or
maliciously-crafted model-loading path was used to reach a host's cloud
metadata endpoint and exfiltrate instance credentials. The lesson that
shaped this design: a firewall `REJECT` rule is not sufficient on its
own, because it still confirms to a probing attacker that something is
listening - `internal/netsec/metadata_unreachable_test.go` verifies the
stronger property that gVisor's own network isolation (`-network=none`)
makes the endpoint categorically unreachable, not merely blocked.
