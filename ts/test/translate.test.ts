/* Copyright (c) 2026 tabnas, MIT License */

// The cross product `alchemy translate` answers for (the Rust crate's
// rs/tests/translate_test.rs): every document of the corpus, read with its
// format's grammar, written in every format the command carries, read back
// with that format's grammar, and compared with the document's own value
// under the target's declared conventions (its loss list). A pair that
// fails to write, that writes a document its own grammar refuses, or that
// reads back as anything but the conventions say is a failure, and so is a
// corpus that shrinks.
//
// The corpus is the sibling checkouts': transduce's fixtures (aless's, one
// document per format at least, and more for YAML and ZON) and the
// documents of JSONTestSuite every JSON parser must accept. The Rust test
// reads JSONTestSuite from jsonc's copy (`jsonc/test/JSONTestSuite`); this
// one reads json's (`json/test/jsontestsuite`), the same files byte for
// byte, from a checkout every gate that runs this suite clones. A fixture
// its own grammar refuses (ZON's repeated fields) is no document, and is
// counted as one refused. The Rust crate's second matrix, every format's
// own fixture corpus in release, stays Rust's.

import { describe, it } from 'node:test'
import assert from 'node:assert'
import { readFileSync, readdirSync } from 'node:fs'
import { extname, join } from 'node:path'

import { translate as alchemyTranslate } from '@tabnas/alchemy'
import { BytesWriter } from '@tabnas/render'
import { Limits, Metrics, toText } from '@tabnas/transduce'
import type { Datum } from '@tabnas/transduce'

import {
  Compiled,
  Format,
  Request,
  compile,
  format as formatNamed,
  formats,
  names,
  runCompiled,
} from '../dist/translate'

import { sibling } from './host'

const UTF8 = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true })

// The format a fixture's extension names, by its id.
function formatOf(extension: string): string | undefined {
  switch (extension) {
    case 'json':
    case 'json5':
    case 'jsonc':
    case 'jsonic':
    case 'jsonl':
    case 'csv':
    case 'toml':
    case 'ini':
    case 'xml':
    case 'yaml':
    case 'zon':
      return extension
    case 'md':
      return 'markdown'
    default:
      return undefined
  }
}

type Doc = { name: string; id: string; text: string }

// Every document of the corpus: its name, its format and its text.
function corpus(): Doc[] {
  const docs: Doc[] = []
  const dirs: Array<[string, string, string | undefined]> = [
    ['transduce', sibling('transduce', 'rs', 'tests', 'fixtures'), undefined],
    ['JSONTestSuite', sibling('json', 'test', 'jsontestsuite', 'test_parsing'), 'y_'],
  ]
  for (const [name, dir, prefix] of dirs) {
    for (const file of readdirSync(dir).sort()) {
      if (undefined !== prefix && !file.startsWith(prefix)) continue
      const id = formatOf(extname(file).slice(1))
      if (undefined === id) continue
      let text: string
      try {
        text = UTF8.decode(readFileSync(join(dir, file)))
      } catch (_err) {
        continue
      }
      docs.push({ name: `${name}/${file}`, id, text })
    }
  }
  return docs
}

function format(id: string): Format {
  const found = formatNamed(id)
  assert.ok(found, `${id} is a format`)
  return found
}

// The compositions compiled so far, by source, target and program: one
// compiled composition serves every document of a pair.
const COMPILED = new Map<string, Compiled>()

// Translate `text`, read as `from`, into `to`, with `program` in front
// when there is one.
function translateText(from: Format, to: Format, text: string, program?: { file: string; text: string }): string {
  const request: Request = {
    from,
    to,
    options: alchemyTranslate.Options.default(),
    program,
    limits: Limits.default(),
  }
  const key = JSON.stringify([from.id, to.id, program?.text ?? null])
  let compiled = COMPILED.get(key)
  if (undefined === compiled) {
    compiled = compile(request)
    COMPILED.set(key, compiled)
  }
  const out = new BytesWriter()
  runCompiled(request, compiled, text, out, new Metrics())
  // A translation writes UTF-8.
  return UTF8.decode(out.bytes())
}

// What a failure says, for a report: a `Fail`'s code, message and
// position, or what else was thrown.
function why(err: unknown): string {
  return String(err)
}

// ---------------------------------------------------------------------
// Values compared as the conventions compare them
// ---------------------------------------------------------------------

