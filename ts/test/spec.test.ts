/* Copyright (c) 2026 tabnas, MIT License */

// alchemy's shared fixtures (its test/spec, read from the checkout beside
// this one) that need the stages, run through @tabnas/support's runner as
// alchemy runs the rest (rs/tests/spec_test.rs is the Rust half): the bytes
// a run writes (run.tsv), and the catalogue's drift test over the error
// rows of all four files.
//
// run.tsv runs each row twice, with the standard compositions native and
// through the library's text (`withNative(false)`), and a row whose two
// paths differ in a byte, a code or a position fails whatever its expected
// cell says.
//
// Moved from alchemy's ts/test/spec.test.ts, which runs reader.tsv,
// pipe.tsv and check.tsv and the catalogue's per-row checks over them:
// run.tsv's rows run programs on transduce's routers and render's
// renderers, which alchemy does not depend on, and the catalogue's
// coverage clause needs those rows.

import { describe, it } from 'node:test'
import assert from 'node:assert'
import { join } from 'node:path'

import { SpecRow, isErrorExpect, loadSpecDir, makeRunner } from '@tabnas/support'
import { make as makeJson } from '@tabnas/json'
import { BytesWriter } from '@tabnas/render'
import { ParserSource, Prune, SourceMode } from '@tabnas/transduce'

import {
  Program,
  Renderer,
  desugarProgram,
  grammarDocument,
  isFail,
  make as makeAlchemy,
  parse,
  rendererNamed,
} from '@tabnas/alchemy'
import { Limits, Metrics } from '@tabnas/alchemy/shared'

import { OWN_CODES, compile, failCode, sibling } from './host'

// alchemy's fixtures, in the checkout beside this one.
const SPEC_DIR = sibling('alchemy', 'test', 'spec')

// The finer code a row pins, and the position the failure names.
const runnerOptions = {
  errorCode: (err: unknown) => failCode(err),
  errorPos: (err: any) => ({ row: err?.row, col: err?.col }),
}

// Compile `program` and run it over `doc`, as `alchemy run` does: the
// document read by the JSON grammar incrementally, pruned under the
// program's row selector when it has one, with the default limits.
function runBoth(program: string, doc: string, render: Renderer | undefined, native: boolean): string {
  let compiled = compile(program, 'run')
  if (!native) compiled = compiled.withNative(false)
  return drive(compiled, doc, render)
}

function drive(program: Program, doc: string, render: Renderer | undefined): string {
  const limits = Limits.default()
  const metrics = new Metrics()
  const writer = new BytesWriter()
  const sink = program.sink(writer, render, limits, metrics)
  const selector = program.rowSelector()
  const prune = undefined === selector ? Prune.never() : Prune.under(selector)
  new ParserSource(makeJson(), doc)
    .grammar('json')
    .mode(SourceMode.incremental(prune))
    .limits(limits)
    .metrics(metrics)
    .run(sink)
  return writer.text()
}

type Outcome = { ok: true; text: string } | { ok: false; fail: unknown }

function outcome(work: () => string): Outcome {
  try {
    return { ok: true, text: work() }
  } catch (fail) {
    return { ok: false, fail }
  }
}

function shown(o: Outcome): string {
  return o.ok ? JSON.stringify(o.text) : String(o.fail)
}

// A program run over a JSON document: the bytes it writes, or the failure.
// See test/AGENTS.md for the columns. Both paths, compared before the
// expected cell is.
makeRunner({
  ...runnerOptions,
  parse: (program: string, row: SpecRow) => {
    const cell = row.unescNamed('doc')
    const doc = '' === cell ? 'null' : cell
    const name = row.named('render')
    let render: Renderer | undefined
    if ('' !== name) {
      render = rendererNamed(name)
      if (undefined === render) throw new Error(`${row.where()}: render is csv, json or empty`)
    }
    const native = outcome(() => runBoth(program, doc, render, true))
    const interpreted = outcome(() => runBoth(program, doc, render, false))
    let agree: boolean
    if (native.ok && interpreted.ok) {
      agree = native.text === interpreted.text
    } else if (!native.ok && !interpreted.ok) {
      // Failures both, whichever copy of transduce made each (the native
      // path's renderers are render's): one code, at one position.
      const a: any = native.fail
      const b: any = interpreted.fail
      agree =
        isFail(a) &&
        isFail(b) &&
        failCode(a) === failCode(b) &&
        a.row === b.row &&
        a.col === b.col
    } else {
      agree = false
    }
    if (!agree) {
      throw new Error(
        `native and interpreted runs disagree:\n  native:      ${shown(native)}\n  interpreted: ${shown(interpreted)}`,
      )
    }
    if (native.ok) return native.text
    throw native.fail
  },
}).file(join(SPEC_DIR, 'run.tsv'))

