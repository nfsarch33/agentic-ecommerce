#!/usr/bin/env bash
# WordPress + WooCommerce fixture init for the MVP-1 loop. Idempotent: safe
# to run on every bring-up after `compose up` of the woocommerce profile.
#
# What it does (and why each step exists):
#   1. installs WordPress core if absent (the image ships files, not an
#      installed site);
#   2. sets the permalink structure to /%postname%/ and flushes rewrite
#      rules - with an empty structure the REST prefix is not routed and
#      /wp-json/... lands on the storefront page;
#   3. installs and activates WooCommerce (needed for the REST API);
#   4. writes a fixture-only must-use plugin that reports HTTPS for REST
#      requests (/wp-json/...) only - the store's REST authentication
#      engages Basic credentials only on an SSL request, and the fixture
#      serves plain HTTP on loopback; on a real store TLS does this job;
#   5. creates ONE read/write REST key owned by the admin user (the prior
#      fixture key is revoked first, so re-running never accumulates keys),
#      WITHOUT printing the secret: ck/cs are written to the env file given
#      by $1 (default .local/fixture-woo.env), mode 600;
#   6. seeds three published products unless any exist.
#
# Environment (defaults mirror docker-compose.dev.yml): WC_CLI_IMAGE,
# WC_DB_NAME, WC_DB_USER, WC_DB_PASSWORD, WC_HOST_PORT (storefront port),
# WOO_SITE_URL, WOO_ADMIN_PASSWORD; WOO_NET/WOO_VOL/WOO_DB_IP override the
# container discovery for non-compose deployments.
#
# Usage: scripts/fixtures/wordpress-fixture-init.sh [env-file]
# Requires: the wc-db and wordpress containers up (make wc-up), and podman.
set -euo pipefail
ENV_OUT="${1:-.local/fixture-woo.env}"

WC_CLI_IMAGE="${WC_CLI_IMAGE:-wordpress:cli-php8.3}"
DB_NAME="${WC_DB_NAME:-wordpress}"
DB_USER="${WC_DB_USER:-wordpress}"
DB_PASS="${WC_DB_PASSWORD:-wordpress}"
SITE_PORT="${WC_HOST_PORT:-8081}"
SITE="${WOO_SITE_URL:-http://127.0.0.1:${SITE_PORT}}"

# podman-compose sometimes creates without starting; the WordPress entrypoint
# that populates the volume only runs once the container starts, and wp-cli
# needs the files present. Ensure both are up and the storefront answers.
# compose cannot start the wordpress service under rootless podman (its
# depends_on health condition never fires without a systemd session), so the
# pair is force-started here and given a generous window: a first boot can
# spend over two minutes before answering at all.
for c in ec-wc-db ec-wordpress; do
  st=$(podman inspect "$c" --format '{{.State.Status}}' 2>/dev/null || echo missing)
  [ "$st" = "running" ] || podman start "$c" >/dev/null 2>&1 || true
done
i=0
until curl -s -o /dev/null --max-time 3 "$SITE/"; do
  i=$((i+1))
  if [ $((i % 10)) -eq 0 ]; then
    echo "  still waiting for $SITE (attempt $i)..." >&2
    for c in ec-wc-db ec-wordpress; do podman start "$c" >/dev/null 2>&1 || true; done
  fi
  [ $i -gt 80 ] && { echo "storefront never came up at $SITE" >&2; exit 1; }
  sleep 3
done
NET="${WOO_NET:-$(podman inspect ec-wc-db --format '{{range .NetworkSettings.Networks}}{{.NetworkID}}{{end}}')}"
VOL="${WOO_VOL:-$(podman inspect ec-wordpress --format '{{range .Mounts}}{{if eq .Destination "/var/www/html"}}{{.Name}}{{end}}{{end}}')}"
# The database address stays the compose service NAME (never a pinned IP:
# wc-down keeps the volume and the database's IP changes on every wc-up, so
# a literal pin breaks the next bring-up). The one-shot wp-cli container
# cannot resolve the compose alias, so the name is mapped to the database
# container's current address via --add-host on the one-shot only.
DB_IP="${WOO_DB_IP:-$(podman inspect ec-wc-db --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')}"
# ONE managed name everywhere: the one-shots and the store both resolve
# wc-db (WORDPRESS_DB_HOST=wc-db:3306); the name maps to the database
# container's current address per run.
WPC=(podman run --rm --network "$NET" --add-host "wc-db:$DB_IP"
  -v "$VOL":/var/www/html --user 33:33
  -e WORDPRESS_DB_HOST=wc-db:3306 -e WORDPRESS_DB_NAME="$DB_NAME"
  -e WORDPRESS_DB_USER="$DB_USER" -e WORDPRESS_DB_PASSWORD="$DB_PASS"
  "$WC_CLI_IMAGE" wp)

