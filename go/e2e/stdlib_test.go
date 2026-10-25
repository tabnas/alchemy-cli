// Copyright (c) 2026 tabnas, MIT License

package e2e

// stdlib_test.go: the standard library's text on transduce's routers and
// render's renderers (rs/tests/stdlib_test.rs): the one implementation of
// table-from-json and csv (the ruling of 2026-10-24: nothing native stands
// in for the text), over every transduce fixture a grammar these tests can
// read, the generated documents transduce's tests and benches share, and
// the documents below. Where an oracle exists the text is held to it: the
// spec's worked-example program writes the bytes transduce's table
// transducer and render's CSV renderer write for the same binding, or
// fails with their code, and the library's csv refuses what the renderer
// refuses. The parts, transduce-tree and transduce-records, are
// transduce_tree_test.go's.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	. "github.com/tabnas/alchemy/go"
	csv "github.com/tabnas/csv/go"
	tabnasjson "github.com/tabnas/json/go"
	tabnasjsonl "github.com/tabnas/jsonl/go"
	tabnas "github.com/tabnas/parser/go"
	tr "github.com/tabnas/render/go"
	tt "github.com/tabnas/transduce/go"
	tabnasyaml "github.com/tabnas/yaml/go"
)

// replay replays events through program under the default limits: the
// output, or the failure.
func replay(program *Program, events []tt.Event, render Renderer) (string, *Fail) {
	return replayEvents(program, events, render, tt.DefaultLimits(), tt.NewMetrics())
}

// recordsSelector is where the worked example's records are.
func recordsSelector() tt.Selector {
	return tt.Root().Property("response").Property("payload").Property("deep").Property("records").EachIndex()
}

// components is what the library's text is held to: transduce's table
// transducer over the worked example's binding (its columns from the
// metadata, or inferred from the first row) and render's CSV renderer in
// its default dialect, the events replayed through the source's guard; the
// bytes, or the failure.
func components(events []tt.Event, limits tt.Limits, infer bool) (string, *Fail) {
	var buffer bytes.Buffer
	renderer, f := tr.NewCSVRenderer(tr.NewWriteOut(&buffer), tr.DefaultCSVOptions())
	if f != nil {
		return "", f
	}
	schema := tt.InferSchema()
	if !infer {
		schema = tt.MetadataSchema(tt.Root().Property("response").Property("metadata").Property("fields"), tt.ColumnFromMeta)
	}
	table, f := tt.NewTableFromJSON(tt.TableBinding{Schema: schema, Rows: recordsSelector()}, limits, tt.Reject, tt.NewMetrics(), renderer)
	if f != nil {
		return "", f
	}
	if _, f := tt.Replay(events, tt.NewGuarded(table, limits, tt.NewAbortFlag(), tt.NewMetrics())); f != nil {
		return "", f
	}
	return buffer.String(), nil
}

// agreesWithComponents runs one document through the library's text and
// through the components: the same bytes, or the same code; a report of
// the difference otherwise.
func agreesWithComponents(name string, program *Program, events []tt.Event, infer bool) string {
	a, fa := replay(program, events, RenderDefault)
	b, fb := components(events, tt.DefaultLimits(), infer)
	switch {
	case fa == nil && fb == nil && a == b:
		return ""
	case fa == nil && fb == nil:
		return fmt.Sprintf("%s: the bytes differ\n  text:       %q\n  components: %q", name, a, b)
	case fa != nil && fb != nil && fa.Code == fb.Code:
		return ""
	case fa != nil && fb != nil:
		return fmt.Sprintf("%s: the codes differ\n  text:       %v\n  components: %v", name, fa, fb)
	case fa == nil:
		return fmt.Sprintf("%s: the text produced %q, the components failed: %v", name, a, fb)
	}
	return fmt.Sprintf("%s: the components produced %q, the text failed: %v", name, b, fa)
}

// testDocument is a document to compare: its name, the grammar that reads
// it, its text.
type testDocument struct {
	name string
	make func() *tabnas.Tabnas
	text string
}

