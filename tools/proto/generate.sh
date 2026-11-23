#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$repository_root"

required_buf_version=1.70.0
if ! command -v buf >/dev/null || [[ $(buf --version) != "$required_buf_version" ]]; then
  printf 'Buf %s is required.\n' "$required_buf_version" >&2
  exit 1
fi

case "${1:-generate}" in
  generate)
    buf generate --template tools/proto/buf.gen.yaml
    ;;
  --check)
    buf format --diff --exit-code examples/assistant/api
    buf lint

    temporary_output=$(mktemp -d)
    trap 'rm -rf "$temporary_output"' EXIT
    buf generate --template tools/proto/buf.gen.yaml --output "$temporary_output"
    diff -ru examples/assistant/internal/wire "$temporary_output/examples/assistant/internal/wire"
    ;;
  *)
    printf 'Usage: bash tools/proto/generate.sh [--check]\n' >&2
    exit 2
    ;;
esac
