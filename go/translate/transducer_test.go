// Copyright (c) 2026 tabnas, MIT License

package translate

// transducer_test.go: a format whose module ships a transducer part is read
// through it (the Rust crate's unit tests in rs/src/translate.rs): the
// route over its parser's rule passes, composed over the part, and what it
// falls back to.

import (
	"bytes"
	"strings"
	"testing"

	alchemy "github.com/tabnas/alchemy/go"
	at "github.com/tabnas/alchemy/go/translate"
	tt "github.com/tabnas/transduce/go"
)

// markingTransduce writes {"ran":true} whatever the document: what it
// writes says which route ran.
const markingTransduce = "def json-transduce [input]\n  as-events\n    scan-emit\n      []\n      fn [s pass] (transition s [])\n      fn [s] [object-start (key \"ran\") (scalar true) object-end]\n      parser-events input\n"

// plainJSON is json without its transducer: read as a format whose module
// ships none is read.
func plainJSON(t *testing.T) *Format {
	t.Helper()
	json := Named("json")
	if json == nil {
		t.Fatal("json is a format")
	}
	return &Format{Part: json.Part, reader: json.reader, passes: json.passes}
}

// transducedJSON is json, read through a transducer part given beside its
// module's parts in place of the one it ships.
func transducedJSON(t *testing.T, text string) *Format {
	t.Helper()
	json := Named("json")
	if json == nil {
		t.Fatal("json is a format")
	}
	return &Format{
		Part:       json.Part,
		reader:     json.reader,
		passes:     json.passes,
		Transducer: &at.Alc{File: "tabnas-json/alchemy/transduce.alc", Entry: "json-transduce", Text: text},
	}
}

func translated(t *testing.T, from *Format, to, path string, program *alchemy.Source, input string) (string, *tt.Fail) {
	t.Helper()
	limits := tt.DefaultLimits()
	target := Named(to)
	if target == nil {
		t.Fatalf("%s is a format", to)
	}
	r := &Request{From: from, To: target, Options: at.DefaultOptions(), Program: program, Limits: limits}
	if path != "" {
		p, f := ParsePath(path, limits)
		if f != nil {
			t.Fatal(f)
		}
		r.Path = p
	}
	var held bytes.Buffer
	if f := Run(r, input, &held, nil); f != nil {
		return "", f
	}
	return held.String(), nil
}

func mustTranslate(t *testing.T, from *Format, to, path string, program *alchemy.Source, input string) string {
	t.Helper()
	out, f := translated(t, from, to, path, program, input)
	if f != nil {
		t.Fatalf("%s from %q: %v", to, input, f)
	}
	return out
}

// A format whose module ships a transducer part is read through it (json,
// through tabnas/json's), its rule passes streamed into the composition
// over the part, and one without is read as before: the two routes write
// the same bytes for the same document, a number's lexeme kept, into a
// tree target, a records target, and with a program over the input; a
// marking part given in the module's place shows the route ran. A path
// keeps the document's value, the one route a path has. A repeated member,
// which the part streams twice and the composition's tree-contract
// refuses, is read again whole, as the incremental source's is.
func TestATransducerPartReadsTheFormatAndTheWalkStandsInWhereItRefuses(t *testing.T) {
	plain := plainJSON(t)
	through := Named("json")
	marking := transducedJSON(t, markingTransduce)
	if plain.Transducer != nil || through.Transducer == nil || through.Transducer.Entry != "json-transduce" || !strings.Contains(through.Transducer.Text, "transduce-tree") {
		t.Fatalf("plain %v, through %v", plain.Transducer, through.Transducer)
	}
	tree := `{"a":[1,2.50,"x",{"b":null}],"c":{},"d":[],"e":1e21,"f":true}`
	records := `[{"a":1,"b":"x"},{"a":2,"b":"y"}]`
	echo := &alchemy.Source{File: "echo.alc", Text: "def export [input] (events input)\n"}
	for _, c := range []struct {
		to, doc string
		program *alchemy.Source
	}{
		{"json", tree, nil},
		{"yaml", tree, nil},
		{"csv", records, nil},
		{"json", tree, echo},
		{"csv", records, echo},
	} {
		expected := mustTranslate(t, plain, c.to, "", c.program, c.doc)
		got := mustTranslate(t, through, c.to, "", c.program, c.doc)
		if !tt.AdapterBuilt() {
			// Without the incremental adapter the plain route reads the
			// document whole and writes a number by its value, where the
			// transducer's route keeps the document's lexeme: 2.5 for
			// 2.50, the one difference between the two.
			got = strings.ReplaceAll(got, "2.50", "2.5")
		}
		if got != expected {
			t.Errorf("%s from %s: the transducer's route wrote %q, the plain one %q", c.to, c.doc, got, expected)
		}
		if marked, f := translated(t, marking, c.to, "", c.program, c.doc); f == nil && marked == expected {
			t.Errorf("%s from %s: the part's route did not run", c.to, c.doc)
		}
	}
	if got := mustTranslate(t, marking, "json", "", nil, tree); got != "{\"ran\":true}\n" {
		t.Errorf("the marking part wrote %q", got)
	}
	// A path selects in the document's value, read whole, as it did: the
	// materialized value spells a number as the engine does.
	if got := mustTranslate(t, marking, "json", `["a",1]`, nil, tree); got != "2.5\n" {
		t.Errorf("a path keeps the document's value: %q", got)
	}
	repeated := `{"a":1,"a":2}`
	if got, want := mustTranslate(t, through, "json", "", nil, repeated), mustTranslate(t, plain, "json", "", nil, repeated); got != want || got != "{\"a\":2}\n" {
		t.Errorf("a repeated member: the transducer's route wrote %q, the plain one %q", got, want)
	}
	if _, f := translated(t, through, "json", "", nil, "[1,"); f == nil || f.Code != tt.CodeInputInvalid {
		t.Errorf("a refused document: %v", f)
	}
}

// Every format with a parser answers its rule passes; one read as a tree
// has none and refuses; json5's check runs first.
func TestAFormatWithAParserGivesItsRulePasses(t *testing.T) {
	var parsed []string
	for _, f := range Formats() {
		if f.Parses() {
			parsed = append(parsed, f.ID())
		}
	}
	// C is parsed here, where the Rust crate reads it as its realized
	// tree; pgn and feed are read as trees here, where it parses them.
	want := "c css csv ini json json5 jsonc jsonic jsonl markdown semver toml xml yaml zon"
	if got := strings.Join(parsed, " "); got != want {
		t.Errorf("the parsed formats are %s", got)
	}
	if got := ParserNames(); got != "c, css, csv, ini, json, json5, jsonc, jsonic, jsonl, markdown, semver, toml, xml, yaml or zon" {
		t.Errorf("ParserNames is %q", got)
	}
	abnf := Named("abnf")
	if abnf.Parses() {
		t.Error("abnf is read as a tree")
	}
	if _, f := abnf.Passes(`x = "y"`); f == nil || f.Code != tt.CodeInputInvalid || !strings.HasPrefix(f.Message, "abnf is read as a tree") {
		t.Errorf("a tree has no passes: %v", f)
	}
	json5 := Named("json5")
	if _, f := json5.Passes("// only a comment"); f == nil || f.Code != tt.CodeInputInvalid {
		t.Errorf("the check refuses: %v", f)
	}
	if source, f := json5.Passes("{a: 1}"); f != nil || source == nil {
		t.Errorf("the check passes: %v", f)
	}
}
