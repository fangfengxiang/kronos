#!/usr/bin/env bash
set -euo pipefail

test_directory=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
if [[ $# -ne 1 ]]; then
  printf 'Usage: bash test/interop/microv2/verify.sh ADDRESS\n' >&2
  exit 2
fi

temporary_directory=$(mktemp -d)
trap 'rm -rf "$temporary_directory"' EXIT
kronos_client="$temporary_directory/kronos-client"
(
  cd "$test_directory/../../.."
  GOWORK=off go build -o "$kronos_client" "$test_directory/testdata/kronos_client.go"
)
for version in v2.0.0 v2.9.1; do
  consumer_directory="$temporary_directory/$version"
  mkdir -p "$consumer_directory"
  cp "$test_directory/testdata/client.go" "$consumer_directory/main.go"
  cp "$test_directory/testdata/upstream_server.go" "$consumer_directory/server.go"
  printf 'module example.com/kronos-microv2-interop\n\ngo 1.25.0\n\nrequire github.com/micro/go-micro/v2 %s\n' "$version" > "$consumer_directory/go.mod"
  printf 'Testing upstream go-micro %s\n' "$version"
  (
    cd "$consumer_directory"
    export GOWORK=off
    # The 2020 x/net socket package uses linknames rejected by modern Go.
    go mod edit -require=golang.org/x/net@v0.54.0
    if [[ "$version" == v2.0.0 ]]; then
      upstream_module=github.com/micro/go-micro/v2
      go mod download "$upstream_module@$version"
      cp -R "$(go env GOMODCACHE)/$upstream_module@$version" ./upstream
      find ./upstream -type d -exec chmod u+w {} +
      chmod u+w ./upstream/go.mod
      # The unused v1 module pulls in a generator whose repository is gone.
      go mod edit -droprequire=github.com/micro/go-micro ./upstream/go.mod
      go mod edit -replace="$upstream_module=./upstream"
    fi
    go build -mod=mod -o ./upstream-fixture .
    ./upstream-fixture "$1"
    ./upstream-fixture serve ./ready &
    upstream_pid=$!
    trap 'kill "$upstream_pid" 2>/dev/null || true; wait "$upstream_pid" || true' EXIT
    for ((attempt=0; attempt<100; attempt++)); do
      if [[ -f ./ready ]]; then
        break
      fi
      if ! kill -0 "$upstream_pid" 2>/dev/null; then
        wait "$upstream_pid"
        exit 1
      fi
      sleep 0.05
    done
    if [[ ! -f ./ready ]]; then
      printf 'Upstream server did not publish a ready address\n' >&2
      exit 1
    fi
    "$kronos_client" "$(cat ./ready)"
  )
done
