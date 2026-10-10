// Copyright (c) 2026 tabnas, MIT License

package main

// translate_test.go: alchemy translate and alchemy formats, run the way a
// script runs them (cli_test.go builds the binary): the flags, the paths,
// the programs, the failures and their statuses, and that standard error
// carries nothing on success. The bytes expected are the Rust binary's,
// which writes the same for the same arguments.
//
// translate reads a document incrementally only where transduce has
// verified the grammar, which a build without the tabnas_nodecell tag has
// done for none, so there a document is read whole and a number written by
// its value: the cases that show a lexeme say both.

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/tabnas/alchemy-cli/go/translate"
	tt "github.com/tabnas/transduce/go"
)

// lexemes is the text a build writes for a number: as the document spelled
// it where the source is read incrementally, by its value where it is read
// whole.
func lexemes(spelled, valued string) string {
	if tt.AdapterBuilt() {
		return spelled
	}
	return valued
}

// people is a document with a nested table and numbers whose lexeme is not
// their shortest text.
const people = `{"people":[{"name":"Ann","age":30.50},{"name":"Bo","age":7}],"n":1.50}`

// wantSuccess is a run that wrote want and nothing on standard error.
func wantSuccess(t *testing.T, o output, want string) {
	t.Helper()
	if o.status != 0 || o.stdout != want || o.stderr != "" {
		t.Errorf("status %d, stdout %q, stderr %q; want %q", o.status, o.stdout, o.stderr, want)
	}
}

// wantFailure is a run that failed with status and code, the message
// beginning with prefix, and nothing on standard output.
func wantFailure(t *testing.T, o output, status int, code, prefix string) map[string]any {
	t.Helper()
	f := failJSON(t, o)
	message, _ := f["message"].(string)
	if o.status != status || o.stdout != "" || f["code"] != code || !strings.HasPrefix(message, prefix) {
		t.Errorf("status %d, stdout %q, failure %v; want status %d, %s, %q", o.status, o.stdout, f, status, code, prefix)
	}
	return f
}

func TestFormatsPrintsTheRegistryAsJSON(t *testing.T) {
	o := invoke(t, []string{"formats"}, nil)
	if o.status != 0 || o.stderr != "" || o.stdout != translate.FormatsJSON()+"\n" || strings.Count(o.stdout, "\n") != 1 {
		t.Fatalf("%+v", o)
	}
	// Each format's fields in their order, csv's whole up to its loss.
	if !strings.HasPrefix(o.stdout, `[{"id":"csv","reads":["tree"],"writes":"records","root":"array","schema":null,"lift":null,"embed":null,"render":"csv","loss":["`) {
		t.Errorf("%s", o.stdout)
	}
	var formats []map[string]any
	if err := json.Unmarshal([]byte(o.stdout), &formats); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, f := range formats {
		ids = append(ids, f["id"].(string))
		if len(f) != 9 {
			t.Errorf("%s: %v", f["id"], f)
		}
		loss, _ := f["loss"].([]any)
		if len(loss) == 0 {
			t.Errorf("%s declares no loss", f["id"])
		}
		for _, sentence := range loss {
			if s, ok := sentence.(string); !ok || s == "" {
				t.Errorf("%s: loss %v", f["id"], loss)
			}
		}
	}
	if strings.Join(ids, " ") != "csv ini json json5 jsonc jsonic jsonl markdown toml xml yaml zon" {
		t.Errorf("%v", ids)
	}
	byID := map[string]map[string]any{}
	for _, f := range formats {
		byID[f["id"].(string)] = f
	}
	if md := byID["markdown"]; md["lift"] != "markdown-lift" || md["schema"] != "markdown-ast" || md["render"] != "markdown-render" ||
		strings.Join([]string{md["reads"].([]any)[0].(string), md["reads"].([]any)[1].(string)}, " ") != "records tree" {
		t.Errorf("%v", md)
	}
	if xml := byID["xml"]; xml["embed"] != "xml-embed" || xml["schema"] != "xml-element" || xml["writes"] != "tree" {
		t.Errorf("%v", xml)
	}
	if toml := byID["toml"]; toml["root"] != "object" || toml["render"] != "toml-render" {
		t.Errorf("%v", toml)
	}
	if o := invoke(t, []string{"formats", "x"}, nil); o.status != 2 || !strings.Contains(failJSON(t, o)["message"].(string), "usage:") {
		t.Errorf("%+v", o)
	}
}

