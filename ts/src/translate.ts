/* Copyright (c) 2026 tabnas, MIT License */

// `alchemy translate`: a document in one of the formats whose grammar
// packages this command carries, written in any of them (the Rust crate's
// rs/src/translate.rs).
//
// Every format's package exports its translation parts: its manifest's
// `translate` object (the shapes it reads and writes, the root its render
// needs, the schema its events carry), its render, and, where it has them,
// its lift and its embedding, each an alchemy file. This module reads them
// into alchemy's `Part`s, has alchemy's `translate` namespace compose the
// route (the source's lift, the root adapters, the embedding, the inferred
// table, the render), compiles the composition with transduce's routers
// and render's renderers, and runs it over the input read with the source
// format's grammar: incrementally where transduce's differential suite has
// verified that grammar (`capability.incremental`), so that a number keeps
// the lexeme the document spelled it with, and materialized otherwise,
// when a path selects a value below the root, and for a document whose
// value the grammar builds otherwise than its events showed (a YAML stream
// of several documents, a merge key, a repeated member), which the
// incremental source refuses part way: the grammar's own value is the
// document, so it is read again whole. Nothing here knows a format by its
// name: a format is what its manifest says, and a package whose manifest
// names no parts this host can take is not a format here.

import { Program, compile as compileProgram, isFail, translate } from '@tabnas/alchemy'
import type { CompileOptions } from '@tabnas/alchemy'
import { Chess, translate as chessParts } from '@tabnas/chess'
import { Css, translate as cssParts } from '@tabnas/css'
import { make as makeCsv, translate as csvParts } from '@tabnas/csv'
import { Expr, translate as exprParts } from '@tabnas/expr'
import { Feed, translate as feedParts } from '@tabnas/feed'
import { Ini, translate as iniParts } from '@tabnas/ini'
import { make as makeJson, translate as jsonParts } from '@tabnas/json'
import { Json5, translate as json5Parts } from '@tabnas/json5'
import { Jsonc, translate as jsoncParts } from '@tabnas/jsonc'
import { jsonic, translate as jsonicParts } from '@tabnas/jsonic'
import { make as makeJsonl, translate as jsonlParts } from '@tabnas/jsonl'
import { Markdown, translate as markdownParts } from '@tabnas/markdown'
import { Tabnas } from '@tabnas/parser'
import { Proto, translate as protoParts } from '@tabnas/proto'
import { BytesWriter, renderers } from '@tabnas/render'
import type { Writer } from '@tabnas/render'
import { Semver, translate as semverParts } from '@tabnas/semver'
import { Toml, translate as tomlParts } from '@tabnas/toml'
import {
  Datum,
  DatumBuilder,
  Ev,
  Fail,
  Limits,
  Metrics,
  ParserSource,
  Prune,
  SourceMode,
  TreeContract,
  capability,
  getPath,
  routers,
  toText,
  walkDatum,
} from '@tabnas/transduce'
import type { JsonEvent, Segment, Sink } from '@tabnas/transduce'
import { Xml, translate as xmlParts } from '@tabnas/xml'
import { Yaml, translate as yamlParts } from '@tabnas/yaml'
import { Zon, translate as zonParts } from '@tabnas/zon'

// What every composition is compiled with: transduce's routers and
// render's renderers.
const OPTIONS: CompileOptions = { routers, renderers }

// A grammar package's parts, as each one's `translate()` hands them over.
// Every package owns its interface types, so this reads the same fields
// from each.
type Parts = {
  readonly manifest: string
  readonly lift?: translate.PartText
  readonly embed?: translate.PartText
  readonly render?: translate.PartText
}

// One format this command reads and writes: its parts, as its package's
// manifest names them, and the grammar its documents are read with, a
// fresh parser per source (a source owns its parser).
export class Format {
  constructor(
    readonly part: translate.Part,
    readonly parser: () => Tabnas,
  ) {}

  // The manifest's `languageId`.
  get id(): string {
    return this.part.id
  }

  // A source over `text`, read with this format's grammar.
  reader(text: string): ParserSource {
    return new ParserSource(this.parser(), text)
  }

  // A document read whole with this format's grammar, as a value: its
  // events as `translate` reads them, incrementally where the grammar is
  // verified (so a number keeps the lexeme the document spelled it with),
  // collected, or the grammar's value where the incremental source cannot
  // follow it. A repeated member keeps its last value.
  read(input: string, limits: Limits): Datum {
    if (capability.incremental(this.id)) {
      try {
        return read(this, input, SourceMode.incremental(Prune.never()), limits)
      } catch (err) {
        if (!unfollowed(err)) throw err
      }
    }
    return read(this, input, SourceMode.materialize(), limits)
  }
}