// The failures a row's program meets: the reader's (reader.tsv), the
// desugarer's (pipe.tsv), compile's (check.tsv), or a run's, both paths
// (run.tsv), as the runners above meet them.
function rowFailures(file: string, input: string, row: SpecRow): unknown[] {
  const met = (work: () => unknown): unknown[] => {
    try {
      work()
      return []
    } catch (fail) {
      return [fail]
    }
  }
  switch (file) {
    case 'reader.tsv':
      return met(() => parse(input))
    case 'pipe.tsv':
      return met(() => desugarProgram(parse(input), input))
    case 'check.tsv':
      return met(() => compile(input, 'check'))
    default: {
      const cell = row.unescNamed('doc')
      const doc = '' === cell ? 'null' : cell
      const name = row.named('render')
      const render = '' === name ? undefined : rendererNamed(name)
      return [true, false].flatMap((native) => met(() => runBoth(input, doc, render, native)))
    }
  }
}

// Whether `text` is an instance of the template `line`. The engine trims
// the messages it writes from a template, so a `{name}` that ends a line
// may take the space before it with it.
function instanceOf(line: string, text: string): boolean {
  if (fills(line, text)) return true
  const bare = line.replace(/\s*\{[a-z_]+\}$/, '')
  return bare !== line && fills(bare, text)
}

// Whether `text` fills the template `line`: its fixed parts in order, each
// `{name}` any text, none included.
function fills(line: string, text: string): boolean {
  const parts = line.split(/\{[a-z_]+\}/)
  if (1 === parts.length) return text === line
  const first = parts[0]
  const last = parts[parts.length - 1]
  const end = text.length - last.length
  if (end < first.length || !text.startsWith(first) || !text.endsWith(last)) return false
  let at = first.length
  for (const part of parts.slice(1, -1)) {
    const found = text.indexOf(part, at)
    if (-1 === found || found + part.length > end) return false
    at = found + part.length
  }
  return true
}

// The fixed text of a template: what a more specific line has more of. A
// failure meets the most specific line it is an instance of, the first of
// equals.
function fixedLength(line: string): number {
  return line.replace(/\{[a-z_]+\}/g, '').length
}

// Every failure the shared fixtures meet is declared in the grammar
// document (rs/tests/spec_test.rs, `the_raised_messages_match_the_document`):
// the finer code that leads the message is a key of the installed
// `options.error`, the engine's and this grammar's, and the text after it is
// a line of that entry, each `{name}` standing for what the raising site
// fills in (a failure raised inside the standard library ends with its
// position there, ` (at stdlib/...)`, which is not part of the text). Each
// line of each code the document declares is the most specific line some
// row meets, so the document holds no text nothing raises, and each of its
// codes has a hint. A finer code comes with the same code wherever it is
// raised (`bad_let` is a DSL_PARSE_ERROR from the desugarer and from the
// resolver alike). A raising site whose code or text drifts fails here.
describe('the catalogue', () => {
  it('the raised messages match the document', () => {
    const catalogue: Record<string, string> = makeAlchemy().options.error
    const document = grammarDocument()
    const met = new Set<string>()
    // The codes each finer code is raised with: one, wherever it is raised.
    const raisedAs = new Map<string, Set<string>>()
    const problems: string[] = []
    let failures = 0
    for (const spec of loadSpecDir(SPEC_DIR)) {
      for (const row of spec.rows) {
        if (!isErrorExpect(row.col(1))) continue
        const input = row.unesc(row.resolve(0))
        for (const fail of rowFailures(spec.file, input, row)) {
          if (!isFail(fail) || !OWN_CODES.includes(fail.code)) continue
          failures++
          const split = fail.message.indexOf(': ')
          if (-1 === split) {
            problems.push(`${row.where()}: ${fail.code} has no finer code: ${fail.message}`)
            continue
          }
          const code = fail.message.substring(0, split)
          if (!raisedAs.has(code)) raisedAs.set(code, new Set())
          raisedAs.get(code)?.add(fail.code)
          let text = fail.message.substring(split + 2)
          const library = text.lastIndexOf(' (at stdlib/')
          if (-1 !== library && text.endsWith(')')) text = text.substring(0, library)
          const entry = catalogue[code]
          if ('string' !== typeof entry) {
            problems.push(`${row.where()}: ${code} is not declared in options.error`)
            continue
          }
          const lines = entry
            .split('\n')
            .filter((line) => instanceOf(line, text))
            .sort((a, b) => fixedLength(b) - fixedLength(a))
          if (0 === lines.length) {
            problems.push(`${row.where()}: no line of options.error.${code} is ${JSON.stringify(text)}`)
          } else {
            met.add(code + '\n' + lines[0])
          }
        }
      }
    }
    assert.ok(failures > 0, 'the fixtures meet failures')
    for (const [code, uppers] of raisedAs) {
      if (uppers.size > 1) problems.push(`${code} is raised as ${[...uppers].sort().join(' and ')}; a finer code has one code`)
    }
    for (const [code, entry] of Object.entries<string>(document.options.error)) {
      if ('string' !== typeof document.options.hint[code]) problems.push(`options.hint.${code} is not declared`)
      for (const line of entry.split('\n')) {
        if (!met.has(code + '\n' + line)) problems.push(`options.error.${code}: no fixture row meets ${JSON.stringify(line)}`)
      }
    }
    assert.deepStrictEqual(problems, [])
  })
})
