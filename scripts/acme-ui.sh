#!/usr/bin/env bash
set -euo pipefail

repo="luodaoyi/acme-ui"

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac

tmp="$(mktemp -d)"
cleanup() {
  rm -rf "$tmp"
}
trap cleanup EXIT INT TERM

asset="acme-ui-linux-${arch}"
base_url="https://github.com/${repo}/releases/latest/download"

curl -fsSL "${base_url}/${asset}" -o "${tmp}/${asset}"
chmod +x "${tmp}/${asset}"

if command -v sha256sum >/dev/null 2>&1; then
  if curl -fsSL "${base_url}/checksums.txt" -o "${tmp}/checksums.txt"; then
    (
      cd "$tmp"
      grep " ${asset}$" checksums.txt | sha256sum -c -
    )
  fi
fi

"${tmp}/${asset}" "$@"
