# tabnas-alchemy-cli

The `alchemy` command: runs programs in the
[alchemy](https://github.com/tabnas/alchemy) language over JSON
documents, in TypeScript, Go and Rust.

alchemy is the language and the shared types: it compiles a program into
a plan and lowers the plan onto routers and renderers that its host
passes in. This repository is the host. It composes three components:

- [tabnas-alchemy](https://github.com/tabnas/alchemy): the language, and
  the event protocol every component shares;
- [tabnas-transduce](https://github.com/tabnas/transduce): the routers a
  plan's streams run through, and the parser source that turns the input
  document into events;
- [tabnas-render](https://github.com/tabnas/render): the renderers and
  writers that turn events into text.

A library that only needs one of them depends on that one alone; this
command is where all of them meet.

```text
alchemy canon FILE       print the program in canonical form
alchemy format FILE      print the program in layout form
alchemy check FILE       parse, desugar, resolve, check and build the plan; print nothing and exit 0
alchemy explain FILE     print the plan report
alchemy run [--render csv|json] [--no-native] [--max-output-bytes N] PROGRAM INPUT
                         run the program over the JSON document INPUT
```

`FILE`, `PROGRAM` and `INPUT` may be `-` for standard input (one of them
per run). A failure is the `Fail` as one JSON object on standard error,
and standard output carries nothing but the answer. The language
reference, [`docs/language.md`](https://github.com/tabnas/alchemy/blob/main/docs/language.md)
in alchemy, describes each command, the `run` options and the exit
statuses.

## Layout

| Path | What it is |
|---|---|
| `ts/` | `@tabnas/alchemy-cli`, the `alchemy` bin |
| `go/` | `github.com/tabnas/alchemy-cli/go`, the `alchemy` command |
| `rs/` | `tabnas-alchemy-cli`, the `alchemy` binary |

## Build and test

See [`AGENTS.md`](AGENTS.md). Each runtime builds against sibling
checkouts of alchemy, transduce, render and the grammars they use.

## License

MIT. See [`LICENSE`](LICENSE).
