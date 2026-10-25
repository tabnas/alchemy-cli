# tabnas-dmog (Rust)

The `tabnas-dmog` crate, binary `dmog`: runs programs in the
[alchemy](https://github.com/tabnas/alchemy) language over JSON
documents. It compiles every program with
[tabnas-transduce](https://github.com/tabnas/transduce)'s routers and
[tabnas-render](https://github.com/tabnas/render)'s renderers, which
alchemy's runtime takes by injection, and reads `run`'s input through
transduce's parser source with the tabnas JSON grammar, from a reader a
window at a time (the grammar declares `lex.window`). See the
repository [README](../README.md) and [AGENTS.md](../AGENTS.md).

```text
dmog canon FILE       print the program in canonical form
dmog format FILE      print the program in layout form
dmog check FILE       parse, desugar, resolve, check and build the plan; print nothing and exit 0
dmog explain FILE     print the plan report
dmog run [--render csv|json] [--max-output-bytes N] PROGRAM INPUT
                      run the program over the JSON document INPUT
```

Install it from crates.io with `cargo install tabnas-dmog`.

To develop it: alchemy, transduce, render and the JSON grammar are sibling checkouts
named by path in `Cargo.toml`, and so are the fixture runner and the
grammars the tests of alchemy's programs read; those tests also read
alchemy's `test/spec/` from its checkout. From this directory:
`cargo build`, `cargo test --all-targets` (`tests/cli_test.rs` runs the
built binary as a script runs it), and
`cargo clippy --all-targets --all-features -- -D warnings`.
