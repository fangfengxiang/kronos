#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repository_root"
module_directories=$(GOWORK="$repository_root/go.work" go list -m -f '{{.Dir}}')

while IFS= read -r module_directory; do
  (
    cd "$module_directory"
    GOWORK=off go test -race -count=1 ./...
  )
done <<< "$module_directories"
