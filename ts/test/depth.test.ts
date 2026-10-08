/* Copyright (c) 2026 tabnas, MIT License */

// The recursive walks are safe at the nesting the reader allows, in
// Node's default stack (AGENTS.md, "Untrusted input"): MAX_NESTING (256)
// levels bound what the printers, the desugarer, the resolver and the
// checker recurse over, and the checker follows a stream into at most
// MAX_APPLIED bodies, each of them that deep. These run the worst
// programs those bounds admit through every stage of the front end.
//
// Evaluation nests at most MAX_EVAL_DEPTH (1,000) levels, `recursion`
// past it, which a function applied to itself reaches. The Rust crate
// reaches the bound before the stack's end only on a thread of 64 MiB;
// here the evaluator runs on an explicit stack (`./trampoline`), so the
// bound is reached in Node's default stack, as a `Fail`, never as a
// `RangeError`, through every path that nests evaluation: a body inside
// the call that applied it, a native that applies a function, a finite
// text written as it applies its function, a chain of definitions; at
// compile time and per item. The boundaries pinned below are the Rust
// binary's, compared program for program: depth is counted as it counts
// it.
//
// Moved from alchemy's ts/test/depth.test.ts, which keeps the tests that
// stop at compile time: the ones here run a program on transduce's routers
// and render's renderers, which alchemy does not depend on.

import { describe, it } from 'node:test'
import assert from 'node:assert'

import { Limits } from '@tabnas/alchemy/shared'

import { compile, drive, err, finer, ok, thrown } from './host'

describe('depth', () => {
  // Per item, at run time: the map stage applies a function that applies
  // itself, and the item fails with `recursion`, nothing written.
  it('self-application per item is recursion at run time', () => {
    const src = 'def w [f] (f f)\ndef export [input]\n  pipe input\n    select (path each-index)\n    map (fn [x] (w w))\n    join ","\n'
    const { fail, out } = err(compile(src, 'omega.alc'), '[1,2]')
    assert.equal(finer(fail), 'recursion')
    assert.equal(out, '')
    // A finite text whose concat-map answers a text that applies it again
    // recurses as it is written, not as it is built: the writer takes a
    // level per node, and the bound ends it there.
    const text = 'def t [f] (concat-map (fn [x] (f f)) ["a"])\ndef export [input] (concat (t t) (json input))\n'
    const program = compile(text, 'text.alc')
    const written = err(program, '[1]')
    assert.equal(finer(written.fail), 'recursion')
    // Interpreted as natively.
    assert.equal(finer(err(program.withNative(false), '[1]').fail), 'recursion')
  })

  // The deepest chains the bound admits, and one link more: a chain of
  // definitions each applying the next nests evaluation two or three
  // levels a link. Each boundary is the Rust binary's. The deepest plan
  // each makes then runs: a pipeline of 499 map stages, a live text under
  // 499 replace-text and 499 concat frames, a finite text of 333 nested
  // joins.
  it('the deepest chains the bound admits build and run', () => {
    const chains: Array<[string, (n: number) => string, number, string, string]> = [
      [
        'map stages',
        (n) => {
          let s = 'def m0 [s] (map (fn [x] x) s)\n'
          for (let i = 1; i < n; i++) s += `def m${i} [s] (map (fn [x] x) (m${i - 1} s))\n`
          return s + `def export [input] (join "," (m${n - 1} (select (path each-index) input)))\n`
        },
        499,
        '["a","b"]',
        'a,b',
      ],
      [
        'replace-text layers',
        (n) => {
          let s = 'def r0 [t] (replace-text "a" "b" t)\n'
          for (let i = 1; i < n; i++) s += `def r${i} [t] (replace-text "a" "b" (r${i - 1} t))\n`
          return s + `def export [input] (r${n - 1} (json input))\n`
        },
        499,
        '["a","ba"]',
        '["b","bb"]\n',
      ],
      [
        'concat frames',
        (n) => {
          let s = 'def c0 [t] (concat "<" t ">")\n'
          for (let i = 1; i < n; i++) s += `def c${i} [t] (concat "<" (c${i - 1} t) ">")\n`
          return s + `def export [input] (c${n - 1} (json input))\n`
        },
        499,
        '1',
        `${'<'.repeat(499)}1\n${'>'.repeat(499)}`,
      ],
      [
        'nested joins',
        (n) => {
          let s = 'def j0 "x"\n'
          for (let i = 1; i < n; i++) s += `def j${i} (join "," [j${i - 1}])\n`
          return s + `def export [input] j${n - 1}\n`
        },
        333,
        'null',
        'x',
      ],
    ]
    for (const [name, chain, deepest, doc, want] of chains) {
      const program = compile(chain(deepest), `${name}.alc`)
      assert.equal(ok(program, doc), want, name)
      assert.equal(ok(program.withNative(false), doc), want, name)
      const over = thrown(() => compile(chain(deepest + 1), `${name}.alc`))
      assert.equal(finer(over), 'recursion', `${name}: ${over}`)
    }
  })

  // The evaluator needs a few frames of JavaScript's stack however deep
  // evaluation goes: a host that calls in with half the default stack
  // already used still meets the bound as `recursion`, and a program at
  // the bound still builds.
  it('the bound holds with half the stack already used', () => {
    let available = 0
    const probe = (n: number): void => {
      available = n
      probe(n + 1)
    }
    try {
      probe(0)
    } catch (_e) {
      // The default stack's depth, in frames of this size.
    }
    const at = <T>(used: number, work: () => T): T => (used <= 0 ? work() : at(used - 1, work))
    const half = Math.floor(available / 2)
    const omega = 'def w [f] (f f)\ndef export [input]\n  let [x (w w)]\n    json input\n'
    const fail = at(half, () => thrown(() => compile(omega, 'omega.alc')))
    assert.equal(finer(fail), 'recursion')
    let s = 'def m0 [s] (map (fn [x] x) s)\n'
    for (let i = 1; i < 499; i++) s += `def m${i} [s] (map (fn [x] x) (m${i - 1} s))\n`
    s += 'def export [input] (join "," (m498 (select (path each-index) input)))\n'
    const program = compile(s, 'maps.alc')
    assert.equal(at(half, () => ok(program.withNative(false), '["a"]')), 'a')
    // And per item, under a small output limit as a host would set one.
    const { fail: perItem } = at(half, () =>
      drive(compile('def w [f] (f f)\ndef export [input] (join "" (map (fn [x] (w w)) (select (path each-index) input)))', 'x.alc'), '[1]', undefined, Limits.with({ max_output_bytes: 10 })),
    )
    assert.equal(finer(perItem), 'recursion')
  })
})
