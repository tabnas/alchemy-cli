// Copyright (c) 2026 tabnas, MIT License

package e2e

// transduce_tree_test.go: the library's `transduce-tree` against
// transduce's native one (decision 3 of alchemy's streaming plan), as
// rs/tests/stdlib_test.rs's transduce_tree_native_and_interpreted_agree
// runs them.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	. "github.com/tabnas/alchemy/go"
	"github.com/tabnas/alchemy/go/shared"
	tabnasjson "github.com/tabnas/json/go"
	tt "github.com/tabnas/transduce/go"
)

// countingRouters is transduce's routers, counting the native
// transduce-trees they make (a spec they refuse makes none), so a test can
// tell the native path from the library's scan.
type countingRouters struct {
	shared.Routers
	made *int
}

func (r countingRouters) TransduceTree(spec shared.TreeSpec, downstream shared.Sink) shared.ParserSink {
	sink := r.Routers.(shared.TreeRouters).TransduceTree(spec, downstream)
	if sink != nil {
		*r.made++
	}
	return sink
}

// TransduceTreeReadsMemberAndText is transduce's answer: the embedded
// Routers carries only the interface's own methods.
func (r countingRouters) TransduceTreeReadsMemberAndText() bool {
	texts, ok := r.Routers.(shared.TreeTextRouters)
	return ok && texts.TransduceTreeReadsMemberAndText()
}

// treeSpecs are the specs the comparison runs: the json part's, and specs
// that take a close pass's value, hand a container on through a replaced
// rule, refuse a key that is not a string, announce a named key, yield a
// rule's text and name a member inside a rule's own container.
var treeSpecs = [][2]string{
	{"the json part's spec", "record\n    entry :map (record (entry :container :object))\n    entry :list (record (entry :container :array))\n    entry :pair (record (entry :key 0))\n    entry :val (record (entry :scalar 0))"},
	{"a close pass's value, keyed by name", "record\n    entry :map (record (entry :container :object))\n    entry :list (record (entry :container :array))\n    entry :pair (record (entry :key 0))\n    entry :val (record (entry :scalar :value))"},
	{"a container handed on through a replaced rule", "record\n    entry :list (record (entry :container :array))\n    entry :elem (record (entry :container :array))\n    entry :val (record (entry :scalar 0))"},
	{"a key whose token is not a string", "record\n    entry :val (record (entry :key 0) (entry :scalar 0))"},
	{"a named key on every value", "record\n    entry :list (record (entry :container :array))\n    entry :val (record (entry :key \"v\") (entry :scalar 0))"},
	{"a pair's value as its text", "record\n    entry :map (record (entry :container :object))\n    entry :pair (record (entry :key 0) (entry :text true))"},
	{"a container's own member and its text", "record\n    entry :list (record (entry :container :object) (entry :member \"items\") (entry :text true))\n    entry :pair (record (entry :member 0))"},
}

// runPasses drives one document's rule passes into program, its output
// rendered as JSON: the output, or the failure.
func runPasses(program *Program, text string) (string, *Fail) {
	return runPassesUnder(program, text, tt.DefaultLimits())
}

// runPassesUnder is runPasses under limits: what the program wrote, and
// the failure.
func runPassesUnder(program *Program, text string, limits tt.Limits) (string, *Fail) {
	var out bytes.Buffer
	sink, f := program.ParserSink(&out, RenderJSON, limits, nil)
	if f != nil {
		return "", f
	}
	if _, f := tt.NewParserEventSource(tabnasjson.Make(), text).Run(sink); f != nil {
		return out.String(), f
	}
	return out.String(), nil
}

