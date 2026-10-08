/* Copyright (c) 2026 tabnas, MIT License */

// `compileSources` (rs/tests/sources_test.rs): several sources linked into
// one program, as a host links a format's parts (libraries of definitions
// prefixed by the format's name, with no `export`) with the program that
// calls them. The program runs as the same text in one file runs; a
// failure in any source carries that source's file, row and column, in the
// field and in the failure's display, at every stage that positions one:
// the reader, the desugarer, the resolver, the checker and the run.
//
// Moved from alchemy's ts/test/sources.test.ts, which keeps the front
// end's half (a failure in the second source, the linking's refusals, one
// source): these run the linked program on transduce's routers and
// render's renderers, which alchemy does not depend on.

import { describe, it } from 'node:test'
import assert from 'node:assert'

import { Program, Source } from '@tabnas/alchemy'

import { compile, compileSources, drive, ok } from './host'

// A render part: definitions prefixed by the format's name, no `export`.
const PART =
  '; A part of the lines format: each item quoted, one to a line.\ndef lines-line [item]\n  concat (quoted item) "\\n"\n\ndef lines-render [items]\n  concat-map lines-line items\n'

const PART_FILE = 'lines/render.alc'

// The program that calls the part.
const MAIN = 'def export [input]\n  lines-render (select (path each-index) input)\n'

const MAIN_FILE = 'main.alc'

function sources(list: Array<[string, string]>): Source[] {
  return list.map(([file, text]) => ({ file, text }))
}

// The part second, as the design's diagnostics test has it.
function linked(part: string): Program {
  return compileSources(
    sources([
      [MAIN_FILE, MAIN],
      [PART_FILE, part],
    ]),
  )
}

// The 1-based row and column of `needle` on the 1-based `row` of `text`,
// as a failure carries them: the column in characters.
function at(text: string, row: number, needle: string): [number, number] {
  const line = text.split('\n')[row - 1]
  const index = line.indexOf(needle)
  assert.ok(-1 !== index, `${needle} is on row ${row}`)
  return [row, [...line.substring(0, index)].length + 1]
}

// The failure names `file` and the position, in the fields and in its
// display.
function assertAt(fail: any, file: string, [row, col]: [number, number]): void {
  assert.equal(fail.file, file, String(fail))
  assert.deepStrictEqual([fail.row, fail.col], [row, col], String(fail))
  const shown = `(${file}:${row}:${col})`
  assert.ok(String(fail).endsWith(shown), `${fail} does not end ${shown}`)
  assert.equal(fail.toJSON().file, file, String(fail))
}

describe('sources', () => {
  // Two sources are one namespace: the program calls the part's
  // definitions, in either order of the sources, and runs as the same
  // texts in one file run.
  it('linked sources run as one program', () => {
    const input = '["a","b\\"c","d\\ne"]'
    const expected = '"a"\n"b\\"c"\n"d\\ne"\n'
    const mainFirst = linked(PART)
    assert.equal(ok(mainFirst, input), expected)
    const partFirst = compileSources(
      sources([
        [PART_FILE, PART],
        [MAIN_FILE, MAIN],
      ]),
    )
    assert.equal(ok(partFirst, input), expected)
    const one = compile(`${MAIN}\n${PART}`, 'one.alc')
    assert.equal(ok(one, input), expected)
    // The program is named by its first source, and its plan is reported
    // as one program's.
    assert.equal(mainFirst.file(), MAIN_FILE)
    assert.equal(partFirst.file(), PART_FILE)
    assert.equal(mainFirst.explain(), one.explain())
    // A part may call back into the program: the namespace is one.
    const back =
      'def lines-line [item]\n  concat (main-mark item) "\\n"\n\ndef lines-render [items]\n  concat-map lines-line items\n'
    const main = `${MAIN}\ndef main-mark [s] (concat "* " s)\n`
    const program = compileSources(
      sources([
        [MAIN_FILE, main],
        [PART_FILE, back],
      ]),
    )
    assert.equal(ok(program, '["x"]'), '* x\n')
  })

  // A failure the run meets in a part, from a `fail` in its definition, is
  // the program's `INPUT_INVALID` at the part's file and position.
  it('a run time failure in a part names the part', () => {
    const strict =
      'def lines-line [item]\n  match item\n    case "bad" (fail "a bad item")\n    case _ (concat (quoted item) "\\n")\n\ndef lines-render [items]\n  concat-map lines-line items\n'
    const program = linked(strict)
    assert.equal(ok(program, '["ok"]'), '"ok"\n')
    const { fail } = drive(program, '["ok","bad"]')
    assert.equal(fail.code, 'INPUT_INVALID', String(fail))
    assert.equal(fail.message, 'a bad item', String(fail))
    assertAt(fail, PART_FILE, at(strict, 3, '(fail'))
    // The interpreted twin positions it the same way.
    assertAt(drive(program.withNative(false), '["bad"]').fail, PART_FILE, at(strict, 3, '(fail'))
  })

  // A program linked under another name feeds a format's render: its
  // `export` is the format's input, in one plan under one set of limits.
  it('a program linked under another name feeds a render', () => {
    const program = compileSources([
      { file: 'program.alc', text: 'def export [input] (select (path each-index) input)\n', exportAs: 'program-export' },
      { file: 'lines.alc', text: PART },
      { file: 'main.alc', text: 'def export [input] (lines-render (program-export input))\n' },
    ])
    assert.equal(ok(program, '["a","b"]'), '"a"\n"b"\n')
    assert.deepStrictEqual([...program.resolved.defs.keys()], [
      'program-export',
      'lines-line',
      'lines-render',
      'export',
    ])
    // A failure the run meets in the renamed source names its file.
    const failing = compileSources([
      {
        file: 'program.alc',
        text: 'def export [input] (map (fn [x] (fail "no")) (select (path each-index) input))\n',
        exportAs: 'program-export',
      },
      { file: 'lines.alc', text: PART },
      { file: 'main.alc', text: 'def export [input] (lines-render (program-export input))\n' },
    ])
    const { fail } = drive(failing, '["a"]')
    assert.equal(fail.code, 'INPUT_INVALID', String(fail))
    assert.equal(fail.file, 'program.alc', String(fail))
    assert.deepStrictEqual([fail.row, fail.col], [1, 33])
  })
})
