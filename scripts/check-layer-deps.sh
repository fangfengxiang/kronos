#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repository_root"
root_module=$(GOWORK=off go list -m -f '{{.Path}}')
module_directories=$(GOWORK="$repository_root/go.work" go list -m -f '{{.Dir}}')

list_package_imports() {
  while IFS= read -r module_directory; do
    (
      cd "$module_directory"
      GOWORK=off go list -f '{{.ImportPath}}{{range .Imports}} {{.}}{{end}}' ./...
    ) || return
  done <<< "$module_directories"
}

list_package_imports |
  awk -v prefix="$root_module/" '
    function layer(path, relative, pieces) {
      if (index(path, prefix) != 1) return ""
      relative = substr(path, length(prefix) + 1)
      split(relative, pieces, "/")
      return pieces[1]
    }
    {
      source = layer($1)
      if (source !~ /^(core|infra|framework|platform)$/) next
      for (i = 2; i <= NF; i++) {
        if (index($i, prefix) != 1 && $i != substr(prefix, 1, length(prefix) - 1)) continue
        target = layer($i)
        if (source == target) continue
        if (source ~ /^(infra|framework)$/ && target == "core") continue
        if (source == "platform" && target ~ /^(core|infra|framework)$/) continue
        print $1 " imports forbidden layer via " $i > "/dev/stderr"
        failed = 1
      }
    }
    END { exit failed }
  '