// Over every JSON fixture of transduce's, the generated documents and the
// documents below: the same bytes both ways, or the same code. The native
// ran each time the natives were on, and never when they were off.
func TestTheNativeAndTheInterpretedTransduceTreeAgree(t *testing.T) {
	var documents [][2]string
	dir := transduceFixtures(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		text, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		documents = append(documents, [2]string{name, string(text)})
	}
	for _, n := range []int{0, 1, 2, 50} {
		documents = append(documents, [2]string{fmt.Sprintf("recordsJSON(%d)", n), recordsJSON(n)})
	}
	for _, text := range []string{
		"1", "2.50", "-0", "1E+2", `"s"`, "null", "[]", "{}", "[[[]],[{}]]",
		`{"a":[1,2.50,"x",{"b":null}],"c":{},"d":[],"e":1e21,"f":true}`,
		`{"é":"日本","a\"b":"\u0000"}`,
		`{"a":1,"a":2}`,
		"[1,",
		`{"a" 1}`,
	} {
		documents = append(documents, [2]string{fmt.Sprintf("%q", text), text})
	}
	made := 0
	counting := countingRouters{Routers: routers, made: &made}
	var failures []string
	compared := 0
	for _, spec := range treeSpecs {
		src := "def spec\n  " + spec[1] + "\n\ndef export [input] (transduce-tree spec input)\n"
		native, f := CompileWith(src, "tree.alc", InputParserEvents, counting, renderers)
		if f != nil {
			t.Fatal(f)
		}
		slow := interpreted(t, native)
		for _, doc := range documents {
			before := made
			a, fa := runPasses(native, doc[1])
			ranNative := made - before
			b, fb := runPasses(slow, doc[1])
			if ranNative != 1 || made-before != 1 {
				t.Errorf("%s, %s: the native ran %d times, %d in all", spec[0], doc[0], ranNative, made-before)
			}
			label := spec[0] + ", " + doc[0]
			switch {
			case fa == nil && fb == nil && a == b:
			case fa != nil && fb != nil && fa.Code == fb.Code:
			default:
				failures = append(failures, fmt.Sprintf("%s\n  native:      %q %v\n  interpreted: %q %v", label, a, fa, b, fb))
			}
			compared++
		}
	}
	if len(failures) > 0 {
		t.Error(strings.Join(failures, "\n"))
	}
	if compared <= 100 {
		t.Errorf("%d documents compared", compared)
	}
	t.Logf("transduce-tree: %d documents agree both ways", compared)
}

// A :text rule's text under a small max_scalar_bytes: the native holds it
// where the library's `string-join` does, so the two write the same bytes,
// or refuse with the same code, limit and message (the message's byte
// count names the join), at each bound from one that refuses every text to
// one that refuses none; and some of the refusals are the texts' own, not
// the guard's. The interpreter ends a failure inside the standard library
// with the library's position, which the native, running none of its
// text, does not (rs/tests/stdlib_test.rs).
func TestTransduceTreesTextsAreBoundedAlike(t *testing.T) {
	var specs [][2]string
	for _, spec := range treeSpecs {
		if strings.Contains(spec[1], ":text") {
			specs = append(specs, spec)
		}
	}
	if len(specs) != 2 {
		t.Fatalf("%d specs with a text", len(specs))
	}
	documents := []string{
		`{"a":{"ab":1}}`,
		`{"a":{"b":1,"c":2222}}`,
		`{"a":{"b":123}}`,
		`{"a":[10,20]}`,
		`[1,[22,333],"x",{"é":"日本"}]`,
		`{"a":[1,2.50,"x",{"b":null}],"c":{},"d":[],"e":1e21,"f":true}`,
	}
	ending := func(f *Fail) string {
		if f == nil {
			return "ok"
		}
		limit := ""
		if f.Limit != nil {
			limit = fmt.Sprintf("%s %d", f.Limit.Name, f.Limit.Value)
		}
		message, _, _ := strings.Cut(f.Message, " (at stdlib/")
		return fmt.Sprintf("%s [%s] %s", f.Code, limit, message)
	}
	made := 0
	counting := countingRouters{Routers: routers, made: &made}
	var failures []string
	textsRefused := 0
	for _, spec := range specs {
		src := "def spec\n  " + spec[1] + "\n\ndef export [input] (transduce-tree spec input)\n"
		native, f := CompileWith(src, "tree.alc", InputParserEvents, counting, renderers)
		if f != nil {
			t.Fatal(f)
		}
		slow := interpreted(t, native)
		for _, text := range documents {
			for _, max := range []int{1, 3, 4, 5, 7, 8, 11, 16, 64} {
				limits := tt.DefaultLimits()
				limits.MaxScalarBytes = max
				before := made
				a, fa := runPassesUnder(native, text, limits)
				b, fb := runPassesUnder(slow, text, limits)
				if made-before != 1 {
					t.Errorf("%s, %s, %d: the native ran %d times", spec[0], text, max, made-before)
				}
				if a != b || ending(fa) != ending(fb) {
					failures = append(failures, fmt.Sprintf("%s, %s, max_scalar_bytes %d\n  native:      %q %s\n  interpreted: %q %s", spec[0], text, max, a, ending(fa), b, ending(fb)))
				}
				if fa != nil && strings.HasPrefix(fa.Message, "string-join:") {
					textsRefused++
				}
			}
		}
	}
	if len(failures) > 0 {
		t.Error(strings.Join(failures, "\n"))
	}
	if textsRefused <= 10 {
		t.Errorf("%d texts refused at the bound", textsRefused)
	}
}
