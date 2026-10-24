/* Copyright (c) 2026 tabnas, MIT License */

// The plan report (rs/src/effects.rs), over a compiled program whose
// report is held to what it writes: the CSV renderer is reported with the
// dialect it is built with, and what it reports is what it writes.
//
// Moved from alchemy's ts/test/effects.test.ts, which keeps every test of
// the report that runs nothing: this one runs its program on transduce's
// routers and render's renderers, which alchemy does not depend on.

import { describe, it } from 'node:test'
import assert from 'node:assert'

import { explainJson } from '@tabnas/alchemy'

import { PROGRAM, RECORDS } from './common'
import { compile, ok } from './host'

// rs/src/effects.rs's tests, over compiled programs: the plan the
// evaluator built of the library's text.
describe('explain, compiled', () => {
  // A program's own `csv` is the library's text, and the report says so:
  // the renderer is the text's own, and what the text writes is the dialect
  // its options record names. The host's renderer, for a table result, is
  // reported with the defaults it is built with.
  it("a program's own csv renders as text, and the host's renderer reports the defaults", () => {
    const lf =
      PROGRAM.replace('    csv csv-options\n', '    csv lf\n') +
      '\ndef lf (record (entry :delimiter ";") (entry :newline "\\n") (entry :header false) (entry :null-text "NULL") (entry :missing "-"))\n'
    const program = compile(lf, 'lf.alc')
    assert.equal((explainJson(program) as any).renderer.name, 'text')
    assert.equal(ok(program, RECORDS), '"123";"Alice";"50.25"\n"456";"Bob";"72"\n')
    // The null and missing texts reach the output as the options say.
    const sparse = RECORDS.replace(
      '{"account":{"balance":72},"person":{"name":"Bob"},"id":456}',
      '{"person":{"name":null},"id":456}',
    )
    assert.notEqual(sparse, RECORDS)
    assert.equal(ok(program, sparse), '"123";"Alice";"50.25"\n"456";"NULL";"-"\n')
    // The default dialect is the text's too; the host's renderer reports
    // the defaults it is built with.
    assert.equal((explainJson(compile(PROGRAM, 'export.alc')) as any).renderer.name, 'text')
    const d: any = (explainJson(compile(PROGRAM.replace('    csv csv-options\n', ''), 'host.alc')) as any).renderer
    assert.deepStrictEqual(
      [d.name, d.host, d.delimiter, d.newline, d.header, d.null_text, d.missing, d.missing_text],
      ['csv', true, ',', '\r\n', true, '', 'error', null],
    )
  })
})
