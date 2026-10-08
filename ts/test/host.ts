/* Copyright (c) 2026 tabnas, MIT License */

// What alchemy's tests that run programs share, as a host composes them:
// compile with transduce's routers and render's renderers, drive a program
// over a JSON text, record and replay a document's events, tell a
// failure's finer code, and find a sibling checkout's files (alchemy's
// ts/test/common.ts and rs/tests/common/mod.rs, where these were defined
// first).
//
// Those tests moved here from alchemy, which depends on neither transduce
// nor render; this package is the composition root that depends on all
// three. They are black-box tests of alchemy's public API: they import its
// package entry (`@tabnas/alchemy`) and its shared unit
// (`@tabnas/alchemy/shared`), never its build's own modules.

import { existsSync } from 'node:fs'
import { join } from 'node:path'

import * as alchemy from '@tabnas/alchemy'
import { EventRecorder, Limits, Metrics, replay } from '@tabnas/alchemy/shared'
import { make as makeJson } from '@tabnas/json'
import { BytesWriter, renderers } from '@tabnas/render'
import { ParserSource, Prune, SourceMode, routers } from '@tabnas/transduce'

// This repository's root: two levels above dist-test/.
export const REPO_ROOT = join(__dirname, '..', '..')

// A path in a checkout beside this one (`parts[0]` names the repository),
// which a test reads, such as alchemy's fixtures (`sibling('alchemy',
// 'test', 'spec')`): required, as it is for the Rust tests. A missing
// checkout fails the tests that read it, naming the path; nothing skips.
export function sibling(...parts: string[]): string {
  const path = join(REPO_ROOT, '..', ...parts)
  if (!existsSync(path)) {
    throw new Error(`${path} does not exist: clone tabnas/${parts[0]} beside this repository`)
  }
  return path
}

// What a host passes `compile`: transduce's routers and render's renderers,
// the stages a program's runtime builds a run from.
export const OPTIONS: alchemy.CompileOptions = { routers, renderers }

// `compile` as a host calls it, with `OPTIONS`.
export function compile(src: string, file: string): alchemy.Program {
  return alchemy.compile(src, file, OPTIONS)
}

// `compileSources` as a host calls it, with `OPTIONS`.
export function compileSources(sources: ReadonlyArray<alchemy.Source>): alchemy.Program {
  return alchemy.compileSources(sources, OPTIONS)
}

// The codes whose failures carry a finer code as the first word of the
// message, before `: `.
export const OWN_CODES = ['DSL_PARSE_ERROR', 'DSL_TYPE_ERROR', 'STREAM_REUSED', 'STREAMABILITY_UNKNOWN']

// The code a fixture pins for a failure: the finer code for alchemy's own
// codes, the transduce code itself for any other. A failure render made
// counts, whichever copy of transduce render resolves (`isFail`).
export function failCode(err: unknown): string | undefined {
  if (!alchemy.isFail(err)) return undefined
  if (OWN_CODES.includes(err.code)) {
    const at = err.message.indexOf(': ')
    return -1 === at ? err.message : err.message.substring(0, at)
  }
  return err.code
}

// The finer code of a failure that must be a `Fail` of one of alchemy's
// codes, or a description of what was thrown instead.
export function finer(err: unknown): string {
  return failCode(err) ?? `not a Fail: ${err}`
}

// What `fn` throws; fails the test when it returns.
export function thrown(fn: () => unknown): any {
  try {
    fn()
  } catch (err) {
    return err
  }
  throw new Error('expected a failure')
}

// What a host does with a program and a JSON text (rs/tests/run_test.rs's
// `drive`): parse the text with the json grammar incrementally, pruned
// under the program's row selector when it has one, push the events into
// the program's sink, and mark a failure as leaving partial output when
// `output_bytes` says bytes reached the writer. The outcome, and the text
// the writer received.
export function drive(
  program: any,
  text: string,
  render?: 'csv' | 'json',
  limits: any = Limits.default(),
  metrics: any = new Metrics(),
): { fail?: any; out: string } {
  const writer = new BytesWriter()
  let sink: any
  try {
    sink = program.sink(writer, render, limits, metrics)
  } catch (fail) {
    return { fail, out: '' }
  }
  const selector = program.rowSelector()
  const prune = undefined === selector ? Prune.never() : Prune.under(selector)
  try {
    new ParserSource(makeJson(), text)
      .grammar('json')
      .mode(SourceMode.incremental(prune))
      .limits(limits)
      .metrics(metrics)
      .run(sink)
  } catch (fail) {
    if (alchemy.isFail(fail) && metrics.output_bytes > 0 && !fail.committedOutput) fail.committed()
    return { fail, out: writer.text() }
  }
  return { out: writer.text() }
}

// The output of a run that must succeed.
export function ok(program: any, text: string, render?: 'csv' | 'json'): string {
  const { fail, out } = drive(program, text, render)
  if (undefined !== fail) throw fail
  return out
}

// The failure of a run that must fail, and what had been written.
export function err(program: any, text: string, limits: any = Limits.default()): { fail: any; out: string } {
  const { fail, out } = drive(program, text, undefined, limits)
  if (undefined === fail) throw new Error(`the run should fail, and wrote ${JSON.stringify(out)}`)
  return { fail, out }
}

// The events of one document, recorded once so several runs replay the
// same stream: the walk of the grammar's value (materialize mode), sound
// for every grammar. Undefined when the grammar refuses the document.
export function events(parser: any, text: string): any[] | undefined {
  const recorder = new EventRecorder()
  try {
    new ParserSource(parser, text).run(recorder)
  } catch (_fail) {
    return undefined
  }
  return recorder.events
}

// Replay `events` through `program`'s sink; the output, or the failure
// thrown.
export function replayed(
  program: any,
  evs: any[],
  render?: 'csv' | 'json',
  limits: any = Limits.default(),
  metrics: any = new Metrics(),
): string {
  const writer = new BytesWriter()
  const sink = program.sink(writer, render, limits, metrics)
  replay(evs, sink)
  return writer.text()
}