func jsonGrammar() *tabnas.Tabnas  { return tabnasjson.Make() }
func jsonlGrammar() *tabnas.Tabnas { return tabnasjsonl.Make() }
func yamlGrammar() *tabnas.Tabnas  { return tabnasyaml.MakeJsonic() }
func csvGrammar() *tabnas.Tabnas {
	j, err := csv.Make()
	if err != nil {
		panic(err)
	}
	return j
}

// Every transduce fixture a grammar here reads, plus the generated
// documents in every shape: the text writes the components' bytes, or
// fails with their code.
func TestTheLibraryWritesWhatTheComponentsWriteForEveryFixtureAndGeneratedDocument(t *testing.T) {
	program := mustCompile(t, workedExample, "export.alc")
	var documents []testDocument
	for _, path := range fixtures(t) {
		make := fixtureGrammar(path)
		if make == nil {
			continue
		}
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		documents = append(documents, testDocument{path, make, string(text)})
	}
	for _, n := range []int{0, 1, 2, 3, 50} {
		documents = append(documents, testDocument{fmt.Sprintf("records_json(%d)", n), jsonGrammar, recordsJSON(n)})
	}
	documents = append(documents,
		testDocument{"records_yaml(3)", yamlGrammar, recordsYAML(3)},
		testDocument{"records_jsonl(3)", jsonlGrammar, recordsJSONL(3)},
		testDocument{"records_csv(3)", csvGrammar, recordsCSV(3)},
	)
	// Cells of every kind, in the worked example's shape: null, missing
	// members (a failure under the standard options), nested containers,
	// booleans, quotes, delimiters, line breaks and non-ASCII text.
	for _, c := range []struct{ name, records string }{
		{"nulls", `{"id":null,"person":{"name":null},"account":{"balance":null}}`},
		{"missing", `{"id":1,"person":{},"account":{"balance":2}}`},
		{"containers", `{"id":[1,2.50],"person":{"name":{"first":"A","last":"B"}},"account":{"balance":{}}}`},
		{"booleans", `{"id":true,"person":{"name":false},"account":{"balance":0}}`},
		{"quotes", `{"id":"say \"hi\"","person":{"name":"a,b"},"account":{"balance":"line\r\nbreak"}}`},
		{"unicode", `{"id":"caf\u00e9","person":{"name":"日本"},"account":{"balance":"\ud83d\ude00"}}`},
		{"lexemes", `{"id":1e2,"person":{"name":"x"},"account":{"balance":-0.0}}`},
		{"big", `{"id":12345678901234567890123,"person":{"name":"x"},"account":{"balance":1E+2}}`},
		{"two rows out of order", `{"account":{"balance":1},"id":2,"person":{"name":"z"}},{"id":3,"person":{"name":"y"},"account":{"balance":4}}`},
	} {
		documents = append(documents, testDocument{c.name, jsonGrammar, shaped(c.records)})
	}
	// The order contract and the absent shapes.
	documents = append(documents,
		testDocument{"metadata after rows", jsonGrammar, `{"response":{"payload":{"deep":{"records":[` + record(1) + `]}},"metadata":` + metadata + `}}`},
		testDocument{"no metadata", jsonGrammar, `{"response":{"payload":{"deep":{"records":[` + record(1) + `]}}}}`},
		testDocument{"no records", jsonGrammar, `{"response":{"metadata":` + metadata + `}}`},
		testDocument{"metadata not an array", jsonGrammar, `{"response":{"metadata":{"fields":{}}}}`},
		testDocument{"descriptor with a bad segment", jsonGrammar, `{"response":{"metadata":{"fields":[{"title":"a","path":[true]}]}}}`},
		testDocument{"row that is not an object", jsonGrammar, shaped("1")},
	)
	total := len(documents)
	var failures []string
	skipped := 0
	for i, doc := range documents {
		if i%10 == 0 {
			t.Logf("sweep: %d of %d (%d%%)", i, total, i*100/total)
		}
		events, f := recordEvents(doc.make(), doc.text)
		if f != nil {
			// The grammar refused the document: nothing reached a program.
			skipped++
			continue
		}
		if report := agreesWithComponents(doc.name, program, events, false); report != "" {
			failures = append(failures, report)
		}
	}
	t.Logf("sweep: %d of %d (100%%), %d unreadable", total, total, skipped)
	if total-skipped <= 20 {
		t.Errorf("only %d documents were read", total-skipped)
	}
	if len(failures) > 0 {
		t.Errorf("%d document(s) differ:\n%s", len(failures), strings.Join(failures, "\n"))
	}
}