// A document read with --from's grammar and written in --to's format,
// from a file or from standard input; nothing reaches standard error,
// though a translation loses what the target's loss sentences say (in
// alchemy formats), as in the Rust binary.
func TestTranslateWritesADocumentInAnotherFormat(t *testing.T) {
	input := tempFile(t, "people.json", people)
	wantSuccess(t, invoke(t, []string{"translate", "--from", "json", "--to", "yaml", input}, nil),
		"\"people\":\n  - \"name\": \"Ann\"\n    \"age\": "+lexemes("30.50", "30.5")+"\n  - \"name\": \"Bo\"\n    \"age\": 7\n\"n\": "+lexemes("1.50", "1.5")+"\n")
	// A later flag replaces an earlier one.
	wantSuccess(t, invoke(t, []string{"translate", "--from", "json", "--to", "yaml", "--to", "json", input}, nil),
		lexemes(people, `{"people":[{"name":"Ann","age":30.5},{"name":"Bo","age":7}],"n":1.5}`)+"\n")
	for _, c := range []struct{ from, to, stdin, want string }{
		{"json", "json", `[1.50,1e2,-0.0]`, lexemes("[1.50,1e2,-0.0]", "[1.5,100,-0]") + "\n"},
		{"csv", "json", "a,b\n1,x\n2,y\n", `[{"a":"1","b":"x"},{"a":"2","b":"y"}]` + "\n"},
		// Markdown read as records through its lift, into a records render.
		{"markdown", "csv", "| a | b |\n| - | - |\n| 1 | x |\n", "\"a\",\"b\"\r\n\"1\",\"x\"\r\n"},
		// A root that is not an array, as the one element of one.
		{"json", "jsonl", `{"a":1}`, `{"a":1}` + "\n"},
		{"json", "jsonl", `[{"a":1},2]`, "{\"a\":1}\n2\n"},
		// Lossy: TOML has no null, and writes the root under the key.
		{"json", "toml", `[1,null]`, "\"items\" = [ 1 ]\n"},
		// The values the incremental source cannot follow, read again whole:
		// a YAML stream of several documents, a repeated member.
		{"yaml", "json", "a: 1\n---\nb: 2\n", `[{"a":1},{"b":2}]` + "\n"},
		{"json", "json", `{"a":1,"a":2}`, `{"a":2}` + "\n"},
	} {
		wantSuccess(t, invoke(t, []string{"translate", "--from", c.from, "--to", c.to, "-"}, text(c.stdin)), c.want)
	}
}

// --key names the member a root is wrapped under for a format whose
// document is an object; --path translates the value it names, read
// whole, whatever the source's shapes.
func TestTranslateTakesAKeyAndAPath(t *testing.T) {
	input := tempFile(t, "people.json", people)
	translateTo := func(to string, flags ...string) output {
		return invoke(t, append(append([]string{"translate", "--from", "json", "--to", to}, flags...), input), nil)
	}
	table := `[ { "name" = "Ann", "age" = 30.5 }, { "name" = "Bo", "age" = 7 } ]` + "\n"
	wantSuccess(t, translateTo("toml", "--path", `["people"]`), `"items" = `+table)
	wantSuccess(t, translateTo("toml", "--key", "rows", "--path", `["people"]`), `"rows" = `+table)
	wantSuccess(t, translateTo("csv", "--path", `["people"]`), "\"name\",\"age\"\r\n\"Ann\",\"30.5\"\r\n\"Bo\",\"7\"\r\n")
	wantSuccess(t, translateTo("markdown", "--path", `["people"]`), "| name | age |\n| --- | --- |\n| Ann | 30.5 |\n| Bo | 7 |\n")
	wantSuccess(t, translateTo("yaml", "--path", `["people",0]`), "\"name\": \"Ann\"\n\"age\": 30.5\n")
	wantSuccess(t, translateTo("ini", "--path", `["n"]`), "\"items\" = 1.5\n")
	wantSuccess(t, invoke(t, []string{"translate", "--from", "json", "--to", "toml", "--key", "", "-"}, text("1.50")),
		`"" = `+lexemes("1.50", "1.5")+"\n")
	// A path that names nothing is the document's failure, status 1.
	f := wantFailure(t, translateTo("yaml", "--path", `["people",5]`), 1, "INPUT_INVALID", "")
	if f["message"] != `the path ["people",5] names nothing in the document` {
		t.Errorf("%v", f)
	}
	// A path that is not one is the command line's, status 2.
	for _, path := range []string{`{}`, `"people"`, `[1.5]`, `[-1]`, `[true]`, `[null]`, ``, `[1`} {
		f := wantFailure(t, translateTo("yaml", "--path", path), 2, "INPUT_INVALID", "")
		if f["message"] != `--path takes a JSON array of keys and indexes, such as ["people",0], not `+path {
			t.Errorf("%q: %v", path, f)
		}
	}
	// A whole number is an index, however it is spelled.
	wantSuccess(t, translateTo("json", "--path", `["people",1e0,"age"]`), "7\n")
}