// A package's parts as alchemy's descriptor, named by the package as the
// Rust crate names it (`tabnas-yaml`), which is the name its parts carry
// in diagnostics.
function descriptor(pkg: string, parts: Parts | undefined): translate.Descriptor | undefined {
  if (undefined === parts) return undefined
  return { package: pkg, manifest: parts.manifest, lift: parts.lift, embed: parts.embed, render: parts.render }
}

// Every grammar package this command carries, with its parser: each one's
// default, as the Rust crate's `make()` builds it (the relaxed grammars on
// jsonic's base) and as transduce's differential suite reads it.
function packages(): Array<[translate.Descriptor | undefined, () => Tabnas]> {
  return [
    [descriptor('tabnas-chess', chessParts()), () => new Tabnas().use(Chess)],
    [descriptor('tabnas-css', cssParts()), () => new Tabnas().use(jsonic).use(Css)],
    [descriptor('tabnas-csv', csvParts()), () => makeCsv()],
    [descriptor('tabnas-expr', exprParts()), () => new Tabnas().use(jsonic).use(Expr)],
    [descriptor('tabnas-feed', feedParts()), () => new Tabnas().use(Feed)],
    [descriptor('tabnas-ini', iniParts()), () => new Tabnas().use(jsonic).use(Ini)],
    [descriptor('tabnas-json', jsonParts()), () => makeJson()],
    [descriptor('tabnas-json5', json5Parts()), () => new Tabnas().use(jsonic).use(Json5)],
    [descriptor('tabnas-jsonc', jsoncParts()), () => new Tabnas().use(jsonic).use(Jsonc)],
    [descriptor('tabnas-jsonic', jsonicParts()), () => new Tabnas().use(jsonic)],
    [descriptor('tabnas-jsonl', jsonlParts()), () => makeJsonl()],
    [descriptor('tabnas-markdown', markdownParts()), () => new Tabnas().use(Markdown)],
    [descriptor('tabnas-proto', protoParts()), () => new Tabnas({ rewind: { history: 8192 } }).use(Proto)],
    [descriptor('tabnas-semver', semverParts()), () => new Tabnas().use(Semver)],
    [descriptor('tabnas-toml', tomlParts()), () => new Tabnas().use(jsonic).use(Toml)],
    [descriptor('tabnas-xml', xmlParts()), () => new Tabnas().use(Xml)],
    [descriptor('tabnas-yaml', yamlParts()), () => new Tabnas().use(jsonic).use(Yaml)],
    [descriptor('tabnas-zon', zonParts()), () => zonParser()],
  ]
}

// ZON's parser, with an integer no double holds exactly as the object
// `{"$big": "<digits>"}`, the digits after a minus sign when it is
// negative: the reader's big integer as ZON's translation part declares it
// and its Rust reader builds it. The TypeScript reader builds a bigint,
// which transduce writes as the nearest double, so the token's value is
// replaced as it is read, before a rule takes it, and the value and the
// events hold the object.
function zonParser(): Tabnas {
  return new Tabnas()
    .use(jsonic)
    .use(Zon)
    .sub({
      lex: (tkn: { val: unknown }) => {
        if ('bigint' === typeof tkn.val) tkn.val = { $big: tkn.val.toString() }
      },
    })
}

let registry: ReadonlyArray<Format> | undefined

// Every format, by id, read once.
export function formats(): ReadonlyArray<Format> {
  if (undefined === registry) {
    const found: Format[] = []
    for (const [d, parser] of packages()) {
      const part = undefined === d ? undefined : translate.Part.fromDescriptor(d)
      if (undefined !== part) found.push(new Format(part, parser))
    }
    found.sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0))
    registry = Object.freeze(found)
  }
  return registry
}

// The format a command line names, by its id.
export function format(id: string): Format | undefined {
  return formats().find((f) => f.id === id)
}

// The formats' ids for a message: `csv, ini, ... or zon`.
export function names(): string {
  const ids = formats().map((f) => f.id)
  if (ids.length < 2) return ids.join('')
  return `${ids.slice(0, -1).join(', ')} or ${ids[ids.length - 1]}`
}

