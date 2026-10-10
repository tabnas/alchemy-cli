#!/usr/bin/env bash
# Rust gate. Kept in one script so local and hosted validation cannot
# quietly drift apart: .github/workflows/rust.yml runs this file, and so can
# you. This is the full gate.
#
# alchemy, transduce, render and the JSON grammar are PATH DEPENDENCIES on
# sibling checkouts (rs/Cargo.toml: `tabnas-alchemy = { path =
# "../../alchemy/rs" }` and the like). Clone each repository named in
# SIBLINGS below next to this one before running.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
# shellcheck source=../phase.sh
source "$ROOT/ci/phase.sh"

# Every sibling any crate in the graph takes by path: alchemy, transduce
# and render, the JSON grammar, the engine, and csv and jsonic, which
# transduce's line sources take; then the other grammars `translate` reads
# and writes, which the tests of alchemy's programs read too (ini takes
# hoover); and this crate's own dev-dependency, the fixture runner. The
# tests also read alchemy's test/spec, transduce's rs/tests and every
# grammar's test/spec from the checkouts.
SIBLINGS="parser json jsonic csv alchemy transduce render support hoover ini json5 jsonc jsonl markdown toml xml yaml zon chess css expr feed proto semver"

# TypeScript- and Go-only additions needed by ci/polyglot/run.sh: none.
TS_SIBLINGS=""

for SIBLING in $SIBLINGS; do
  if [[ ! -f "$ROOT/../$SIBLING/rs/Cargo.toml" ]]; then
    echo "no $SIBLING checkout at $ROOT/../$SIBLING/rs" >&2
    echo "clone https://github.com/tabnas/$SIBLING as a sibling of $(basename "$ROOT")" >&2
    exit 1
  fi
done

cd "$ROOT/rs"

# Run through the MSRV toolchain when one is available; loud when it is
# not, because a newer toolchain accepts what the MSRV rejects.
MSRV=$(awk -F'"' '/^rust-version = /{print $2; exit}' Cargo.toml)
CARGO=(cargo)
if [[ -n "$MSRV" ]] && command -v rustup >/dev/null 2>&1; then
  # The INSTALLED toolchain's full name (`1.85.1-x86_64-...`), not the
  # `1.85` channel: `cargo +1.85` names a release channel, which rustup
  # would try to synchronize over the network even when 1.85.1 is already
  # installed, and an offline checkout would fail before cargo ran.
  TOOLCHAIN=$(rustup toolchain list 2>/dev/null | awk -v m="$MSRV" 'index($1, m) == 1 { print $1; exit }')
  if [[ -n "$TOOLCHAIN" ]]; then
    CARGO=(cargo "+$TOOLCHAIN")
  else
    echo "warning: MSRV $MSRV is not installed; running on $(rustc --version 2>/dev/null)" >&2
    echo "         install it with: rustup toolchain install $MSRV" >&2
  fi
elif [[ -n "$MSRV" ]]; then
  echo "warning: rustup is not installed; running on $(rustc --version 2>/dev/null)" >&2
  echo "         install rustup and toolchain $MSRV to validate the MSRV locally" >&2
fi

# The lock's entry for THIS crate must match the manifest, before any cargo
# command gets a chance to fix it up.
CRATE=$(awk -F'"' '/^name = /{print $2; exit}' Cargo.toml)
WANT=$(awk -F'"' '/^version = /{print $2; exit}' Cargo.toml)
HAVE=$(awk -v c="$CRATE" -F'"' '
  $0 == "name = \"" c "\"" { f = 1; next }
  f && /^version = / { print $2; exit }
' Cargo.lock)
if [[ "$WANT" != "$HAVE" ]]; then
  echo "Cargo.lock records $CRATE ${HAVE:-<missing>}, but Cargo.toml says $WANT" >&2
  echo "run a cargo command and commit the updated rs/Cargo.lock" >&2
  exit 1
fi

# The whole resolution is compared before and after, exempting only the
# recorded VERSION of each sibling path crate, which legitimately moves
# whenever the sibling checkouts do. Every tabnas crate in the graph is a
# sibling, so the exemption is by name prefix.
lock_without_sibling_versions() {
  awk '
    /^\[\[package\]\]$/            { sib = 0 }
    /^name = "tabnas-parser"$/            { sib = 1 }
    /^name = "tabnas-[a-z0-9]+"$/  { sib = 1 }
    sib && /^version = /           { print "version = \"<sibling>\""; next }
                                   { print }
  ' "$1"
}

LOCK_BEFORE=$(mktemp)
cp Cargo.lock "$LOCK_BEFORE"
trap 'if [ -f "$LOCK_BEFORE" ] && ! cmp -s "$LOCK_BEFORE" Cargo.lock; then cp "$LOCK_BEFORE" Cargo.lock; fi; rm -f "$LOCK_BEFORE"' EXIT

# Not `--locked`: the siblings resolve from checkouts of main, and their
# versions move under this lock. Not `--all` on fmt: that reaches into the
# sibling checkouts.
phase "gate: fmt" "${CARGO[@]}" fmt --check
phase "gate: build" "${CARGO[@]}" build --all-targets
phase "gate: test" "${CARGO[@]}" test --all-targets
# The cross product of every format's own fixture corpus (tests/translate_test.rs,
# ignored in a debug run): about 63,000 pairs, minutes in release.
phase "gate: the formats' fixtures, every format into every other (release)" \
  "${CARGO[@]}" test --release --test translate_test -- --ignored --nocapture
# No `test --doc`: the crate is a binary with no library target, which
# cargo refuses to doc-test.
phase "gate: clippy" "${CARGO[@]}" clippy --all-targets --all-features -- -D warnings

if ! diff -q <(lock_without_sibling_versions "$LOCK_BEFORE") \
             <(lock_without_sibling_versions Cargo.lock) >/dev/null; then
  echo "rs/Cargo.lock does not match rs/Cargo.toml -- cargo rewrote it:" >&2
  diff <(lock_without_sibling_versions "$LOCK_BEFORE") \
       <(lock_without_sibling_versions Cargo.lock) >&2 || true
  echo "run a cargo command and commit the updated rs/Cargo.lock" >&2
  cp "$LOCK_BEFORE" Cargo.lock
  exit 1
fi
if ! cmp -s "$LOCK_BEFORE" Cargo.lock; then
  cp "$LOCK_BEFORE" Cargo.lock
fi
echo "gate: green"