// --with runs a program over the input first, and its export's JSON
// events or table stand in the source's place; a program that writes its
// own text, or does not check, is refused before the input is read.
func TestTranslateRunsAProgramFirst(t *testing.T) {
	input := tempFile(t, "people.json", people)
	echo := tempFile(t, "echo.alc", "def export [input] input\n")
	table := tempFile(t, "table.alc", "def binding\n  record\n    entry :columns :infer\n    entry :rows (path \"people\" each-index)\n\n"+
		"def export [input] (table-from-json binding input)\n")
	ownText := tempFile(t, "text.alc", "def export [input] (csv csv-options (table-from-json (record (entry :columns :infer) (entry :rows (path each-index))) input))\n")
	broken := tempFile(t, "broken.alc", "def export [input] (nope input)\n")
	age := lexemes("30.50", "30.5")
	wantSuccess(t, invoke(t, []string{"translate", "--from", "json", "--to", "csv", "--with", table, input}, nil),
		"\"name\",\"age\"\r\n\"Ann\",\""+age+"\"\r\n\"Bo\",\"7\"\r\n")
	wantSuccess(t, invoke(t, []string{"translate", "--from", "json", "--to", "json", "--with", table, input}, nil),
		`[{"name":"Ann","age":`+age+`},{"name":"Bo","age":7}]`+"\n")
	wantSuccess(t, invoke(t, []string{"translate", "--from", "json", "--to", "yaml", "--with", "-", input}, text("def export [input] input\n")),
		"\"people\":\n  - \"name\": \"Ann\"\n    \"age\": "+age+"\n  - \"name\": \"Bo\"\n    \"age\": 7\n\"n\": "+lexemes("1.50", "1.5")+"\n")
	wantSuccess(t, invoke(t, []string{"translate", "--from", "json", "--to", "yaml", "--with", echo, "-"}, text("[1]")), "- 1\n")
	// Refused before the input is read: an input that cannot be read is
	// never reached.
	f := wantFailure(t, invoke(t, []string{"translate", "--from", "json", "--to", "yaml", "--with", ownText, "/nonexistent/input.json"}, nil),
		2, "DSL_TYPE_ERROR", "bad_output: ")
	if f["message"] != "bad_output: "+ownText+"'s export writes its own text, and translate takes a program whose export answers JSON events or a table, which a format's render then writes" {
		t.Errorf("%v", f)
	}
	wantFailure(t, invoke(t, []string{"translate", "--from", "json", "--to", "yaml", "--with", broken, "/nonexistent/input.json"}, nil),
		2, "DSL_TYPE_ERROR", "unknown_name: nope")
	wantFailure(t, invoke(t, []string{"translate", "--from", "json", "--to", "yaml", "--with", "/nonexistent/program.alc", input}, nil),
		2, "INPUT_INVALID", "cannot read /nonexistent/program.alc: ")
	f = wantFailure(t, invoke(t, []string{"translate", "--from", "json", "--to", "yaml", "--with", "-", "-"}, text("[1]")), 2, "INPUT_INVALID", "")
	if f["message"] != "only one of PROGRAM and INPUT may be - (standard input)" {
		t.Errorf("%v", f)
	}
}

