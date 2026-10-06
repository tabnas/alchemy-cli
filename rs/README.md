# tabnas-alchemy-cli (Rust)

The `tabnas-alchemy-cli` crate, binary `alchemy`: runs programs in the
[alchemy](https://github.com/tabnas/alchemy) language over JSON
documents. It compiles every program with
[tabnas-transduce](https://github.com/tabnas/transduce)'s routers and
[tabnas-render](https://github.com/tabnas/render)'s renderers, which
alchemy's runtime takes by injection, and reads `run`'s input through
transduce's parser source with the tabnas JSON grammar. See the
repository [README](../README.md) and [AGENTS.md](../AGENTS.md).

```text
alchemy canon FILE       print the program in canonical form
alchemy format FILE      print the program in layout form
alchemy check FILE       parse, desugar, resolve, check and build the plan; print nothing and exit 0
alchemy explain FILE     print the plan report
alchemy run [--render csv|json] [--no-native] [--max-output-bytes N] PROGRAM INPUT
                         run the program over the JSON document INPUT
```

Install it from crates.io with `cargo install tabnas-alchemy-cli`.

To develop it: alchemy, transduce, render and the JSON grammar are sibling checkouts
named by path in `Cargo.toml`. From this directory: `cargo build`,
`cargo test --all-targets` (`tests/cli_test.rs` runs the built binary as
a script runs it), and
`cargo clippy --all-targets --all-features -- -D warnings`.
