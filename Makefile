# Convenience targets for local development. CI invokes the underlying commands
# directly; see .github/workflows/ci.yml and CONTRIBUTING.md.

.PHONY: test-db

# Run the database-backed Go suites against a throwaway PostgreSQL database that
# scripts/test-db.sh creates and drops. It never touches the live/production
# database, and refuses to run if ARTEX_TEST_DSN points at the live DB. Override
# TEST_DB_ROLE / TEST_DB_HOST / TEST_DB_NAME to match your local PostgreSQL.
# See docs/VERIFICATION.md ("Running database-backed tests").
test-db:
	./scripts/test-db.sh

.PHONY: advisory-sync

# Refresh the advisory register (NSM RSS) and the CISA KEV snapshot. Host-side; needs network.
advisory-sync:
	go run ./advisory/cmd/advisory-sync

.PHONY: instance

# Launch an ephemeral per-instance scan (compose project per instance). Pass a
# blueprint: `make instance BP=blueprint.yaml`. Other subcommands
# (status/report/destroy/ls) are invoked directly:
# ./scripts/sw-instance.sh status ID=<instance-id>.
instance:
	./scripts/sw-instance.sh launch $(BP)
