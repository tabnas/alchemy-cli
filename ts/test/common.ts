/* Copyright (c) 2026 tabnas, MIT License */

// What the command's tests share with alchemy's own (alchemy's
// test/common.ts, where these are defined first): the spec's worked
// example, its expected CSV and the spec's program.

// The spec's worked example, byte for byte as aless's fixture has it
// (329 bytes; the metadata before the rows; Bob's members in another
// order; `50.25` and `72` as written).
export const RECORDS =
  '{"response":{"metadata":{"fields":[{"title":"Identifier","path":["id"]},{"title":"Full name","path":["person","name"]},{"title":"Balance","path":["account","balance"]}]},"payload":{"deep":{"records":[{"id":123,"person":{"name":"Alice"},"account":{"balance":50.25}},{"account":{"balance":72},"person":{"name":"Bob"},"id":456}]}}}}'

export const EXPECTED_CSV = '"Identifier","Full name","Balance"\r\n"123","Alice","50.25"\r\n"456","Bob","72"\r\n'

// The spec's program (sections 12.1 and 13.4).
export const PROGRAM =
  'def column-from-meta [source]\n  record\n    entry :label (get "title" source)\n    entry :source\n      as-path\n        get "path" source\n\ndef api-binding\n  record\n    entry :columns\n      path "response" "metadata" "fields"\n    entry :rows\n      path "response" "payload" "deep" "records" each-index\n    entry :column column-from-meta\n\ndef api-table [input]\n  table-from-json api-binding input\n\ndef export [input]\n  pipe input\n    api-table\n    csv csv-options\n'
