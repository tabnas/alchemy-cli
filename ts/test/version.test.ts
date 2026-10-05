/* Copyright (c) 2026 tabnas, MIT License */

// The exported VERSION must equal package.json "version". A release that
// bumps one and forgets the other fails here instead of shipping a package
// that misstates its own version. Mirrors go/cmd/alchemy/version_test.go and
// rs/tests/version_test.rs.

import { describe, it } from 'node:test'
import assert from 'node:assert'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

// The package as a consumer loads it: ts/, whose main is dist/cli.js. This
// file runs from ts/dist-test, one level below.
const api: { VERSION: unknown } = require('..')
const pkg: { name: string; version: string } = JSON.parse(
  readFileSync(join(__dirname, '..', 'package.json'), 'utf8'),
)

describe('version', () => {
  it('VERSION matches package.json', () => {
    assert.equal(
      api.VERSION,
      pkg.version,
      `VERSION drift: ${pkg.name} exports ${String(api.VERSION)} but package.json is ` +
        `${pkg.version}. Both are rewritten by admin/publish.sh at release; ` +
        `if you bumped one by hand, bump the other.`,
    )
  })

  it('VERSION is exported and looks like a semver', () => {
    assert.equal(typeof api.VERSION, 'string', 'VERSION must be exported as a string')
    assert.match(String(api.VERSION), /^\d+\.\d+\.\d+$/, 'VERSION must be x.y.z')
  })
})