const NULL: Datum = { type: 'null' }

function arr(items: Datum[]): Datum {
  return { type: 'array', items }
}

function obj(members: Iterable<[string, Datum]>): Datum {
  return { type: 'object', members: new Map(members) }
}

// Two values the same: numbers by value (NaN is NaN), objects by their
// members whatever their order, arrays in order.
function same(a: Datum, b: Datum): boolean {
  if ('number' === a.type && 'number' === b.type) {
    return a.value === b.value || (Number.isNaN(a.value) && Number.isNaN(b.value))
  }
  if ('array' === a.type && 'array' === b.type) {
    return a.items.length === b.items.length && a.items.every((x, i) => same(x, b.items[i]))
  }
  if ('object' === a.type && 'object' === b.type) {
    if (a.members.size !== b.members.size) return false
    for (const [k, v] of a.members) {
      const w = b.members.get(k)
      if (undefined === w || !same(v, w)) return false
    }
    return true
  }
  switch (a.type) {
    case 'null':
      return 'null' === b.type
    case 'bool':
      return 'bool' === b.type && a.value === b.value
    case 'string':
      return 'string' === b.type && a.value === b.value
    default:
      return false
  }
}

// The value with every number that is not finite replaced.
function mapNonFinite(d: Datum, f: (n: number) => Datum): Datum {
  switch (d.type) {
    case 'number':
      return Number.isFinite(d.value) ? d : f(d.value)
    case 'array':
      return arr(d.items.map((i) => mapNonFinite(i, f)))
    case 'object':
      return obj([...d.members].map(([k, v]): [string, Datum] => [k, mapNonFinite(v, f)]))
    default:
      return d
  }
}

function wrapObject(d: Datum, key: string): Datum {
  return 'object' === d.type ? d : obj([[key, d]])
}

function wrapArray(d: Datum): Datum {
  return 'array' === d.type ? d : arr([d])
}

// TOML's conventions: no null (a member whose value is null is not
// written, a null element is skipped).
function withoutNulls(d: Datum): Datum {
  switch (d.type) {
    case 'array':
      return arr(d.items.filter((i) => 'null' !== i.type).map(withoutNulls))
    case 'object':
      return obj(
        [...d.members].filter(([, v]) => 'null' !== v.type).map(([k, v]): [string, Datum] => [k, withoutNulls(v)]),
      )
    default:
      return d
  }
}

