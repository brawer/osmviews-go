<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Security policy

## Supported versions

This module is pre-1.0. Security fixes are made on the most recent release
only; please upgrade to the latest version before reporting.

## Reporting a vulnerability

Please report suspected vulnerabilities privately, **not** as a public issue:

- Preferred: **[open a private report](https://github.com/brawer/osmviews-go/security/advisories/new)**
  via GitHub’s “Report a vulnerability” (Security tab).
- Or email **sascha@brawer.ch**.

Please include a description of the issue, the affected version, and a minimal
way to reproduce it. You can expect an initial response within about a week.

## Disclosure

Fixed vulnerabilities are published as GitHub Security Advisories for this
repository and submitted to the [Go vulnerability database](https://vuln.go.dev),
so users running `govulncheck` are notified automatically. A patched release is
tagged at the same time.

## Scope

This policy covers the `github.com/brawer/osmviews-go` module. The OSMViews
dataset and the pipeline that produces it live in a separate project,
[brawer/osmviews](https://github.com/brawer/osmviews).
