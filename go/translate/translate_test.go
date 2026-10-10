// Copyright (c) 2026 tabnas, MIT License

package translate_test

// translate_test.go: the cross product `alchemy translate` answers for
// (rs/tests/translate_test.rs): every document of the corpora, read with
// its format's grammar, written in every format the command carries, read
// back with that format's grammar, and compared with the document's own
// value under the target's declared conventions (its loss list). A pair
// that fails to write, that writes a document its own grammar refuses, or
// that reads back as anything but the conventions say is a failure, and so
// is a corpus that shrinks.
//
// The corpora are the sibling checkouts': transduce's fixtures (aless's,
// one document per format at least, and more for YAML and ZON) and the
// documents of JSONTestSuite every JSON parser must accept (jsonc's
// conformance pins). A fixture its own grammar refuses is no document, and
// is counted as one refused. The Rust suite's cross product of every
// format's own fixture corpus, which it runs in release, stays Rust's.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/tabnas/alchemy-cli/go/translate"
	alchemy "github.com/tabnas/alchemy/go"
	at "github.com/tabnas/alchemy/go/translate"
	tt "github.com/tabnas/transduce/go"
)

// siblings is the directory the sibling checkouts are in: the repository
// root's parent.
func siblings(t testing.TB) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// formatOf is the format a fixture's extension names, by its id.
func formatOf(extension string) string {
	switch extension {
	case "json", "json5", "jsonc", "jsonic", "jsonl", "csv", "toml", "ini", "xml", "yaml", "zon":
		return extension
	case "md":
		return "markdown"
	}
	return ""
}

// extension is a file name's extension as Rust's Path::extension reads it:
// what follows the last dot, and none for a name that begins with its only
// dot.
func extension(name string) string {
	ext := filepath.Ext(name)
	if ext == name {
		return ""
	}
	return strings.TrimPrefix(ext, ".")
}

// document is one document of a corpus: its name, its format and its text.
type document struct {
	name, format, text string
}

// corpus is every document of the corpora.
func corpus(t testing.TB) []document {
	t.Helper()
	var docs []document
	for _, c := range []struct{ corpus, dir, prefix string }{
		{"transduce", filepath.Join(siblings(t), "transduce", "rs", "tests", "fixtures"), ""},
		{"JSONTestSuite", filepath.Join(siblings(t), "jsonc", "test", "JSONTestSuite", "test_parsing"), "y_"},
	} {
		entries, err := os.ReadDir(c.dir)
		if err != nil {
			t.Fatalf("%s: cannot read %s: %v", c.corpus, c.dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasPrefix(name, c.prefix) {
				continue
			}
			id := formatOf(extension(name))
			if id == "" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(c.dir, name))
			if err != nil || !utf8.Valid(data) {
				continue
			}
			docs = append(docs, document{c.corpus + "/" + name, id, string(data)})
		}
	}
	return docs
}

func format(t testing.TB, id string) *translate.Format {
	t.Helper()
	f := translate.Named(id)
	if f == nil {
		t.Fatalf("%s is a format", id)
	}
	return f
}

// compiledKey is a composition's source, target and program.
type compiledKey struct {
	from, to   string
	program    string
	hasProgram bool
}

// compiled is the compositions compiled so far, by source, target and
// program: one compiled composition serves every document of a pair.
var compiled = map[compiledKey]*translate.Compiled{}

// translateText translates text, read as from, into to, with program in
// front when there is one.
func translateText(from, to *translate.Format, text string, program *alchemy.Source) (string, *tt.Fail) {
	request := &translate.Request{
		From:    from,
		To:      to,
		Options: at.DefaultOptions(),
		Program: program,
		Limits:  tt.DefaultLimits(),
	}
	key := compiledKey{from: from.ID(), to: to.ID()}
	if program != nil {
		key.program, key.hasProgram = program.Text, true
	}
	c, ok := compiled[key]
	if !ok {
		var f *tt.Fail
		if c, f = translate.Compile(request); f != nil {
			return "", f
		}
		compiled[key] = c
	}
	var out bytes.Buffer
	if f := translate.RunCompiled(request, c, text, &out, tt.NewMetrics()); f != nil {
		return "", f
	}
	return out.String(), nil
}

// ---------------------------------------------------------------------
// Values compared as the conventions compare them
// ---------------------------------------------------------------------

