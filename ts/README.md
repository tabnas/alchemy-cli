# @tabnas/alchemy-cli

The `alchemy` command for [alchemy](https://github.com/tabnas/alchemy), the
small transformation language for streaming structured data through the
tabnas parser and transducer stack.

```sh
npm install -g @tabnas/alchemy-cli
```

```
alchemy canon FILE       print the program in canonical form
alchemy format FILE      print the program in layout form
alchemy check FILE       parse, desugar, resolve, check and build the plan
alchemy explain FILE     print the plan report
alchemy run [--render csv|json] [--no-native] [--max-output-bytes N] PROGRAM INPUT
                         run the program over the JSON document INPUT
alchemy translate --from FORMAT --to FORMAT [--path PATH] [--key KEY]
                  [--with PROGRAM] [--max-output-bytes N] INPUT
                         write INPUT, read as FORMAT, in another format
alchemy formats          print the formats translate reads and writes, as JSON
```

A `FILE`, `PROGRAM` or `INPUT` may be `-` for standard input. A failure is
one JSON object on standard error, and the exit status follows its code;
`src/cli.ts` documents both.

The command composes `@tabnas/alchemy` with the stages it compiles onto:
`@tabnas/transduce`'s `routers` and `@tabnas/render`'s `renderers`, passed
to alchemy's `compile`, and `run` reads `INPUT` with `@tabnas/json`.
`translate` reads and writes the formats of the grammar packages it
carries (`@tabnas/chess`, `css`, `csv`, `expr`, `feed`, `ini`, `json`,
`json5`, `jsonc`, `jsonic`, `jsonl`, `markdown`, `proto`, `semver`,
`toml`, `xml`, `yaml` and `zon`), composing each pair's route
from the translation parts the packages export with alchemy's `translate`;
`src/translate.ts` documents it.

This package includes its TypeScript sources under `src/` alongside the
compiled JavaScript and declarations under `dist/`.
