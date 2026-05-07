# Security Policy

This repository is safe to publish only while it contains generic source,
tests, documentation, redacted examples, and placeholder configuration.
Do not commit live operational data.

Never commit:

- `.env` files or exported environment dumps
- WooCommerce consumer keys or consumer secrets
- MiniMax, OpenAI, GitHub, AWS, 1Password, JFrog, or other API tokens
- SSH keys, `.pem`, `.key`, `.crt`, or identity files
- Browser session profiles, cookies, or screenshots containing account data
- Private hostnames, fleet inventories, internal IPs, OCI IDs, or Tailscale node details
- Customer, candidate, proposal, or application data

## MiniMax policy

This backend must not call `api.minimaxi.com` directly from the MacBook.
MiniMax traffic runs through the fleet-side `minimax-openai-bridge`, with
key selection state managed through `runx minimax` and the approved
Tailscale/OCI nodes.

## Reporting

Report vulnerabilities privately through GitHub security advisories.