// same is whether two values are the same: numbers by value (NaN is NaN),
// objects by their members whatever their order, arrays in order.
func same(a, b *tt.Datum) bool {
	switch {
	case a.Kind == tt.DatumNumber && b.Kind == tt.DatumNumber:
		return a.Value == b.Value || (math.IsNaN(a.Value) && math.IsNaN(b.Value))
	case a.Kind == tt.DatumArray && b.Kind == tt.DatumArray:
		if len(a.Items) != len(b.Items) {
			return false
		}
		for i := range a.Items {
			if !same(&a.Items[i], &b.Items[i]) {
				return false
			}
		}
		return true
	case a.Kind == tt.DatumObject && b.Kind == tt.DatumObject:
		if len(a.Members) != len(b.Members) {
			return false
		}
		for i := range a.Members {
			w, ok := b.Get(a.Members[i].Key)
			if !ok || !same(&a.Members[i].Value, w) {
				return false
			}
		}
		return true
	}
	return a.Equal(*b)
}

// mapNonFinite is the value with every number that is not finite
// replaced.
func mapNonFinite(d tt.Datum, f func(float64) tt.Datum) tt.Datum {
	switch {
	case d.Kind == tt.DatumNumber && (math.IsInf(d.Value, 0) || math.IsNaN(d.Value)):
		return f(d.Value)
	case d.Kind == tt.DatumArray:
		items := make([]tt.Datum, len(d.Items))
		for i, item := range d.Items {
			items[i] = mapNonFinite(item, f)
		}
		return tt.ArrayDatum(items...)
	case d.Kind == tt.DatumObject:
		members := make([]tt.Member, len(d.Members))
		for i, m := range d.Members {
			members[i] = tt.Member{Key: m.Key, Value: mapNonFinite(m.Value, f)}
		}
		return tt.ObjectDatum(members...)
	}
	return d
}

func wrapObject(d tt.Datum, key string) tt.Datum {
	if d.Kind == tt.DatumObject {
		return d
	}
	return tt.ObjectDatum(tt.Member{Key: key, Value: d})
}

func wrapArray(d tt.Datum) tt.Datum {
	if d.Kind == tt.DatumArray {
		return d
	}
	return tt.ArrayDatum(d)
}

// withoutNulls is TOML's conventions: no null (a member whose value is
// null is not written, a null element is skipped).
func withoutNulls(d tt.Datum) tt.Datum {
	switch d.Kind {
	case tt.DatumArray:
		items := []tt.Datum{}
		for _, item := range d.Items {
			if item.Kind != tt.DatumNull {
				items = append(items, withoutNulls(item))
			}
		}
		return tt.ArrayDatum(items...)
	case tt.DatumObject:
		members := []tt.Member{}
		for _, m := range d.Members {
			if m.Value.Kind != tt.DatumNull {
				members = append(members, tt.Member{Key: m.Key, Value: withoutNulls(m.Value)})
			}
		}
		return tt.ObjectDatum(members...)
	}
	return d
}

// rustFloat is the grammar Rust's f64 parse takes: a sign at most, then
// inf, infinity or nan in any case, or decimal digits with a point and an
// exponent at most; no blanks, underscores or hexadecimal.
var rustFloat = regexp.MustCompile(`^[+-]?(?:(?i:inf|infinity|nan)|(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?)$`)

