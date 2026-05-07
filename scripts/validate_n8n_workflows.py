#!/usr/bin/env python3
"""Validate checked-in n8n workflow templates stay importable and credential-free."""

from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any


FORBIDDEN_SUBSTRINGS = (
    "hooks.slack.com/services/",
    "discord.com/api/webhooks/",
    "xoxb-",
    "xoxp-",
    "smtp://",
    "smtps://",
    "sendgrid.net",
    "mailgun.net",
)


def walk(value: Any, path: str = "$") -> list[tuple[str, Any]]:
    items: list[tuple[str, Any]] = [(path, value)]
    if isinstance(value, dict):
        for key, child in value.items():
            items.extend(walk(child, f"{path}.{key}"))
    elif isinstance(value, list):
        for index, child in enumerate(value):
            items.extend(walk(child, f"{path}[{index}]"))
    return items


def validate(path: Path) -> list[str]:
    errors: list[str] = []
    try:
        workflow = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        return [f"{path}: invalid JSON: {exc}"]

    if not isinstance(workflow, dict):
        return [f"{path}: workflow root must be a JSON object"]

    if not workflow.get("name"):
        errors.append(f"{path}: missing workflow name")
    if workflow.get("active") is not False:
        errors.append(f"{path}: templates must be inactive by default")
    if not isinstance(workflow.get("nodes"), list) or not workflow["nodes"]:
        errors.append(f"{path}: missing non-empty nodes array")
    if not isinstance(workflow.get("connections"), dict):
        errors.append(f"{path}: missing connections object")

    webhook_nodes = [
        node
        for node in workflow.get("nodes", [])
        if isinstance(node, dict) and node.get("type") == "n8n-nodes-base.webhook"
    ]
    if not webhook_nodes:
        errors.append(f"{path}: expected at least one Webhook trigger node")

    for node in workflow.get("nodes", []):
        if not isinstance(node, dict):
            errors.append(f"{path}: every node must be a JSON object")
            continue
        for field in ("id", "name", "type", "typeVersion", "position"):
            if field not in node:
                errors.append(f"{path}: node {node.get('name', '<unnamed>')} missing {field}")

    for item_path, value in walk(workflow):
        if item_path.endswith(".credentials"):
            errors.append(f"{path}: credentials references are not allowed at {item_path}")
        if isinstance(value, str):
            lowered = value.lower()
            for forbidden in FORBIDDEN_SUBSTRINGS:
                if forbidden in lowered:
                    errors.append(f"{path}: forbidden live credential/webhook marker at {item_path}")
            if ("http://" in lowered or "https://" in lowered) and "$env." not in value:
                errors.append(f"{path}: hard-coded URL must be replaced by an env placeholder at {item_path}")

    return errors


def main() -> int:
    root = Path(sys.argv[1]) if len(sys.argv) > 1 else Path("deploy/n8n/workflows")
    files = sorted(root.glob("*.json"))
    if not files:
        print(f"no workflow JSON files found under {root}", file=sys.stderr)
        return 1

    errors: list[str] = []
    for file_path in files:
        errors.extend(validate(file_path))

    if errors:
        for error in errors:
            print(error, file=sys.stderr)
        return 1

    print(f"validated {len(files)} n8n workflow template(s)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
