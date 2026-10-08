// Copyright (c) 2026 tabnas, MIT License

package e2e

// sources_test.go: CompileSources (rs/tests/sources_test.rs): several
// sources linked into one program, as a host links a format's parts
// (libraries of definitions prefixed by the format's name, with no export)
// with the program that calls them. The program runs as the same text in
// one file runs; a failure in any source carries that source's file, row
// and column, in the field and in the failure's display, at every stage
// that positions one: here the run, on transduce's routers and render's
// renderers. From alchemy's go/sources_test.go, whose tests of the reader,
// the desugarer, the resolver, the checker and the linking's refusals,
// which only compile, stay there.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	. "github.com/tabnas/alchemy/go"
	tt "github.com/tabnas/transduce/go"
)

// part is a render part: definitions prefixed by the format's name, no
// export.
const part = "; A part of the lines format: each item quoted, one to a line.\ndef lines-line [item]\n  concat (quoted item) \"\\n\"\n\ndef lines-render [items]\n  concat-map lines-line items\n"

const partFile = "lines/render.alc"

// mainSrc is the program that calls the part.
const mainSrc = "def export [input]\n  lines-render (select (path each-index) input)\n"

const mainFile = "main.alc"

func sourcesOf(list ...[2]string) []Source {
	out := make([]Source, len(list))
	for i, s := range list {
		out[i] = Source{File: s[0], Text: s[1]}
	}
	return out
}

// linked is the program with the part second, as the design's diagnostics
// test has it.
func linked(partText string) (*Program, *Fail) {
	return CompileSources(sourcesOf([2]string{mainFile, mainSrc}, [2]string{partFile, partText}), routers, renderers)
}

func mustLink(t testing.TB, sources []Source) *Program {
	t.Helper()
	p, f := CompileSources(sources, routers, renderers)
	if f != nil {
		t.Fatal(f)
	}
	return p
}

func render(t testing.TB, program *Program, text string) string {
	t.Helper()
	out, f := hostRun(t, program, text, RenderDefault, tt.DefaultLimits())
	if f != nil {
		t.Fatalf("%s: %v", text, f)
	}
	return out
}

// at is the 1-based row and column of needle on the 1-based row of text,
// as a failure carries them.
func at(t testing.TB, text string, row int, needle string) (uint64, uint64) {
	t.Helper()
	line := strings.Split(text, "\n")[row-1]
	i := strings.Index(line, needle)
	if i < 0 {
		t.Fatalf("%q is not on row %d", needle, row)
	}
	return uint64(row), uint64(utf8.RuneCountInString(line[:i]) + 1)
}

// assertAt: the failure names file and the position (row, column), in the
// fields, in its display, and in its JSON.
func assertAt(t testing.TB, f *Fail, file string, row, column uint64) {
	t.Helper()
	if f.File != file || f.Row != row || f.Column != column {
		t.Errorf("%v: want %s:%d:%d", f, file, row, column)
	}
	if shown := fmt.Sprintf("(%s:%d:%d)", file, row, column); !strings.HasSuffix(f.Error(), shown) {
		t.Errorf("%v does not end %s", f, shown)
	}
	data, _ := f.MarshalJSON()
	var j map[string]any
	if err := json.Unmarshal(data, &j); err != nil || j["file"] != file {
		t.Errorf("%s", data)
	}
}

// Two sources are one namespace: the program calls the part's definitions,
// in either order of the sources, and runs as the same texts in one file
// run.
func TestLinkedSourcesRunAsOneProgram(t *testing.T) {
	input := `["a","b\"c","d\ne"]`
	expected := "\"a\"\n\"b\\\"c\"\n\"d\\ne\"\n"
	mainFirst, f := linked(part)
	if f != nil {
		t.Fatal(f)
	}
	if got := render(t, mainFirst, input); got != expected {
		t.Errorf("%q", got)
	}
	partFirst := mustLink(t, sourcesOf([2]string{partFile, part}, [2]string{mainFile, mainSrc}))
	if got := render(t, partFirst, input); got != expected {
		t.Errorf("%q", got)
	}
	one := mustCompile(t, mainSrc+"\n"+part, "one.alc")
	if got := render(t, one, input); got != expected {
		t.Errorf("%q", got)
	}
	// The program is named by its first source, and its plan is reported
	// as one program's.
	if mainFirst.File() != mainFile || partFirst.File() != partFile || mainFirst.Explain() != one.Explain() {
		t.Errorf("%s %s", mainFirst.File(), partFirst.File())
	}
	// A part may call back into the program: the namespace is one.
	back := "def lines-line [item]\n  concat (main-mark item) \"\\n\"\n\ndef lines-render [items]\n  concat-map lines-line items\n"
	withMark := mainSrc + "\ndef main-mark [s] (concat \"* \" s)\n"
	program := mustLink(t, sourcesOf([2]string{mainFile, withMark}, [2]string{partFile, back}))
	if got := render(t, program, `["x"]`); got != "* x\n" {
		t.Errorf("%q", got)
	}
}

// A failure the run meets in a part, from a fail in its definition, is the
// program's INPUT_INVALID at the part's file and position.
func TestARunTimeFailureInAPartNamesThePart(t *testing.T) {
	strict := "def lines-line [item]\n  match item\n    case \"bad\" (fail \"a bad item\")\n    case _ (concat (quoted item) \"\\n\")\n\ndef lines-render [items]\n  concat-map lines-line items\n"
	program, f := linked(strict)
	if f != nil {
		t.Fatal(f)
	}
	if got := render(t, program, `["ok"]`); got != "\"ok\"\n" {
		t.Errorf("%q", got)
	}
	row, col := at(t, strict, 3, "(fail")
	_, f = hostRun(t, program, `["ok","bad"]`, RenderDefault, tt.DefaultLimits())
	if f == nil || f.Code != CodeInputInvalid || f.Message != "a bad item" {
		t.Fatalf("%v", f)
	}
	assertAt(t, f, partFile, row, col)
	// The interpreted twin positions it the same way.
	_, f = hostRun(t, interpreted(t, program), `["bad"]`, RenderDefault, tt.DefaultLimits())
	if f == nil {
		t.Fatal("the interpreted run succeeded")
	}
	assertAt(t, f, partFile, row, col)
}
