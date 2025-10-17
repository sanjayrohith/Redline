#!/usr/bin/env bash
#
# entrypoint.sh — deterministic vLLM startup: reads model and tuning
# parameters from explicit environment variables (set by the Nomad job
# spec's task.Env, never free-form shell arguments) and translates them
# into a fixed `vllm serve` invocation. The same environment always
# produces the same command line - no conditional logic that could drift
# between deployments of "the same" configuration.

set -euo pipefail

: "${VLLM_MODEL:?VLLM_MODEL is required (e.g. the cached artifact path)}"
: "${VLLM_HOST:=0.0.0.0}"
: "${VLLM_PORT:=8000}"
: "${VLLM_DTYPE:=auto}"
: "${VLLM_MAX_MODEL_LEN:=}"
: "${VLLM_TENSOR_PARALLEL_SIZE:=1}"
: "${VLLM_GPU_MEMORY_UTILIZATION:=0.85}"

args=(
	serve "${VLLM_MODEL}"
	--host "${VLLM_HOST}"
	--port "${VLLM_PORT}"
	--dtype "${VLLM_DTYPE}"
	--tensor-parallel-size "${VLLM_TENSOR_PARALLEL_SIZE}"
	--gpu-memory-utilization "${VLLM_GPU_MEMORY_UTILIZATION}"
)

if [[ -n "${VLLM_MAX_MODEL_LEN}" ]]; then
	args+=(--max-model-len "${VLLM_MAX_MODEL_LEN}")
fi

exec python3 -m vllm.entrypoints.openai.api_server "${args[@]}"
