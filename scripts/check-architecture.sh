#!/bin/sh
set -eu

scan_roots=""

for root in services/*/internal/domain services/*/internal/usecase; do
  if [ -d "$root" ]; then
    scan_roots="$scan_roots $root"
  fi
done

if [ -z "$scan_roots" ]; then
  echo "No domain/usecase packages found; architecture guard has nothing to scan."
  exit 0
fi

violations="$(
  grep -R -n -E '"[^"]+/internal/(application|infrastructure)(/[^"]*)?"' $scan_roots     --include='*.go' || true
)"

if [ -n "$violations" ]; then
  echo "Architecture dependency violation detected."
  echo "Domain and usecase packages must not import application or infrastructure packages."
  echo "$violations"
  exit 1
fi

echo "Architecture dependency guard passed."