// parseF64 is text read as Rust's `str::parse::<f64>` reads it, which the
// Rust suite compares a cell's text with: an overflow is an infinity, not
// a failure.
func parseF64(text string) (float64, bool) {
	if !rustFloat.MatchString(text) {
		return 0, false
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil && !math.IsInf(value, 0) {
		return math.NaN(), true
	}
	return value, true
}

// cell is the text a cell is written as in CSV and Markdown: a string as
// it is, a number by its value (compared as one), a non-finite one by its
// word, a boolean by its name, null and an absent member as the empty
// field, a container as its compact JSON text.
type cell struct {
	text     string
	number   float64
	isNumber bool
}

func cellOf(d *tt.Datum) cell {
	switch {
	case d == nil || d.Kind == tt.DatumNull:
		return cell{}
	case d.Kind == tt.DatumBool:
		return cell{text: strconv.FormatBool(d.Bool)}
	case d.Kind == tt.DatumNumber && math.IsNaN(d.Value):
		return cell{text: "NaN"}
	case d.Kind == tt.DatumNumber && math.IsInf(d.Value, 1):
		return cell{text: "Infinity"}
	case d.Kind == tt.DatumNumber && math.IsInf(d.Value, -1):
		return cell{text: "-Infinity"}
	case d.Kind == tt.DatumNumber:
		return cell{number: d.Value, isNumber: true}
	case d.Kind == tt.DatumString:
		return cell{text: d.Text}
	}
	return cell{text: d.String()}
}

func (c cell) is(got string) bool {
	if !c.isNumber {
		return c.text == got
	}
	g, ok := parseF64(got)
	return ok && g == c.number
}

func (c cell) String() string {
	if c.isNumber {
		return strconv.FormatFloat(c.number, 'g', -1, 64)
	}
	return strconv.Quote(c.text)
}

// table is the table the inferred binding makes of a value: the rows are
// the root array's elements (a root of another kind is one row), the
// columns the first row's (an object's keys, an array's positions, or one
// value column for a scalar), each row's cell found by the column's path.
func table(d tt.Datum) ([]string, [][]cell) {
	rows := wrapArray(d).Items
	// A column's path: a member's key, an element's index, or the row
	// itself.
	const (
		byKey = iota
		byIndex
		itself
	)
	type column struct {
		label string
		path  int
		key   string
		index int
	}
	var columns []column
	if len(rows) > 0 {
		switch first := rows[0]; first.Kind {
		case tt.DatumObject:
			for _, m := range first.Members {
				columns = append(columns, column{label: m.Key, path: byKey, key: m.Key})
			}
		case tt.DatumArray:
			for i := range first.Items {
				columns = append(columns, column{label: strconv.Itoa(i), path: byIndex, index: i})
			}
		default:
			columns = append(columns, column{label: "value", path: itself})
		}
	}
	cells := make([][]cell, len(rows))
	for r := range rows {
		row := &rows[r]
		for _, c := range columns {
			var found *tt.Datum
			switch c.path {
			case byKey:
				if row.Kind == tt.DatumObject {
					found, _ = row.Get(c.key)
				}
			case byIndex:
				if row.Kind == tt.DatumArray && c.index < len(row.Items) {
					found = &row.Items[c.index]
				}
			default:
				found = row
			}
			cells[r] = append(cells[r], cellOf(found))
		}
	}
	labels := make([]string, len(columns))
	for i, c := range columns {
		labels[i] = c.label
	}
	return labels, cells
}

// checkRecords is whether a read-back table (an array of objects keyed by
// label, every value a string) is the table the inferred binding makes of
// source, with each cell's text passed through cellText first (Markdown's
// normalisation of what it writes).
func checkRecords(source, back tt.Datum, cellText func(string) string) error {
	labels, rows := table(source)
	if back.Kind != tt.DatumArray {
		return fmt.Errorf("read back as %s, not an array of records", back)
	}
	backRows := back.Items
	if len(labels) == 0 {
		if len(backRows) == 0 {
			return nil
		}
		return fmt.Errorf("a table of no columns read back as %s", back)
	}
	if len(backRows) != len(rows) {
		return fmt.Errorf("%d rows read back, %d written: %s", len(backRows), len(rows), back)
	}
	for i, row := range rows {
		got := backRows[i]
		if got.Kind != tt.DatumObject {
			return fmt.Errorf("row %d read back as %s", i, got)
		}
		for j, label := range labels {
			// A label is a header cell, written and read back as any cell.
			text := ""
			if v, ok := got.Get(cellText(label)); ok {
				switch v.Kind {
				case tt.DatumString:
					text = v.Text
				case tt.DatumNull:
				default:
					text = v.String()
				}
			}
			expected := row[j]
			if !expected.isNumber {
				expected.text = cellText(expected.text)
			}
			if !expected.is(text) {
				return fmt.Errorf("row %d, column %q: read back %q, where %s was written", i, label, text, expected)
			}
		}
	}
	return nil
}

// markdownCell is Markdown's normalisation of a written cell: a line break
// is a space, a U+0000 is U+FFFD, and the whitespace at either end is not
// kept, as the reader trims it: what JavaScript's trim takes, which is
// Unicode's whitespace without U+0085 and with U+FEFF.
func markdownCell(text string) string {
	text = strings.ReplaceAll(text, "\r\n", " ")
	text = strings.NewReplacer("\n", " ", "\r", " ", "\x00", "\uFFFD").Replace(text)
	return strings.TrimFunc(text, func(c rune) bool {
		return (unicode.IsSpace(c) && c != '\u0085') || c == '\uFEFF'
	})
}

// ---------------------------------------------------------------------
// The cross product
// ---------------------------------------------------------------------

// iniSame is whether an INI document read back (back) is what INI's
// conventions make of expected: an object is a section (or the root) and
// an array of scalars is key[] lines, each read back as itself; a number
// reads back as its text, and one that is not finite as its word; a
// container INI has no place for (inside an array, an empty array, or
// under a key no header can spell) reads back as its compact JSON text, a
// string; true, false and null read back as themselves, and a string as
// itself.
func iniSame(expected, back *tt.Datum) bool {
	switch {
	case expected.Kind == tt.DatumObject && back.Kind == tt.DatumObject:
		if len(expected.Members) != len(back.Members) {
			return false
		}
		for i := range expected.Members {
			k := expected.Members[i].Key
			w, ok := back.Get(k)
			if !ok {
				w, ok = back.Get(strings.TrimFunc(k, unicode.IsSpace))
			}
			if !ok || !iniSame(&expected.Members[i].Value, w) {
				return false
			}
		}
		return true
	case expected.Kind == tt.DatumArray && back.Kind == tt.DatumArray && len(expected.Items) > 0:
		if len(expected.Items) != len(back.Items) {
			return false
		}
		for i := range expected.Items {
			if !iniItem(&expected.Items[i], &back.Items[i]) {
				return false
			}
		}
		return true
	case expected.Kind == tt.DatumNumber && back.Kind == tt.DatumString:
		return numberTextIs(expected.Value, back.Text)
	case expected.IsContainer() && back.Kind == tt.DatumString:
		return jsonTextIs(expected, back.Text)
	}
	return same(expected, back)
}

// iniItem is an array item: a scalar as iniSame reads it, a container as
// its JSON text.
func iniItem(expected, back *tt.Datum) bool {
	if expected.IsContainer() && back.Kind == tt.DatumString {
		return jsonTextIs(expected, back.Text)
	}
	return iniSame(expected, back)
}

// numberTextIs is whether text spells the number value: its digits, or
// the word of one that is not finite.
func numberTextIs(value float64, text string) bool {
	switch text {
	case "Infinity":
		return math.IsInf(value, 1)
	case "-Infinity":
		return math.IsInf(value, -1)
	case "NaN":
		return math.IsNaN(value)
	}
	n, ok := parseF64(text)
	return ok && n == value
}

// readJSON is text read as one JSON value, as the Rust suite reads it with
// serde_json: numbers kept as text, nothing after the value but blanks, and
// a number past the largest double refused.
func readJSON(text string) (any, bool) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false
	}
	var finite func(any) bool
	finite = func(v any) bool {
		switch v := v.(type) {
		case json.Number:
			f, err := strconv.ParseFloat(v.String(), 64)
			return err == nil && !math.IsInf(f, 0)
		case []any:
			for _, item := range v {
				if !finite(item) {
					return false
				}
			}
		case map[string]any:
			for _, item := range v {
				if !finite(item) {
					return false
				}
			}
		}
		return true
	}
	return value, finite(value)
}

