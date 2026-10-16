#!/usr/bin/env bash
# sw-instance.sh — launch, watch and destroy ephemeral ScopeWeaver instances (D.5a).
#
# One instance = one compose project (`-p <id>`): the app plus its own
# Postgres, volumes and networks. Secrets reach the instance as read-only
# mounted FILES (owner decision D6), never environment variables; the
# deliverable lands in an operator-chosen host directory and stays there
# until deleted by hand (owner decision D4).
#
# Usage:
#   scripts/sw-instance.sh [--dry-run] launch BP=<blueprint.yaml> [DELIVERABLES=<dir>]
#   scripts/sw-instance.sh [--dry-run] status  ID=<instance-id>
#   scripts/sw-instance.sh [--dry-run] report  ID=<instance-id>
#   scripts/sw-instance.sh [--dry-run] destroy ID=<instance-id>
#   scripts/sw-instance.sh ls
#
# Environment:
#   SW_HOME         instance-state root (default ~/.scopeweaver/instances)
#   SW_IMAGE        app image (default scopeweaver:local)
#   SW_LLM_API_KEY_FILE  host file whose contents are copied into the
#                        instance's secrets dir as llm_api_key (default
#                        ${XDG_CONFIG_HOME:-~/.config}/sw/llm_api_key, used
#                        when it exists)
#
# --dry-run prints every docker/curl/go command instead of running it, but
# still creates the instance directory so the generated files can be checked.
set -euo pipefail

DRY_RUN=0
if [ "${1:-}" = "--dry-run" ]; then
	DRY_RUN=1
	shift
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="$REPO_ROOT/deploy/instance/compose.yml"
SW_HOME="${SW_HOME:-$HOME/.scopeweaver/instances}"
SW_IMAGE="${SW_IMAGE:-scopeweaver:local}"
DEFAULT_DELIVERABLES="$HOME/scopeweaver-deliverables"
DEFAULT_LLM_API_KEY_FILE="${XDG_CONFIG_HOME:-$HOME/.config}/sw/llm_api_key"
ADMIN_USER="ARTEX"

# run <cmd...> — echo, then execute unless --dry-run was given.
run() {
	printf '[dry-run] %s\n' "$*" >&2
	if [ "$DRY_RUN" -eq 0 ]; then
		"$@"
	fi
}

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

# rand_hex <n-bytes> -> 2n lowercase hex chars. od reads a FIXED number of
# bytes from /dev/urandom, so nothing is left writing to a closed pipe:
# `tr … </dev/urandom | dd` dies on SIGPIPE (exit 141) under `set -o pipefail`
# once the reader stops.
rand_hex() {
	od -An -N"$1" -tx1 /dev/urandom | tr -d ' \n'
}
gen_password() {
	rand_hex 16
	printf '\n'
}
instance_dir() {
	printf '%s/%s' "$SW_HOME" "$1"
}

# require_id — validate the ID=… argument.
require_id() {
	local id="${1:-}"
	[ -n "$id" ] || die "ID=<instance-id> is required"
	case "$id" in
	sw-*) ;;
	*) die "instance id must start with 'sw-' (got '$id')" ;;
	esac
}

# instance_url ID — the loopback URL the app's 8787 is published on.
instance_url() {
	local out
	out="$(docker compose -p "$1" -f "$COMPOSE_FILE" port app 8787 2>/dev/null | head -n1)"
	[ -n "$out" ] || die "could not resolve the host port for instance $1 (is it running?)"
	printf 'http://%s' "$out"
}

# login URL PASSWORD — print a JWT for the instance.
login() {
	local url="$1" password="$2" resp
	resp="$(curl -sf -X POST "$url/api/auth/login" \
		-H 'Content-Type: application/json' \
		-d "{\"username\":\"$ADMIN_USER\",\"password\":\"$password\"}")" \
		|| die "login failed against $url (check the admin password file)"
	printf '%s' "$resp" | sed -n 's/.*"token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1
}

