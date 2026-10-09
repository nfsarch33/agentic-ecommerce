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
#   pilot-instance.sh approver-access-test [bind-ip] [web-repo-dir]
#                                         PROOF: an external approver reaches
#                                         ONLY their own inbox (two throwaway
#                                         pilots with the full approvals loop;
#                                         per-pilot login, own items listed,
#                                         cross-tenant id 404, one-port share;
#                                         then teardown)
#
# Everything runs through podman; raw docker is prohibited fleet-wide.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
pilots_dir="$here/pilots"
backups_dir="$here/backups"
compose_bin="${HLXN_PILOT_COMPOSE:-podman-compose}"

die() { echo "pilot-instance: $*" >&2; exit 2; }
[ $# -ge 1 ] || die "usage: pilot-instance.sh create|up|down|backup|isolation-test|approver-access-test <name>"
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
    # The name lands in file paths, env values and container names:
    # [a-z0-9-] only — nothing that sed or a shell could reinterpret.
    printf '%s' "$name" | grep -qE '^[a-z0-9][a-z0-9-]{1,30}$' || die "pilot name must be [a-z0-9-] (2-31 chars), got '$name'"
    [ -e "$env_file" ] && die "pilots/$name.env already exists"
    sed "s/PNAME/$name/g" "$pilots_dir/example.env" > "$env_file"
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
      sed "s/PNAME/$n/g" "$pilots_dir/example.env" \
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
  approver-access-test)
    # The pilot-approver acceptance: an external approver reaches ONLY
    # their own inbox. Two throwaway pilots run the full approvals loop
    # (postgres, redis, temporal, temporal-worker, mc-api, inbox); the
    # inbox port is the single port published on the bind IP (the private
    # network). Arms, each able to fail the test on its own:
    #   L1 A's approver login on A's inbox          -> 200
    #   L2 A's approver login on B's inbox          -> 401  (per-pilot login)
    #   E1 Playwright spec (web repo) drives A's inbox as the approver:
    #      own item listed, foreign id invisible
    #   E2 review for B's item id through A's inbox -> 404  (cross-tenant id)
    #   P1 only the inbox port answers on the bind IP; every other
    #      published port of both pilots answers on loopback ONLY
    # Teardown always runs; the verdict line prints only when all arms held.
    bind_ip="${2:-$( (ip -4 addr show tailscale0 2>/dev/null | sed -n 's/.*inet \([0-9.]*\).*/\1/p' | head -n 1) || true )}"
    web_repo="${3:-${PILOT_WEB_REPO:-$HOME/Code/agentic-ecommerce-web}}"
    [ -n "$bind_ip" ] || die "approver-access-test needs a bind IP (arg 1, or a tailscale0 address)"
    [ -d "$web_repo/e2e" ] || die "web repo with e2e/ not found at $web_repo (arg 2 or PILOT_WEB_REPO)"
    t="acc$$"; a="$t-a"; b="$t-b"
    a_pg=15501; a_rd=15502; a_api=15503; a_web=15504; a_tg=15505; a_tui=15506
    b_pg=15511; b_rd=15512; b_api=15513; b_web=15514; b_tg=15515; b_tui=15516
    cleanup() {
      pc "$a" down --remove-orphans >/dev/null 2>&1 || true
      pc "$b" down --remove-orphans >/dev/null 2>&1 || true
      rm -f "$pilots_dir/$a.env" "$pilots_dir/$b.env"
    }
    trap cleanup EXIT
    for n in "$a" "$b"; do
      sed "s/PNAME/$n/g" "$pilots_dir/example.env" \
        | sed -e "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=pw-$n-only/" \
              -e "s/^ECOMMERCE_ADMIN_PASSWORD=.*/ECOMMERCE_ADMIN_PASSWORD=apw-$n-only/" \
              -e "s/^ECOMMERCE_JWT_SECRET=.*/ECOMMERCE_JWT_SECRET=jwtsecret-$n-only-0123456789abcdef0123456789/" \
        > "$pilots_dir/$n.env"
    done
    for spec in "$a:$a_pg:$a_rd:$a_api:$a_web:$a_tg:$a_tui" "$b:$b_pg:$b_rd:$b_api:$b_web:$b_tg:$b_tui"; do
      IFS=: read -r n pg rd api web tg tui <<<"$spec"
      printf 'POSTGRES_HOST_PORT=%s\nREDIS_HOST_PORT=%s\nMC_API_HOST_PORT=%s\nWEB_HOST_PORT=%s\nTEMPORAL_GRPC_HOST_PORT=%s\nTEMPORAL_UI_HOST_PORT=%s\nINBOX_BIND_HOST=%s\n' \
        "$pg" "$rd" "$api" "$web" "$tg" "$tui" "$bind_ip" >> "$pilots_dir/$n.env"
    done
    # Images: build locally (the published ghcr packages are not
    # anonymously pullable and the local registry needs credentials a
    # throwaway test must not handle). Both pilots share the built images.
    # --profile temporal-worker also enables temporal (its profiles list
    # includes that name) so profile-gated services resolve.
    echo "building images (cached after the first run): mc-api, temporal-worker, inbox…"
    pc "$a" --profile temporal-worker build mc-api temporal-worker >/dev/null || die "mc-api/temporal-worker image build failed"
    podman build -q -t ghcr.io/nfsarch33/agentic-ecommerce-web:main "$web_repo" >/dev/null \
      || die "inbox image build failed (from $web_repo)"
    # Phase 1: data stores first, so migrations and the seed land before
    # mc-api boots (admin bootstrap and product loads read these tables).
    pc "$a" --profile temporal-worker up -d postgres redis temporal >/dev/null || die "pilot A stores failed to start"
    pc "$b" --profile temporal-worker up -d postgres redis temporal >/dev/null || die "pilot B stores failed to start"
    for n in "$a" "$b"; do
      for i in $(seq 1 60); do
        podman exec "pilot-${n}_postgres_1" pg_isready -U "pilot_$n" -d "pilot_$n" >/dev/null 2>&1 && break
        sleep 1
      done
      podman exec "pilot-${n}_postgres_1" pg_isready -U "pilot_$n" -d "pilot_$n" >/dev/null 2>&1 \
        || die "pilot $n postgres never became ready"
      # The FULL chain does not apply on a fresh database: 0016 references
      # orders.channel, a column no migration in the chain ever creates
      # (schema drift against evolved dev databases — named as a follow-up
      # on the ticket). The approvals loop needs exactly these files:
      # products (0001), media assets (0003 — the product loader joins it
      # and the media gate requires one image), the tenant-setting
      # function the publish gate's RLS policy calls (0011, which skips
      # absent tables), the publish gate (0040, 0041) and the cost
      # ledger (0042).
      for f in 0001_create_products 0003_create_product_media_assets 0011_rls 0040_publish_gate 0041_publish_gate_dry_run 0042_cost_ledger; do
        timeout -k 5 90 podman exec -i "pilot-${n}_postgres_1" psql -q -v ON_ERROR_STOP=1 -U "pilot_$n" -d "pilot_$n" < "$here/migrations/$f.up.sql" >/dev/null 2>&1 \
          || die "migration $f failed on pilot $n"
      done
      timeout -k 5 90 podman exec -i "pilot-${n}_postgres_1" psql -q -v ON_ERROR_STOP=1 -U "pilot_$n" -d "pilot_$n" < "$here/seed/products.sql" >/dev/null 2>&1 \
        || die "product seed failed on pilot $n"
      # The approvals loop reads media from product_media_assets (not the
      # shop's product_images): give the seeded product the one image the
      # media gate requires.
      timeout -k 5 90 podman exec -i "pilot-${n}_postgres_1" psql -q -v ON_ERROR_STOP=1 -U "pilot_$n" -d "pilot_$n" 2>/dev/null <<'SQL' >/dev/null \
        || die "media asset seed failed on pilot $n"
