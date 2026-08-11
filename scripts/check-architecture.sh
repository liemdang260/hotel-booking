#!/bin/sh
set -eu

domain_roots=""
usecase_roots=""

for root in services/*/internal/domain; do
  if [ -d "$root" ]; then
    domain_roots="$domain_roots $root"
  fi
done

for root in services/*/internal/usecase; do
  if [ -d "$root" ]; then
    usecase_roots="$usecase_roots $root"
  fi
done

if [ -z "$domain_roots" ] && [ -z "$usecase_roots" ]; then
  echo "No domain/usecase packages found; architecture guard has nothing to scan."
  exit 0
fi

violations=""

if [ -n "$domain_roots" ]; then
  domain_violations="$(
    grep -R -n -E '"[^"]+/internal/(usecase|application|infrastructure)(/[^"]*)?"' $domain_roots --include='*.go' || true
  )"
  if [ -n "$domain_violations" ]; then
    violations="$violations\nDomain must not import usecase/application/infrastructure:\n$domain_violations"
  fi
fi

if [ -n "$usecase_roots" ]; then
  usecase_violations="$(
    grep -R -n -E '"[^"]+/internal/(application|infrastructure)(/[^"]*)?"' $usecase_roots --include='*.go' || true
  )"
  if [ -n "$usecase_violations" ]; then
    violations="$violations\nUsecase must not import application/infrastructure:\n$usecase_violations"
  fi
fi

if [ -n "$violations" ]; then
  echo "Architecture dependency violation detected."
  printf '%b\n' "$violations"
  exit 1
fi

echo "Architecture dependency guard passed."
