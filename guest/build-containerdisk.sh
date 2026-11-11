#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <qcow2-file> <image-reference>" >&2
  exit 2
fi

disk=$1
image=$2
if [[ ! -f "$disk" ]]; then
  echo "guest disk does not exist: $disk" >&2
  exit 1
fi
if ! command -v qemu-img >/dev/null 2>&1; then
  echo "qemu-img is required to verify the guest disk" >&2
  exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required to verify the guest disk" >&2
  exit 1
fi
if ! command -v "${CONTAINER_TOOL:-docker}" >/dev/null 2>&1; then
  echo "container tool is unavailable: ${CONTAINER_TOOL:-docker}" >&2
  exit 1
fi
if ! qemu-img info --output=json "$disk" | jq -e '.format == "qcow2"' >/dev/null; then
  echo "guest disk must be qcow2: $disk" >&2
  exit 1
fi

repo_root=$(cd "$(dirname "$0")/.." && pwd)
context="$repo_root/bin/guest-containerdisk"
mkdir -p "$context"
cp "$disk" "$context/disk.qcow2"
"${CONTAINER_TOOL:-docker}" build --platform linux/amd64 -f "$repo_root/guest/Containerfile" -t "$image" "$context"
