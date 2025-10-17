#!/usr/bin/env bash
#
# install-gvisor.sh — node provisioning: install gVisor (runsc) and
# register it as a containerd runtime handler.
#
# This is run once per GPU worker node during provisioning (e.g. from a
# cloud-init script or a configuration management run), never by the
# gateway itself. It requires root and modifies host container runtime
# configuration - it is not something application code should invoke.
#
# Reference: https://gvisor.dev/docs/user_guide/install/
# Reference: https://gvisor.dev/docs/user_guide/containerd/quick_start/

set -euo pipefail

GVISOR_RELEASE_URL="https://storage.googleapis.com/gvisor/releases/release/latest/${ARCH:-x86_64}"
INSTALL_DIR="/usr/local/bin"
CONTAINERD_CONFIG_DIR="/etc/containerd"
CONTAINERD_CONFIG="${CONTAINERD_CONFIG_DIR}/config.toml"

require_root() {
	if [[ "${EUID}" -ne 0 ]]; then
		echo "install-gvisor.sh: must run as root" >&2
		exit 1
	fi
}

install_runsc() {
	local tmp
	tmp="$(mktemp -d)"
	trap 'rm -rf "${tmp}"' RETURN

	for bin in runsc containerd-shim-runsc-v1; do
		curl -fsSL "${GVISOR_RELEASE_URL}/${bin}" -o "${tmp}/${bin}"
		curl -fsSL "${GVISOR_RELEASE_URL}/${bin}.sha512" -o "${tmp}/${bin}.sha512"
		(cd "${tmp}" && sha512sum -c "${bin}.sha512")
		chmod a+rx "${tmp}/${bin}"
		mv "${tmp}/${bin}" "${INSTALL_DIR}/${bin}"
	done
}

register_containerd_runtime() {
	mkdir -p "${CONTAINERD_CONFIG_DIR}"

	# Registers the "runsc" runtime handler. This block is applied
	# idempotently via the sibling gvisor-runtime.toml snippet, which the
	# node's config management layer merges into config.toml rather than
	# this script editing it in place.
	install -m 0644 "$(dirname "$0")/../containerd/gvisor-runtime.toml" \
		"${CONTAINERD_CONFIG_DIR}/gvisor-runtime.toml"

	if ! grep -q 'gvisor-runtime.toml' "${CONTAINERD_CONFIG}" 2>/dev/null; then
		printf '\nimports = ["%s/gvisor-runtime.toml"]\n' "${CONTAINERD_CONFIG_DIR}" >>"${CONTAINERD_CONFIG}"
	fi

	systemctl restart containerd
}

verify_installation() {
	runsc --version
	ctr version >/dev/null # confirms containerd itself is reachable post-restart
}

main() {
	require_root
	install_runsc
	register_containerd_runtime
	verify_installation
	echo "install-gvisor.sh: runsc installed and registered with containerd"
}

main "$@"