// jsonTextIs is whether text is the compact JSON text of container, read
// back as JSON and compared as values, a number that is not finite
// matching null or its word.
func jsonTextIs(container *tt.Datum, text string) bool {
	value, ok := readJSON(text)
	if !ok {
		return false
	}
	var matches func(d *tt.Datum, j any) bool
	matches = func(d *tt.Datum, j any) bool {
		switch j := j.(type) {
		case nil:
			return d.Kind == tt.DatumNull || d.Kind == tt.DatumNumber
		case bool:
			return d.Kind == tt.DatumBool && d.Bool == j
		case json.Number:
			f, err := strconv.ParseFloat(j.String(), 64)
			return d.Kind == tt.DatumNumber && err == nil && f == d.Value
		case string:
			switch d.Kind {
			case tt.DatumNumber:
				return numberTextIs(d.Value, j)
			case tt.DatumString:
				return d.Text == j
			}
			return false
		case []any:
			if d.Kind != tt.DatumArray || len(d.Items) != len(j) {
				return false
			}
			for i := range d.Items {
				if !matches(&d.Items[i], j[i]) {
					return false
				}
			}
			return true
		case map[string]any:
			if d.Kind != tt.DatumObject || len(d.Members) != len(j) {
				return false
			}
			for i := range d.Members {
				w, ok := j[d.Members[i].Key]
				if !ok || !matches(&d.Members[i].Value, w) {
					return false
				}
			}
			return true
		}
		return false
	}
	return matches(container, value)
}

