<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Releasing

Releases are automated with
[release-please](https://github.com/googleapis/release-please). A Go module
has no package registry to upload to: a release is a `vX.Y.Z` git tag, which
the [Go module proxy](https://proxy.golang.org) fetches the first time someone
asks for it and the [checksum database](https://sum.golang.org) records, so
every later download of that version is verified by `go`.

## How it works

1. Every pull request has a [Conventional Commits](https://www.conventionalcommits.org)
   title (`feat:`, `fix:`, `docs:`, `perf:`, `refactor:`, `test:`, `build:`,
   `ci:`, `chore:`). PRs are squash-merged, so the title becomes the commit on
   `main`. A CI check enforces this.
2. `.github/workflows/release-please.yml` watches `main` and keeps a single
   open **“chore(main): release x.y.z”** pull request. It updates `CHANGELOG.md`
   from the commit history, the `version` in `CITATION.cff`, and
   `.release-please-manifest.json`. It runs as the account-wide
   **`brawer-release-bot`** GitHub App (see the
   [Rust client's RELEASING.md](https://github.com/brawer/osmviews-rs/blob/main/RELEASING.md#one-time-setup)
   for how it is set up), because a pull request opened with the built-in
   `GITHUB_TOKEN` would not trigger CI.
3. Review that PR and squash-merge it when you want to cut the release.
   release-please then pushes the `vX.Y.Z` tag and creates the GitHub release.
4. Optionally, warm the proxy so that pkg.go.dev picks the release up at once:
   `GOPROXY=https://proxy.golang.org go list -m github.com/brawer/osmviews-go@vX.Y.Z`.

## Choosing the version number

release-please picks the bump from the commit types since the last release:
`fix:` → patch, `feat:` → minor, and a `!` after the type or a
`BREAKING CHANGE:` footer → a breaking bump. While the module is `v0.x`,
`bump-minor-pre-major` maps a breaking change to a **minor** bump (`v0.1.z` →
`v0.2.0`) and everything else to a **patch** bump.

Only `feat:`, `fix:` and `perf:` commits cut a release (and appear in
`CHANGELOG.md`). `docs:`, `refactor:`, `test:`, `build:`, `ci:` and `chore:`
(including Dependabot’s `chore(deps):`) are silent — they ride along with the
next real release. Use `Release-As:` in a commit body if you need to ship one of
those alone.

The public API is every exported identifier: `Raster` and its methods, `Open`,
`New`, `Option` and `WithCacheTiles`, `Metrics` and its fields and methods,
`ErrFormat`, `DataPackageURL`, `DefaultCacheTiles`, and the documented
behaviour of `Rank`. Adding a field to `Metrics` is backwards-compatible in Go
(callers construct it only by receiving it), as is adding a new `Option`.

**v1 and beyond.** Go's
[import compatibility rule](https://go.dev/doc/modules/release-workflow#breaking)
means that after `v1.0.0`, a breaking change needs a new major version whose
module path ends in `/v2` (`module github.com/brawer/osmviews-go/v2` in
`go.mod`, and in every import). `gorelease` (from `golang.org/x/exp`) reports
whether a change is compatible with the previous release.

## Verifying a release

```sh
# The GitHub release's own attestation (tag, commit) — once immutable releases
# are enabled on the repo:
gh release verify vX.Y.Z --repo brawer/osmviews-go

# What the checksum database recorded for the release:
curl -fsS https://sum.golang.org/lookup/github.com/brawer/osmviews-go@vX.Y.Z
```