// What a translation was asked.
export type Request = {
  // The format the input is read with.
  readonly from: Format
  // The format it is written in.
  readonly to: Format
  // A value below the root to translate instead of the whole document, as
  // segments: a plain tree, whatever the source's shapes.
  readonly path?: ReadonlyArray<Segment>
  // What the host chooses: the key a root is wrapped under.
  readonly options: translate.Options
  // A program, as its file name and its text, whose export stands in the
  // source's place: its JSON events a tree, its table records.
  readonly program?: { readonly file: string; readonly text: string }
  readonly limits: Limits
}

// A request's composition, and the program it compiles to.
export type Compiled = {
  readonly composition: translate.Composition
  readonly program: Program
}

// The composition a request makes, and the program it compiles to.
export function compile(request: Request): Compiled {
  const given = request.program
  if (undefined !== given) {
    const { file, text } = given
    const output = compileProgram(text, file, OPTIONS).output
    if ('Text' === output) {
      throw new Fail(
        'DSL_TYPE_ERROR',
        `bad_output: ${file}'s export writes its own text, and translate takes a program whose ` +
          "export answers JSON events or a table, which a format's render then writes",
      )
    }
    const composition = translate.composeProgram(output, request.to.part, request.options, 'translate')
    return { composition, program: composition.compile({ file, text }, OPTIONS) }
  }
  const source = undefined === request.path ? request.from.part : undefined
  const composition = translate.compose(source, request.to.part, request.options, 'translate')
  return { composition, program: composition.compile(undefined, OPTIONS) }
}

// Whether the incremental source refused a document because the grammar's
// value is not what its events showed (a root wrapped or replaced after it
// streamed, a map rewritten, a repeated member that the grammar keeps
// once): the grammar's own value is the document, so it is read again
// whole.
function unfollowed(err: unknown): boolean {
  return isFail(err) && ('STREAMABILITY_UNKNOWN' === err.code || 'DUPLICATE_MEMBER' === err.code)
}

// Run a request over `input`, writing the document to `out` (render's
// `Writer`, the counterpart of Rust's `io::Write`) once the run has
// succeeded; `metrics` collects what the stages report, the output's bytes
// among it.
export function run(request: Request, input: string, out: Writer, metrics: Metrics): void {
  runCompiled(request, compile(request), input, out, metrics)
}

// `run`, with the request's composition compiled already (`compile`), so
// that one compiled composition serves many inputs.
export function runCompiled(
  request: Request,
  compiled: Compiled,
  input: string,
  out: Writer,
  metrics: Metrics,
): void {
  const { composition, program } = compiled
  // What a run writes is held until the run has succeeded, so that a run
  // the incremental source gives up part way is run again from the start
  // rather than written twice.
  const attempt = (mode: SourceMode, metrics: Metrics): Buffer => {
    const held = new BytesWriter()
    const inner = program.sink(held, undefined, request.limits, metrics)
    // The source's events reach a tree's render as the source made them,
    // so a stream that is no tree's is refused in front of it rather than
    // written half way.
    const sink: Sink = 'tree' === composition.front ? new TreeContract(inner) : inner
    const path = request.path
    if (undefined === path) {
      request.from
        .reader(input)
        .grammar(request.from.id)
        .mode(mode)
        .limits(request.limits)
        .metrics(metrics)
        .run(sink)
    } else {
      const value = read(request.from, input, SourceMode.materialize(), request.limits)
      const selected = getPath(value, path)
      if (undefined === selected) {
        throw Fail.input(`the path ${pathText(path)} names nothing in the document`)
      }
      if ('continue' === walkDatum(selected, sink)) sink.event(Ev.end)
    }
    return held.bytes()
  }
  const incremental = undefined === request.path && capability.incremental(request.from.id)
  let bytes: Buffer
  if (incremental) {
    const selector = program.rowSelector()
    const mode = SourceMode.incremental(undefined === selector ? Prune.never() : Prune.under(selector))
    // The incremental attempt counts into metrics of its own, which the
    // caller's take on once it has succeeded: a run given up part way
    // counts nothing.
    const own = new Metrics()
    try {
      bytes = attempt(mode, own)
      absorb(metrics, own)
    } catch (err) {
      if (!unfollowed(err)) throw err
      bytes = attempt(SourceMode.materialize(), metrics)
    }
  } else {
    bytes = attempt(SourceMode.materialize(), metrics)
  }
  let written = 0
  try {
    let rest: Uint8Array = bytes
    while (0 < rest.length) {
      const n = out.write(rest)
      if (n <= 0) throw new Error('failed to write whole buffer')
      written += n
      rest = rest.subarray(n)
    }
    out.flush?.()
  } catch (err) {
    const why = err instanceof Error ? err.message : String(err)
    const fail = new Fail('OUTPUT_FAILED', `the output could not be written: ${why}`)
    // Bytes the writer took before it failed have left.
    throw 0 < written ? fail.committed() : fail
  }
}

