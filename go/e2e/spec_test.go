// Copyright (c) 2026 tabnas, MIT License

package e2e

// spec_test.go: alchemy's shared fixtures in ../alchemy/test/spec that run
// programs, through tabnas/support's runner as every grammar repository
// runs its own: run.tsv, a program run over a JSON document, the bytes it
// writes or the failure, on transduce's routers and render's renderers;
// and the catalogue drift test over the failures of all four fixtures,
// which needs run.tsv's rows. From alchemy's go/spec_test.go: reader.tsv,
// pipe.tsv and check.tsv run there, with the drift test's checks of the
// rows they meet.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	. "github.com/tabnas/alchemy/go"
	tabnasjson "github.com/tabnas/json/go"
	tabnas "github.com/tabnas/parser/go"
	support "github.com/tabnas/support/go"
	tt "github.com/tabnas/transduce/go"
)

// specDir is alchemy's fixtures, in its checkout.
func specDir(t testing.TB) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "test", "spec")
}

// repoRoot is alchemy's checkout, beside this one (../alchemy from
// alchemy-cli's root; the tests run in go/e2e): the fixtures these tests
// read are alchemy's, and transduce's checkout is beside it too. A missing
// sibling fails the test, naming the path.
func repoRoot(t testing.TB) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "alchemy"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "test", "spec")); err != nil {
		t.Fatalf("alchemy's checkout, with its fixtures, is beside this one, at %s: %v", root, err)
	}
	return root
}

// failError is a *Fail as the runner sees it: an error whose code is the
// one a row pins (runCode: the finer code for alchemy's own codes,
// the transduce code for any other, in every file, as test/AGENTS.md asks
// of a port's runners) and whose position is the failure's.
type failError struct{ f *Fail }

func (e failError) Error() string { return e.f.Error() }

func asErr(f *Fail) error {
	if f == nil {
		return nil
	}
	return failError{f}
}

func failOf(err error) (*Fail, bool) {
	var fe failError
	if errors.As(err, &fe) {
		return fe.f, true
	}
	return nil, false
}

// readerRow, pipeRow and checkRow are what alchemy's reader.tsv,
// pipe.tsv and check.tsv rows become (alchemy's go/spec_test.go runs
// them); the drift test below meets their failures.
func readerRow(input string) (string, *Fail) {
	program, f := Parse(input)
	if f != nil {
		return "", f
	}
	return Canonical(program), nil
}

func pipeRow(input string) (string, *Fail) {
	program, f := Parse(input)
	if f != nil {
		return "", f
	}
	core, f := Desugar(program, input)
	if f != nil {
		return "", f
	}
	return Canonical(core), nil
}

// checkRow is a program that checks as its plan report; one that does not
// fails with the resolver's, the checker's or the plan evaluation's code,
// at the position it names.
func checkRow(input string) (string, *Fail) {
	program, f := Compile(input, "check", routers, renderers)
	if f != nil {
		return "", f
	}
	return program.Explain(), nil
}

// runCode is the code a run row pins: the finer code (the first word of
// the message) for alchemy's own codes, which carry one, and the
// transduce or render code itself for every other failure, whose message
// is free text (`fail "a: b"` must not pin `a`).
func runCode(f *Fail) string {
	switch f.Code {
	case CodeDSLParseError, CodeDSLTypeError, CodeStreamReused, CodeStreamabilityUnknown:
		return FinerCode(f)
	}
	return f.Code.String()
}

// runRender is the renderer a run row names.
func runRender(t testing.TB, row *support.Row) Renderer {
	name := row.Named("render")
	if name == "" {
		return RenderDefault
	}
	render, ok := RendererNamed(name)
	if !ok {
		t.Fatalf("%s: render is csv, json or empty, not %q", row.Where(), name)
	}
	return render
}

// runDoc is the document a run row reads: an empty cell is null.
func runDoc(row *support.Row) string {
	if doc := row.UnescNamed("doc"); doc != "" {
		return doc
	}
	return "null"
}

// runOutcome is one run of a row's program: the bytes it wrote, or the
// failure.
type runOutcome struct {
	out  string
	fail *Fail
}

func (o runOutcome) String() string {
	if o.fail != nil {
		return "Err(" + o.fail.Error() + ")"
	}
	return fmt.Sprintf("Ok(%q)", o.out)
}

