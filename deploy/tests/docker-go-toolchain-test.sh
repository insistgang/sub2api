#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

go_version=$(awk '$1 == "go" { print $2 }' backend/go.mod)
if [ -z "$go_version" ]; then
  printf 'backend/go.mod must declare a Go version\n' >&2
  exit 1
fi

failed=0
for dockerfile in Dockerfile backend/Dockerfile deploy/Dockerfile; do
  # Resolve ARG-backed FROM images as well as literal golang images without
  # requiring Docker or downloading a toolchain.
  builder_images=$(awk '
    $1 == "ARG" {
      separator = index($2, "=")
      if (separator > 1) {
        arguments[substr($2, 1, separator - 1)] = substr($2, separator + 1)
      }
    }
    $1 == "FROM" {
      image = $2
      if (image ~ /^--platform=/) image = $3
      if (image ~ /^\$\{[A-Za-z_][A-Za-z0-9_]*\}$/) {
        name = image
        sub(/^\$\{/, "", name)
        sub(/\}$/, "", name)
        image = arguments[name]
      }
      if (image ~ /^golang:/) print image
    }
  ' "$dockerfile")

  if [ -z "$builder_images" ]; then
    printf '%s must declare a pinned golang builder image\n' "$dockerfile" >&2
    failed=1
    continue
  fi

  while IFS= read -r image; do
    image_version=${image#golang:}
    image_version=${image_version%%@*}
    image_version=${image_version%%-*}
    if [ "$image_version" != "$go_version" ]; then
      printf '%s uses %s; backend/go.mod requires Go %s\n' \
        "$dockerfile" "$image" "$go_version" >&2
      failed=1
    fi
  done <<EOF
$builder_images
EOF
done

if [ "$failed" -ne 0 ]; then
  exit 1
fi

printf 'docker Go toolchain test passed (Go %s)\n' "$go_version"
