# Redline

A secure, multi-tenant GPU inference platform for running arbitrary AI models with OpenAI-compatible APIs.

## Overview

Redline is a production-grade inference platform that enables hosting and serving machine learning models at scale while maintaining strict security boundaries between tenants. It provides an OpenAI-compatible API surface for chat completions and model management, backed by a hardened architecture that treats every model container as potentially hostile.

## Motivation

Modern AI platforms face a fundamental security challenge: running arbitrary, often third-party model code on shared GPU infrastructure while preventing cross-tenant data leakage, resource exhaustion, and infrastructure compromise. Existing solutions often rely on trust assumptions about model code or implement security as an afterthought.

Redline was built from the ground up with a zero-trust architecture where:
- **Every model container is treated as hostile by design** - there is no "trusted tier" of workloads
- **Security boundaries are enforced at multiple layers** - from sandboxing to GPU memory isolation
- **The control plane never executes tenant code** - maintaining strict separation between orchestration and execution

## Key Features

### Security-First Architecture
- **gVisor sandboxing** - Every inference container runs under gVisor's application kernel, not the host kernel
- **GPU memory isolation** - Exclusive GPU binding per tenant with VRAM zeroing between allocations
- **Default-deny networking** - Containers can only reach the artifact cache, with explicit blocking of cloud metadata endpoints
- **nvproxy validation** - GPU driver access is mediated through validated ioctls rather than raw passthrough
- **Node repaving policy** - Worker nodes are regularly reimaged and treated as ephemeral, untrusted infrastructure

### Production-Ready Operations
- **OpenAI-compatible API** - Drop-in replacement for OpenAI's chat completions and models endpoints
- **Intelligent scheduling** - Precision selection (FP16/FP8/INT4) and placement based on GPU capabilities and VRAM requirements
- **Degraded-mode operation** - Continues serving existing workloads even when the scheduler is unreachable
- **Automatic recovery** - Detects and recovers from stuck inference requests without operator intervention
- **Cost tracking** - Real-time cost attribution and idle timeout management

### Developer Experience
- **Dashboard UI** - React-based frontend for model management, deployment, and telemetry
- **Streaming responses** - Server-sent events (SSE) for token-by-token generation
- **Benchmark suite** - Automated performance testing and scoring
- **Comprehensive telemetry** - Time-to-first-token (TTFT), tokens-per-output-token (TPOT), and GPU utilization metrics

## Architecture

### Control Plane / Data Plane Separation

Redline draws a hard line through the entire system:

**Control Plane** (trusted boundary):
- API gateway handling all client connections
- Authentication and authorization (JWT sessions, API keys, scoped permissions)
- Scheduling decisions and placement logic
- Durable state in PostgreSQL and ephemeral state in Redis
- **Never executes tenant model code**

**Data Plane** (untrusted, sandboxed):
- vLLM/SGLang inference engines running in gVisor containers
- Scheduled via HashiCorp Nomad
- Speaks back to control plane only through HTTP inference API and Prometheus metrics
- **No credentials or network path to control plane infrastructure**

### Request Flow

```
Client Request
    ↓
API Gateway (auth, abuse detection, rate limiting)
    ↓
Control Plane (routing, telemetry)
    ↓
─────────────── Sandbox Boundary ───────────────
    ↓
Inference Container (gVisor-sandboxed vLLM/SGLang)
    ↓
GPU (exclusive per-tenant binding, VRAM zeroed between tenants)
```

## Technology Stack

- **Language**: Go 1.27
- **Container Runtime**: gVisor (runsc) for sandboxing
- **Orchestration**: HashiCorp Nomad
- **Databases**: PostgreSQL (durable state), Redis (ephemeral state, queues)
- **Inference Engines**: vLLM, SGLang
- **Frontend**: Next.js (React)
- **Metrics**: Prometheus
- **Object Storage**: MinIO (artifact cache)

## Quick Start

### Prerequisites

- Go 1.27+
- PostgreSQL
- Redis
- HashiCorp Nomad cluster
- GPU nodes with NVIDIA drivers and gVisor configured
- MinIO or S3-compatible object storage

### Build

```bash
make build
```

### Run Tests

```bash
make test
```

### Configuration

Create a `config.yaml` or set `REDLINE_CONFIG_FILE`:

```yaml
# See internal/config for full configuration schema
database:
  host: localhost
  port: 5432
  database: redline
  
redis:
  addr: localhost:6379
  
nomad:
  address: http://localhost:4646
  
# Additional configuration for auth, storage, etc.
```

### Run

```bash
make run
```

