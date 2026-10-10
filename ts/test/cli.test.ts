/* Copyright (c) 2026 tabnas, MIT License */

// The `alchemy` command (alchemy's rs/tests/cli_test.rs), run the way a
// script runs it: bin/alchemy in a child process, the five commands of
// programs and the two of formats (`translate` and `formats`), standard
// input as `-`, the statuses, and that nothing but the answer reaches
// standard output.

import { describe, it } from 'node:test'
import assert from 'node:assert'
import { SpawnSyncReturns, spawn, spawnSync } from 'node:child_process'
import { accessSync, constants, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { source } from '@tabnas/alchemy'

import { EXPECTED_CSV, PROGRAM, RECORDS } from './common'

// The command, as the package's `bin` names it.
const BIN = join(__dirname, '..', 'bin', 'alchemy')

const TMP = mkdtempSync(join(tmpdir(), 'alchemy-cli-'))

type Output = { status: number | null; stdout: string; stderr: string }

function alchemy(args: string[], stdin?: string | Buffer, cwd?: string): Output {
  const out: SpawnSyncReturns<Buffer> = spawnSync(process.execPath, [BIN, ...args], {
    input: stdin,
    stdio: [undefined === stdin ? 'ignore' : 'pipe', 'pipe', 'pipe'],
    cwd,
    maxBuffer: 64 * 1024 * 1024,
  })
  if (out.error) throw out.error
  return { status: out.status, stdout: out.stdout.toString('utf8'), stderr: out.stderr.toString('utf8') }
}

// The failure: one JSON object on standard error.
function failJson(output: Output): any {
  try {
    return JSON.parse(output.stderr.trim())
  } catch (_e) {
    assert.fail(`one JSON object on stderr, not ${JSON.stringify(output.stderr)}`)
  }
}

function tempFile(name: string, text: string): string {
  const path = join(TMP, name)
  writeFileSync(path, text)
  return path
}

const EXPORT = 'def export [input]\n  pipe input\n    table-from-json api-binding\n    csv csv-options\n'

describe('cli', () => {
  it('the bin is an executable script', () => {
    accessSync(BIN, constants.X_OK)
    // Its interpreter line names node, which POSIX runs and npm reads to
    // write the shim it installs for the command on Windows.
    assert.equal(readFileSync(BIN, 'utf8').split(/\r?\n/)[0], '#!/usr/bin/env node')
    // Run as a script runs it: by its own interpreter line, and on Windows,
    // which runs executables only and reads no interpreter line, by node,
    // as that shim runs it.
    const out =
      'win32' === process.platform
        ? spawnSync(process.execPath, [BIN, 'canon', '-'], { input: 'a b' })
        : spawnSync(BIN, ['canon', '-'], { input: 'a b' })
    if (out.error) throw out.error
    assert.equal(out.status, 0, out.stderr.toString())
    assert.equal(out.stdout.toString(), '(a b)\n')
  })

  it('canon prints the canonical form', () => {
    const file = tempFile('canon.alc', EXPORT)
    const out = alchemy(['canon', file])
    assert.equal(out.status, 0, out.stderr)
    assert.equal(out.stdout, '(def export [input] (pipe input (table-from-json api-binding) (csv csv-options)))\n')
    assert.equal(out.stderr, '')
  })

  it('format prints the layout form and reads standard input', () => {
    const out = alchemy(['format', '-'], '(def export [input] (pipe input (table-from-json api-binding) (csv csv-options)))')
    assert.equal(out.status, 0, out.stderr)
    assert.equal(out.stdout, EXPORT)
  })

  it('check is silent on a program that checks', () => {
    const out = alchemy(['check', '-'], PROGRAM)
    assert.equal(out.status, 0, out.stderr)
    assert.equal(out.stdout, '')
    assert.equal(out.stderr, '')
  })

  // A library definition over a selection's items, which are typed
  // `Value`: `check` passes it, and `run` checks each one and prints the
  // result.
  it('a definition over selected items checks and runs', () => {
    for (const [name, program, input, want] of [
      [
        'cols.alc',
        'def cols [input] (map public-column (select (path "cols" each-index) input))\ndef export [input] (join "," (map (fn [c] (get :label c)) (cols input)))\n',
        '{"cols":[{"label":"a","source":1},{"label":"b"}]}',
        'a,b',
      ],
      [
        'rows.alc',
        'def export [input]\n  concat-map (partial csv-row csv-options) (select (path "rows" each-index) input)\n',
        '{"rows":[[1,"x"],[2,"y"]]}',
        '"1","x"\r\n"2","y"\r\n',
      ],
    ]) {
      const path = tempFile(name, program)
      let out = alchemy(['check', path])
      assert.equal(out.status, 0, `${name}: ${out.stderr}`)
      out = alchemy(['run', path, '-'], input)
      assert.equal(out.status, 0, `${name}: ${out.stderr}`)
      assert.equal(out.stdout, want, name)
    }
  })

  it('check reports a reader failure as JSON on stderr with status 2', () => {
    const out = alchemy(['check', '-'], 'pipe x\n  f\n  []\n')
    assert.equal(out.status, 2)
    assert.equal(out.stdout, '')
    const fail = failJson(out)
    assert.equal(fail.code, 'DSL_PARSE_ERROR')
    assert.ok(fail.message.startsWith('empty_step: '), JSON.stringify(fail))
    assert.equal(fail.row, 3)
    assert.equal(fail.col, 3)
    assert.equal(fail.output, 'none')
  })

  // `check` reaches the resolver and the checker: an unknown name, a reused
  // stream and a dynamic function each fail with their code, at the
  // position they name, with status 2.
  it('check reports the resolver and checker codes', () => {
    let out = alchemy(['check', '-'], EXPORT)
    assert.equal(out.status, 2)
    let fail = failJson(out)
    assert.equal(fail.code, 'DSL_TYPE_ERROR')
    assert.ok(fail.message.startsWith('unknown_name: api-binding'), JSON.stringify(fail))
    assert.deepStrictEqual([fail.row, fail.col], [3, 21])
    out = alchemy(['check', '-'], 'def export [input] (concat (json input) (json input))')
    assert.equal(out.status, 2)
    fail = failJson(out)
    assert.equal(fail.code, 'STREAM_REUSED')
    assert.ok(fail.message.startsWith('reused: '), JSON.stringify(fail))
    out = alchemy(['check', '-'], 'def a [x] (b x)\ndef b [x] (a x)\ndef export [input] (a input)')
    assert.equal(out.status, 2)
    assert.equal(failJson(out).code, 'STREAMABILITY_UNKNOWN')
  })

  it('explain prints the plan report', () => {
    let out = alchemy(['explain', '-'], PROGRAM)
    assert.equal(out.status, 0, out.stderr)
    const text = out.stdout
    assert.ok(text.startsWith('export: api-table → csv\n\nSource reads:          1\n'), text)
    assert.ok(text.includes('Protocol:              JsonEvents/1 → TableRows/1 → Text\n'), text)
    assert.ok(
      text.includes('Row capture:           one .response.payload.deep.records[*], capped at max_record_bytes\n'),
      text,
    )
    assert.ok(text.endsWith('A later error can occur after earlier output has been written.\n'), text)
    assert.equal(out.stderr, '')
    out = alchemy(['explain', '-'], 'def x 1')
    assert.equal(out.status, 2)
    assert.ok(failJson(out).message.startsWith('no_export: '), out.stderr)
  })

  it('run prints the worked example csv both ways', () => {
    const program = tempFile('export.alc', PROGRAM)
    const input = tempFile('records.json', RECORDS)
    for (const args of [
      ['run', program, input],
      ['run', '--no-native', program, input],
      ['run', program, '-'],
      ['run', '--no-native', '-', input],
    ]) {
      const stdin = '-' === args[args.length - 1] ? RECORDS : '-' === args[2] ? PROGRAM : undefined
      const out = alchemy(args, stdin)
      assert.equal(out.status, 0, `${args}: ${out.stderr}`)
      assert.equal(out.stdout, EXPECTED_CSV, String(args))
      assert.equal(out.stderr, '', String(args))
    }
  })

  it('run renders a table result as csv or json', () => {
    const program = tempFile('table.alc', PROGRAM.replace('    csv csv-options\n', ''))
    let out = alchemy(['run', program, '-'], RECORDS)
    assert.equal(out.status, 0, out.stderr)
    assert.equal(out.stdout, EXPECTED_CSV)
    out = alchemy(['run', '--render', 'json', program, '-'], RECORDS)
    assert.equal(out.status, 0, out.stderr)
    assert.equal(
      out.stdout,
      '[{"Identifier":123,"Full name":"Alice","Balance":50.25},{"Identifier":456,"Full name":"Bob","Balance":72}]\n',
    )
    // The echo, and a renderer refused for a program that renders itself.
    const echo = tempFile('echo.alc', 'def export [input] input\n')
    out = alchemy(['run', echo, '-'], '[1.50, {"a": null}]')
    assert.equal(out.status, 0, out.stderr)
    assert.equal(out.stdout, '[1.50,{"a":null}]\n')
    out = alchemy(['run', '--render', 'csv', echo, '-'], '[1]')
    assert.equal(out.status, 2)
    assert.ok(failJson(out).message.startsWith('protocol_mismatch: '), out.stderr)
    const text = tempFile('text.alc', PROGRAM)
    out = alchemy(['run', '--render', 'json', text, '-'], RECORDS)
    assert.equal(out.status, 2)
    assert.ok(failJson(out).message.startsWith('render_of_text: '), out.stderr)
    // Before the input is read: an input that cannot be read is never
    // reached, so the refusal is the renderer's, not `cannot read`.
    for (const [file, render, prefix] of [
      [echo, 'csv', 'protocol_mismatch: '],
      [text, 'json', 'render_of_text: '],
    ]) {
      out = alchemy(['run', '--render', render, file, '/nonexistent/input.json'])
      assert.equal(out.status, 2, prefix)
      assert.ok(failJson(out).message.startsWith(prefix), out.stderr)
    }
  })

  // The statuses follow the code: 1 for an input or protocol failure, 5 for
  // a limit, 2 for the program; the failure is one JSON object on standard
  // error and standard output carries nothing but the answer.
  it('run statuses follow the failure code', () => {
    const program = tempFile('export2.alc', PROGRAM)
    // Metadata after the rows: INPUT_ORDER_VIOLATION, status 1, no output.
    const reordered =
      '{"response":{"payload":{"deep":{"records":[{"id":1,"person":{"name":"a"},"account":{"balance":2}}]}},"metadata":{"fields":[{"title":"Identifier","path":["id"]}]}}}'
    let out = alchemy(['run', program, '-'], reordered)
    assert.equal(out.status, 1, out.stderr)
    assert.equal(out.stdout, '')
    let fail = failJson(out)
    assert.equal(fail.code, 'INPUT_ORDER_VIOLATION')
    assert.equal(fail.output, 'none')
    assert.ok(String(fail.path).startsWith('.response.payload'), JSON.stringify(fail))
    // Invalid JSON after the rows: INPUT_INVALID, status 1; the rows were
    // buffered, not committed, so nothing reached standard output.
    out = alchemy(['run', program, '-'], `${RECORDS} x`)
    assert.equal(out.status, 1, out.stderr)
    assert.equal(out.stdout, '')
    fail = failJson(out)
    assert.equal(fail.code, 'INPUT_INVALID')
    assert.equal(fail.output, 'none')
    // A missing cell under the standard options: MISSING_VALUE, status 1.
    const missing =
      '{"response":{"metadata":{"fields":[{"title":"Identifier","path":["id"]},{"title":"Full name","path":["person","name"]}]},"payload":{"deep":{"records":[{"id":1}]}}}}'
    out = alchemy(['run', program, '-'], missing)
    assert.equal(out.status, 1, out.stderr)
    assert.equal(failJson(out).code, 'MISSING_VALUE')
    // A program that does not check: status 2, nothing read from the input.
    const bad = tempFile('bad.alc', 'def export [input] (nope input)\n')
    out = alchemy(['run', bad, '-'], 'not json')
    assert.equal(out.status, 2)
    assert.equal(failJson(out).code, 'DSL_TYPE_ERROR')
  })

  it('a reader error carries the engine code and position', () => {
    const out = alchemy(['canon', '-'], 'a\n  b\n c\n')
    assert.equal(out.status, 2)
    const fail = failJson(out)
    assert.equal(fail.code, 'DSL_PARSE_ERROR')
    assert.ok(fail.message.startsWith('bad_dedent: '), JSON.stringify(fail))
    assert.deepStrictEqual([fail.row, fail.col], [3, 2])
  })

  // The reader bounds nesting, so a program nested far past the bound is a
  // `too_deep` failure with status 2 from every command, never a stack
  // overflow.
  it('a program nested beyond the bound is a failure, not an abort', () => {
    const depth = 100_000
    const parens = `${'('.repeat(depth)}x${')'.repeat(depth)}`
    let indented = ''
    for (let level = 0; level < 600; level++) indented += '  '.repeat(level) + 'x\n'
    // Flat to the reader, one level per step to the desugarer.
    const pipe = `pipe x${' f'.repeat(depth)}\n`
    // The failure names the opener, the line or the form that passed the
    // bound: the 256th paren, the line that would open the 256th level, the
    // pipe.
    for (const [command, program, row] of [
      ['canon', parens, 1],
      ['format', parens, 1],
      ['check', parens, 1],
      ['explain', parens, 1],
      ['canon', indented, 257],
      ['check', pipe, 1],
    ] as Array<[string, string, number]>) {
      const out = alchemy([command, '-'], program)
      assert.equal(out.status, 2, `${command}: ${out.stderr}`)
      assert.equal(out.stdout, '', command)
      const fail = failJson(out)
      assert.equal(fail.code, 'DSL_PARSE_ERROR', command)
      assert.ok(fail.message.startsWith('too_deep: '), `${command}: ${JSON.stringify(fail)}`)
      assert.equal(fail.row, row, `${command}: ${JSON.stringify(fail)}`)
    }
  })

  it('usage errors and unreadable files exit 2', () => {
    let out = alchemy([])
    assert.equal(out.status, 2)
    assert.ok(out.stderr.includes('usage:'), out.stderr)
    assert.equal(out.stdout, '')
    // An unknown command is refused before standard input is read, so what
    // it holds does not matter: unparsable input is not a parse error here.
    out = alchemy(['bogus', '-'], '(')
    assert.equal(out.status, 2)
    assert.equal(failJson(out).code, 'INPUT_INVALID')
    assert.equal(out.stdout, '')
    // A wrong argument count, a bad renderer, two standard inputs.
    for (const args of [
      ['run', '-'],
      ['run', '--render', 'xml', 'a.alc', 'b.json'],
      ['run', '--bogus', 'a.alc', 'b.json'],
      ['run', '-', '-'],
      ['explain'],
    ]) {
      out = alchemy(args)
      assert.equal(out.status, 2, String(args))
      assert.equal(failJson(out).code, 'INPUT_INVALID', String(args))
      assert.equal(out.stdout, '', String(args))
    }
    out = alchemy(['canon', '/nonexistent/program.alc'])
    assert.equal(out.status, 2)
    assert.equal(failJson(out).code, 'INPUT_INVALID')
    assert.equal(out.stdout, '')
    // Malformed UTF-8 cannot be read, from a file or from standard input,
    // as in the Rust binary and the Go command.
    const bad = join(TMP, 'bad.alc')
    writeFileSync(bad, Buffer.concat([Buffer.from('def export [input] '), Buffer.from([0xff, 0x0a])]))
    for (const [file, stdin] of [[bad, undefined], ['-', Buffer.from([0xff])]] as const) {
      out = alchemy(['canon', file], stdin)
      assert.equal(out.status, 2, file)
      assert.equal(failJson(out).code, 'INPUT_INVALID', file)
      assert.equal(failJson(out).message, `cannot read ${file}: stream did not contain valid UTF-8`)
      assert.equal(out.stdout, '', file)
    }
  })

  // A program inside the nesting bound checks, runs and explains: the
  // evaluator's stack is its own, where a pipe of 245 steps once overflowed
  // the Rust crate's main thread.
  it('a program inside the bound runs', () => {
    const program = tempFile(
      'pipe245.alc',
      `def export [input]\n  pipe input\n    select (path each-index)\n${'    map (fn [x] x)\n'.repeat(245)}    join ","\n`,
    )
    for (const args of [
      ['check', program],
      ['explain', program],
    ]) {
      const out = alchemy(args)
      assert.equal(out.status, 0, `${args}: ${out.stderr}`)
    }
    const out = alchemy(['run', program, '-'], '["a","b"]')
    assert.equal(out.status, 0, out.stderr)
    assert.equal(out.stdout, 'a,b')
  })

  // A function applied to itself is `recursion` with status 2 from `check`
  // (which builds the plan) and from `run`, not a stack overflow.
  it('self-application is a failure, not an abort', () => {
    const omega = tempFile('omega.alc', 'def w [f] (f f)\ndef export [input]\n  let [x (w w)]\n    json input\n')
    let out = alchemy(['check', omega])
    assert.equal(out.status, 2, out.stderr)
    const fail = failJson(out)
    assert.equal(fail.code, 'STREAMABILITY_UNKNOWN')
    assert.ok(fail.message.startsWith('recursion: '), JSON.stringify(fail))
    const perItem = tempFile(
      'omega-item.alc',
      'def w [f] (f f)\ndef export [input]\n  pipe input\n    select (path each-index)\n    map (fn [x] (w w))\n    join ","\n',
    )
    out = alchemy(['run', perItem, '-'], '[1]')
    assert.equal(out.status, 2, out.stderr)
    assert.equal(failJson(out).code, 'STREAMABILITY_UNKNOWN')
  })

  // `check` builds the plan, so a `fail` on `export`'s own path is reported
  // there, with its code (`INPUT_INVALID`, status 1) and the form's
  // position, although no document was read.
  it('check reports a fail the plan reaches', () => {
    const program = tempFile('fail.alc', 'def export [input] (concat "a" (fail "boom"))\n')
    const out = alchemy(['check', program])
    assert.equal(out.status, 1, out.stderr)
    const fail = failJson(out)
    assert.equal(fail.code, 'INPUT_INVALID')
    assert.equal(fail.message, 'boom')
    assert.deepStrictEqual([fail.row, fail.col], [1, 32])
  })

  // A failure inside the standard library names the library's file, row
  // and column in its message, and gives no row or column of the user's
  // file; a program that happens to share a library file's name is still
  // read as its own text.
  it('a library failure names the library file', () => {
    const lib = source('stdlib/table.alc') as string
    const lines = lib.split('\n')
    const index = lines.findIndex((l) => l.includes('fail "Required metadata was not found"'))
    const [row, col] = [index + 1, lines[index].indexOf('fail') + 1]
    const dir = join(TMP, 'lib')
    mkdirSync(join(dir, 'stdlib'), { recursive: true })
    for (const name of ['export.alc', 'stdlib/table.alc']) {
      writeFileSync(join(dir, name), PROGRAM)
      const out = alchemy(['run', '--no-native', name, '-'], '{}', dir)
      assert.equal(out.status, 1, `${name}: ${out.stderr}`)
      const fail = failJson(out)
      assert.equal(fail.code, 'INPUT_INVALID', name)
      assert.equal(fail.message, `Required metadata was not found (at stdlib/table.alc:${row}:${col})`, name)
      assert.equal(fail.row, undefined, `${name}: ${JSON.stringify(fail)}`)
    }
  })

  // A standard error that cannot be written loses the report, not the
  // status: the failure's own status comes back.
  it('a closed standard error keeps the status', async () => {
    const bad = tempFile('closed.alc', 'def export [input] (nope input)\n')
    const child = spawn(process.execPath, [BIN, 'check', bad], { stdio: ['ignore', 'ignore', 'pipe'] })
    child.stderr!.destroy()
    const status = await new Promise<number | null>((resolve) => child.on('exit', (code) => resolve(code)))
    assert.equal(status, 2)
  })

  // `--max-output-bytes` bounds what `run` writes, and a text longer than
  // it fails with the limit named, status 5.
  it('run takes an output limit', () => {
    const program = tempFile('limit.alc', PROGRAM)
    let out = alchemy(['run', '--max-output-bytes', '20', program, '-'], RECORDS)
    assert.equal(out.status, 5, out.stderr)
    assert.equal(failJson(out).limit.name, 'max_output_bytes')
    assert.ok(Buffer.byteLength(out.stdout) <= 20)
    out = alchemy(['run', '--max-output-bytes', '1000', program, '-'], RECORDS)
    assert.equal(out.status, 0, out.stderr)
    assert.equal(out.stdout, EXPECTED_CSV)
    out = alchemy(['run', '--max-output-bytes', 'many', program, '-'], RECORDS)
    assert.equal(out.status, 2)
    assert.equal(failJson(out).code, 'INPUT_INVALID')
  })
})

// `translate` and `formats`, as the Rust binary has them: a document read
// with one format's grammar and written in another, the flags, and the
// failures with their statuses. ./translate.test.ts writes every document
// of its corpus into every format.
describe('cli translate', () => {
  const PEOPLE = '{"people":[{"name":"Ann","age":31.50},{"name":"Bo","age":7}],"n":null}'
  const IDS = ['csv', 'ini', 'json', 'json5', 'jsonc', 'jsonic', 'jsonl', 'markdown', 'toml', 'xml', 'yaml', 'zon']
  const TABLE =
    'def export [input]\n' +
    '  table-from-json (record (entry :columns :infer) (entry :rows (path "people" each-index))) input\n'
  const TEXT = 'def export [input] (join "," (map (fn [p] (get :name p)) (select (path "people" each-index) input)))\n'

  it('formats prints the formats translate reads and writes, as JSON', () => {
    const out = alchemy(['formats'])
    assert.equal(out.status, 0, out.stderr)
    assert.equal(out.stderr, '')
    assert.ok(out.stdout.endsWith('}]\n'), out.stdout)
    const formats = JSON.parse(out.stdout)
    assert.deepStrictEqual(
      formats.map((f: any) => f.id),
      IDS,
    )
    for (const f of formats) {
      assert.deepStrictEqual(Object.keys(f), [
        'id',
        'reads',
        'writes',
        'root',
        'schema',
        'lift',
        'embed',
        'render',
        'loss',
      ])
      assert.ok(0 < f.loss.length, `${f.id} declares what it does not keep`)
    }
    assert.deepStrictEqual(formats[2], {
      id: 'json',
      reads: ['tree'],
      writes: 'tree',
      root: 'any',
      schema: null,
      lift: null,
      embed: null,
      render: 'json',
      loss: ['JSON has no spelling for Infinity or NaN, so a number that is not finite is written as null.'],
    })
    const markdown = formats[7]
    assert.deepStrictEqual(
      [markdown.reads, markdown.writes, markdown.root, markdown.schema, markdown.lift, markdown.embed, markdown.render],
      [['records', 'tree'], 'records', 'array', 'markdown-ast', 'markdown-lift', null, 'markdown-render'],
    )
    const xml = formats[9]
    assert.deepStrictEqual([xml.schema, xml.embed, xml.render], ['xml-element', 'xml-embed', 'xml-render'])
    // No format writes a schema's tree without an embedding into it, so the
    // composition's `schema_only` refusal is not reachable from here: a
    // plain tree reaches every format.
    for (const f of formats) assert.ok(null === f.schema || 'records' === f.writes || null !== f.embed, f.id)
    // `formats` takes no arguments.
    const extra = alchemy(['formats', 'json'])
    assert.equal(extra.status, 2)
    assert.equal(failJson(extra).code, 'INPUT_INVALID')
    assert.ok(failJson(extra).message.startsWith('usage: '), extra.stderr)
    assert.equal(extra.stdout, '')
  })

  it('translate writes a document in another format', () => {
    const input = tempFile('people.json', PEOPLE)
    let out = alchemy(['translate', '--from', 'json', '--to', 'yaml', input])
    assert.equal(out.status, 0, out.stderr)
    // A number keeps the lexeme the document spelled it with.
    assert.equal(
      out.stdout,
      '"people":\n  - "name": "Ann"\n    "age": 31.50\n  - "name": "Bo"\n    "age": 7\n"n": null\n',
    )
    // What a format does not keep is its loss list, which `formats` prints:
    // a translation writes the document and nothing else.
    assert.equal(out.stderr, '')
    for (const [args, stdin, want] of [
      [
        ['--from', 'json', '--to', 'toml', '-'],
        PEOPLE,
        '"people" = [ { "name" = "Ann", "age" = 31.50 }, { "name" = "Bo", "age" = 7 } ]\n',
      ],
      [['--from', 'json', '--to', 'jsonl', '-'], PEOPLE, `${PEOPLE}\n`],
      [['--from', 'json', '--to', 'xml', '-'], '42', '<document type="number">42</document>\n'],
      [['--from', 'csv', '--to', 'json', '-'], 'a,b\n1,x\n', '[{"a":"1","b":"x"}]\n'],
      [['--from', 'csv', '--to', 'markdown', '-'], 'a,b\n1,x\n', '| a | b |\n| --- | --- |\n| 1 | x |\n'],
      // A YAML stream of several documents is read whole, as the grammar's
      // own value, which the incremental source cannot follow.
      [['--from', 'yaml', '--to', 'json', '-'], 'a: 1\n---\nb: 2\n', '[{"a":1},{"b":2}]\n'],
      // A byte order mark is the document's, and its grammar's to read.
      [['--from', 'csv', '--to', 'json', '-'], '\ufeffa\n1\n', '[{"\ufeffa":"1"}]\n'],
    ] as Array<[string[], string, string]>) {
      out = alchemy(['translate', ...args], stdin)
      assert.equal(out.status, 0, `${args}: ${out.stderr}`)
      assert.equal(out.stdout, want, String(args))
      assert.equal(out.stderr, '', String(args))
    }
  })

  // `--key` names the member a root that is not an object is written under,
  // for a format whose document is one; `--path` selects the value to write.
  it('translate takes a key and a path', () => {
    const input = tempFile('people-path.json', PEOPLE)
    const number = tempFile('number.json', '42')
    for (const [args, want] of [
      [['--to', 'toml', number], '"items" = 42\n'],
      [['--to', 'toml', '--key', 'value', number], '"value" = 42\n'],
      [['--to', 'ini', '--key', 'value', number], '"value" = 42\n'],
      // A format whose document may be any value takes no key.
      [['--to', 'yaml', '--key', 'value', number], '42\n'],
      [['--to', 'toml', '--key', 'age', '--path', '["people",1,"age"]', input], '"age" = 7\n'],
      // A selected value is read whole, so its numbers keep no lexeme.
      [['--to', 'csv', '--path', '["people"]', input], '"name","age"\r\n"Ann","31.5"\r\n"Bo","7"\r\n'],
      [
        ['--to', 'json', '--path', '[]', input],
        '{"people":[{"name":"Ann","age":31.5},{"name":"Bo","age":7}],"n":null}\n',
      ],
    ] as Array<[string[], string]>) {
      const out = alchemy(['translate', '--from', 'json', ...args])
      assert.equal(out.status, 0, `${args}: ${out.stderr}`)
      assert.equal(out.stdout, want, String(args))
    }
    // A path that names nothing is the document's failure, status 1.
    let out = alchemy(['translate', '--from', 'json', '--to', 'yaml', '--path', '["people",5]', input])
    assert.equal(out.status, 1, out.stderr)
    assert.equal(out.stdout, '')
    assert.deepStrictEqual(failJson(out), {
      code: 'INPUT_INVALID',
      message: 'the path ["people",5] names nothing in the document',
      output: 'none',
    })
    // A path that is not an array of keys and indexes is the command line's,
    // status 2, before the input is read.
    for (const path of ['people', '[0.5]', '[-1]', '[true]', '{"a":1}']) {
      out = alchemy(['translate', '--from', 'json', '--to', 'yaml', '--path', path, '/nonexistent/input.json'])
      assert.equal(out.status, 2, `${path}: ${out.stderr}`)
      assert.deepStrictEqual(failJson(out), {
        code: 'INPUT_INVALID',
        message: `--path takes a JSON array of keys and indexes, such as ["people",0], not ${path}`,
        output: 'none',
      })
    }
  })

  // `--with` runs a program over the input first; its export's events or
  // table are what the format writes.
  it('translate takes a program', () => {
    const input = tempFile('people-with.json', PEOPLE)
    const table = tempFile('table.alc', TABLE)
    for (const [to, want] of [
      ['csv', '"name","age"\r\n"Ann","31.50"\r\n"Bo","7"\r\n'],
      ['markdown', '| name | age |\n| --- | --- |\n| Ann | 31.50 |\n| Bo | 7 |\n'],
      ['json', '[{"name":"Ann","age":31.50},{"name":"Bo","age":7}]\n'],
    ]) {
      const out = alchemy(['translate', '--from', 'json', '--to', to, '--with', table, input])
      assert.equal(out.status, 0, `${to}: ${out.stderr}`)
      assert.equal(out.stdout, want, to)
    }
    // The program from standard input.
    let out = alchemy(['translate', '--from', 'json', '--to', 'csv', '--with', '-', input], TABLE)
    assert.equal(out.status, 0, out.stderr)
    assert.equal(out.stdout, '"name","age"\r\n"Ann","31.50"\r\n"Bo","7"\r\n')
    // A program that writes its own text has nothing for a render to write:
    // refused before the input is read.
    const text = tempFile('text-export.alc', TEXT)
    out = alchemy(['translate', '--from', 'json', '--to', 'yaml', '--with', text, '/nonexistent/input.json'])
    assert.equal(out.status, 2, out.stderr)
    assert.equal(out.stdout, '')
    assert.deepStrictEqual(failJson(out), {
      code: 'DSL_TYPE_ERROR',
      message:
        `bad_output: ${text}'s export writes its own text, and translate takes a program whose export ` +
        "answers JSON events or a table, which a format's render then writes",
      output: 'none',
    })
    // A program that does not check, at its position.
    const broken = tempFile('broken-export.alc', 'def export [input] (nope input)\n')
    out = alchemy(['translate', '--from', 'json', '--to', 'yaml', '--with', broken, input])
    assert.equal(out.status, 2, out.stderr)
    const fail = failJson(out)
    assert.equal(fail.code, 'DSL_TYPE_ERROR')
    assert.ok(fail.message.startsWith('unknown_name: nope'), JSON.stringify(fail))
    assert.deepStrictEqual([fail.row, fail.col], [1, 21])
  })

  it('translate failures exit with their statuses', () => {
    const input = tempFile('people-fail.json', PEOPLE)
    // Usage errors, status 2, nothing read: no --from, no input, two inputs,
    // a flag without its value, a flag translate does not take.
    for (const args of [
      ['--to', 'yaml', input],
      ['--from', 'json', '--to', 'yaml'],
      ['--from', 'json', '--to', 'yaml', input, input],
      ['--from', 'json', '--to'],
      ['--from', 'json', '--to', 'yaml', '--render', 'json', input],
    ]) {
      const out = alchemy(['translate', ...args])
      assert.equal(out.status, 2, String(args))
      assert.equal(out.stdout, '', String(args))
      assert.ok(failJson(out).message.startsWith('usage: '), out.stderr)
    }
    // A format no package names, two standard inputs, an output limit that
    // is not a number: status 2, with what was wrong.
    for (const [args, message] of [
      [['--from', 'json', '--to', 'docx', input], `--to takes ${IDS.slice(0, -1).join(', ')} or zon, not "docx"`],
      [['--from', 'JSON', '--to', 'yaml', input], `--from takes ${IDS.slice(0, -1).join(', ')} or zon, not "JSON"`],
      [
        ['--from', 'json', '--to', 'yaml', '--with', '-', '-'],
        'only one of PROGRAM and INPUT may be - (standard input)',
      ],
      [
        ['--from', 'json', '--to', 'yaml', '--max-output-bytes', 'many', input],
        '--max-output-bytes takes a number of bytes, not "many"',
      ],
    ] as Array<[string[], string]>) {
      const out = alchemy(['translate', ...args])
      assert.equal(out.status, 2, String(args))
      assert.equal(out.stdout, '', String(args))
      assert.deepStrictEqual(failJson(out), { code: 'INPUT_INVALID', message, output: 'none' }, String(args))
    }
    // An input that cannot be read: status 2.
    let out = alchemy(['translate', '--from', 'json', '--to', 'yaml', '/nonexistent/input.json'])
    assert.equal(out.status, 2, out.stderr)
    assert.ok(failJson(out).message.startsWith('cannot read /nonexistent/input.json: '), out.stderr)
    // A document its grammar refuses: status 1, at the engine's position.
    out = alchemy(['translate', '--from', 'json', '--to', 'yaml', '-'], '{"a":')
    assert.equal(out.status, 1, out.stderr)
    assert.equal(out.stdout, '')
    let fail = failJson(out)
    assert.equal(fail.code, 'INPUT_INVALID')
    assert.deepStrictEqual([fail.row, fail.col], [1, 6])
    out = alchemy(['translate', '--from', 'toml', '--to', 'json', input])
    assert.equal(out.status, 1, out.stderr)
    assert.deepStrictEqual([failJson(out).code, failJson(out).row, failJson(out).col], ['INPUT_INVALID', 1, 1])
    // An output past `--max-output-bytes`: status 5, the limit named, and
    // nothing written, since a translation is written once it has succeeded.
    out = alchemy(['translate', '--from', 'json', '--to', 'yaml', '--max-output-bytes', '10', input])
    assert.equal(out.status, 5, out.stderr)
    assert.equal(out.stdout, '')
    fail = failJson(out)
    assert.equal(fail.code, 'RESOURCE_LIMIT_EXCEEDED')
    assert.deepStrictEqual(fail.limit, { name: 'max_output_bytes', value: 10 })
    out = alchemy(['translate', '--from', 'json', '--to', 'yaml', '--max-output-bytes', '1000', input])
    assert.equal(out.status, 0, out.stderr)
  })
})