// The statuses follow the code, and every failure is one JSON object on
// standard error with nothing on standard output: what a run writes is
// held until it has succeeded.
func TestTranslateFailuresAndStatuses(t *testing.T) {
	input := tempFile(t, "people.json", people)
	for _, args := range [][]string{
		{"translate"},
		{"translate", "--from", "json", input},
		{"translate", "--to", "json", input},
		{"translate", "--from", "json", "--to", "yaml"},
		{"translate", "--from", "json", "--to", "yaml", input, input},
		{"translate", "--from", "json", "--to", "yaml", "--bogus", input},
		{"translate", "--from", "json", "--to"},
		{"translate", "--bogus", "--from", "bogus", input},
	} {
		f := wantFailure(t, invoke(t, args, nil), 2, "INPUT_INVALID", "usage: ")
		if !strings.Contains(f["message"].(string), "alchemy translate --from FORMAT --to FORMAT") {
			t.Errorf("%v: %v", args, f)
		}
	}
	names := "csv, ini, json, json5, jsonc, jsonic, jsonl, markdown, toml, xml, yaml or zon"
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--from", "bogus", "--to", "yaml"}, `--from takes ` + names + `, not "bogus"`},
		// Found where it stands, before the usage error after it.
		{[]string{"--to", "Yaml", "--bogus"}, `--to takes ` + names + `, not "Yaml"`},
		// Quoted as the Rust binary quotes it.
		{[]string{"--from", "json", "--to", "a\tb\x01\u0301\"\\"}, `--to takes ` + names + `, not "a\tb\u{1}\u{301}\"\\"`},
		{[]string{"--from", "json", "--to", "yaml", "--max-output-bytes", "many"}, `--max-output-bytes takes a number of bytes, not "many"`},
	} {
		f := wantFailure(t, invoke(t, append(append([]string{"translate"}, c.args...), input), nil), 2, "INPUT_INVALID", "")
		if f["message"] != c.want {
			t.Errorf("%v: %v", c.args, f)
		}
	}
	f := wantFailure(t, invoke(t, []string{"translate", "--from", "json", "--to", "yaml", "/nonexistent/input.json"}, nil), 2, "INPUT_INVALID", "")
	if f["message"] != "cannot read /nonexistent/input.json: No such file or directory (os error 2)" {
		t.Errorf("%v", f)
	}
	// A document its grammar refuses: status 1, with the engine's position.
	f = wantFailure(t, invoke(t, []string{"translate", "--from", "json", "--to", "json", "-"}, text(`{"a":`)), 1, "INPUT_INVALID", "unexpected: ")
	if f["row"] != 1.0 || f["col"] != 6.0 || f["output"] != "none" {
		t.Errorf("%v", f)
	}
	// An output longer than --max-output-bytes: status 5, the limit named,
	// and nothing written.
	f = wantFailure(t, invoke(t, []string{"translate", "--from", "json", "--to", "yaml", "--max-output-bytes", "10", input}, nil),
		5, "RESOURCE_LIMIT_EXCEEDED", "the output would exceed 10 bytes")
	if limit, _ := f["limit"].(map[string]any); limit["name"] != "max_output_bytes" || f["output"] != "none" {
		t.Errorf("%v", f)
	}
	wantSuccess(t, invoke(t, []string{"translate", "--from", "json", "--to", "json", "--max-output-bytes", "+100", "-"}, text(`[1]`)), "[1]\n")
}

// A standard output that cannot be written is OUTPUT_FAILED, status 3,
// with the error the Rust binary's message names.
func TestTranslateToAClosedStandardOutputIsOutputFailed(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	cmd := exec.Command(binary, "translate", "--from", "json", "--to", "yaml", "-")
	cmd.Stdin = strings.NewReader(people)
	cmd.Stdout = w
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	w.Close()
	err = cmd.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("%v", err)
	}
	f := failJSON(t, output{stderr: stderr.String()})
	if f["code"] != "OUTPUT_FAILED" || f["message"] != "the output could not be written: Broken pipe (os error 32)" {
		t.Errorf("%v", f)
	}
}