func (o runOutcome) agrees(other runOutcome) bool {
	switch {
	case o.fail == nil && other.fail == nil:
		return o.out == other.out
	case o.fail != nil && other.fail != nil:
		return runCode(o.fail) == runCode(other.fail) &&
			o.fail.Row == other.fail.Row && o.fail.Column == other.fail.Column
	}
	return false
}

// runBoth compiles program and runs it over doc as `alchemy run` does: the
// document read by the JSON grammar incrementally, pruned under the
// program's row selector when it has one, with the default limits; the
// standard compositions native or through the library's text.
func runBoth(program, doc string, render Renderer, native bool) runOutcome {
	compiled, f := Compile(program, "run", routers, renderers)
	if f == nil && !native {
		compiled, f = compiled.WithNative(false)
	}
	if f != nil {
		return runOutcome{fail: f}
	}
	out, f := driveRun(compiled, doc, render, tt.DefaultLimits(), nil)
	return runOutcome{out: out, fail: f}
}

// driveRun parses doc with the json grammar incrementally, pruned under
// the program's row selector when it has one, and pushes its events
// through the program's sink: the output, or the failure. metrics may be
// nil.
func driveRun(program *Program, doc string, render Renderer, limits tt.Limits, metrics *tt.Metrics) (string, *Fail) {
	if metrics == nil {
		metrics = tt.NewMetrics()
	}
	var buffer bytes.Buffer
	sink, f := program.Sink(&buffer, render, limits, metrics)
	if f != nil {
		return "", f
	}
	prune := tt.Prune{}
	if selector, ok := program.RowSelector(); ok {
		prune = tt.PruneUnderSelector(selector)
	}
	_, f = tt.NewParserSource(tabnasjson.Make(), doc).
		Grammar("json").
		Mode(tt.IncrementalMode(prune)).
		Limits(limits).
		Metrics(metrics).
		Run(sink)
	return buffer.String(), f
}

// beforeTheSource is whether a row's run ends before its document is read:
// its program fails to compile, natively or interpreted, or its sink
// cannot be built (a renderer for a program that renders its own text).
// Such a row runs in every build; any other needs the incremental source.
func beforeTheSource(program string, render Renderer) bool {
	for _, native := range []bool{true, false} {
		compiled, f := Compile(program, "run", routers, renderers)
		if f == nil && !native {
			compiled, f = compiled.WithNative(false)
		}
		if f == nil {
			_, f = compiled.Sink(io.Discard, render, tt.DefaultLimits(), nil)
		}
		if f != nil {
			return true
		}
	}
	return false
}

// needsIncremental is the reason a test that reads a document as `alchemy
// run` does is skipped in a build without the incremental adapter.
const needsIncremental = "reads its document incrementally, as alchemy run does, and this build has no incremental adapter (build with -tags tabnas_nodecell)"

