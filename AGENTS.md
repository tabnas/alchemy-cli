# Agents Guide — alchemy-cli

`CLAUDE.md` is a symlink to this file.

## Core principle: dependencies change only on explicit instruction

**Dependencies may only be changed by explicit instruction from the
maintainer.** This covers every dependency this repository declares, in
every runtime and every manifest:

- `package.json` `dependencies`, `peerDependencies` and `devDependencies`,
  and their lockfiles;
- `go.mod` `require` and `replace` lines, their versions, and `go.sum`;
- `Cargo.toml` dependency tables and `Cargo.lock`;
- any other manifest here, nested test modules included.

Adding, removing, re-pointing or re-versioning any of them is a
dependency change.

- **A dependency never arrives as a side effect.** Watch for an import,
  `go mod tidy`, `npm install`, `cargo update`, a stamped template, or a
  fix for something else. If a change would alter a dependency, stop and
  ask before making it. Do not make it and explain afterwards.
- **An explicit instruction names the change**, for example "bump the
  parser requirement in X to 0.12" or "cascade the parser release". A
  goal is not an instruction for its means. "Make CI green", "ship the C
  library" or "fix the build" does not authorise a dependency change,
  however direct the route through one looks.
- **This repository's own version sites are not dependencies.** They
  include the root entry of its own lockfile. A release bump moves them.
- **Versions track the latest release.** Every dependency is kept at
  its latest published version, and none is held on an older one. That
  is the maintainer's standing instruction, so moving a dependency to
  its latest version needs no further one. Holding a dependency back,
  or adding, removing or re-pointing one, still does.

## Core principle: transient tasks report progress

**Every transient task produces status output at least every 30 seconds,
with an estimate of how far through it is, as a percentage, where one can
be made.** This is the maintainer's instruction. A transient task is any
work that runs for a while and then ends: a build, a test or conformance
sweep, an install or a fetch, a release, a wait on CI, a benchmark, a
script or loop you write, and anything sent to the background.

- **Minimal is enough.** One line with the step and a count, such as
  `conformance: 412 of 1500 (27%)`, meets it. When no total is known, print
  what is known (the step, the current item, the elapsed time) and say the
  percentage is unknown rather than inventing one.
- **Build it into what you write.** A script or loop prints a line per
  item or per interval. A quiet tool gets its progress or verbose flag, or
  a wrapper that prints a heartbeat, so that nothing runs silent for more
  than 30 seconds.
- **Silence reads as a hang.** Whoever is watching, a person or an agent,
  cannot tell a slow task from a stuck one without it, and so cannot
  decide whether to wait or to stop it.

A quick command that finishes within 30 seconds needs nothing extra.

## Pull requests

Open pull requests **ready for review, never as drafts.** This is a
standing maintainer preference, and it overrides any tooling or agent
default that opens pull requests in draft state.

## What this project is