# The store container itself cannot resolve the compose service name on this
# podman network (no aardvark DNS; resolv.conf is the host's), and the
# config template is env-driven: WORDPRESS_DB_HOST=wc-db:3306. Map the name
# to the database container's CURRENT address in the STORE CONTAINER's
# /etc/hosts. The rewrite must keep the inode: /etc/hosts is a bind mount,
# so sed -i (rename over) fails with "Resource busy". grep -v to a scratch
# file + cat > back in place + one append = the managed line is replaced,
# never accumulated. The container is recreated on every wc-up, and this
# run re-points the entry whenever the database address moved.
podman exec ec-wordpress sh -c "grep -v ' wc-db\$' /etc/hosts > /tmp/h; cat /tmp/h > /etc/hosts; echo '$DB_IP wc-db' >> /etc/hosts"

# Undo any literal-IP DB_HOST a previous init wrote into the shared config:
# the env-driven template line is restored (the store container's hosts
# entry above is the resolver now).
"${WPC[@]}" config set DB_HOST "getenv_docker('WORDPRESS_DB_HOST', 'mysql')" --raw >/dev/null 2>&1 || true

echo "== 1/6 core =="
"${WPC[@]}" core is-installed 2>/dev/null || \
  "${WPC[@]}" core install --url="$SITE" --title="Fixture Store" \
    --admin_user=fixture --admin_password="${WOO_ADMIN_PASSWORD:-fixture-admin-1}" \
    --admin_email=fixture@example.invalid --skip-email >/dev/null
echo "core ok"

echo "== 2/6 permalinks =="
[ "$("${WPC[@]}" option get permalink_structure 2>/dev/null | tr -d "'\"" )" = "/%postname%/" ] || \
  "${WPC[@]}" rewrite structure '/%postname%/' --hard >/dev/null
echo "permalinks ok"

echo "== 3/6 woocommerce =="
"${WPC[@]}" plugin is-active woocommerce 2>/dev/null || \
  "${WPC[@]}" plugin install woocommerce --activate >/dev/null
echo "woocommerce ok"

echo "== 4/6 fixture HTTPS (REST scope only) =="
# Piped through the one-shot's stdin: no /tmp path, nothing to clean up.
cat <<'PHP' | podman run --rm -i -v "$VOL":/var/www/html --user 33:33 "$WC_CLI_IMAGE" \
  sh -c 'cat > /var/www/html/wp-content/mu-plugins/zz-fixture-https.php'
<?php
// FIXTURE ONLY: the loop serves plain HTTP on loopback; the store's REST
// authentication engages Basic credentials only on SSL requests. HTTPS is
// reported ONLY for /wp-json/ requests so the site URLs and admin keep
// their plain-HTTP scheme. On a real store TLS termination does this job;
// never ship this file.
if ( 0 === strpos( $_SERVER['REQUEST_URI'] ?? '', '/wp-json/' ) ) {
	$_SERVER['HTTPS'] = 'on';
}
PHP
echo "https fixture ok"

echo "== 5/6 rest key (revoked-then-created: exactly one after any runs) =="
# A failed insert fails the script: the guard checks the query result
# ($wpdb->query === false), not the echo.
CK_CS=$("${WPC[@]}" eval '
global $wpdb;
$t = $wpdb->prefix . "woocommerce_api_keys";
$wpdb->query($wpdb->prepare("DELETE FROM $t WHERE description = %s", "mvp1 fixture loop"));
$ck = "ck_" . wp_generate_password(20, false);
$cs = "cs_" . wp_generate_password(40, false);
$ok = $wpdb->query($wpdb->prepare(
  "INSERT INTO $t (user_id, description, permissions, consumer_key, consumer_secret, truncated_key)
   VALUES (1, %s, %s, %s, %s, %s)",
  "mvp1 fixture loop", "read_write", wc_api_hash($ck), $cs, substr($ck, 0, 7)));
if ( false === $ok ) { echo "KEYFAIL " . $wpdb->last_error; exit(1); }
echo $ck . " " . $cs;
' 2>/dev/null)
case "$CK_CS" in
  KEYFAIL*|"") echo "key creation failed: ${CK_CS:-<empty>}" >&2; exit 1 ;;
esac
CK=$(echo "$CK_CS" | awk '{print $1}')
CS=$(echo "$CK_CS" | awk '{print $2}')
mkdir -p "$(dirname "$ENV_OUT")"
umask 177
{
  echo "# Generated by scripts/fixtures/wordpress-fixture-init.sh - gitignored."
  echo "WOO_BASE_URL=$SITE"
  echo "WOO_CONSUMER_KEY=$CK"
  echo "WOO_CONSUMER_SECRET=$CS"
} > "$ENV_OUT"
echo "rest key written to $ENV_OUT (secret not printed)"

echo "== 6/6 seed products =="
N=$("${WPC[@]}" post list --post_type=product --format=count 2>/dev/null | tr -d '[:space:]')
if [ "${N:-0}" -lt 1 ]; then
  for i in 1 2 3; do
    "${WPC[@]}" post create --post_type=product --post_title="Fixture Product $i" \
      --post_status=publish --post_content="Fixture product $i for the MVP-1 loop." \
      --meta_input="{\"_sku\":\"FIX-$i\",\"_regular_price\":\"$((10*i)).50\",\"_price\":\"$((10*i)).50\"}" >/dev/null
  done
fi
echo "seed ok"
echo "FIXTURE-INIT-DONE"
