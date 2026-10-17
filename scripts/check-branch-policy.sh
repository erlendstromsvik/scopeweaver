#!/usr/bin/env bash
set -euo pipefail

case "${BASE_REF:-}" in
  dev)
    case "${HEAD_REF:-}" in
      feat/?*|fix/?*) exit 0 ;;
    esac
    printf 'Contributions to dev must use a feat/* or fix/* branch created from dev.\n' >&2
    ;;
  main)
    if [[ "${HEAD_REF:-}" == dev && -n "${BASE_REPOSITORY:-}" && "${HEAD_REPOSITORY:-}" == "$BASE_REPOSITORY" ]]; then
      exit 0
    fi
    printf 'Only this repository\047s dev branch may promote to main after all checks and E2E pass.\n' >&2
    ;;
  *) printf 'Pull requests must target dev, or promote dev to main.\n' >&2 ;;
esac
exit 1