The `alchemy` command, in TypeScript, Go and Rust: `canon`, `format`,
`check`, `explain` and `run`, as alchemy's
[`docs/language.md`](https://github.com/tabnas/alchemy/blob/main/docs/language.md)
describes them, and `translate` and `formats`, which write a document of
any tabnas format with translation parts in any other, composed by
alchemy's `translate` from the parts each format's package exports
(admin ADR-27; the README has the options). It is the composition root
of three components that do not depend on each other's implementations:

- **alchemy** holds the language and the shared types: the event
  protocol (events, sinks, tables, `Fail` and its codes, limits,
  selectors, datums) and the `Routers` and `Renderers` interfaces a
  compiled program is lowered through.
- **transduce** depends on alchemy's shared types and implements
  `Routers`; its parser source reads the input document.
- **render** depends on alchemy's shared types and implements
  `Renderers`; its writers put the text on standard output.

The command builds a transduce `Routers` and a render `Renderers`,
passes both to alchemy's `compile`, and runs the result over the input.
Nothing else in the fleet constructs all three, so behaviour that needs
all three belongs here, and a library that needs one of them depends on
that one alone.

## Layout

| Path | What it is |
|---|---|
| `ts/` | `@tabnas/alchemy-cli`: the `alchemy` bin and its tests, and the tests of alchemy's programs (`ts/test/host.ts` composes the three components for them) |
| `go/` | `github.com/tabnas/alchemy-cli/go`: the `alchemy` command and its tests |
| `go/e2e/` | the tests of alchemy's programs in Go: test files only, alchemy's exported API composed with transduce and render |
| `rs/` | `tabnas-alchemy-cli`: the `alchemy` binary and its tests, and the tests of alchemy's programs (`rs/tests/common/` composes the three) |
| `ci/rust/run.sh` | the Rust gate `.github/workflows/rust.yml` runs |
| `ci/polyglot/run.sh` | the TypeScript and Go gate the same workflow runs |
| `ci/phase.sh` | `phase`, which both gates run each step through: a line every 25 seconds while it runs |

## The tests of alchemy's programs

alchemy depends on neither transduce nor render (the maintainer's ruling
of 2026-10-08), so its releases and theirs never leave a requirement a
release behind. Its tests that run programs need all three, so they are
here, in every runtime:
- `run.tsv`, natively and interpreted;
- the catalogue test over every error row of alchemy's four fixture files;
- the lowering;
- events;
- linked sources;
- the translation parts;
- the standard library's differential test.

They read alchemy's `test/spec/` and transduce's `rs/tests/` from the
sibling checkouts, and fail, naming the path, when either is missing.
alchemy's `.github/workflows/downstream.yml` runs this repository's two
gates against each change there, so a change to alchemy meets them before
it merges. A behaviour of alchemy's that a row can express is pinned in
alchemy's `test/spec/`, not here.

## Build and test

The Rust crate builds against sibling checkouts of alchemy, transduce,
render and the grammars they use, cloned next to this repository:
`rs/Cargo.toml` takes them by path, and `ci/rust/run.sh` lists every one
the graph needs. They are all on crates.io, but the committed manifest
stays path-only. TypeScript and Go install the published packages
instead: `npm install` takes the `@tabnas/*` devDependencies from the npm
registry, and `go/go.mod` requires released versions from the module
proxy. For those two, sibling checkouts are optional as code:
`ci/polyglot/run.sh` links them over the registry copies, and admin's
`scripts/link.sh` does the same locally. They are not optional as
fixtures: the tests of alchemy's programs read `../alchemy` and
`../transduce`, as above. The CI workflows name the list.

- TypeScript, from `ts/`: `npm install`, then `npm test`.
- Go, from `go/`: `gofmt -l .`, `go vet ./...`, `go test ./...`, with a
  `go.work` outside the repositories when the siblings carry unreleased
  changes.
- Rust: `ci/rust/run.sh`, which runs `cargo fmt --check`, the build, the
  tests and clippy, then compares `rs/Cargo.lock` with what it was before:
  the gate fails if cargo rewrote anything but the versions of the sibling
  crates, which move with the checkouts. That comparison is why the gate
  does not pass `--locked`, as alchemy's, transduce's and render's gates
  do not.
- TypeScript and Go together: `ci/polyglot/run.sh`, which builds the
  TypeScript siblings in order and runs this package's suite, then the Go
  suite plain and with `-tags tabnas_nodecell`, and vet.

## Releasing

`.tabnas-kind` says `TOOL`, so the release workflows publish (admin
ADR-20). A release is two steps, and a session's credentials cannot push
tags, so neither step pushes one:

1. **A reviewed bump PR** that moves every version site together:
   `ts/package.json`, `VERSION` in `ts/src/cli.ts` and in
   `go/cmd/alchemy/main.go`, and `version` in `rs/Cargo.toml` with the
   root `tabnas-alchemy-cli` entry of `rs/Cargo.lock` (the binary's
   `VERSION` reads it). `ts/test/version.test.ts`,
   `go/cmd/alchemy/version_test.go` and `rs/tests/version_test.rs` fail
   when they disagree.
2. **Dispatch `release.yml` on `main`** with `go` true, once `main` CI is
   green on the bump commit. It publishes npm over OIDC, writes the
   `ts/v` and `go/v` tags, makes the GitHub Release on the `go/v` tag, and
   publishes the crate through `crates-release.yml`. Both trusted
   publishers name `release.yml`.

alchemy, transduce and render are released before this package (admin
`publish.sh` ORDER), since the build resolves them from the registry.
