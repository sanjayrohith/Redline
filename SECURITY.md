# Security model

This document describes the threat model Redline's inference platform is
designed against, the trust boundaries that follow from it, and the
operational policy that keeps those boundaries meaningful over time.

## Threat model

Every model container is assumed hostile. Redline runs arbitrary,
often third-party, model weights and inference code supplied by tenants.
A model container is treated the same way a platform would treat an
attacker's own code running with the tenant's credentials: it may attempt
to escape its sandbox, scan or reach internal services, exfiltrate host
credentials, exhaust shared resources, or read another tenant's data left
behind in GPU memory. None of these are edge cases carved out for
"untrusted" workloads specifically - there is no trusted tier of model
container.

The control plane (the gateway, the scheduler, the database, the
artifact cache) is the trust boundary's inside. Everything scheduled
onto a worker node to run a tenant's model is outside it.

## Trust boundaries

**The sandbox boundary (gVisor / runsc).** Every inference container runs
under gVisor's application kernel rather than the host kernel
(`internal/nomadclient.DefaultRuntime`, `deploy/provisioning/docker-daemon.json`).
The container's read-only root filesystem, dropped capabilities, and
scoped tmpfs/shm mounts (`internal/nomadclient/jobspec.go`) mean a
compromised process inside the sandbox has no path to host mount
operations, raw sockets, or kernel module loading regardless of what it
attempts - those syscalls are not merely permission-denied, they are
unimplemented in gVisor's sentry (verified directly in
`internal/sandbox/syscall_interception_test.go`).

**The nvproxy boundary.** GPU access is the one deliberate, narrow hole
punched through the sandbox boundary: gVisor's nvproxy intercepts and
validates NVIDIA driver ioctls rather than passing them through
unchecked, so a container gets compute access without gaining an
unmediated path to the host's GPU driver. This is treated as its own,
narrower trust boundary than the rest of the sandbox - nvproxy's
supported-driver list is deliberately explicit
(`runsc nvproxy list-supported-drivers`) rather than best-effort, and a
driver version outside it should fail closed, not silently fall back to
an unvalidated passthrough.

**The network boundary.** Each allocation gets its own network namespace
(`internal/netsec.AllocationNetwork`, `deploy/cni/redline-alloc.conflist`)
with a default-drop egress ruleset (`internal/netsec.BuildEgressRuleset`)
permitting exactly one destination: the internal artifact cache a
container needs to pull model weights from. Every other destination -
the rest of the private address space, the link-local range cloud
metadata services are served from, and the public internet - is denied
by default rather than enumerated as an exception. Verified directly
against a live sandbox in `internal/netsec/metadata_unreachable_test.go`
and `internal/netsec/lateral_movement_test.go`.

**The GPU boundary.** A physical GPU is bound exclusively to one tenant
for an allocation's lifetime (`internal/scheduler.GPUBinding`); Redline
refuses any placement that would time-slice a device across tenants,
because sharing compute undermines every other boundary in this
document even when memory-region isolation would technically hold. When
a device changes tenants, its VRAM is zeroed and the device stays
unclaimable by anyone - not even its previous tenant - until that pass
completes (`GPUBinding.Release`, `GPUBinding.ZeroDevice`), closing the
cross-tenant memory leakage class where a new tenant's process could
otherwise read whatever the previous tenant left resident in device
memory.

## Zero-trust posture of worker nodes

A worker node is not a long-lived trusted asset. It runs hostile code by
design, on every allocation, indefinitely. Redline does not attempt to
detect compromise and remediate in place: a worker node that has run
tenant code is assumed to be in an unknown state the moment that code
exits, and no state on it - filesystem contents, driver state, leftover
processes - is trusted for the next allocation without going through the
isolation boundaries above again from scratch.

The control plane never trusts a worker node's self-reported health as
sufficient to route traffic to it; readiness is gated on what the
allocation's own container reports (see `internal/inference/vllm_readiness.go`),
not on the node believing itself healthy.

## Node repaving policy

Worker nodes are repaved - reimaged from a known-good base, not patched
in place - on a fixed cadence, and immediately, out of cadence, on any of:

- a sandbox escape or suspected escape on that node,
- an nvproxy driver mismatch or unsupported-driver fallback being
  observed on that node,
- three or more allocation crashes classified as a placement or
  sandbox-boundary failure (as opposed to an ordinary application crash)
  on that node within a rolling window.

Repaving is the primary defense against slow state drift and against any
single compromise persisting past the allocation that caused it. A node
is never trusted to "clean itself up"; it is trusted only to be replaced.