// TestSpecRun runs run.tsv: a program run over a JSON document, the bytes
// it writes or the failure. Every row runs twice, with the standard
// compositions native and through the library's text, and the two must
// agree to the byte, or on the failure's code and position: a row whose
// two paths differ fails whatever its expected cell says.
//
// A run reads its document incrementally. A build without the adapter
// (no tabnas_nodecell tag) runs the rows that end before the document is
// read, and skips the rest by name, saying why.
func TestSpecRun(t *testing.T) {
	spec, err := support.LoadSpec(filepath.Join(specDir(t), "run.tsv"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Rows) == 0 {
		t.Fatal("run.tsv holds no rows")
	}
	if got := strings.Join(spec.Rows[0].Header, " "); got != "input expected doc render" {
		t.Fatalf("run.tsv's columns are %s", got)
	}
	r := support.Runner{
		ParseRow: func(input string, row *support.Row) (any, error) {
			doc, render := runDoc(row), runRender(t, row)
			native := runBoth(input, doc, render, true)
			interpreted := runBoth(input, doc, render, false)
			if !native.agrees(interpreted) {
				return nil, fmt.Errorf("native and interpreted runs disagree:\n  native:      %s\n  interpreted: %s", native, interpreted)
			}
			if native.fail != nil {
				return nil, asErr(native.fail)
			}
			return native.out, nil
		},
		ErrorCode: func(err error) string {
			if f, ok := failOf(err); ok {
				return runCode(f)
			}
			return err.Error()
		},
		ErrorPos: func(err error) (int, int, bool) {
			if f, ok := failOf(err); ok && f.Row > 0 {
				return int(f.Row), int(f.Column), true
			}
			return 0, 0, false
		},
	}
	ran, skipped := 0, 0
	for _, row := range spec.Rows {
		input := row.Unesc(0)
		t.Run(fmt.Sprintf("row %d", row.Line), func(t *testing.T) {
			if !tt.AdapterBuilt() && !beforeTheSource(input, runRender(t, row)) {
				skipped++
				t.Skip(row.Where() + ": " + needsIncremental)
			}
			ran++
			if err := r.CheckRow(row, input, row.Col(1)); err != nil {
				t.Error(err)
			}
		})
	}
	t.Logf("run.tsv: %d rows run, %d skipped (no incremental adapter)", ran, skipped)
}

// rowFailures is what a row's program meets, as the runners above meet it:
// the reader's (reader.tsv), the desugarer's (pipe.tsv), compile's
// (check.tsv), or a run's on both paths (run.tsv). ran is false for a run
// row this build cannot run: one that reads its document, in a build
// without the incremental adapter.
func rowFailures(t *testing.T, file string, row *support.Row) (fails []*Fail, ran bool) {
	input := row.Unesc(0)
	var f *Fail
	switch file {
	case "reader.tsv":
		_, f = readerRow(input)
	case "pipe.tsv":
		_, f = pipeRow(input)
	case "check.tsv":
		_, f = checkRow(input)
	default:
		render := runRender(t, row)
		if !tt.AdapterBuilt() && !beforeTheSource(input, render) {
			return nil, false
		}
		for _, native := range []bool{true, false} {
			if o := runBoth(input, runDoc(row), render, native); o.fail != nil {
				fails = append(fails, o.fail)
			}
		}
		return fails, true
	}
	if f != nil {
		fails = append(fails, f)
	}
	return fails, true
}

// placeholder is a template's `{name}`; finalPlaceholder one that ends it,
// with the space before it.
var (
	placeholder      = regexp.MustCompile(`\{[a-z_]+\}`)
	finalPlaceholder = regexp.MustCompile(`\s*\{[a-z_]+\}$`)
)

// fills reports whether text fills the template line: its fixed parts in
// order, each `{name}` any text, none included.
func fills(line, text string) bool {
	parts := placeholder.Split(line, -1)
	if len(parts) == 1 {
		return text == line
	}
	first, last := parts[0], parts[len(parts)-1]
	if len(text) < len(first)+len(last) || !strings.HasPrefix(text, first) || !strings.HasSuffix(text, last) {
		return false
	}
	end := len(text) - len(last)
	at := len(first)
	for _, part := range parts[1 : len(parts)-1] {
		found := strings.Index(text[at:end], part)
		if found < 0 {
			return false
		}
		at += found + len(part)
	}
	return true
}

// instanceOf reports whether text is an instance of the template line. The
// engine trims the messages it writes from a template, so a `{name}` that
// ends a line may take the space before it with it.
func instanceOf(line, text string) bool {
	if fills(line, text) {
		return true
	}
	bare := finalPlaceholder.ReplaceAllString(line, "")
	return bare != line && fills(bare, text)
}

// fixedLength is the fixed text of a template: what a more specific line
// has more of.
func fixedLength(line string) int {
	return len(placeholder.ReplaceAllString(line, ""))
}

// TestTheRaisedMessagesMatchTheDocument: every failure the shared fixtures
// meet is declared in the grammar document (rs/tests/spec_test.rs,
// the_raised_messages_match_the_document). The finer code that leads the
// message is a key of the installed error messages, the engine's and this
// grammar's, and the text after it is a line of that entry, each `{name}`
// standing for what the raising site fills in (a failure raised inside the
// standard library ends with its position there, ` (at stdlib/...)`, which
// is not part of the text). Each line of each code the document declares is
// the most specific line some row meets, so the document holds no text
// nothing raises, and each of its codes has a hint. A finer code comes
// with the same code wherever it is raised (bad_let is a DSL_PARSE_ERROR
// from the desugarer and from the resolver alike). A raising site whose
// code or text drifts from the document fails here.
//
// A build without the incremental adapter cannot run the run rows that
// read their document; it checks the failures it meets, and leaves the
// lines only those rows meet to a build with the adapter.
//
// This is the whole test, over every error row of all four fixtures.
// alchemy runs it too, over reader.tsv's, pipe.tsv's and check.tsv's
// rows alone, without the clause that each line is met, which needs
// run.tsv's.
func TestTheRaisedMessagesMatchTheDocument(t *testing.T) {
	installed := Make().Config().ErrorMessages
	doc, err := documentValue()
	if err != nil {
		t.Fatal(err)
	}
	options := doc["options"].(map[string]any)
	declared := options["error"].(map[string]any)
	hints := options["hint"].(map[string]any)
	specs, err := support.LoadSpecDir(specDir(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	met := map[string]bool{}
	// The codes each finer code is raised with: one, wherever it is raised.
	raisedAs := map[string]map[Code]bool{}
	failures, skipped := 0, 0
	for _, spec := range specs {
		for _, row := range spec.Rows {
			if !support.IsErrorExpect(row.Col(1)) {
				continue
			}
			fails, ran := rowFailures(t, spec.Name, row)
			if !ran {
				skipped++
				continue
			}
			for _, f := range fails {
				switch f.Code {
				case CodeDSLParseError, CodeDSLTypeError, CodeStreamReused, CodeStreamabilityUnknown:
				default:
					continue
				}
				failures++
				code, text, ok := strings.Cut(f.Message, ": ")
				if !ok {
					t.Errorf("%s: %s has no finer code: %s", row.Where(), f.Code, f.Message)
					continue
				}
				if raisedAs[code] == nil {
					raisedAs[code] = map[Code]bool{}
				}
				raisedAs[code][f.Code] = true
				if library := strings.LastIndex(text, " (at stdlib/"); library >= 0 && strings.HasSuffix(text, ")") {
					text = text[:library]
				}
				entry, ok := installed[code]
				if !ok {
					t.Errorf("%s: %s is not declared in options.error", row.Where(), code)
					continue
				}
				best := ""
				found := false
				for _, line := range strings.Split(entry, "\n") {
					if instanceOf(line, text) && (!found || fixedLength(line) > fixedLength(best)) {
						best, found = line, true
					}
				}
				if !found {
					t.Errorf("%s: no line of options.error.%s is %q", row.Where(), code, text)
					continue
				}
				met[code+"\n"+best] = true
			}
		}
	}
	if failures == 0 {
		t.Fatal("the fixtures meet no failures")
	}
	for code, uppers := range raisedAs {
		if len(uppers) > 1 {
			names := make([]string, 0, len(uppers))
			for upper := range uppers {
				names = append(names, upper.String())
			}
			sort.Strings(names)
			t.Errorf("%s is raised as %s; a finer code has one code", code, strings.Join(names, " and "))
		}
	}
	for code := range declared {
		if _, ok := hints[code].(string); !ok {
			t.Errorf("options.hint.%s is not declared", code)
		}
	}
	if skipped > 0 {
		t.Logf("%d run rows read their document, which this build cannot (build with -tags tabnas_nodecell); every line met is left to a build that can", skipped)
		return
	}
	codes := make([]string, 0, len(declared))
	for code := range declared {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		entry, _ := declared[code].(string)
		for _, line := range strings.Split(entry, "\n") {
			if !met[code+"\n"+line] {
				t.Errorf("options.error.%s: no fixture row meets %q", code, line)
			}
		}
	}
}

// documentValue is the grammar document as a plain value, as alchemy's own
// (unexported) documentValue reads it: GrammarText, the text alchemy
// embeds, read with the strict JSON grammar, comment lexing on.
func documentValue() (map[string]any, error) {
	reader := tabnasjson.Make()
	on := true
	reader.SetOptions(tabnas.Options{Comment: &tabnas.CommentOptions{Lex: &on}})
	value, err := reader.Parse(GrammarText())
	if err != nil {
		return nil, fmt.Errorf("alchemy-grammar.jsonic does not read: %w", err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	err = json.Unmarshal(data, &out)
	return out, err
}