// The other renderings the text offers: the table as records, rendered as
// JSON (records then json), and a csv whose options record names another
// dialect, which the text writes as named.
func TestTheOtherRenderingsWriteAsTheirProgramsSay(t *testing.T) {
	program := mustCompile(t, strings.Replace(workedExample, "    csv csv-options\n", "    records\n    json\n", 1), "records.alc")
	dialect := mustCompile(t, strings.Replace(workedExample, "    csv csv-options\n",
		"    csv options\n\ndef options\n  record\n    entry :delimiter \";\"\n    entry :newline \"\\n\"\n    entry :header false\n    entry :null-text \"NULL\"\n    entry :missing \"-\"\n", 1), "dialect.alc")
	three := mustEvents(t, recordsJSON(3))
	out, f := replay(program, three, RenderDefault)
	if f != nil {
		t.Fatal(f)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 3 || rows[2]["Full name"] != "Person number 2" {
		t.Errorf("%q %v", out, err)
	}
	csvOut, f := replay(dialect, three, RenderDefault)
	if f != nil {
		t.Fatal(f)
	}
	lines := strings.Split(strings.TrimSuffix(csvOut, "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("%q", csvOut)
	}
	for _, line := range lines {
		if strings.Count(line, ";") != 2 {
			t.Errorf("%q", csvOut)
		}
	}
	sparse := mustEvents(t, `{"response":{"metadata":`+metadata+`,"payload":{"deep":{"records":[{"id":null,"person":{},"account":{"balance":"a;b"}}]}}}}`)
	if out, f := replay(dialect, sparse, RenderDefault); f != nil || out != "\"NULL\";\"-\";\"a;b\"\n" {
		t.Errorf("%q %v", out, f)
	}
	// A missing cell is no member of the record.
	if out, f := replay(program, sparse, RenderDefault); f != nil || out != "[{\"Identifier\":null,\"Balance\":\"a;b\"}]\n" {
		t.Errorf("%q %v", out, f)
	}
}

// outcome is what a run must come to: the bytes, or the code.
type outcome struct {
	ok   bool
	out  string
	code Code
}

func okOut(out string) outcome  { return outcome{ok: true, out: out} }
func errCode(code Code) outcome { return outcome{code: code} }

// expect runs program over events under limits to one outcome: the bytes,
// or the code.
func expect(t testing.TB, name string, program *Program, events []tt.Event, limits tt.Limits, expected outcome) {
	t.Helper()
	out, f := replayEvents(program, events, RenderDefault, limits, tt.NewMetrics())
	switch {
	case f == nil && expected.ok:
		if out != expected.out {
			t.Errorf("%s:\n  got:  %q\n  want: %q", name, out, expected.out)
		}
	case f != nil && !expected.ok:
		if f.Code != expected.code {
			t.Errorf("%s: want %s, got %v", name, expected.code, f)
		}
	case f == nil:
		t.Errorf("%s: want %s, got %q", name, expected.code, out)
	default:
		t.Errorf("%s: want %q, got %v", name, expected.out, f)
	}
}

// The library's csv validates the table events it renders as render's CSV
// renderer does (spec 13.2: the protocol validator is not omitted), so a
// program that produces its own table events, or a document or an options
// record the renderer refuses, fails with the renderer's code, and a sound
// one prints the renderer's bytes.
func TestTheLibraryCsvValidatesWhatTheRendererValidates(t *testing.T) {
	items := mustEvents(t, `{"items":[{"n":"a","v":1},{"n":"b\"q","v":-2},{"n":"c","v":null}],"tail":"t"}`)
	limits := tt.DefaultLimits()
	label := func(l string) string { return `(record (entry :label "` + l + `"))` }
	table := func(step, finish string) string {
		return "def export [input] (csv csv-options (scan-emit no-schema " + step + " " + finish + " (select (path \"items\" each-index) input)))"
	}
	// One schema on the first item, a row per item: the sound shape.
	first := func(schema, row string) string {
		return "(fn [s x] (transition (ready []) (if (is-ready s) [(row " + row + ")] [(schema " + schema + ") (row " + row + ")])))"
	}
	n, v := label("N"), label("V")
	for _, c := range []struct {
		name, src string
		want      outcome
	}{
		{"sound", table(first("["+n+" "+v+"]", "[(get :n x) (get :v x)]"), "(fn [s] [table-end])"),
			okOut("\"N\",\"V\"\r\n\"a\",\"1\"\r\n\"b\"\"q\",\"-2\"\r\n\"c\",\"\"\r\n")},
		{"rows without a schema", table("(fn [s x] (transition s [(row [(get :n x)])]))", "(fn [s] [table-end])"), errCode(CodeProtocolOrderError)},
		{"a schema per item", table("(fn [s x] (transition s [(schema ["+n+"])]))", "(fn [s] [table-end])"), errCode(CodeProtocolOrderError)},
		{"a row wider than the schema", table(first("["+n+"]", "[(get :n x) (get :v x)]"), "(fn [s] [table-end])"), errCode(CodeProtocolOrderError)},
		{"no table-end", table(first("["+n+"]", "[(get :n x)]"), "(fn [s] [])"), errCode(CodeProtocolOrderError)},
		{"two table-ends", table(first("["+n+"]", "[(get :n x)]"), "(fn [s] [table-end table-end])"), errCode(CodeProtocolOrderError)},
		{"an item that is not a table event", "def export [input] (csv csv-options (map (fn [x] (get :n x)) (select (path \"items\" each-index) input)))", errCode(CodeProtocolOrderError)},
		{"no items at all", "def export [input] (csv csv-options (scan-emit no-schema (fn [s x] (transition s [x])) (fn [s] []) (select (path \"none\" each-index) input)))", errCode(CodeProtocolOrderError)},
		{"a schema of no columns", table(first("[]", "[]"), "(fn [s] [table-end])"), errCode(CodeTargetValueUnrepresentable)},
		{"a column without a label", table(first(`[(record (entry :x "N"))]`, "[(get :n x)]"), "(fn [s] [table-end])"), errCode(CodeMissingValue)},
		{"a label that is a record", table(first("[(record (entry :label (record)))]", "[(get :n x)]"), "(fn [s] [table-end])"), errCode(CodeInputInvalid)},
	} {
		program, f := Compile(c.src, "table.alc", routers, renderers)
		if f != nil {
			t.Fatalf("%s: %v", c.name, f)
		}
		expect(t, c.name, program, items, limits, c.want)
	}

	// The worked example over a document whose metadata is empty: a table
	// of no columns has no CSV form.
	program := mustCompile(t, workedExample, "export.alc")
	empty := mustEvents(t, `{"response":{"metadata":{"fields":[]},"payload":{"deep":{"records":[{"id":1}]}}}}`)
	expect(t, "no columns", program, empty, limits, errCode(CodeTargetValueUnrepresentable))

	// A delimiter no CSV reader could take is refused before anything runs
	// (csv-table, as the renderer refuses it).
	recordEventsList := mustEvents(t, records)
	for _, delimiter := range []string{`\"`, `\n`, `\r`} {
		src := strings.Replace(workedExample, "    csv csv-options\n",
			"    csv (record (entry :delimiter \""+delimiter+"\") (entry :newline \"\\r\\n\") (entry :header true) (entry :null-text \"\") (entry :missing :error))\n", 1)
		expect(t, "delimiter "+delimiter, mustCompile(t, src, "delimiter.alc"), recordEventsList, limits, errCode(CodeTargetValueUnrepresentable))
	}

	// A column function that answers something other than a record fails
	// as get does.
	keyword := mustCompile(t, "def b (record (entry :columns (path \"m\")) (entry :rows (path \"r\" each-index)) (entry :column (fn [d] :oops)))\ndef export [input] (csv csv-options (table-from-json b input))", "keyword.alc")
	doc := mustEvents(t, `{"m":[{"title":"t","path":["a"]}],"r":[{"a":"x"}]}`)
	expect(t, "a column that is a keyword", keyword, doc, limits, errCode(CodeDSLTypeError))
}

// The library's table holds the scopes it captures to the host's limits
// (the metadata under max_metadata_bytes, each row under max_record_bytes,
// at most max_columns columns), so under the host's own limits it fails
// naming the limit, or prints, and counts its rows.
func TestTheLimitsHold(t *testing.T) {
	program := mustCompile(t, workedExample, "export.alc")
	events := mustEvents(t, records)
	with := func(change func(*tt.Limits)) tt.Limits {
		l := tt.DefaultLimits()
		change(&l)
		return l
	}
	for _, c := range []struct {
		name   string
		limits tt.Limits
		want   outcome
	}{
		{"defaults", tt.DefaultLimits(), okOut(expectedCSV)},
		{"max_record_bytes", with(func(l *tt.Limits) { l.MaxRecordBytes = 100 }), errCode(CodeResourceLimitExceeded)},
		{"max_metadata_bytes", with(func(l *tt.Limits) { l.MaxMetadataBytes = 50 }), errCode(CodeResourceLimitExceeded)},
		{"max_columns", with(func(l *tt.Limits) { l.MaxColumns = 2 }), errCode(CodeResourceLimitExceeded)},
		{"max_capture_bytes decides neither table", with(func(l *tt.Limits) { l.MaxCaptureBytes = 100 }), okOut(expectedCSV)},
	} {
		expect(t, c.name, program, events, c.limits, c.want)
	}
	// A cell that is a vector is its compact JSON text, one scalar of the
	// output, held to max_scalar_bytes.
	containers := mustEvents(t, strings.Replace(records, `"id":123`, `"id":[1,2,3,4,5,6]`, 1))
	expect(t, "a container cell under max_scalar_bytes", program, containers,
		with(func(l *tt.Limits) { l.MaxScalarBytes = 12 }), errCode(CodeResourceLimitExceeded))
	expect(t, "a container cell within it", program, containers,
		with(func(l *tt.Limits) { l.MaxScalarBytes = 13 }),
		okOut("\"Identifier\",\"Full name\",\"Balance\"\r\n\"[1,2,3,4,5,6]\",\"Alice\",\"50.25\"\r\n\"456\",\"Bob\",\"72\"\r\n"))
	metrics := tt.NewMetrics()
	if _, f := replayEvents(program, events, RenderDefault, tt.DefaultLimits(), metrics); f != nil {
		t.Fatal(f)
	}
	if metrics.Rows.Load() != 2 {
		t.Errorf("%d rows", metrics.Rows.Load())
	}
}

// inferred is a program over an inferred binding (:columns :infer): a root
// array of rows, written as CSV with a missing cell as an empty field.
const inferred = "def options\n  record\n    entry :delimiter \",\"\n    entry :newline \"\\r\\n\"\n    entry :header true\n    entry :null-text \"\"\n    entry :missing \"\"\n\ndef rows-binding\n  record\n    entry :columns :infer\n    entry :rows (path each-index)\n\ndef export [input]\n  pipe input\n    table-from-json rows-binding\n    csv options\n"

// The inferred binding: the columns are the first row's keys, in its
// order, each reading its own key; the library's text infers them with
// keys, over every shape a first row and a later row can take, and holds
// them to the limits. Over the worked example's documents it writes what
// transduce's InferSchema and render's CSV renderer write.
func TestAnInferredBindingInfersTheFirstRowsColumns(t *testing.T) {
	program := mustCompile(t, inferred, "inferred.alc")
	ok := func(name, text, want string) {
		expect(t, name, program, mustEvents(t, text), tt.DefaultLimits(), okOut(want))
	}
	fails := func(name, text string, code Code) {
		expect(t, name, program, mustEvents(t, text), tt.DefaultLimits(), errCode(code))
	}
	ok("the first row's keys, in its order", `[{"b":1,"a":"x"},{"a":"y","b":2}]`, "\"b\",\"a\"\r\n\"1\",\"x\"\r\n\"2\",\"y\"\r\n")
	ok("a key a later row lacks is a missing cell", `[{"a":1,"b":2},{"a":3}]`, "\"a\",\"b\"\r\n\"1\",\"2\"\r\n\"3\",\"\"\r\n")
	ok("a key only a later row has is no column", `[{"a":1},{"c":3,"a":2}]`, "\"a\"\r\n\"1\"\r\n\"2\"\r\n")
	ok("cells of every kind", `[{"s":"q\"x","n":1.5,"t":true,"z":null,"o":{"k":[1,2]}}]`,
		"\"s\",\"n\",\"t\",\"z\",\"o\"\r\n\"q\"\"x\",\"1.5\",\"true\",\"\",\"{\"\"k\"\":[1,2]}\"\r\n")
	ok("a key that spells an index names a member", `[{"0":"zero","1":"one"}]`, "\"0\",\"1\"\r\n\"zero\",\"one\"\r\n")
	ok("a later row that is not an object has missing cells", `[{"a":1},2]`, "\"a\"\r\n\"1\"\r\n\"\"\r\n")
	// A first row of another kind than an object: a scalar is one `value`
	// column, the row itself; an array's columns are its positions. A later
	// row projects through the first row's paths, so an object under
	// positional columns is a missing cell, and under a `value` column its
	// compact JSON text.
	ok("a first row that is a scalar", `[1,{"a":1}]`, "\"value\"\r\n\"1\"\r\n\"{\"\"a\"\":1}\"\r\n")
	ok("a first row that is an array", `[[1,"x"],{"a":1},[2]]`, "\"0\",\"1\"\r\n\"1\",\"x\"\r\n\"\",\"\"\r\n\"2\",\"\"\r\n")
	// A table of no columns, from no rows or from an empty first row, is
	// one the CSV renderer refuses, as it refuses any.
	fails("no rows", "[]", CodeTargetValueUnrepresentable)
	fails("an empty first row", `[{},{"a":1}]`, CodeTargetValueUnrepresentable)
	three := mustEvents(t, `[{"a":1,"b":2,"c":3}]`)
	with := func(change func(*tt.Limits)) tt.Limits {
		l := tt.DefaultLimits()
		change(&l)
		return l
	}
	expect(t, "max_columns holds the inferred columns", program, three, with(func(l *tt.Limits) { l.MaxColumns = 2 }), errCode(CodeResourceLimitExceeded))
	// Three one-byte names take 16 + 3 * (16 + 1) = 67 bytes as the labels'
	// array; the library's state holding them is larger still.
	expect(t, "max_metadata_bytes holds the inferred columns", program, three, with(func(l *tt.Limits) { l.MaxMetadataBytes = 40 }), errCode(CodeResourceLimitExceeded))
	expect(t, "max_record_bytes holds each row", program, three, with(func(l *tt.Limits) { l.MaxRecordBytes = 8 }), errCode(CodeResourceLimitExceeded))
	// The column count holds where the schema is built, so it holds when
	// the table events reach no renderer that would check them.
	for _, c := range []struct{ name, binding string }{
		{"an inferred table's events as items", "(record (entry :columns :infer) (entry :rows (path each-index)))"},
		{"a described table's events as items", "(record (entry :columns (path \"m\")) (entry :rows (path \"r\" each-index)) (entry :column (fn [d] (record (entry :label d) (entry :source (path d))))))"},
	} {
		items := mustCompile(t, "def b "+c.binding+"\ndef export [input] (join \"\" (map (fn [e] \"x\") (table-from-json b input)))", "items.alc")
		events := three
		if !strings.HasPrefix(c.name, "an inferred") {
			events = mustEvents(t, `{"m":["a","b","c"],"r":[{"a":1,"b":2,"c":3}]}`)
		}
		expect(t, c.name, items, events, with(func(l *tt.Limits) { l.MaxColumns = 2 }), errCode(CodeResourceLimitExceeded))
		expect(t, c.name, items, events, tt.DefaultLimits(), okOut("xxx"))
	}
	// The worked example's documents, bound by inference from their rows.
	api := mustCompile(t, strings.Replace(inferred, "(path each-index)", "(path \"response\" \"payload\" \"deep\" \"records\" each-index)", 1), "inferred-api.alc")
	for _, n := range []int{0, 1, 2, 3, 50} {
		if report := agreesWithComponents(fmt.Sprintf("records_json(%d)", n), api, mustEvents(t, recordsJSON(n)), true); report != "" {
			t.Error(report)
		}
	}
}

// Each row counts once in metrics.Rows, by the last table stage it passes:
// the adapter to a renderer, or a csv-table whose rows reach no later
// table stage. A map, a filter or a scan-emit between two table stages
// hands the count on, so a row a filter drops is never counted, and rows
// that only ever become a text of items are not rows of any table.
func TestEachRowCountsOnceAcrossComposedTableStages(t *testing.T) {
	tail := func(s string) string { return strings.Replace(workedExample, "    csv csv-options\n", s, 1) }
	identity := "    map (fn [e] e)\n"
	dropRows := "    filter (fn [e] (match e (case (row cells) false) (case _ true)))\n"
	passScan := "    scan-emit null (fn [s e] (transition s [e])) (fn [s] []) \n"
	asText := "    map (fn [e] \"x\")\n    join \",\"\n"
	events := mustEvents(t, records)
	for _, c := range []struct {
		name   string
		src    string
		render Renderer
		rows   uint64
	}{
		{"csv", tail("    csv csv-options\n"), RenderDefault, 2},
		{"csv-table as the result", tail("    csv-table csv-options\n"), RenderDefault, 2},
		{"csv-table as the result, as json", tail("    csv-table csv-options\n"), RenderJSON, 2},
		{"csv over csv-table", tail("    csv-table csv-options\n    csv csv-options\n"), RenderDefault, 2},
		{"records over csv-table", tail("    csv-table csv-options\n    records\n    json\n"), RenderDefault, 2},
		{"csv-table twice", tail("    csv-table csv-options\n    csv-table csv-options\n    csv csv-options\n"), RenderDefault, 2},
		{"the library csv with its own dialect", tail("    csv opts\n\ndef opts\n  record\n    entry :delimiter \"||\"\n    entry :newline \"\\r\\n\"\n    entry :header true\n    entry :null-text \"\"\n    entry :missing :error\n"), RenderDefault, 2},
		{"a map, then the program's csv", tail(identity + "    csv csv-options\n"), RenderDefault, 2},
		{"a map, csv-table, csv", tail(identity + "    csv-table csv-options\n    csv csv-options\n"), RenderDefault, 2},
		{"csv-table, a map, csv", tail("    csv-table csv-options\n" + identity + "    csv csv-options\n"), RenderDefault, 2},
		{"a map, the host's renderer", tail(identity), RenderCSV, 2},
		{"a map, the host's json", tail(identity), RenderJSON, 2},
		{"a filter that keeps every row, the host's renderer", tail("    filter (fn [e] true)\n"), RenderCSV, 2},
		{"a scan that passes each event on, then csv", tail(passScan + "    csv csv-options\n"), RenderDefault, 2},
		{"a filter that drops every row, then csv", tail(dropRows + "    csv csv-options\n"), RenderDefault, 0},
		{"a filter that drops every row, the host's renderer", tail(dropRows), RenderCSV, 0},
		{"the table's events as a text: no table stage", tail(asText), RenderDefault, 0},
		{"csv-table's events as a text", tail("    csv-table csv-options\n" + asText), RenderDefault, 2},
	} {
		program := mustCompile(t, c.src, "rows.alc")
		metrics := tt.NewMetrics()
		if _, f := replayEvents(program, events, c.render, tt.DefaultLimits(), metrics); f != nil {
			t.Fatalf("%s: %v", c.name, f)
		}
		if metrics.Rows.Load() != c.rows {
			t.Errorf("%s: %d rows counted, want %d", c.name, metrics.Rows.Load(), c.rows)
		}
	}
}

// Where the text and the components answer apart, pinned so a change to
// either is seen. Metadata selected twice is the text's own refusal, fail,
// which is INPUT_INVALID, where transduce's table transducer calls the
// same document an order violation, a table having one schema. A
// descriptor without a title is a column without a label to the text's
// column-from-meta, which the table refuses as a missing value, where
// transduce's ColumnFromMeta refuses the descriptor itself. The standard
// shapes reach neither. And a numeric title renders by its lexeme, one
// label policy serving every table.
func TestWhereTheTextAndTheComponentsAnswerApart(t *testing.T) {
	twice := mustCompile(t, strings.Replace(workedExample, "      path \"response\" \"metadata\" \"fields\"\n", "      path \"response\" \"metadata\" each-index\n", 1), "twice.alc")
	doc := `{"response":{"metadata":[[{"title":"a","path":["id"]}],[{"title":"b","path":["id"]}]],"payload":{"deep":{"records":[` + record(1) + `]}}}}`
	events := mustEvents(t, doc)
	if _, f := replay(twice, events, RenderDefault); f == nil || f.Code != CodeInputInvalid {
		t.Errorf("text: %v", f)
	}
	if _, f := components(events, tt.DefaultLimits(), false); f == nil || f.Code != CodeInputOrderViolation {
		t.Errorf("components: %v", f)
	}
	program := mustCompile(t, workedExample, "export.alc")
	untitled := mustEvents(t, `{"response":{"metadata":{"fields":[{"path":["a"]}]}}}`)
	if _, f := replay(program, untitled, RenderDefault); f == nil || f.Code != CodeMissingValue {
		t.Errorf("text: %v", f)
	}
	if _, f := components(untitled, tt.DefaultLimits(), false); f == nil || f.Code != CodeInputInvalid {
		t.Errorf("components: %v", f)
	}
	numeric := mustEvents(t, `{"response":{"metadata":{"fields":[{"title":42,"path":["id"]}]},"payload":{"deep":{"records":[`+record(1)+`]}}}}`)
	expect(t, "a numeric title", program, numeric, tt.DefaultLimits(), okOut("\"42\"\r\n\"1\"\r\n"))
}

// The events end with End, as every source promises; a recording that
// does not is a source defect, and the sink says so rather than dropping
// the output silently: nothing is flushed.
func TestAStreamWithoutEndWritesNothing(t *testing.T) {
	program := mustCompile(t, workedExample, "export.alc")
	events := mustEvents(t, recordsJSON(2))
	if last := events[len(events)-1]; last.Kind != tt.End {
		t.Fatalf("the recording ends with %v", last)
	}
	events = events[:len(events)-1]
	var buffer bytes.Buffer
	sink, f := program.Sink(&buffer, RenderDefault, tt.DefaultLimits(), tt.NewMetrics())
	if f != nil {
		t.Fatal(f)
	}
	if _, f := tt.Replay(events, sink); f != nil {
		t.Fatal(f)
	}
	if buffer.Len() != 0 {
		t.Errorf("%q before the end", buffer.String())
	}
	// And the End then completes it.
	if _, f := sink.Event(tt.EvEnd()); f != nil {
		t.Fatal(f)
	}
	if buffer.Len() == 0 {
		t.Error("nothing after the end")
	}
}
