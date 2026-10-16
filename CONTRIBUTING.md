<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Contributing 👋

Thanks for looking! This is a small, focused package and contributions of
every size are welcome — a typo fix, a clearer doc sentence, a missing test
case, a bug report, or a new feature. No contribution is too small. 🙂

## Getting set up 🛠️

```sh
go test -race ./...
golangci-lint run ./...   # v2.14.0, configured in .golangci.yml
govulncheck ./...
```

CI requires `golangci-lint` and `govulncheck` to be clean, and runs the test
suite on both the latest Go release and the module’s minimum supported version
(the `go` directive in `go.mod`).

## Running the tests against the real dataset 🌍

Most tests build tiny synthetic GeoTIFFs and need nothing extra. The tests in
`online_test.go` run against the real ~594 MB dataset and skip themselves when
it is missing. To run them, fetch the file and point the tests at it:

```sh
path=$(curl -fsSL https://osmviews.brawer.ch/data/datapackage.json \
  | jq -er '.resources[] | select(.name == "osmviews") | .path')
curl -fL -o osmviews.tiff "https://osmviews.brawer.ch/data/$path"
OSMVIEWS_TIFF="$PWD/osmviews.tiff" go test -run Online -v .
```

`osmviews.tiff` in the repository root is picked up automatically (and is
git-ignored), so `go test ./...` runs them once the file is there.

## Benchmarks and fuzzing 📈

```sh
go test -run '^$' -bench .
go test -run '^$' -fuzz FuzzNew       # Ctrl-C to stop
```

The benchmarks report `ns/op` for a cache hit, a cache hit under parallel
load, and the re-decode path; they are informational, not pass/fail. A fuzzing
failure is written to `testdata/fuzz/`; commit it with the fix so it stays a
regression test.

## Commit and PR style 📝

We use [Conventional Commits](https://www.conventionalcommits.org) for pull
request titles (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `perf:`,
`build:`, `chore:`, `ci:`), and CI checks the PR title. PRs are squash-merged,
so the title becomes the commit message on `main`.

Please keep changes focused, add tests for behaviour changes, and credit any
sources you adapt code or data from.

## Reporting issues and asking questions 🤝

Open an issue on GitHub. For anything sensitive, or to report a Code of Conduct
concern, email Sascha (sascha@brawer.ch). By contributing you agree that your
work is licensed under the [MIT License](LICENSE), and to abide by our
[Code of Conduct](CODE_OF_CONDUCT.md).
