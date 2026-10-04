#!/usr/bin/env bash
# Seed the enrichment corpus into the fixture store: the ten planted
# grounding-eval products (ENR-P-001..010, from the committed testdata) plus
# deterministic filler products to $1 (default 55) so the overnight batch has
# >= 50 drafts to make. Fixture SETUP, not the publish path under test: the
# same direct-REST seeding wordpress-fixture-init.sh uses; the content
# agent's own writes go through the audited proxy and an approval row.
# Idempotent: existing SKUs are skipped (listed, not re-created).
set -euo pipefail
cd "$(dirname "$0")/../.."   # repo root
ENV="${WOO_ENV_FILE:-.local/fixture-woo.env}"
[ -f "$ENV" ] || { echo "FAIL: no fixture env at $ENV (run scripts/fixtures/wordpress-fixture-init.sh)" >&2; exit 1; }
# shellcheck disable=SC1090
set -a; . "$ENV"; set +a
TARGET="${1:-55}"
python3 - "$TARGET" <<'PY'
import json, os, sys, urllib.request, base64

target = int(sys.argv[1])
base = os.environ["WOO_BASE_URL"].rstrip("/")
auth = base64.b64encode(
    (os.environ["WOO_CONSUMER_KEY"] + ":" + os.environ["WOO_CONSUMER_SECRET"]).encode()
).decode()

def api(method, path, payload=None):
    data = json.dumps(payload).encode() if payload is not None else None
    req = urllib.request.Request(base + path, data=data, method=method)
    req.add_header("Authorization", "Basic " + auth)
    req.add_header("Content-Type", "application/json")
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.loads(r.read() or b"null")

existing = {p["sku"] for p in api("GET", "/wp-json/wc/v3/products?per_page=100&status=any")}

planted = json.load(open("internal/agent/enrichment/testdata/planted-products.json"))
made = 0
for p in planted["products"]:
    if p["sku"] in existing:
        continue
    attrs = [
        {"name": k, "options": v, "visible": True, "variation": False}
        for k, v in p["attributes"].items()
    ]
    api("POST", "/wp-json/wc/v3/products", {
        "name": p["name"], "sku": p["sku"], "regular_price": p["price"],
        "type": "simple", "status": "publish",
        "categories": [{"name": c} for c in p["categories"]],
        "tags": [{"name": t} for t in p["tags"]],
        "attributes": attrs,
    })
    made += 1
print(f"planted: {made} created, {len(planted['products']) - made} already present")

cats = ["Home", "Kitchen", "Office", "Apparel", "Outdoors"]
mats = ["Bamboo", "Ceramic", "Linen", "Stainless steel", "Walnut", "Cotton"]
i = 0
made = 0
while len([s for s in existing if s.startswith("ENR-F-")]) + made < target - len(planted["products"]):
    i += 1
    sku = f"ENR-F-{i:03d}"
    if sku in existing:
        continue
    mat = mats[i % len(mats)]
    api("POST", "/wp-json/wc/v3/products", {
        "name": f"Fixture Enrichment Item {i:03d}",
        "sku": sku, "regular_price": f"{20 + (i * 7) % 80}.00",
        "type": "simple", "status": "publish",
        "categories": [{"name": cats[i % len(cats)]}],
        "attributes": [{"name": "Material", "options": [mat], "visible": True, "variation": False}],
    })
    made += 1
print(f"filler: {made} created")
PY
echo "ENRICH-SEED-DONE"