// zonBigDigits is whether a string spells an integer as ZON's reader
// writes a big integer's digits, which is when the render writes a lone
// $big as the integer itself: a minus sign at most, and first, then 0 or
// digits that do not begin with 0, but not -0.
func zonBigDigits(s string) bool {
	digits := strings.TrimPrefix(s, "-")
	if digits == "" || s == "-0" {
		return false
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return false
		}
	}
	return digits == "0" || digits[0] != '0'
}

// zonBig is the integer an object whose only member is $big spells, when
// its value is a big integer's digits: ZON's render writes that object as
// the integer, and its reader builds an integer no double holds exactly as
// that object.
func zonBig(d *tt.Datum) (tt.Datum, bool) {
	if d.Kind != tt.DatumObject || len(d.Members) != 1 {
		return tt.Datum{}, false
	}
	v, ok := d.Get("$big")
	if !ok || v.Kind != tt.DatumString || !zonBigDigits(v.Text) {
		return tt.Datum{}, false
	}
	value, ok := parseF64(v.Text)
	if !ok {
		return tt.Datum{}, false
	}
	return tt.NumberDatumLexeme(value, v.Text), true
}

// zonReading is what ZON's conventions make of a value it is given: an
// empty struct reads back as an empty tuple, and a lone $big holding a big
// integer's digits as that integer.
func zonReading(d tt.Datum) tt.Datum {
	switch d.Kind {
	case tt.DatumObject:
		if len(d.Members) == 0 {
			return tt.ArrayDatum()
		}
		if big, ok := zonBig(&d); ok {
			return big
		}
		members := make([]tt.Member, len(d.Members))
		for i, m := range d.Members {
			members[i] = tt.Member{Key: m.Key, Value: zonReading(m.Value)}
		}
		return tt.ObjectDatum(members...)
	case tt.DatumArray:
		items := make([]tt.Datum, len(d.Items))
		for i, item := range d.Items {
			items[i] = zonReading(item)
		}
		return tt.ArrayDatum(items...)
	}
	return d
}

// zonName is a field name as ZON's render wrote it, read back by the
// declared reverse of its convention: $empty is the empty name; $$ and a
// rest is $ and the rest; $json: and a text is the string the text spells
// as a double-quoted JSON string; any other name is as it is.
func zonName(written string) string {
	switch {
	case written == "$empty":
		return ""
	case strings.HasPrefix(written, "$$"):
		return "$" + written[2:]
	case strings.HasPrefix(written, "$json:"):
		var s string
		if err := json.Unmarshal([]byte(written[len("$json:"):]), &s); err != nil {
			return written
		}
		return s
	}
	return written
}

// zonBack is what ZON's reader made of a document its render wrote, as
// the value it was: a lone $big (the reader's big integer) is the integer,
// and every field name reads back by the reverse of the convention that
// wrote it.
func zonBack(d tt.Datum) tt.Datum {
	if big, ok := zonBig(&d); ok {
		return big
	}
	switch d.Kind {
	case tt.DatumArray:
		items := make([]tt.Datum, len(d.Items))
		for i, item := range d.Items {
			items[i] = zonBack(item)
		}
		return tt.ArrayDatum(items...)
	case tt.DatumObject:
		members := make([]tt.Member, len(d.Members))
		for i, m := range d.Members {
			members[i] = tt.Member{Key: zonName(m.Key), Value: zonBack(m.Value)}
		}
		return tt.ObjectDatum(members...)
	}
	return d
}

