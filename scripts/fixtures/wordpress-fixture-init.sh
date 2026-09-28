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
#   4. writes a fixture-only must-use plugin that reports HTTPS for every
#      request - the store's REST authentication only engages Basic
#      credentials on an SSL request, and the fixture serves plain HTTP on
#      loopback; on a real store TLS does this job instead;
#   5. creates a read/write REST key owned by the admin user, WITHOUT
#      printing the secret: ck/cs are written to the env file given by $1
#      (default .local/fixture-woo.env), mode 600;
#   6. seeds three published products unless any exist.
#
# Usage: scripts/fixtures/wordpress-fixture-init.sh [env-file]
# Requires: the wc-db and wordpress containers up (make wc-up), and podman.
set -euo pipefail
ENV_OUT="${1:-.local/fixture-woo.env}"

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
SITE="${WOO_SITE_URL:-http://127.0.0.1:8081}"
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
DB_IP="${WOO_DB_IP:-$(podman inspect ec-wc-db --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')}"
WPC="podman run --rm --network $NET -v $VOL:/var/www/html --user 33:33 \
  -e WORDPRESS_DB_HOST=${DB_IP}:3306 -e WORDPRESS_DB_NAME=wordpress \
  -e WORDPRESS_DB_USER=wordpress -e WORDPRESS_DB_PASSWORD=wordpress \
  wordpress:cli-php8.3 wp"

echo "== 1/6 core =="
$WPC core is-installed 2>/dev/null || \
  $WPC core install --url="${WOO_SITE_URL:-http://127.0.0.1:8081}" --title="Fixture Store" \
    --admin_user=fixture --admin_password="${WOO_ADMIN_PASSWORD:-fixture-admin-1}" \
    --admin_email=fixture@example.invalid --skip-email >/dev/null
echo "core ok"

# Pin the DB host in wp-config to the database container's address: the
# WordPress container resolves the compose service name, but one-shot wp-cli
# containers on the same network do not reliably resolve it, and the config
# is shared. Idempotent: wp config set overwrites.
$WPC config set DB_HOST "${DB_IP}:3306" --quiet 2>/dev/null

echo "== 2/6 permalinks =="
[ "$($WPC option get permalink_structure 2>/dev/null | tr -d "'\"" )" = "/%postname%/" ] || \
  $WPC rewrite structure '/%postname%/' --hard >/dev/null
echo "permalinks ok"

echo "== 3/6 woocommerce =="
$WPC plugin is-active woocommerce 2>/dev/null || \
  $WPC plugin install woocommerce --activate >/dev/null
echo "woocommerce ok"

echo "== 4/6 fixture HTTPS =="
cat > /tmp/zz-fixture-https.php <<'PHP'
<?php
// FIXTURE ONLY: the loop serves plain HTTP on loopback; the store's REST
// authentication engages Basic credentials only on SSL requests. This
// reports HTTPS for every request so credentials are honoured. On a real
// store TLS termination does this job; never ship this file.
$_SERVER['HTTPS'] = 'on';
PHP
# Written through a one-shot with the wpdata volume mounted: podman cp
# cannot write into the volume path directly.
podman run --rm -v "$VOL":/var/www/html --user 33:33   -v /tmp/zz-fixture-https.php:/tmp/in.php:ro,Z   wordpress:cli-php8.3 sh -c 'mkdir -p /var/www/html/wp-content/mu-plugins && cp /tmp/in.php /var/www/html/wp-content/mu-plugins/zz-fixture-https.php'
rm -f /tmp/zz-fixture-https.php
echo "https fixture ok"

echo "== 5/6 rest key =="
CK_CS=$($WPC eval '
global $wpdb;
$t = $wpdb->prefix . "woocommerce_api_keys";
$ck = "ck_" . wp_generate_password(20, false);
$cs = "cs_" . wp_generate_password(40, false);
$wpdb->query($wpdb->prepare(
  "INSERT INTO $t (user_id, description, permissions, consumer_key, consumer_secret, truncated_key)
   VALUES (1, %s, %s, %s, %s, %s)",
  "mvp1 fixture loop", "read_write", wc_api_hash($ck), $cs, substr($ck, 0, 7)));
echo $ck . " " . $cs;
' 2>/dev/null)
CK=$(echo "$CK_CS" | awk '{print $1}')
CS=$(echo "$CK_CS" | awk '{print $2}')
[ -n "$CK" ] && [ -n "$CS" ] || { echo "key creation failed" >&2; exit 1; }
mkdir -p "$(dirname "$ENV_OUT")"
umask 177
cat > "$ENV_OUT" <<ENVEOF
# Generated by scripts/fixtures/wordpress-fixture-init.sh - gitignored.
WOO_BASE_URL=${WOO_SITE_URL:-http://127.0.0.1:8081}
WOO_CONSUMER_KEY=$CK
WOO_CONSUMER_SECRET=$CS
ENVEOF
echo "rest key written to $ENV_OUT (secret not printed)"

echo "== 6/6 seed products =="
N=$($WPC post list --post_type=product --format=count 2>/dev/null | tr -d '[:space:]')
if [ "${N:-0}" -lt 1 ]; then
  for i in 1 2 3; do
    $WPC post create --post_type=product --post_title="Fixture Product $i" \
      --post_status=publish --post_content="Fixture product $i for the MVP-1 loop." \
      --meta_input="{\"_sku\":\"FIX-$i\",\"_regular_price\":\"$((10*i)).50\",\"_price\":\"$((10*i)).50\"}" >/dev/null
  done
fi
echo "seed ok"
echo "FIXTURE-INIT-DONE"
