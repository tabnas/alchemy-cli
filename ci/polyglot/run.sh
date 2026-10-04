#!/usr/bin/env bash
# TypeScript and Go gate. It builds each TypeScript sibling in dependency
# order, links it into the packages that take it, and runs this package's
# suite; then a temporary Go workspace over the sibling checkouts runs the
# Go suite, plain and with the tabnas_nodecell build tag. The siblings are
# the ones ci/rust/run.sh names, cloned next to this repository (the
# workflow takes each from the branch of this one's name when it has one).
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
FLEET_ROOT=$(cd "$ROOT/.." && pwd)

SIBLINGS=$(sed -n 's/^SIBLINGS="\(.*\)"$/\1/p' "$ROOT/ci/rust/run.sh")
# The engine and the grammars, then alchemy (its build compiles src only),
# then transduce and render, which build on alchemy's shared types.
TS_PACKAGES="parser json jsonic csv alchemy transduce render"

for package in $TS_PACKAGES; do
  if [[ ! -f "$FLEET_ROOT/$package/ts/package.json" ]]; then
    echo "no $package TypeScript checkout at $FLEET_ROOT/$package/ts" >&2
    exit 1
  fi
done

# Replace each registry copy npm installed with a link to the sibling.
link_siblings() {
  local package_dir=$1 sibling name target
  for sibling in $TS_PACKAGES; do
    [[ "$FLEET_ROOT/$sibling/ts" != "$package_dir" ]] || continue
    name=$(cd "$FLEET_ROOT/$sibling/ts" && node -p "require('./package.json').name")
    target="$package_dir/node_modules/$name"
    [[ -e "$target" || -L "$target" ]] || continue
    rm -rf "$target"
    mkdir -p "$(dirname "$target")"
    ln -s "$FLEET_ROOT/$sibling/ts" "$target"
  done
}

step=0
total=$(( $(wc -w <<<"$TS_PACKAGES") + 1 ))
for package in $TS_PACKAGES; do
  step=$((step + 1))
  package_dir="$FLEET_ROOT/$package/ts"
  echo "typescript: $step of $total ($((step * 100 / total))%) install and build $package"
  (cd "$package_dir" && npm install --ignore-scripts --no-save)
  link_siblings "$package_dir"
  (cd "$package_dir" && npm run build --if-present)
done

echo "typescript: $total of $total (100%) test alchemy-cli"
(cd "$ROOT/ts" && npm install --ignore-scripts --no-save)
link_siblings "$ROOT/ts"
(cd "$ROOT/ts" && npm test)

WORK_DIR=$(mktemp -d)
trap 'rm -rf "$WORK_DIR"' EXIT
(
  cd "$WORK_DIR"
  go work init
  for sibling in $SIBLINGS; do
    module="$FLEET_ROOT/$sibling/go"
    [[ -f "$module/go.mod" ]] || continue
    go work use "$module"
  done
  go work use "$ROOT/go"
)

echo "go: test alchemy-cli (33%)"
(cd "$ROOT/go" && GOWORK="$WORK_DIR/go.work" go test -count=1 ./...)
echo "go: test alchemy-cli with tabnas_nodecell (67%)"
(cd "$ROOT/go" && GOWORK="$WORK_DIR/go.work" go test -count=1 -tags tabnas_nodecell ./...)
echo "go: vet alchemy-cli (100%)"
(cd "$ROOT/go" && GOWORK="$WORK_DIR/go.work" go vet ./...)
echo "polyglot gate: green"