// check is whether the document read back from written in target is what
// the target's conventions make of source, read as from.
func check(t testing.TB, from, target *translate.Format, source tt.Datum, written string) error {
	limits := tt.DefaultLimits()
	id := target.ID()
	// A source whose events carry the target's own schema (XML's element
	// tree into XML) is written as it is, and reads back as it is.
	embedded := target.Part.Schema != "" && from.Part.Schema != target.Part.Schema
	var back tt.Datum
	switch {
	case id == "markdown":
		// A Markdown table reads back as records through the format's
		// lift, as a host reads it for a records target.
		lift := target.Part.Lift
		if lift == nil {
			t.Fatal("markdown has a lift")
		}
		program := lift.Text + "\ndef export [input] (records (" + lift.Entry + " input))\n"
		yaml, f := translateText(target, format(t, "yaml"), written, &alchemy.Source{File: "markdown-records.alc", Text: program})
		if f != nil {
			return fmt.Errorf("the written table does not read back: %v", f)
		}
		if back, f = format(t, "yaml").Read(yaml, limits); f != nil {
			return fmt.Errorf("its records do not read back: %v", f)
		}
	case id == "xml" && embedded:
		// An embedding reads back through its reverse: the element tree, as
		// JSON, unembedded, and written where a non-finite number has a
		// spelling.
		tree, f := target.Read(written, limits)
		if f != nil {
			return fmt.Errorf("the written document does not read back: %v", f)
		}
		embed := target.Part.Embed
		if embed == nil {
			t.Fatal("xml has an embed")
		}
		program := embed.Text + "\ndef export [input] (xml-unembed input)\n"
		yaml, f := translateText(format(t, "json"), format(t, "yaml"), tree.String(), &alchemy.Source{File: "xml-unembed.alc", Text: program})
		if f != nil {
			return fmt.Errorf("the element tree does not unembed: %v", f)
		}
		if back, f = format(t, "yaml").Read(yaml, limits); f != nil {
			return fmt.Errorf("the unembedded tree does not read back: %v", f)
		}
	default:
		var f *tt.Fail
		if back, f = target.Read(written, limits); f != nil {
			return fmt.Errorf("the written document does not read back: %v", f)
		}
	}
	key := at.DefaultOptions().Key
	null := func(float64) tt.Datum { return tt.NullDatum() }
	var expected tt.Datum
	switch id {
	case "csv":
		return checkRecords(source, back, func(t string) string { return t })
	case "markdown":
		return checkRecords(source, back, markdownCell)
	case "ini":
		expected := wrapObject(source, key)
		if iniSame(&expected, &back) {
			return nil
		}
		return fmt.Errorf("read back as %s, where %s was written", back, expected)
	case "json", "jsonc", "jsonic":
		expected = mapNonFinite(source, null)
	case "jsonl":
		expected = mapNonFinite(wrapArray(source), null)
	case "toml":
		expected = withoutNulls(wrapObject(source, key))
	case "zon":
		expected = zonReading(source)
		back = zonBack(back)
	default:
		expected = source
	}
	if same(&expected, &back) {
		return nil
	}
	return fmt.Errorf("read back as %s, where %s was written", back, expected)
}

// depth is how deep a value nests: a scalar is 0, a container one more
// than its deepest member.
func depth(d *tt.Datum) int {
	deepest := 0
	switch d.Kind {
	case tt.DatumArray:
		for i := range d.Items {
			deepest = max(deepest, depth(&d.Items[i]))
		}
	case tt.DatumObject:
		for i := range d.Members {
			deepest = max(deepest, depth(&d.Members[i].Value))
		}
	default:
		return 0
	}
	return 1 + deepest
}

// depthBound is the nesting every format reads: the readers guard nesting
// at different depths (tabnas-json past 128 levels, YAML's and ZON's near
// it, the transducer at 256 events deep, which XML's embedding reaches at
// about 127 levels, two elements a level), and a root adapter or an
// embedding adds a level or two. A document nested deeper than this is at
// one format's guard and past another's, a limit and not a shape, so the
// matrix leaves it out, and pins how few such documents there are.
const depthBound = 100

// divergent is the pairs that fail in this runtime, and not in Rust's, for
// a defect outside this repository: by the name the matrix gives a pair,
// each with its defect. The matrix holds each to failing, so an entry
// cannot outlive the defect it records: one that passes, or that names no
// pair of the corpus, fails the test until it is deleted. There is none.
var divergent = map[string]string{}

