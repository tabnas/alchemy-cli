// Copyright (c) 2026 tabnas, MIT License

package e2e

// effects_test.go: the plan report held to the run (alchemy's
// go/effects_test.go, rs/src/effects.rs's tests): a custom CSV dialect is
// reported as it runs, and what it reports is what it writes. The reports
// that only compile stay in alchemy; the helpers below are copies of its
// own.

import (
	"strings"
	"testing"

	. "github.com/tabnas/alchemy/go"
	tt "github.com/tabnas/transduce/go"
)

// workedExample is the spec's program (sections 12.1 and 13.4).
const workedExample = "def column-from-meta [source]\n  record\n    entry :label (get \"title\" source)\n    entry :source\n      as-path\n        get \"path\" source\n\ndef api-binding\n  record\n    entry :columns\n      path \"response\" \"metadata\" \"fields\"\n    entry :rows\n      path \"response\" \"payload\" \"deep\" \"records\" each-index\n    entry :column column-from-meta\n\ndef api-table [input]\n  table-from-json api-binding input\n\ndef export [input]\n  pipe input\n    api-table\n    csv csv-options\n"

// mustCompile is src compiled, or the test's end.
func mustCompile(t testing.TB, src, file string) *Program {
	t.Helper()
	p, f := Compile(src, file, routers, renderers)
	if f != nil {
		t.Fatalf("%s: %v", file, f)
	}
	return p
}

// interpreted is p with the standard compositions run through the
// library's text.
func interpreted(t testing.TB, p *Program) *Program {
	t.Helper()
	q, f := p.WithNative(false)
	if f != nil {
		t.Fatal(f)
	}
	return q
}

func jsonMember(t testing.TB, o *JSONObject, path ...string) string {
	t.Helper()
	var v any = o
	for _, key := range path {
		obj, ok := v.(*JSONObject)
		if !ok {
			t.Fatalf("%v is not an object at %s", v, key)
		}
		if v, ok = obj.Get(key); !ok {
			t.Fatalf("no member %s", key)
		}
	}
	return EncodeJSON(v)
}

// The CSV renderer is reported with the dialect it is built with: the
// program's own options when its csv runs natively, the defaults when the
// host renders a table; and what it reports is what it writes.
func TestACustomCsvDialectIsReportedAsItRuns(t *testing.T) {
	lf := strings.Replace(workedExample, "    csv csv-options\n", "    csv lf\n", 1) +
		"\ndef lf (record (entry :delimiter \";\") (entry :newline \"\\n\") (entry :header false) (entry :null-text \"NULL\") (entry :missing \"-\"))\n"
	program := mustCompile(t, lf, "lf.alc")
	if !program.Native() {
		t.Fatal("not native")
	}
	if got := jsonMember(t, program.ExplainJSON(), "renderer"); got != `{"name":"csv","quoting":"always","delimiter":";","newline":"\n","header":false,"null_text":"NULL","missing":"text","missing_text":"-","host":false}` {
		t.Errorf("%s", got)
	}
	if out, f := replayRun(t, program, records, RenderDefault, tt.DefaultLimits(), nil); f != nil || out != "\"123\";\"Alice\";\"50.25\"\n\"456\";\"Bob\";\"72\"\n" {
		t.Errorf("%q %v", out, f)
	}
	// The null and missing texts reach the output as reported: a record
	// with a null name and no balance.
	sparse := strings.Replace(records, `{"account":{"balance":72},"person":{"name":"Bob"},"id":456}`, `{"person":{"name":null},"id":456}`, 1)
	if sparse == records {
		t.Fatal("the document did not change")
	}
	if out, f := replayRun(t, program, sparse, RenderDefault, tt.DefaultLimits(), nil); f != nil || out != "\"123\";\"Alice\";\"50.25\"\n\"456\";\"NULL\";\"-\"\n" {
		t.Errorf("%q %v", out, f)
	}
	// The default dialect, and the host's renderer, report the defaults.
	for _, c := range []struct {
		name string
		src  string
		host string
	}{
		{"default", workedExample, "false"},
		{"host", strings.Replace(workedExample, "    csv csv-options\n", "", 1), "true"},
	} {
		got := jsonMember(t, mustCompile(t, c.src, c.name+".alc").ExplainJSON(), "renderer")
		want := `{"name":"csv","quoting":"always","delimiter":",","newline":"\r\n","header":true,"null_text":"","missing":"error","missing_text":null,"host":` + c.host + `}`
		if got != want {
			t.Errorf("%s: %s", c.name, got)
		}
	}
}
