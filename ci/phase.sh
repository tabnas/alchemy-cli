# Sourced by ci/rust/run.sh and ci/polyglot/run.sh.
#
# `phase NAME COMMAND...` prints NAME, runs COMMAND, and prints a line
# every 25 seconds while it runs: AGENTS.md asks every transient task for
# a status line at least every 30 seconds, and the gates' tools go quiet
# for longer than that, cargo while one crate compiles, npm while it
# resolves, `go test` until its package finishes. COMMAND runs in a
# subshell with the caller's `set -e`, so a failing step inside a shell
# function still stops it, and its status is phase's.
phase() {
  local name=$1
  shift
  echo "$name"
  ( "$@" ) &
  local job=$! start=$SECONDS next=25
  while kill -0 "$job" 2>/dev/null; do
    sleep 1
    if (( SECONDS - start >= next )); then
      echo "$name: still running, $((SECONDS - start))s, percentage unknown"
      next=$((next + 25))
    fi
  done
  wait "$job"
}