// matrix is the cross product of docs and every format: each document read
// with its format's grammar, written in every format, and read back under
// the target's conventions. At least floor documents, and at most
// tooDeepAtMost of them deeper than every format reads.
func matrix(t *testing.T, docs []document, floor, tooDeepAtMost int) {
	limits := tt.DefaultLimits()
	targets := translate.Formats()
	if len(targets) != 12 {
		t.Fatalf("the formats: %s", translate.Names())
	}
	total := len(docs) * len(targets)
	var failures, refusedSources, tooDeep, diverged, repaired []string
	met := map[string]bool{}
	pairs := 0
	for n, doc := range docs {
		from := format(t, doc.format)
		source, f := from.Read(doc.text, limits)
		if f != nil {
			refusedSources = append(refusedSources, fmt.Sprintf("%s: %v", doc.name, f))
			continue
		}
		if d := depth(&source); d > depthBound {
			tooDeep = append(tooDeep, fmt.Sprintf("%s: %d levels", doc.name, d))
			continue
		}
		for _, to := range targets {
			pairs++
			written, f := translateText(from, to, doc.text, nil)
			var why error
			switch {
			case f != nil:
				why = fmt.Errorf("does not write: %v", f)
			// A records source read through its lift writes its table, not
			// its tree: compare with the table.
			case from.Part.Reads[0] == at.ShapeRecords && to.Part.Writes == at.ShapeRecords:
				if _, f := to.Read(written, limits); f != nil {
					why = fmt.Errorf("the written document does not read back: %v", f)
				}
			default:
				why = check(t, from, to, source, written)
			}
			name := fmt.Sprintf("%s (%s) -> %s", doc.name, from.ID(), to.ID())
			_, known := divergent[name]
			if known {
				met[name] = true
			}
			switch {
			case why != nil && known:
				diverged = append(diverged, fmt.Sprintf("%s: %v", name, why))
			case why != nil:
				failures = append(failures, fmt.Sprintf("%s: %v", name, why))
			case known:
				repaired = append(repaired, name)
			}
		}
		if (n+1)%25 == 0 || n+1 == len(docs) {
			t.Logf("matrix: %d of %d documents (%d%%), %d failures", n+1, len(docs), (n+1)*100/len(docs), len(failures))
		}
	}
	for _, line := range refusedSources {
		t.Logf("refused source: %s", line)
	}
	for _, line := range tooDeep {
		t.Logf("deeper than every format reads: %s", line)
	}
	for _, line := range diverged {
		t.Logf("divergent, as registered: %s", line)
	}
	for _, line := range failures {
		t.Logf("FAIL %s", line)
	}
	t.Logf("matrix: %d pairs of %d documents; %d refused by their own reader, %d too deep, %d divergent as registered",
		pairs, len(docs), len(refusedSources), len(tooDeep), len(diverged))
	if pairs+(len(refusedSources)+len(tooDeep))*len(targets) != total || len(docs) < floor {
		t.Fatalf("the corpora shrank: %d documents", len(docs))
	}
	if len(tooDeep) > tooDeepAtMost {
		t.Fatalf("%d documents are deeper than every format reads (above)", len(tooDeep))
	}
	for name := range divergent {
		if !met[name] {
			t.Errorf("the registered divergent pair %q is no pair of the corpus: delete its entry", name)
		}
	}
	for _, name := range repaired {
		t.Errorf("the registered divergent pair %q translates as the conventions say: delete its entry", name)
	}
	if len(failures) > 0 {
		t.Fatalf("%d of %d pairs failed (above)", len(failures), pairs)
	}
}

