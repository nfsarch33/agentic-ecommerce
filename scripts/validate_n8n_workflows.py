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


def load_workflow(path: Path) -> tuple[dict[str, Any] | None, list[str]]:
    try:
        workflow = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        return None, [f"{path}: invalid JSON: {exc}"]

    if not isinstance(workflow, dict):
        return None, [f"{path}: workflow root must be a JSON object"]
    return workflow, []


def validate_root(path: Path, workflow: dict[str, Any]) -> list[str]:
    errors: list[str] = []
    if not workflow.get("name"):
        errors.append(f"{path}: missing workflow name")
    if workflow.get("active") is not False:
        errors.append(f"{path}: templates must be inactive by default")
    if not isinstance(workflow.get("nodes"), list) or not workflow["nodes"]:
        errors.append(f"{path}: missing non-empty nodes array")
    if not isinstance(workflow.get("connections"), dict):
        errors.append(f"{path}: missing connections object")
    return errors


def node_name(node: dict[str, Any]) -> str:
    return str(node.get("name", "<unnamed>"))


def validate_webhook_node(path: Path, node: dict[str, Any]) -> list[str]:
    errors: list[str] = []
    parameters = node.get("parameters", {})
    if not isinstance(parameters, dict):
        return [f"{path}: webhook node {node_name(node)} parameters must be an object"]
    if parameters.get("httpMethod") != "POST":
        errors.append(f"{path}: webhook node {node_name(node)} must use POST")
    if not parameters.get("path"):
        errors.append(f"{path}: webhook node {node_name(node)} missing path")
    if not node.get("webhookId"):
        errors.append(f"{path}: webhook node {node_name(node)} missing webhookId")
    if parameters.get("responseMode") != "responseNode":
        errors.append(f"{path}: webhook node {node_name(node)} must acknowledge through responseNode")
    return errors


def validate_http_request_node(path: Path, node: dict[str, Any]) -> list[str]:
    parameters = node.get("parameters", {})
    url = parameters.get("url") if isinstance(parameters, dict) else None
    if isinstance(url, str) and url.startswith("={{$env.") and url.endswith("}}"):
        return []
    return [f"{path}: HTTP Request node {node_name(node)} must use an env placeholder URL"]


def validate_nodes(path: Path, nodes: list[Any]) -> tuple[set[str], list[dict[str, Any]], list[str]]:
    errors: list[str] = []
    node_names: set[str] = set()
    for node in nodes:
        if not isinstance(node, dict):
            errors.append(f"{path}: every node must be a JSON object")
            continue
        for field in ("id", "name", "type", "typeVersion", "position"):
            if field not in node:
                errors.append(f"{path}: node {node.get('name', '<unnamed>')} missing {field}")
        if isinstance(node.get("name"), str):
            node_names.add(node["name"])
        if node.get("type") == "n8n-nodes-base.webhook":
            errors.extend(validate_webhook_node(path, node))
        if node.get("type") == "n8n-nodes-base.httpRequest":
            errors.extend(validate_http_request_node(path, node))

    webhook_nodes = [node for node in nodes if isinstance(node, dict) and node.get("type") == "n8n-nodes-base.webhook"]
    if not webhook_nodes:
        errors.append(f"{path}: expected at least one Webhook trigger node")
    if not any(isinstance(node, dict) and node.get("type") == "n8n-nodes-base.respondToWebhook" for node in nodes):
        errors.append(f"{path}: expected a Respond to Webhook node for synchronous acknowledgement")
    return node_names, webhook_nodes, errors


def validate_connection_targets(path: Path, source: str, channels: Any, node_names: set[str]) -> list[str]:
    if not isinstance(channels, dict):
        return [f"{path}: connection source {source!r} must map output channels"]

    errors: list[str] = []
    for channel_name, channel_groups in channels.items():
        if not isinstance(channel_groups, list):
            errors.append(f"{path}: connection {source!r}.{channel_name} must be a list")
            continue
        for group in channel_groups:
            if not isinstance(group, list):
                errors.append(f"{path}: connection {source!r}.{channel_name} entries must be lists")
                continue
            for target in group:
                if not isinstance(target, dict) or target.get("node") not in node_names:
                    errors.append(f"{path}: connection {source!r}.{channel_name} has unknown target {target!r}")
    return errors


def validate_connections(
    path: Path,
    connections: dict[str, Any],
    node_names: set[str],
    webhook_nodes: list[dict[str, Any]],
) -> list[str]:
    errors: list[str] = []
    connected_sources = set(connections)
    for source, channels in connections.items():
        if source not in node_names:
            errors.append(f"{path}: connection source {source!r} does not match a node name")
        errors.extend(validate_connection_targets(path, source, channels, node_names))
    for webhook_node in webhook_nodes:
        name = webhook_node.get("name")
        if isinstance(name, str) and name not in connected_sources:
            errors.append(f"{path}: webhook node {name!r} is not connected to the workflow")
    return errors


def validate_secret_free(path: Path, workflow: dict[str, Any]) -> list[str]:
    errors: list[str] = []
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


def validate(path: Path) -> list[str]:
    workflow, errors = load_workflow(path)
    if workflow is None:
        return errors

    errors.extend(validate_root(path, workflow))
    nodes = workflow.get("nodes", [])
    node_names, webhook_nodes, node_errors = validate_nodes(path, nodes if isinstance(nodes, list) else [])
    errors.extend(node_errors)
    connections = workflow.get("connections", {})
    if isinstance(connections, dict):
        errors.extend(validate_connections(path, connections, node_names, webhook_nodes))
    errors.extend(validate_secret_free(path, workflow))

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
