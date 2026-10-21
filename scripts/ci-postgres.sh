#!/usr/bin/env bash
# Own one disposable database for the entire verification run; never use app data.
set -euo pipefail
# CI checks transaction/role/worker behavior, not power-loss durability. Avoid
# coupling their bounded deadlines to the shared runner's block-device latency.
# Keep a disk-backed option for storage-sensitive qualification.
fixture_storage_args=()
case "${OVERLAID_CI_POSTGRES_STORAGE:-memory}" in
  memory) fixture_storage_args=(--tmpfs "/var/lib/postgresql/data:rw,nosuid,noexec,size=2g") ;;
  volume) ;;
  *) echo "OVERLAID_CI_POSTGRES_STORAGE must be memory or volume" >&2; exit 2 ;;
esac
fixture_name="overlaid-ci-${GITHUB_RUN_ID:-local}-$$"
fixture_password="fixture-$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')"
cleanup_fixture() {
  local result=$?
  # An otherwise green run must not hide a failed cleanup and leak fixture data.
  if ! docker rm -fv "$fixture_name" >/dev/null; then result=1; fi
  exit "$result"
}
trap cleanup_fixture EXIT
# Frequent fixture schema creation generates enough WAL to crowd the 2 GiB
# mount at the default 1 GiB checkpoint target. Checkpoint sooner; keep fsync,
# synchronous_commit and full_page_writes enabled. max_wal_size is a soft target.
docker run -d --name "$fixture_name" --label overlaid.fixture=ci "${fixture_storage_args[@]}" \
  -e POSTGRES_PASSWORD="$fixture_password" -e POSTGRES_DB=overlaid_ci \
  -p 0:5432 docker.io/library/postgres:17 \
  -c max_wal_size=256MB >/dev/null
for ((attempt=0; attempt<60; attempt++)); do
  if docker exec "$fixture_name" pg_isready -U postgres -d overlaid_ci >/dev/null 2>&1; then break; fi
  sleep 1
done
fixture_port=$(docker inspect --format '{{(index (index .NetworkSettings.Ports "5432/tcp") 0).HostPort}}' "$fixture_name")
fixture_ip=$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$fixture_name")
fixture_gateway=$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.Gateway}}{{end}}' "$fixture_name")
# Hosted runners may themselves be containers. Select an actually reachable
# address instead of assuming their localhost is the Docker host.
fixture_endpoint=$(python3 - "$fixture_port" "$fixture_ip" "$fixture_gateway" <<'PY'
import socket,sys
for host,port in [('127.0.0.1',int(sys.argv[1])),(sys.argv[2],5432),(sys.argv[3],int(sys.argv[1]))]:
    try:
        with socket.create_connection((host,port),timeout=2):
            print(f'{host}:{port}'); break
    except OSError: pass
else: raise SystemExit('Disposable PostgreSQL is unreachable from this runner')
PY
)
export OVERLAID_TEST_DATABASE_URL="postgres://postgres:$fixture_password@$fixture_endpoint/overlaid_ci?sslmode=disable"
make verify
(cd backend && go test -race -count=1 ./...)
(cd backend/spikes/river && go test -race -count=1 ./...)