cmd_launch() {
	local bp="" deliverables="${DELIVERABLES:-$DEFAULT_DELIVERABLES}"
	local kv
	for kv in "$@"; do
		case "$kv" in
		BP=*) bp="${kv#BP=}" ;;
		DELIVERABLES=*) deliverables="${kv#DELIVERABLES=}" ;;
		*) die "unknown launch argument '$kv' (want BP=<blueprint.yaml> [DELIVERABLES=<dir>])" ;;
		esac
	done
	[ -n "$bp" ] || die "launch needs BP=<blueprint.yaml>"
	[ -f "$bp" ] || die "blueprint not found: $bp"

	# D6: instances read secrets from mounted files. A blueprint that still
	# uses ${env:...} for the admin password would silently depend on the
	# host environment instead — refuse it.
	if ! grep -q 'password:[[:space:]]*\${file:' "$bp"; then
		die "blueprint $bp: admin.password must be a file secret ref (\${file:/run/secrets/admin_password}); \${env:…} refs are not supported for instances (owner decision D6)"
	fi

	# The LLM key: an explicit SW_LLM_API_KEY_FILE must exist; otherwise the
	# default per-user key file is used when present. A blueprint that reads
	# /run/secrets/llm_api_key without a key to copy would boot and then fail
	# every LLM call, so refuse it here instead.
	local llm_key_file=""
	if [ -n "${SW_LLM_API_KEY_FILE:-}" ]; then
		[ -f "$SW_LLM_API_KEY_FILE" ] || die "SW_LLM_API_KEY_FILE not found: $SW_LLM_API_KEY_FILE"
		llm_key_file="$SW_LLM_API_KEY_FILE"
	elif [ -f "$DEFAULT_LLM_API_KEY_FILE" ]; then
		llm_key_file="$DEFAULT_LLM_API_KEY_FILE"
	fi
	if [ -z "$llm_key_file" ] && grep -q '/run/secrets/llm_api_key' "$bp"; then
		die "blueprint $bp reads /run/secrets/llm_api_key but no key file was found: put the key in $DEFAULT_LLM_API_KEY_FILE or set SW_LLM_API_KEY_FILE"
	fi

	local id rand
	rand="$(rand_hex 2)"
	id="sw-$(date -u +%Y%m%d-%H%M%S)-${rand}"
	local dir
	dir="$(instance_dir "$id")"

	mkdir -p "$dir/secrets" "$dir/sw"
	chmod 700 "$dir" "$dir/secrets" "$dir/sw"

	local admin_password pg_password
	admin_password="$(gen_password)"
	pg_password="$(gen_password)"
	(umask 077; printf '%s\n' "$admin_password" >"$dir/secrets/admin_password")
	(umask 077; printf '%s\n' "$pg_password" >"$dir/secrets/pg_password")
	if [ -n "$llm_key_file" ]; then
		(umask 077; cp "$llm_key_file" "$dir/secrets/llm_api_key")
		chmod 600 "$dir/secrets/llm_api_key"
	fi

	# The DSN lives in a read-only mounted config file, not in the
	# container environment (D6).
	(umask 077; printf '{"database":{"dsn":"postgres://scopeweaver:%s@postgres:5432/scopeweaver?sslmode=disable"}}\n' "$pg_password" >"$dir/sw/config.json")
	cp "$bp" "$dir/sw/blueprint.yaml"

	# Freshen the advisory register on the host before the instance boots
	# (E.7e hook). Skip with a note when the command isn't on this branch.
	if [ -d "$REPO_ROOT/advisory/cmd/advisory-sync" ]; then
		( cd "$REPO_ROOT" && run go run ./advisory/cmd/advisory-sync )
	else
		printf 'note: advisory/cmd/advisory-sync not present on this branch; skipping the pre-launch advisory sync\n' >&2
	fi

	SW_IMAGE="$SW_IMAGE" SW_INSTANCE_ID="$id" SW_INSTANCE_DIR="$dir" SW_DELIVERABLES="$deliverables" \
		run docker compose -p "$id" -f "$COMPOSE_FILE" up -d

	printf 'instance:  %s\n' "$id"
	if [ "$DRY_RUN" -eq 0 ]; then
		printf 'url:     %s\n' "$(instance_url "$id")"
	else
		printf 'url:     (dry run — run `docker compose -p %s -f %s port app 8787`)\n' "$id" "$COMPOSE_FILE"
	fi
	if [ -n "$llm_key_file" ]; then
		printf 'secrets: %s/secrets (admin_password, pg_password, llm_api_key from %s; mode 600)\n' "$dir" "$llm_key_file"
	else
		printf 'secrets: %s/secrets (admin_password, pg_password; mode 600)\n' "$dir"
	fi
	printf 'deliverables: %s/%s\n' "$deliverables" "$id"
	printf 'login:   username %s, password in %s/secrets/admin_password\n' "$ADMIN_USER" "$dir"
}