// A string as Rust's `str::parse::<f64>` reads it: an optional sign, then
// `inf`, `infinity` or `nan` in any case, or digits with an optional point
// and exponent; nothing else, no blanks.
function rustF64(text: string): number | undefined {
  const m = /^([+-]?)(?:(inf|infinity|nan)|((?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?))$/i.exec(text)
  if (null === m) return undefined
  if (undefined !== m[2]) {
    const word = m[2].toLowerCase()
    if ('nan' === word) return Number.NaN
    return '-' === m[1] ? Number.NEGATIVE_INFINITY : Number.POSITIVE_INFINITY
  }
  return Number(m[1] + m[3])
}

// Rust's `char::is_whitespace`: Unicode's White_Space property.
function rustSpace(c: number): boolean {
  return (
    (0x09 <= c && c <= 0x0d) ||
    0x20 === c ||
    0x85 === c ||
    0xa0 === c ||
    0x1680 === c ||
    (0x2000 <= c && c <= 0x200a) ||
    0x2028 === c ||
    0x2029 === c ||
    0x202f === c ||
    0x205f === c ||
    0x3000 === c
  )
}

// Rust's `str::trim`: Unicode's whitespace at either end.
function rustTrim(text: string): string {
  let start = 0
  let end = text.length
  while (start < end && rustSpace(text.charCodeAt(start))) start++
  while (end > start && rustSpace(text.charCodeAt(end - 1))) end--
  return text.slice(start, end)
}

// The text a cell is written as in CSV and Markdown: a string as it is, a
// number by its value (compared as one), a non-finite one by its word, a
// boolean by its name, null and an absent member as the empty field, a
// container as its compact JSON text.
type Cell = { kind: 'text'; text: string } | { kind: 'number'; value: number }

function textCell(text: string): Cell {
  return { kind: 'text', text }
}

function cell(d: Datum | undefined): Cell {
  if (undefined === d || 'null' === d.type) return textCell('')
  switch (d.type) {
    case 'bool':
      return textCell(String(d.value))
    case 'number':
      if (Number.isNaN(d.value)) return textCell('NaN')
      if (!Number.isFinite(d.value)) return textCell(d.value > 0 ? 'Infinity' : '-Infinity')
      return { kind: 'number', value: d.value }
    case 'string':
      return textCell(d.value)
    default:
      return textCell(toText(d))
  }
}

function cellIs(expected: Cell, got: string): boolean {
  if ('text' === expected.kind) return expected.text === got
  const n = rustF64(got)
  return undefined !== n && n === expected.value
}

function cellText(c: Cell): string {
  return 'text' === c.kind ? JSON.stringify(c.text) : String(c.value)
}

// The table the inferred binding makes of a value: the rows are the root
// array's elements (a root of another kind is one row), the columns the
// first row's (an object's keys, an array's positions, or one `value`
// column for a scalar), each row's cell found by the column's path.
function table(d: Datum): { labels: string[]; rows: Cell[][] } {
  const root = wrapArray(d)
  const rows = 'array' === root.type ? root.items : []
  type Path = { key: string } | { index: number } | { itself: true }
  let columns: Array<[string, Path]> = []
  const first = rows[0]
  if (undefined === first) columns = []
  else if ('object' === first.type) columns = [...first.members.keys()].map((k): [string, Path] => [k, { key: k }])
  else if ('array' === first.type) columns = first.items.map((_, i): [string, Path] => [String(i), { index: i }])
  else columns = [['value', { itself: true }]]
  const cells = rows.map((row) =>
    columns.map(([, path]) => {
      if ('key' in path) return cell('object' === row.type ? row.members.get(path.key) : undefined)
      if ('index' in path) return cell('array' === row.type ? row.items[path.index] : undefined)
      return cell(row)
    }),
  )
  return { labels: columns.map(([label]) => label), rows: cells }
}

// Whether a read-back table (an array of objects keyed by label, every
// value a string) is the table the inferred binding makes of `source`,
// with each cell's text passed through `normal` first (Markdown's
// normalisation of what it writes). Undefined when it is; why not when it
// is not.
function checkRecords(source: Datum, back: Datum, normal: (text: string) => string): string | undefined {
  const { labels, rows } = table(source)
  if ('array' !== back.type) return `read back as ${toText(back)}, not an array of records`
  const backRows = back.items
  if (0 === labels.length) {
    return 0 === backRows.length ? undefined : `a table of no columns read back as ${toText(back)}`
  }
  if (backRows.length !== rows.length) {
    return `${backRows.length} rows read back, ${rows.length} written: ${toText(back)}`
  }
  for (let i = 0; i < rows.length; i++) {
    const got = backRows[i]
    if ('object' !== got.type) return `row ${i} read back as ${toText(got)}`
    for (let c = 0; c < labels.length; c++) {
      const label = labels[c]
      // A label is a header cell, written and read back as any cell.
      const found = got.members.get(normal(label))
      const text =
        undefined === found || 'null' === found.type ? '' : 'string' === found.type ? found.value : toText(found)
      const written = rows[i][c]
      const expected: Cell = 'text' === written.kind ? textCell(normal(written.text)) : written
      if (!cellIs(expected, text)) {
        return (
          `row ${i}, column ${JSON.stringify(label)}: read back ${JSON.stringify(text)}, ` +
          `where ${cellText(expected)} was written`
        )
      }
    }
  }
  return undefined
}

// Markdown's normalisation of a written cell: a line break is a space, a
// U+0000 is U+FFFD, and the whitespace at either end is not kept, as the
// reader trims it: what JavaScript's `trim` takes, which is Unicode's
// whitespace without U+0085 and with U+FEFF.
function markdownCell(text: string): string {
  return text.replace(/\r\n/g, ' ').replace(/[\n\r]/g, ' ').replace(/\0/g, '\ufffd').trim()
}

// ---------------------------------------------------------------------
// The cross product
// ---------------------------------------------------------------------

// Whether an INI document read back (`back`) is what INI's conventions
// make of `expected`: an object is a section (or the root) and an array of
// scalars is `key[]` lines, each read back as itself; a number reads back
// as its text, and one that is not finite as its word; a container INI has
// no place for (inside an array, an empty array, or under a key no header
// can spell) reads back as its compact JSON text, a string; true, false
// and null read back as themselves, and a string as itself.
function iniSame(expected: Datum, back: Datum): boolean {
  if ('object' === expected.type && 'object' === back.type) {
    if (expected.members.size !== back.members.size) return false
    for (const [k, v] of expected.members) {
      const w = back.members.get(k) ?? back.members.get(rustTrim(k))
      if (undefined === w || !iniSame(v, w)) return false
    }
    return true
  }
  if ('array' === expected.type && 'array' === back.type && 0 < expected.items.length) {
    return (
      expected.items.length === back.items.length && expected.items.every((x, i) => iniItem(x, back.items[i]))
    )
  }
  if ('number' === expected.type && 'string' === back.type) return numberTextIs(expected.value, back.value)
  if (('object' === expected.type || 'array' === expected.type) && 'string' === back.type) {
    return jsonTextIs(expected, back.value)
  }
  return same(expected, back)
}

// An array item: a scalar as `iniSame` reads it, a container as its JSON
// text.
function iniItem(expected: Datum, back: Datum): boolean {
  if (('object' === expected.type || 'array' === expected.type) && 'string' === back.type) {
    return jsonTextIs(expected, back.value)
  }
  return iniSame(expected, back)
}

// Whether `text` spells the number `value`: its digits, or the word of one
// that is not finite.
function numberTextIs(value: number, text: string): boolean {
  switch (text) {
    case 'Infinity':
      return Number.POSITIVE_INFINITY === value
    case '-Infinity':
      return Number.NEGATIVE_INFINITY === value
    case 'NaN':
      return Number.isNaN(value)
    default: {
      const n = rustF64(text)
      return undefined !== n && n === value
    }
  }
}

function own(o: object, k: string): unknown {
  return Object.prototype.hasOwnProperty.call(o, k) ? (o as Record<string, unknown>)[k] : undefined
}

// Whether `text` is the compact JSON text of `container`, read back as
// JSON and compared as values, a number that is not finite matching null
// or its word.
function jsonTextIs(container: Datum, text: string): boolean {
  let json: unknown
  try {
    json = JSON.parse(text)
  } catch (_err) {
    return false
  }
  const matches = (d: Datum, j: unknown): boolean => {
    if (null === j) return 'null' === d.type || 'number' === d.type
    switch (d.type) {
      case 'bool':
        return 'boolean' === typeof j && d.value === j
      case 'number':
        if ('number' === typeof j) return j === d.value
        return 'string' === typeof j && numberTextIs(d.value, j)
      case 'string':
        return 'string' === typeof j && d.value === j
      case 'array':
        return Array.isArray(j) && d.items.length === j.length && d.items.every((x, i) => matches(x, j[i]))
      case 'object': {
        if ('object' !== typeof j || Array.isArray(j)) return false
        if (d.members.size !== Object.keys(j as object).length) return false
        for (const [k, v] of d.members) {
          if (!Object.prototype.hasOwnProperty.call(j, k) || !matches(v, own(j as object, k))) return false
        }
        return true
      }
      default:
        return false
    }
  }
  return matches(container, json)
}

// Whether a string spells an integer as ZON's reader writes a big
// integer's digits, which is when the render writes a lone `$big` as the
// integer itself: a minus sign at most, and first, then `0` or digits that
// do not begin with `0`, but not `-0`.
function zonBigDigits(s: string): boolean {
  const digits = s.startsWith('-') ? s.slice(1) : s
  return 0 < digits.length && /^[0-9]+$/.test(digits) && ('0' === digits || !digits.startsWith('0')) && '-0' !== s
}

// The integer an object whose only member is `$big` spells, when its
// value is a big integer's digits: ZON's render writes that object as the
// integer, and its reader builds an integer no double holds exactly as
// that object.
function zonBig(d: Datum): Datum | undefined {
  if ('object' !== d.type || 1 !== d.members.size) return undefined
  const big = d.members.get('$big')
  if (undefined === big || 'string' !== big.type || !zonBigDigits(big.value)) return undefined
  return { type: 'number', value: Number(big.value), lexeme: big.value }
}

// What ZON's conventions make of a value it is given: an empty struct
// reads back as an empty tuple, and a lone `$big` holding a big integer's
// digits as that integer.
function zonReading(d: Datum): Datum {
  if ('object' === d.type && 0 === d.members.size) return arr([])
  const big = zonBig(d)
  if (undefined !== big) return big
  if ('array' === d.type) return arr(d.items.map(zonReading))
  if ('object' === d.type) return obj([...d.members].map(([k, v]): [string, Datum] => [k, zonReading(v)]))
  return d
}

// A field name as ZON's render wrote it, read back by the declared reverse
// of its convention: `$empty` is the empty name; `$$` and a rest is `$`
// and the rest; `$json:` and a text is the string the text spells as a
// double-quoted JSON string; any other name is as it is.
function zonName(written: string): string {
  if ('$empty' === written) return ''
  if (written.startsWith('$$')) return '$' + written.slice(2)
  if (written.startsWith('$json:')) {
    try {
      const name = JSON.parse(written.slice('$json:'.length))
      return 'string' === typeof name ? name : written
    } catch (_err) {
      return written
    }
  }
  return written
}

// What ZON's reader made of a document its render wrote, as the value it
// was: a lone `$big` (the reader's big integer) is the integer, and every
// field name reads back by the reverse of the convention that wrote it.
function zonBack(d: Datum): Datum {
  const big = zonBig(d)
  if (undefined !== big) return big
  if ('array' === d.type) return arr(d.items.map(zonBack))
  if ('object' === d.type) return obj([...d.members].map(([k, v]): [string, Datum] => [zonName(k), zonBack(v)]))
  return d
}

// Whether the document read back from `written` in `target` is what the
// target's conventions make of `source`, read as `from`: undefined when it
// is, why not when it is not.
function check(from: Format, target: Format, source: Datum, written: string): string | undefined {
  const limits = Limits.default()
  const id = target.id
  // A source whose events carry the target's own schema (XML's element
  // tree into XML) is written as it is, and reads back as it is.
  const embedded = undefined !== target.part.schema && from.part.schema !== target.part.schema
  let back: Datum
  if ('markdown' === id) {
    // A Markdown table reads back as records through the format's lift, as
    // a host reads it for a records target.
    const lift = target.part.lift
    assert.ok(lift, 'markdown has a lift')
    const program = `${lift.text}\ndef export [input] (records (${lift.entry} input))\n`
    let json: string
    try {
      json = translateText(target, format('yaml'), written, { file: 'markdown-records.alc', text: program })
    } catch (err) {
      return `the written table does not read back: ${why(err)}`
    }
    try {
      back = format('yaml').read(json, limits)
    } catch (err) {
      return `its records do not read back: ${why(err)}`
    }
  } else if ('xml' === id && embedded) {
    // An embedding reads back through its reverse: the element tree, as
    // JSON, unembedded, and written where a non-finite number has a
    // spelling.
    let tree: Datum
    try {
      tree = target.read(written, limits)
    } catch (err) {
      return `the written document does not read back: ${why(err)}`
    }
    const embed = target.part.embed
    assert.ok(embed, 'xml has an embed')
    const program = `${embed.text}\ndef export [input] (xml-unembed input)\n`
    let yaml: string
    try {
      yaml = translateText(format('json'), format('yaml'), toText(tree), { file: 'xml-unembed.alc', text: program })
    } catch (err) {
      return `the element tree does not unembed: ${why(err)}`
    }
    try {
      back = format('yaml').read(yaml, limits)
    } catch (err) {
      return `the unembedded tree does not read back: ${why(err)}`
    }
  } else {
    try {
      back = target.read(written, limits)
    } catch (err) {
      return `the written document does not read back: ${why(err)}`
    }
  }
  const key = alchemyTranslate.Options.default().key
  let expected: Datum
  switch (id) {
    case 'csv':
      return checkRecords(source, back, (t) => t)
    case 'markdown':
      return checkRecords(source, back, markdownCell)
    case 'ini': {
      const wrapped = wrapObject(source, key)
      return iniSame(wrapped, back) ? undefined : `read back as ${toText(back)}, where ${toText(wrapped)} was written`
    }
    case 'json':
    case 'jsonc':
    case 'jsonic':
      expected = mapNonFinite(source, () => NULL)
      break
    case 'jsonl':
      expected = mapNonFinite(wrapArray(source), () => NULL)
      break
    case 'toml':
      expected = withoutNulls(wrapObject(source, key))
      break
    case 'zon':
      expected = zonReading(source)
      break
    default:
      expected = source
  }
  const read = 'zon' === id ? zonBack(back) : back
  return same(expected, read) ? undefined : `read back as ${toText(read)}, where ${toText(expected)} was written`
}

// How deep a value nests: a scalar is 0, a container one more than its
// deepest member.
function depth(d: Datum): number {
  if ('array' === d.type) return 1 + d.items.reduce((m, i) => Math.max(m, depth(i)), 0)
  if ('object' === d.type) return 1 + [...d.members.values()].reduce((m, v) => Math.max(m, depth(v)), 0)
  return 0
}

// The nesting every format reads: the readers guard nesting at different
// depths (tabnas-json past 128 levels, YAML's and ZON's near it, the
// transducer at 256 events deep, which XML's embedding reaches at about
// 127 levels, two elements a level), and a root adapter or an embedding
// adds a level or two. A document nested deeper than this is at one
// format's guard and past another's, a limit and not a shape, so the
// matrix leaves it out, and pins how few such documents there are.
const DEPTH_BOUND = 100

// The cross product of `docs` and every format: each document read with
// its format's grammar, written in every format, and read back under the
// target's conventions. At least `floor` documents, and at most
// `tooDeepAtMost` of them deeper than every format reads.
function matrix(docs: Doc[], floor: number, tooDeepAtMost: number): void {
  const limits = Limits.default()
  const targets = formats()
  assert.equal(targets.length, 12, `the formats: ${names()}`)
  const total = docs.length * targets.length
  const failures: string[] = []
  const refusedSources: string[] = []
  const tooDeep: string[] = []
  let pairs = 0
  let reported = Date.now()
  docs.forEach(({ name, id, text }, n) => {
    const from = format(id)
    let source: Datum
    try {
      source = from.read(text, limits)
    } catch (err) {
      refusedSources.push(`${name}: ${why(err)}`)
      return
    }
    if (depth(source) > DEPTH_BOUND) {
      tooDeep.push(`${name}: ${depth(source)} levels`)
      return
    }
    for (const to of targets) {
      pairs += 1
      let failure: string | undefined
      let written: string | undefined
      try {
        written = translateText(from, to, text)
      } catch (err) {
        failure = `does not write: ${why(err)}`
      }
      if (undefined !== written) {
        // A records source read through its lift writes its table, not its
        // tree: compare with the table.
        const lifted = 'records' === from.part.reads[0] && 'records' === to.part.writes
        if (lifted) {
          try {
            to.read(written, limits)
          } catch (err) {
            failure = `the written document does not read back: ${why(err)}`
          }
        } else {
          try {
            failure = check(from, to, source, written)
          } catch (err) {
            failure = `the check failed: ${why(err)}`
          }
        }
      }
      if (undefined !== failure) failures.push(`${name} (${from.id}) -> ${to.id}: ${failure}`)
    }
    if ((n + 1) % 25 === 0 || n + 1 === docs.length || Date.now() - reported > 20_000) {
      reported = Date.now()
      process.stderr.write(
        `matrix: ${n + 1} of ${docs.length} documents (${Math.floor(((n + 1) * 100) / docs.length)}%), ` +
          `${failures.length} failures\n`,
      )
    }
  })
  for (const line of refusedSources) process.stderr.write(`refused source: ${line}\n`)
  for (const line of tooDeep) process.stderr.write(`deeper than every format reads: ${line}\n`)
  for (const line of failures) process.stderr.write(`FAIL ${line}\n`)
  process.stderr.write(
    `matrix: ${pairs} pairs of ${docs.length} documents; ${refusedSources.length} refused by their own ` +
      `reader, ${tooDeep.length} too deep\n`,
  )
  assert.ok(
    pairs + (refusedSources.length + tooDeep.length) * targets.length === total && docs.length >= floor,
    `the corpus shrank: ${docs.length} documents`,
  )
  assert.ok(tooDeep.length <= tooDeepAtMost, `${tooDeep.length} documents are deeper than every format reads (above)`)
  assert.ok(0 === failures.length, `${failures.length} of ${pairs} pairs failed (above)`)
}

describe('translate', () => {
  // transduce's fixtures, one document per format at least, and the
  // documents of JSONTestSuite every JSON parser must accept.
  it('every document translates into every format', () => {
    matrix(corpus(), 120, 0)
  })
})
