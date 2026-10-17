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
	var out bytes.Buffer
	sink, f := program.ParserSink(&out, RenderJSON, tt.DefaultLimits(), nil)
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