cmd_status() {
	local id="${ID:-}"
	require_id "$id"
	local dir
	dir="$(instance_dir "$id")"
	[ -f "$dir/secrets/admin_password" ] || die "no instance state at $dir"
	local url token
	url="$(instance_url "$id")"
	token="$(login "$url" "$(cat "$dir/secrets/admin_password")")"
	run curl -sf -H "Authorization: Bearer $token" "$url/api/instance/status"
}

cmd_report() {
	local id="${ID:-}"
	require_id "$id"
	local dir
	dir="$(instance_dir "$id")"
	[ -f "$dir/secrets/admin_password" ] || die "no instance state at $dir"
	local bundle
	if [ "$DRY_RUN" -eq 1 ]; then
		run curl -sf -X POST -H "Authorization: Bearer $token" "$url/api/instance/deliverable"
		printf '[dry-run] would print the bundle dir name and host path\n' >&2
		return 0
	fi
	bundle="$(curl -sf -X POST -H "Authorization: Bearer $token" "$url/api/instance/deliverable" | sed -n 's/.*"dir"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
	[ -n "$bundle" ] || die "deliverable response had no dir field"
	printf 'bundle: %s\n' "$bundle"
	printf 'host path: %s/%s/%s\n' "${DELIVERABLES:-$DEFAULT_DELIVERABLES}" "$id" "$bundle"
}

cmd_destroy() {
	local id="${ID:-}"
	require_id "$id"
	local dir
	dir="$(instance_dir "$id")"
	run docker compose -p "$id" -f "$COMPOSE_FILE" down -v --remove-orphans
	# The instance dir holds the generated secrets; the deliverables dir does
	# not live under it, so it survives (D4: kept until deleted by hand).
	run rm -rf "$dir"
	printf 'destroyed %s (instance dir and secrets removed; deliverables kept)\n' "$id"
}

cmd_ls() {
	printf 'instance dirs under %s:\n' "$SW_HOME"
	if [ -d "$SW_HOME" ]; then
		ls -1 "$SW_HOME" 2>/dev/null || printf '(none)\n'
	else
		printf '(none)\n'
	fi
	printf 'running compose projects:\n'
	docker compose ls --filter name=sw-
}

usage() {
	printf 'usage: %s [--dry-run] launch BP=<blueprint.yaml> [DELIVERABLES=<dir>]\n' "$0" >&2
	printf '       %s [--dry-run] status|report|destroy ID=<instance-id>\n' "$0" >&2
	printf '       %s ls\n' "$0" >&2
	exit 2
}

cmd="${1:-}"
shift || true
ID=""
for kv in "$@"; do
	case "$kv" in
	ID=*) ID="${kv#ID=}" ;;
	esac
done

case "$cmd" in
launch) cmd_launch "$@" ;;
status) cmd_status ;;
report) cmd_report ;;
destroy) cmd_destroy ;;
ls) cmd_ls ;;
*) usage ;;
esac
