# Redline inference image: pinned CUDA, PyTorch, and vLLM, running as an
# unprivileged user, with a deterministic entrypoint. Built and pushed by
# CI, then referenced by digest (not tag) from InferenceJobSpec.Image so a
# running deployment can never silently pick up a mutated image. CI
# resolves and locks the base image digest on build (see
# deploy/vllm-image/base-image.lock) rather than one being hand-pinned
# here, since a digest copied into this file without verifying it against
# the registry is worse than no pin at all.
#
# Every layer above the CUDA base runs as the unprivileged "vllm" user;
# nothing in this image ever needs root at runtime, and the sandbox
# (gVisor + dropped capabilities, see steps 74/77) does not depend on that
# but is defense in depth alongside it.

FROM nvidia/cuda:12.4.1-runtime-ubuntu22.04

ARG PYTHON_VERSION=3.11
ARG TORCH_VERSION=2.4.0
ARG VLLM_VERSION=0.6.2

ENV DEBIAN_FRONTEND=noninteractive \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    HF_HUB_OFFLINE=0

RUN apt-get update && apt-get install -y --no-install-recommends \
        python${PYTHON_VERSION} \
        python${PYTHON_VERSION}-venv \
        python3-pip \
    && rm -rf /var/lib/apt/lists/*

RUN useradd --create-home --uid 10001 --shell /usr/sbin/nologin vllm

WORKDIR /home/vllm

# Pinned exact versions, not ranges: a floating "torch>=2.4" would let a
# transitive dependency bump silently change numerical behavior between
# otherwise-identical deployments.
RUN pip install --no-cache-dir \
        torch==${TORCH_VERSION} \
        vllm==${VLLM_VERSION}

COPY --chown=vllm:vllm entrypoint.sh /home/vllm/entrypoint.sh
RUN chmod 0555 /home/vllm/entrypoint.sh

USER 10001

# No shell form, no implicit argument concatenation: every argument vLLM
# receives came through entrypoint.sh's explicit translation, not string
# interpolation into a shell.
ENTRYPOINT ["/home/vllm/entrypoint.sh"]