// What an attempt counted, added to the caller's metrics: the counts
// summed and the high-water marks raised, so that the metrics a run hands
// back are those of the attempt whose output was written.
function absorb(into: Metrics, from: Metrics): void {
  into.events += from.events
  into.keys += from.keys
  into.scalars += from.scalars
  into.rows += from.rows
  into.captured_bytes += from.captured_bytes
  into.output_bytes += from.output_bytes
  into.captured_bytes_high = Math.max(into.captured_bytes_high, from.captured_bytes_high)
  into.retained_bytes_high = Math.max(into.retained_bytes_high, from.retained_bytes_high)
}

// The events of a document, collected into a value.
class Collect implements Sink {
  constructor(readonly builder: DatumBuilder) {}

  event(ev: JsonEvent): 'continue' {
    if ('end' !== ev.type) this.builder.event(ev)
    return 'continue'
  }
}

// A document read whole with a format's grammar, as a value. A repeated
// member keeps its last value, as the grammar's own value does.
function read(format: Format, input: string, mode: SourceMode, limits: Limits): Datum {
  const collect = new Collect(new DatumBuilder(Number.POSITIVE_INFINITY, 'max_capture_bytes', 'last_wins'))
  format.reader(input).grammar(format.id).mode(mode).limits(limits).run(collect)
  const value = collect.builder.take()
  if (undefined === value) throw Fail.input('the document holds no value')
  return value
}

// The largest index a path holds: Rust's `usize::MAX`, which a double
// reaches as 2^64.
const INDEX_MAX = 2 ** 64

// A path given as a JSON array of segments, each a string (a member's key)
// or a whole number (an element's index), the form alchemy's `as-path`
// validates.
export function parsePath(text: string, limits: Limits): Segment[] {
  const json = format('json')
  if (undefined === json) throw Fail.input('no JSON grammar to read the path')
  const refuse = () =>
    Fail.input(`--path takes a JSON array of keys and indexes, such as ["people",0], not ${text}`)
  let value: Datum
  try {
    value = json.read(text, limits)
  } catch (_err) {
    throw refuse()
  }
  if ('array' !== value.type) throw refuse()
  return value.items.map((item): Segment => {
    if ('string' === item.type) return item.value
    if ('number' === item.type && Number.isInteger(item.value) && item.value >= 0 && item.value <= INDEX_MAX) {
      // `-0` is the index 0, as Rust's cast makes it.
      return 0 === item.value ? 0 : item.value
    }
    throw refuse()
  })
}

// An index's digits, as Rust writes the `usize` it was cast to: exactly,
// and 2^64 as `usize::MAX`.
function indexText(index: number): string {
  return index >= INDEX_MAX ? (2n ** 64n - 1n).toString() : BigInt(index).toString()
}

// A path as the JSON array it was given as.
function pathText(path: ReadonlyArray<Segment>): string {
  return toText(
    Datum.array(path.map((s) => ('string' === typeof s ? Datum.string(s) : Datum.number(s, indexText(s))))),
  )
}

// The registry as JSON, one object per format, for `alchemy formats`: its
// id, the shapes it reads and writes, the root its render needs, its
// schema, its parts' entries, and its loss sentences.
export function formatsJson(): string {
  const text = (s: string): Datum => Datum.string(s)
  const orNull = (s: string | undefined): Datum => (undefined === s ? Datum.null : text(s))
  const items = formats().map((f) => {
    const p = f.part
    const render = p.render
    return Datum.object([
      ['id', text(p.id)],
      ['reads', Datum.array(p.reads.map(text))],
      ['writes', text(p.writes)],
      ['root', text(p.root)],
      ['schema', orNull(p.schema)],
      ['lift', orNull(p.lift?.entry)],
      ['embed', orNull(p.embed?.entry)],
      ['render', text('alc' === render.kind ? render.alc.entry : render.kind)],
      ['loss', Datum.array(p.loss.map(text))],
    ])
  })
  return toText(Datum.array(items))
}
