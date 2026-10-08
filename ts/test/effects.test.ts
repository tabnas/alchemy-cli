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
// evaluator built, natively and through the library's text.
describe('explain, compiled', () => {
  // The CSV renderer is reported with the dialect it is built with, and
  // what it reports is what it writes.
  it('a custom csv dialect is reported as it runs', () => {
    const lf =
      PROGRAM.replace('    csv csv-options\n', '    csv lf\n') +
      '\ndef lf (record (entry :delimiter ";") (entry :newline "\\n") (entry :header false) (entry :null-text "NULL") (entry :missing "-"))\n'
    const program = compile(lf, 'lf.alc')
    assert.ok(program.native)
    const r: any = (explainJson(program) as any).renderer
    assert.deepStrictEqual(
      [r.name, r.host, r.newline, r.header, r.delimiter, r.null_text, r.missing, r.missing_text],
      ['csv', false, '\n', false, ';', 'NULL', 'text', '-'],
    )
    assert.equal(ok(program, RECORDS), '"123";"Alice";"50.25"\n"456";"Bob";"72"\n')
    // The null and missing texts reach the output as reported.
    const sparse = RECORDS.replace(
      '{"account":{"balance":72},"person":{"name":"Bob"},"id":456}',
      '{"person":{"name":null},"id":456}',
    )
    assert.notEqual(sparse, RECORDS)
    assert.equal(ok(program, sparse), '"123";"Alice";"50.25"\n"456";"NULL";"-"\n')
    // The default dialect, and the host's renderer, report the defaults.
    for (const [name, p, isHost] of [
      ['default', compile(PROGRAM, 'export.alc'), false],
      ['host', compile(PROGRAM.replace('    csv csv-options\n', ''), 'host.alc'), true],
    ] as const) {
      const d: any = (explainJson(p) as any).renderer
      assert.deepStrictEqual(
        [d.name, d.host, d.delimiter, d.newline, d.header, d.null_text, d.missing, d.missing_text],
        ['csv', isHost, ',', '\r\n', true, '', 'error', null],
        name,
      )
    }
  })
})