The gateway will start on the configured port and expose:
- `POST /v1/chat/completions` - OpenAI-compatible chat API
- `GET /v1/models` - List available models
- `POST /v1/dashboard/*` - Admin and dashboard endpoints

## Project Structure

```
├── cmd/gateway/           # Main application entry point
├── internal/
│   ├── api/              # HTTP handlers and request validation
│   ├── auth/             # Authentication (sessions, API keys, password hashing)
│   ├── db/               # Database models and queries
│   ├── scheduler/        # GPU binding, placement, idle tracking
│   ├── inference/        # vLLM/SGLang backend abstraction
│   ├── ingest/           # Model ingestion and validation
│   ├── sandbox/          # gVisor boundary and test suite
│   ├── netsec/           # Egress firewall and network isolation
│   ├── nomadclient/      # Nomad job construction and API
│   ├── resilience/       # Stuck request detection and recovery
│   ├── policy/           # Abuse detection and account suspension
│   ├── telemetry/        # Metrics collection and WebSocket push
│   └── ...               # Additional internal packages
├── web/                  # Next.js frontend dashboard
├── deploy/               # Deployment configurations
│   ├── cni/             # Container network interface configs
│   ├── containerd/      # Container runtime configs
│   └── provisioning/    # Node provisioning scripts
└── docs/
    ├── ARCHITECTURE.md  # Detailed architecture reference
    └── RUNBOOK.md       # Operations and incident response
```

## Security

Redline's security model is built on **defense in depth** with multiple layers:

1. **Sandbox boundary** - gVisor application kernel isolates containers from the host
2. **Network isolation** - Default-deny egress with explicit allowlist
3. **GPU isolation** - Exclusive per-tenant binding with VRAM zeroing
4. **Node repaving** - Regular reimaging of worker nodes as untrusted infrastructure
5. **Control plane separation** - Tenant code never executes in the control plane

See [SECURITY.md](SECURITY.md) for the complete threat model and trust boundaries.

## Operations

### Node Repaving

Worker nodes are treated as ephemeral and are regularly reimaged. They are immediately repaved on:
- Suspected sandbox escape
- nvproxy driver mismatch
- Repeated allocation failures

### Incident Response

The platform is designed to handle common failure modes gracefully:
- **Nomad unreachable** - Queues new deployments, continues serving existing workloads
- **Stuck inference** - Automatic detection and recovery via watchdog
- **Corrupted downloads** - Retry with backoff and quarantine
- **Abuse escalation** - Automatic rate limiting and suspension

See [docs/RUNBOOK.md](docs/RUNBOOK.md) for detailed operational procedures.

## Documentation

- [ARCHITECTURE.md](docs/ARCHITECTURE.md) - Comprehensive architecture and design decisions
- [RUNBOOK.md](docs/RUNBOOK.md) - Operations, incident response, and troubleshooting
- [SECURITY.md](SECURITY.md) - Threat model, trust boundaries, and security policies

## Implementation Highlights

### How We Achieved Security

- **Syscall interception verification** - `internal/sandbox/syscall_interception_test.go` validates that dangerous syscalls are categorically unimplemented in gVisor
- **Metadata endpoint blocking** - `internal/netsec/metadata_unreachable_test.go` verifies 169.254.169.254 is unreachable, not just firewalled
- **VRAM zeroing** - `internal/scheduler.GPUBinding` ensures GPU memory is cleared between tenants
- **Read-only root filesystem** - `internal/nomadclient/jobspec.go` constructs jobs with immutable roots and dropped capabilities

### How We Achieved Reliability

- **Degraded-mode scheduler** - `internal/scheduler.DegradedModeDispatcher` queues work when Nomad is unreachable
- **Watchdog and reaper** - `internal/resilience` detects stuck generations by monitoring token cadence
- **Retry with backoff** - `internal/ingest.RetryingDownload` handles transient failures
- **Durable job queues** - `internal/queue` ensures work isn't lost during failures

### How We Achieved Performance

- **Precision selection** - `internal/quant.SelectPrecision` chooses FP16/FP8/INT4 based on GPU capabilities
- **Footprint computation** - `internal/ingest.ComputeVRAMFootprint` calculates requirements before scheduling
- **Streaming responses** - SSE-based token streaming for low latency
- **Prometheus metrics** - `internal/metrics/vllm_scraper.go` provides observability without overhead

## License

See [LICENSE](LICENSE) for details.

## Contributing

This project maintains strict security and quality standards. When contributing:
- All sandboxing code changes require security review
- Test coverage is mandatory for critical paths
- Follow the existing code structure and patterns
- Read ARCHITECTURE.md and SECURITY.md before working on core infrastructure

---

Built with a zero-trust mindset for production AI workloads.
