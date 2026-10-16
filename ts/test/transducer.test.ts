/* Copyright (c) 2026 tabnas, MIT License */

// A format whose package ships a transducer part is read through it (the
// Rust crate's unit tests in rs/src/translate.rs): the route over its
// parser's rule passes, composed over the part, and what it falls back to.

import { describe, it } from 'node:test'
import assert from 'node:assert'

import { translate as alchemyTranslate } from '@tabnas/alchemy'
import { make as makeJson } from '@tabnas/json'
import { BytesWriter } from '@tabnas/render'
import { Fail, Limits, Metrics } from '@tabnas/transduce'

import { Format, format as formatNamed, formats, parsePath, parserNames, run } from '../dist/translate'

// The json transducer, as its package's part spells it: the library's
// `transduce-tree` over the json grammar's rules.
const JSON_TRANSDUCE =
  'def json-transduce [input]\n' +
  '  transduce-tree\n' +
  '    record\n' +
  '      entry :map (record (entry :container :object))\n' +
  '      entry :list (record (entry :container :array))\n' +
  '      entry :pair (record (entry :key 0))\n' +
  '      entry :val (record (entry :scalar 0))\n' +
  '    input\n'

// A transducer that writes `{"ran":true}` whatever the document: what it
// writes says which route ran.
const MARKING_TRANSDUCE =
  'def json-transduce [input]\n' +
  '  as-events\n' +
  '    scan-emit\n' +
  '      []\n' +
  '      fn [s pass] (transition s [])\n' +
  '      fn [s] [object-start (key "ran") (scalar true) object-end]\n' +
  '      parser-events input\n'

// json, read through a transducer part given beside its package's parts,
// as a package that ships one is read.
function transducedJson(text: string): Format {
  const json = formatNamed('json')
  assert.ok(json)
  return new Format(json.part, () => makeJson(), {
    file: 'tabnas-json/alchemy/transduce.alc',
    entry: 'json-transduce',
    text,
  })
}

function translated(
  from: Format,
  to: string,
  path: string | undefined,
  program: { file: string; text: string } | undefined,
  input: string,
): string {
  const limits = Limits.default()
  const target = formatNamed(to)
  assert.ok(target)
  const held = new BytesWriter()
  run(
    {
      from,
      to: target,
      path: undefined === path ? undefined : parsePath(path, limits),
      options: alchemyTranslate.Options.default(),
      program,
      limits,
    },
    input,
    held,
    new Metrics(),
  )
  return held.text()
}

describe('a transducer part', () => {
  // A format whose package ships a transducer part is read through it, its
  // rule passes streamed into the composition over the part, and one
  // without is read as before: the two routes write the same bytes for the
  // same document, a number's lexeme kept, into a tree target, a records
  // target, and with a program over the input; the part's own output shows
  // the route ran. A path keeps the document's value, the one route a
  // path has. A repeated member, which the part streams twice and the
  // composition's tree-contract refuses, is read again whole, as the
  // incremental source's is.
  it('reads the format, and the walk stands in where it refuses', () => {
    const plain = formatNamed('json')
    assert.ok(plain)
    const through = transducedJson(JSON_TRANSDUCE)
    const marking = transducedJson(MARKING_TRANSDUCE)
    assert.equal(plain.transducer, undefined)
    assert.equal(through.transducer?.entry, 'json-transduce')
    const tree = '{"a":[1,2.50,"x",{"b":null}],"c":{},"d":[],"e":1e21,"f":true}'
    const records = '[{"a":1,"b":"x"},{"a":2,"b":"y"}]'
    const echo = { file: 'echo.alc', text: 'def export [input] (events input)\n' }
    const cases: Array<[string, string, { file: string; text: string } | undefined]> = [
      ['json', tree, undefined],
      ['yaml', tree, undefined],
      ['csv', records, undefined],
      ['json', tree, echo],
      ['csv', records, echo],
    ]
    for (const [to, doc, program] of cases) {
      const expected = translated(plain, to, undefined, program, doc)
      assert.equal(translated(through, to, undefined, program, doc), expected, `${to} from ${doc}`)
      let marked: string | undefined
      try {
        marked = translated(marking, to, undefined, program, doc)
      } catch {
        marked = undefined
      }
      assert.notEqual(marked, expected, `${to} from ${doc}: the part's route ran`)
    }
    assert.equal(translated(marking, 'json', undefined, undefined, tree), '{"ran":true}\n')
    // A path selects in the document's value, read whole, as it did: the
    // materialized value spells a number as the engine does.
    assert.equal(translated(marking, 'json', '["a",1]', undefined, tree), '2.5\n')
    const repeated = '{"a":1,"a":2}'
    assert.equal(translated(through, 'json', undefined, undefined, repeated), translated(plain, 'json', undefined, undefined, repeated))
    assert.equal(translated(through, 'json', undefined, undefined, repeated), '{"a":2}\n')
    assert.throws(
      () => translated(through, 'json', undefined, undefined, '[1,'),
      (err: unknown) => err instanceof Fail && 'INPUT_INVALID' === err.code,
    )
  })

  // Every format with a parser answers its rule passes; one read as a tree
  // has none and refuses; json5's check runs first.
  it('every format with a parser gives its rule passes', () => {
    const parsed = formats()
      .filter((f) => f.parses())
      .map((f) => f.id)
    // C is parsed here, where the Rust crate reads it as its realized tree.
    assert.deepStrictEqual(parsed, [
      'c', 'css', 'csv', 'feed', 'ini', 'json', 'json5', 'jsonc', 'jsonic', 'jsonl', 'markdown', 'pgn',
      'semver', 'toml', 'xml', 'yaml', 'zon',
    ])
    assert.equal(
      parserNames(),
      'c, css, csv, feed, ini, json, json5, jsonc, jsonic, jsonl, markdown, pgn, semver, toml, xml, yaml or zon',
    )
    const abnf = formatNamed('abnf')
    assert.ok(abnf)
    assert.equal(abnf.parses(), false)
    assert.throws(
      () => abnf.passes('x = "y"'),
      (err: unknown) => err instanceof Fail && 'INPUT_INVALID' === err.code && err.message.startsWith('abnf is read as a tree'),
    )
    const json5 = formatNamed('json5')
    assert.ok(json5)
    assert.throws(
      () => json5.passes('// only a comment'),
      (err: unknown) => err instanceof Fail && 'INPUT_INVALID' === err.code,
    )
    let passes = 0
    json5.passes('{a: 1}').run({
      event: () => {
        passes += 1
        return 'continue'
      },
    })
    assert.ok(0 < passes)
  })
})
