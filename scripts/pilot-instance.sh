#!/usr/bin/env bash
# pilot-instance.sh — one isolated compose project per pilot.
#
# A pilot is a customer's own-shop instance: its own compose project
# (own networks, own volumes, own postgres), its own secrets in
# pilots/<name>.env (gitignored), its own loopback ports, hard resource
# caps via docker-compose.pilot.yml, and its own pg_dump backups.
#
# Usage:
#   pilot-instance.sh create  <name>       stamp pilots/<name>.env from the template
#   pilot-instance.sh up      <name> [svc…]  start the pilot (postgres only by default)
#   pilot-instance.sh down    <name>       stop it (volumes stay)
#   pilot-instance.sh backup  <name>       pg_dump to backups/<name>-<date>.sql.gz
#   pilot-instance.sh isolation-test       PROOF: pilot A's credentials cannot
#                                         read pilot B's data (two throwaway
#                                         instances, then teardown)
#
# Everything runs through podman; raw docker is prohibited fleet-wide.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
pilots_dir="$here/pilots"
backups_dir="$here/backups"
compose_bin="${HLXN_PILOT_COMPOSE:-podman-compose}"

die() { echo "pilot-instance: $*" >&2; exit 2; }
[ $# -ge 1 ] || die "usage: pilot-instance.sh create|up|down|backup|isolation-test <name>"
cmd="$1"; name="${2:-}"

env_file="$pilots_dir/$name.env"
pc() { # pc <name> <compose args…>
  local n="$1"; shift
  ( cd "$here" && "$compose_bin" -f docker-compose.yml -f docker-compose.pilot.yml \
      --env-file "$pilots_dir/$n.env" -p "pilot-$n" "$@" )
}

case "$cmd" in
  create)
    [ -n "$name" ] || die "create needs a pilot name"
    [ -e "$env_file" ] && die "pilots/$name.env already exists"
    sed "s/NAME/$name/g" "$pilots_dir/example.env" > "$env_file"
    chmod 600 "$env_file"
    echo "stamped $env_file — set POSTGRES_PASSWORD before 'up'"
    ;;
  up)
    [ -n "$name" ] || die "up needs a pilot name"
    [ -e "$env_file" ] || die "no pilots/$name.env (create first)"
    grep -q "generate-one-per-pilot" "$env_file" && die "pilots/$name.env still carries the template password"
    shift 2 2>/dev/null || shift 1
    pc "$name" up -d "${@:-postgres}"
    ;;
  down)
    [ -n "$name" ] || die "down needs a pilot name"
    pc "$name" down
    ;;
  backup)
    [ -n "$name" ] || die "backup needs a pilot name"
    mkdir -p "$backups_dir"
    out="$backups_dir/$name-$(date -u +%Y%m%dT%H%M%SZ).sql.gz"
    ( cd "$here" && podman exec "pilot-${name}_postgres_1" pg_dump -U "$(sed -n 's/^POSTGRES_USER=//p' "$env_file")" "$(sed -n 's/^POSTGRES_DB=//p' "$env_file")" ) | gzip > "$out"
    echo "wrote $out"
    ;;
  isolation-test)
    # Two throwaway pilots with distinct credentials and ports. The proof
    # has two arms: (1) pilot A's credentials REJECTED by pilot B's
    # postgres (authentication is per-instance); (2) pilot A's postgres
    # cannot even RESOLVE pilot B (separate compose networks). Teardown
    # always runs.
    t="iso$$"; a="$t-a"; b="$t-b"
    cleanup() { pc "$a" down --remove-orphans >/dev/null 2>&1 || true; pc "$b" down --remove-orphans >/dev/null 2>&1 || true; rm -f "$pilots_dir/$a.env" "$pilots_dir/$b.env"; }
    trap cleanup EXIT
    for n in "$a" "$b"; do
      sed "s/NAME/$n/g" "$pilots_dir/example.env" \
        | sed "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=pw-$n-only/" > "$pilots_dir/$n.env"
    done
    # Distinct ports so both can be up on the same host.
    printf 'POSTGRES_HOST_PORT=5599\n' >> "$pilots_dir/$a.env"
    printf 'POSTGRES_HOST_PORT=5699\n' >> "$pilots_dir/$b.env"
    pc "$a" up -d postgres >/dev/null
    pc "$b" up -d postgres >/dev/null
    # Wait for readiness (bounded).
    for i in $(seq 1 30); do
      podman exec "pilot-${a}_postgres_1" pg_isready -U "pilot_$a" >/dev/null 2>&1 && \
      podman exec "pilot-${b}_postgres_1" pg_isready -U "pilot_$b" >/dev/null 2>&1 && break
      sleep 1
    done
    # Arm 1: A's credentials against B's database → MUST be rejected.
    if podman exec -e PGPASSWORD="pw-$a-only" "pilot-${b}_postgres_1" \
        psql -U "pilot_$a" -d "pilot_$b" -tAc "select 1" >/dev/null 2>&1; then
      die "ARM 1 FAILED: pilot A's credentials were ACCEPTED by pilot B"
    fi
    echo "arm 1 ok: pilot A credentials rejected by pilot B (authentication is per-instance)"
    # Arm 2: A's postgres cannot resolve B's container (separate networks).
    if podman exec "pilot-${a}_postgres_1" getent hosts "pilot-${b}_postgres_1" >/dev/null 2>&1; then
      die "ARM 2 FAILED: pilot A's postgres resolved pilot B — networks are not isolated"
    fi
    echo "arm 2 ok: pilot A's postgres cannot resolve pilot B (networks are isolated)"
    echo "ISOLATION TEST PASSED"
    ;;
  *) die "unknown command: $cmd" ;;
esac
