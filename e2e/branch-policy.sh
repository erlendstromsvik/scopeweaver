#!/usr/bin/env bash
set -euo pipefail
repo="$(cd "$(dirname "$0")/.." && pwd)"

check() {
  local expected="$1" base="$2" head="$3" owner="$4" result=0
  BASE_REF="$base" HEAD_REF="$head" HEAD_REPOSITORY="$owner" BASE_REPOSITORY=cskwork/scopeweaver \
    bash "$repo/scripts/check-branch-policy.sh" >/dev/null 2>&1 || result=$?
  if [[ "$result" != "$expected" ]]; then
    printf 'FAIL: %s -> %s (%s): exit %s, expected %s\n' "$head" "$base" "$owner" "$result" "$expected" >&2
    exit 1
  fi
}

check 0 dev feat/chat cskwork/scopeweaver
check 0 dev fix/chat contributor/scopeweaver
check 0 main dev cskwork/scopeweaver
check 1 main dev contributor/scopeweaver
check 1 main fix/chat cskwork/scopeweaver
check 1 dev main cskwork/scopeweaver
check 1 dev feat/ cskwork/scopeweaver
check 1 dev docs/chat cskwork/scopeweaver
check 1 release dev cskwork/scopeweaver
check 1 '' '' ''
printf 'PASS: 10 contribution branch-policy cases\n'