// A path is a JSON array of keys and whole numbers: an index is the
// number's value, however it is spelled, up to 2^64, where it saturates
// as Rust's float-to-integer cast does; anything else is refused with the
// text as given.
func TestParsePathReadsKeysAndIndexes(t *testing.T) {
	limits := tt.DefaultLimits()
	for _, c := range []struct{ text, want string }{
		{`[]`, `[]`},
		{`["people",0,"name"]`, `["people",0,"name"]`},
		{`[1e2,-0,0.0]`, `[100,0,0]`},
		{`[9007199254740993]`, `[9007199254740992]`},
		{`[1e19]`, `[10000000000000000000]`},
		{`[18446744073709551616]`, `[18446744073709551615]`},
	} {
		path, f := translate.ParsePath(c.text, limits)
		if f != nil || path.String() != c.want {
			t.Errorf("%s: %v %v", c.text, path, f)
		}
	}
	for _, text := range []string{`{}`, `"a"`, `[1.5]`, `[-1]`, `[true]`, `[null]`, `[[0]]`, `[1e300]`, ``, `[1`} {
		path, f := translate.ParsePath(text, limits)
		if f == nil || f.Code != tt.CodeInputInvalid ||
			f.Message != `--path takes a JSON array of keys and indexes, such as ["people",0], not `+text {
			t.Errorf("%q: %v %v", text, path, f)
		}
	}
}

// TestEveryDocumentTranslatesIntoEveryFormat is transduce's fixtures, one
// document per format at least, and the documents of JSONTestSuite every
// JSON parser must accept, into every format.
func TestEveryDocumentTranslatesIntoEveryFormat(t *testing.T) {
	matrix(t, corpus(t), 120, 0)
}

// request is a request from from to to, with no path and no program.
func request(from, to *translate.Format) *translate.Request {
	return &translate.Request{From: from, To: to, Options: at.DefaultOptions(), Limits: tt.DefaultLimits()}
}

// ZON's reader builds an integer no float64 holds exactly as a *big.Int,
// which this command reads as the object {"$big": "<digits>"}, the digits
// after a minus sign when it is negative, as ZON's part declares and its
// Rust reader builds; ZON's render writes it back as the integer.
func TestAZonIntegerNoFloatHoldsIsTheBigObject(t *testing.T) {
	zon, json := format(t, "zon"), format(t, "json")
	text := ".{ 1, 12345678901234567890, -12345678901234567890, 0xc1ce108124179e16 }\n"
	want := `[1,{"$big":"12345678901234567890"},{"$big":"-12345678901234567890"},{"$big":"13965117641364839958"}]`
	got, f := translateText(zon, json, text, nil)
	if f != nil {
		t.Fatal(f)
	}
	if strings.TrimRight(got, "\n") != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	back, f := translateText(zon, zon, text, nil)
	if f != nil {
		t.Fatal(f)
	}
	if got, f = translateText(zon, json, back, nil); f != nil || strings.TrimRight(got, "\n") != want {
		t.Fatalf("read back from %q: got %s (%v), want %s", back, got, f, want)
	}
}

// The metrics a run is given are its own, whether the incremental attempt
// wrote the output or one it gave up was read again whole.
func TestARunsMetricsAreThoseOfTheAttemptThatWroteTheOutput(t *testing.T) {
	json := format(t, "json")
	for _, text := range []string{`{"a": [1, 2]}`, `{"a": 1, "a": 2}`} {
		var out bytes.Buffer
		metrics := tt.NewMetrics()
		if f := translate.Run(request(json, json), text, &out, metrics); f != nil {
			t.Fatalf("%s: %v", text, f)
		}
		if out.Len() == 0 || metrics.OutputBytes.Load() != uint64(out.Len()) || metrics.Events.Load() == 0 {
			t.Errorf("%s: wrote %d bytes; the metrics count %d output bytes and %d events",
				text, out.Len(), metrics.OutputBytes.Load(), metrics.Events.Load())
		}
	}
}

// takes is a writer that takes n bytes, then fails.
type takes struct{ n int }

func (w *takes) Write(p []byte) (int, error) {
	n := min(w.n, len(p))
	w.n -= n
	if n < len(p) {
		return n, errors.New("the disk is full")
	}
	return n, nil
}

// A writer that fails once it has taken some of the output: the failure
// says the output had left, and says it had not when it took none.
func TestAnOutputFailureAfterBytesLeftIsCommitted(t *testing.T) {
	json := format(t, "json")
	for _, c := range []struct {
		takes     int
		committed bool
	}{{0, false}, {3, true}} {
		f := translate.Run(request(json, json), "[1, 2, 3]", &takes{c.takes}, tt.NewMetrics())
		if f == nil || f.Code != tt.CodeOutputFailed || f.CommittedOutput != c.committed {
			t.Errorf("taking %d bytes: %v", c.takes, f)
		}
	}
}