INSERT INTO product_media_assets (id, product_id, storage_key, public_url, mime_type, size_bytes, alt_text, sort_order)
VALUES ('c1000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000001',
        'seed/resistance-band-set.jpg', '/media/products/resistance-band-set.jpg',
        'image/jpeg', 48213, 'Resistance band set with 5 colour-coded bands', 0)
ON CONFLICT (storage_key) DO NOTHING;
SQL
    done
    # Phase 2: the API and its worker first, the inbox after. This host's
    # CNI podman backend carries no embedded DNS on container networks,
    # so every cross-service address is PINNED to the live container IP
    # in the pilot env file before the consumer is created (mc-api dials
    # temporal exactly once at boot; a name it cannot resolve leaves the
    # workflow client nil and every workflow endpoint 503). The compose
    # overlay exposes the DB, redis and temporal addresses as
    # overridable; MC_API_BASE_URL is interpolated by the base file.
    # || true on every helper: the script runs under set -e, and a failed
    # command substitution in an ASSIGNMENT (curl refused, inspect racing
    # a restart) would otherwise exit silently with no arm verdict.
    ctr_ip() { timeout -k 5 30 podman inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "pilot-$1_$2_1" 2>/dev/null || true; }
    for n in "$a" "$b"; do
      pg_ip=""; rd_ip=""; tg_ip=""
      for i in $(seq 1 12); do
        pg_ip="$(ctr_ip "$n" postgres)"; rd_ip="$(ctr_ip "$n" redis)"; tg_ip="$(ctr_ip "$n" temporal)"
        [ -n "$pg_ip" ] && [ -n "$rd_ip" ] && [ -n "$tg_ip" ] && break
        timeout -k 5 60 podman start "pilot-${n}_temporal_1" >/dev/null 2>&1 || true
        sleep 10
      done
      [ -n "$pg_ip" ] && [ -n "$rd_ip" ] && [ -n "$tg_ip" ] \
        || die "pilot $n: could not read every store IP (pg=${pg_ip:-none} redis=${rd_ip:-none} temporal=${tg_ip:-none})"
      {
        printf 'ECOMMERCE_DB_URL=postgres://%s:%s@%s:5432/%s?sslmode=disable\n' "pilot_$n" "pw-$n-only" "$pg_ip" "pilot_$n"
        printf 'ECOMMERCE_REDIS_ADDR=%s:6379\n' "$rd_ip"
        printf 'ECOMMERCE_EVENTBUS_REDIS_ADDR=%s:6379\n' "$rd_ip"
        printf 'ECOMMERCE_TEMPORAL_ADDR=%s:7233\n' "$tg_ip"
      } >> "$pilots_dir/$n.env"
    done
    pc "$a" --profile temporal-worker up -d temporal-worker mc-api >/dev/null || die "pilot A services failed to start"
    pc "$b" --profile temporal-worker up -d temporal-worker mc-api >/dev/null || die "pilot B services failed to start"
    for n in "$a" "$b"; do
      api_ip=""
      for i in $(seq 1 12); do
        api_ip="$(ctr_ip "$n" mc-api)"
        [ -n "$api_ip" ] && break
        sleep 5
      done
      [ -n "$api_ip" ] || die "pilot $n: mc-api has no IP to pin for the inbox"
      printf 'MC_API_BASE_URL=http://%s:8080\n' "$api_ip" >> "$pilots_dir/$n.env"
    done
    pc "$a" --profile temporal-worker up -d frontend >/dev/null || die "pilot A inbox failed to start"
    pc "$b" --profile temporal-worker up -d frontend >/dev/null || die "pilot B inbox failed to start"

    api_token() { # api_token <mc-port> <email> <password> -> bearer, or empty
      curl -sS -X POST "http://127.0.0.1:$1/api/v1/auth/login" \
        -H 'content-type: application/json' -d "{\"email\":\"$2\",\"password\":\"$3\"}" 2>/dev/null \
        | python3 -c 'import json,sys; print(json.load(sys.stdin).get("access_token",""))' 2>/dev/null || true
    }
    wait_api() { # wait_api <mc-port> <label>
      for i in $(seq 1 90); do
        code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$1/healthz" 2>/dev/null || true)
        [ "$code" = "200" ] && return 0
        sleep 2
      done
      die "pilot $2 mc-api never became healthy"
    }
    wait_api "$a_api" "$a"; wait_api "$b_api" "$b"
    a_tok="$(api_token "$a_api" "approver-$a@pilot.test" "apw-$a-only")"
    b_tok="$(api_token "$b_api" "approver-$b@pilot.test" "apw-$b-only")"
    [ -n "$a_tok" ] && [ -n "$b_tok" ] || die "per-pilot approver login against mc-api failed"

    # Diag files, not variables: start_wf runs inside $( ) subshells where
    # variable writes never reach the parent (set -u then kills on read).
    WF_DIAG_A="$(mktemp)"; WF_DIAG_B="$(mktemp)"
    start_wf() { # start_wf <mc-port> <token> <diag-file> -> workflow id
      { curl -sS -w '\nHTTP=%{http_code}' -X POST "http://127.0.0.1:$1/api/v1/workflows/product-publish" \
        -H "authorization: Bearer $2" -H 'content-type: application/json' \
        -d '{"product_id":"b1000000-0000-0000-0000-000000000001"}' 2>/dev/null || true; } | tr '\n' ' ' > "$3"
      python3 -c 'import json,re,sys
t=open(sys.argv[1]).read()
m=re.search(r"HTTP=(\d+)\s*$", t)
body=t[:m.start()] if m else t
try:
    print(json.loads(body).get("workflow_id",""))
except Exception:
    print("")' "$3" 2>/dev/null || true
    }
    wf_waiting() { # wf_waiting <mc-port> <token> <wf-id> -> 0 when listed as waiting_review
      curl -sS "http://127.0.0.1:$1/api/v1/workflows?status=waiting_review" \
        -H "authorization: Bearer $2" 2>/dev/null \
        | python3 -c 'import json,sys,sys; wid=sys.argv[1]; ws=json.load(sys.stdin).get("workflows",[]); print(int(any(w.get("id")==wid or w.get("workflow_id")==wid for w in ws)))' "$3" 2>/dev/null
    }
    a_wf="$(start_wf "$a_api" "$a_tok" "$WF_DIAG_A")"; b_wf="$(start_wf "$b_api" "$b_tok" "$WF_DIAG_B")"
    [ -n "$a_wf" ] && [ -n "$b_wf" ] || die "starting the product-publish workflow failed (A: $(cat "$WF_DIAG_A" 2>/dev/null || echo 'no response') | B: $(cat "$WF_DIAG_B" 2>/dev/null || echo 'no response'); mc-api A tail: $(timeout -k 5 30 podman logs --tail 6 "pilot-${a}_mc-api_1" 2>/dev/null | tr '\n' ' ' || true))"
    for spec in "$a_api:$a_tok:$a_wf:$a" "$b_api:$b_tok:$b_wf:$b"; do
      IFS=: read -r p tok wf n <<<"$spec"
      for i in $(seq 1 60); do
        [ "$(wf_waiting "$p" "$tok" "$wf")" = "1" ] && break
        sleep 2
      done
      [ "$(wf_waiting "$p" "$tok" "$wf")" = "1" ] || die "pilot $n workflow $wf never reached waiting_review"
    done

    code_of() { curl -s -o /dev/null -w '%{http_code}' "$@" || true; }
    # L1/L2: the per-pilot login, THROUGH each inbox port on the bind IP.
    l1=$(code_of -X POST "http://$bind_ip:$a_web/api/auth/login" -H 'content-type: application/json' \
      -d "{\"email\":\"approver-$a@pilot.test\",\"password\":\"apw-$a-only\"}")
    [ "$l1" = "200" ] || die "ARM L1 FAILED: A's approver login on A's inbox returned HTTP $l1 (want 200)"
    echo "arm L1 ok: A's approver logs into A's inbox (HTTP 200)"
    l2=$(code_of -X POST "http://$bind_ip:$b_web/api/auth/login" -H 'content-type: application/json' \
      -d "{\"email\":\"approver-$a@pilot.test\",\"password\":\"apw-$a-only\"}")
    [ "$l2" = "401" ] || die "ARM L2 FAILED: A's approver login on B's inbox returned HTTP $l2 (want 401)"
    echo "arm L2 ok: A's approver credentials are rejected by B's inbox (HTTP 401, per-pilot login)"

    # E2: a cross-tenant item id through A's inbox must 404, never act.
    ck="$(mktemp)"
    curl -sS -c "$ck" -X POST "http://$bind_ip:$a_web/api/auth/login" -H 'content-type: application/json' \
      -d "{\"email\":\"approver-$a@pilot.test\",\"password\":\"apw-$a-only\"}" >/dev/null
    e2=$(code_of -b "$ck" -X POST "http://$bind_ip:$a_web/api/admin/workflows/$b_wf/signals/review" \
      -H 'content-type: application/json' -H "origin: http://$bind_ip:$a_web" -d '{"signal":"approve"}')
    rm -f "$ck"
    [ "$e2" = "404" ] || die "ARM E2 FAILED: review for B's item id through A's inbox returned HTTP $e2 (want 404)"
    echo "arm E2 ok: B's item id is a 404 through A's inbox (cross-tenant id unknown to A's stack)"

    # E1: Playwright as the test approver (browser login, own item listed,
    # foreign id invisible) — the spec lives in the web repo.
    ( cd "$web_repo" && E2E_PILOT_APPROVER=true \
        PLAYWRIGHT_BASE_URL="http://$bind_ip:$a_web" \
        PLAYWRIGHT_REUSE_SERVER=true PLAYWRIGHT_DISABLE_WEBSERVER=true \
        PILOT_APPROVER_EMAIL="approver-$a@pilot.test" PILOT_APPROVER_PASSWORD="apw-$a-only" \
        PILOT_APPROVER_ITEM_ID="$a_wf" PILOT_APPROVER_FOREIGN_ID="$b_wf" \
        npx playwright test e2e/pilot-approver.spec.ts --reporter=line ) \
      || die "ARM E1 FAILED: the Playwright approver spec failed (own items listed / foreign id invisible)"
    echo "arm E1 ok: the test approver sees A's item listed and B's id nowhere in A's inbox"

    # P1: the one-port share. Both pilots' data and API ports answer on
    # loopback only; on the bind IP exactly the two inbox ports answer.
    conn_ok() { timeout 2 bash -c "</dev/tcp/$1/$2" 2>/dev/null; }
    for spec in "$a:$a_pg:$a_rd:$a_api:$a_tg:$a_web" "$b:$b_pg:$b_rd:$b_api:$b_tg:$b_web"; do
      IFS=: read -r n pg rd api tg web <<<"$spec"
      for p in "$pg" "$rd" "$api" "$tg"; do
        conn_ok "$bind_ip" "$p" && die "ARM P1 FAILED: pilot $n port $p answers on $bind_ip"
      done
      conn_ok 127.0.0.1 "$pg" || die "ARM P1 FAILED: pilot $n postgres not even reachable on loopback (bring-up broken)"
      conn_ok "$bind_ip" "$web" || die "ARM P1 FAILED: pilot $n inbox port $web does not answer on $bind_ip"
    done
    echo "arm P1 ok: only the inbox port answers on $bind_ip; data and API ports stay loopback-only"
    rm -f "$WF_DIAG_A" "$WF_DIAG_B"
    echo "APPROVER ACCESS TEST PASSED"
    ;;
  *) die "unknown command: $cmd" ;;
esac
