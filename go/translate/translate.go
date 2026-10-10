// Copyright (c) 2026 tabnas, MIT License

// Package translate is `alchemy translate`: a document in one of the
// formats whose grammar modules this command carries, written in any of
// them. It is the Go port of rs/src/translate.rs, and the command
// (cmd/alchemy) and its tests both build on it.
//
// Every format's module exports its translation parts: its manifest's
// translate object (the shapes it reads and writes, the root its render
// needs, the schema its events carry), its render, and, where it has them,
// its lift and its embedding, each an alchemy file. This package reads them
// into alchemy's Parts, has alchemy's translate package compose the route
// (the source's lift, the root adapters, the embedding, the inferred table,
// the render), compiles the composition with transduce's routers and
// render's renderers, and runs it over the input read with the source
// format's grammar: incrementally where transduce's differential suite has
// verified that grammar (tt.Incremental), so that a number keeps the lexeme
// the document spelled it with, and materialized otherwise, when a path
// selects a value below the root, and for a document whose value the
// grammar builds otherwise than its events showed (a YAML stream of several
// documents, a merge key, a repeated member), which the incremental source
// refuses part way: the grammar's own value is the document, so it is read
// again whole. Where a module's parser builds a value that is not yet the
// tree its parts declare (an expression's operators, a typed descriptor,
// feed or database), the tree the module's API reads a document as is the
// document, read whole. Nothing here knows a format by its name: a format
// is what its manifest says, and a module whose manifest names no parts
// this host can take is not a format here.
//
// The incremental source needs the engine's node-cell identity, which
// transduce builds only with the tabnas_nodecell tag. Without it no grammar
// is verified, so every document is read whole, and a number is written by
// its value rather than by the lexeme the document spelled it with.
package translate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	alchemy "github.com/tabnas/alchemy/go"
	at "github.com/tabnas/alchemy/go/translate"
	tabnaschess "github.com/tabnas/chess/go"
	tabnascss "github.com/tabnas/css/go"
	tabnascsv "github.com/tabnas/csv/go"
	tabnasexpr "github.com/tabnas/expr/go"
	tabnasfeed "github.com/tabnas/feed/go"
	tabnasini "github.com/tabnas/ini/go"
	tabnasjson "github.com/tabnas/json/go"
	tabnasjson5 "github.com/tabnas/json5/go"
	tabnasjsonc "github.com/tabnas/jsonc/go"
	tabnasjsonic "github.com/tabnas/jsonic/go"
	tabnasjsonl "github.com/tabnas/jsonl/go"
	tabnasmarkdown "github.com/tabnas/markdown/go"
	tabnas "github.com/tabnas/parser/go"
	tabnasproto "github.com/tabnas/proto/go"
	tr "github.com/tabnas/render/go"
	tabnassemver "github.com/tabnas/semver/go"
	tabnastoml "github.com/tabnas/toml/go"
	tt "github.com/tabnas/transduce/go"
	tabnasxml "github.com/tabnas/xml/go"
	tabnasyaml "github.com/tabnas/yaml/go"
	tabnaszon "github.com/tabnas/zon/go"
)

// Format is one format this command reads and writes: its parts, as its
// module's manifest names them, and the grammar its documents are read
// with.
type Format struct {
	Part *at.Part
	// reader is a source over a text with a fresh parser of the format's
	// grammar under its own options: a ParserSource takes its parser over,
	// so each read has one. It is nil where tree is not.
	reader func(text string) *tt.ParserSource
	// tree is a document as the tree the format's module reads it as with
	// its own API, where its parser's value is not yet the tree its parts
	// declare (an expression's operators, or a typed value). It is nil
	// where reader is not.
	tree func(text string) (any, *tt.Fail)
}

// ID is the manifest's languageId.
func (f *Format) ID() string { return f.Part.ID }

// run drives sink with a document's events as the format reads it: its
// parser's, through transduce's ParserSource in mode, or the tree its
// module's API reads, read whole whatever the mode and walked as the
// ParserSource walks a parser's value, under the same limits and counts.
func (f *Format) run(input string, mode tt.SourceMode, limits tt.Limits, metrics *tt.Metrics, sink tt.Sink) (tt.Flow, *tt.Fail) {
	if f.tree == nil {
		return f.reader(input).Grammar(f.ID()).Mode(mode).Limits(limits).Metrics(metrics).Run(sink)
	}
	guarded := tt.NewGuarded(sink, limits, tt.NewAbortFlag(), metrics)
	defer guarded.Flush()
	tree, fail := f.tree(input)
	if fail != nil {
		return tt.Continue, fail
	}
	flow, fail := tt.WalkValue(tree, guarded)
	if fail != nil || flow == tt.Stop {
		return flow, fail
	}
	return guarded.Event(tt.EvEnd())
}

// Read is a document read whole with this format's grammar, as a value:
// its events as translate reads them, incrementally where the grammar is
// verified (so a number keeps the lexeme the document spelled it with),
// collected, or the grammar's value where the incremental source cannot
// follow it. A repeated member keeps its last value.
func (f *Format) Read(input string, limits tt.Limits) (tt.Datum, *tt.Fail) {
	if tt.Incremental(f.ID()) {
		value, fail := read(f, input, tt.IncrementalMode(tt.Prune{}), limits)
		if fail == nil || !unfollowed(fail) {
			return value, fail
		}
	}
	return read(f, input, tt.MaterializeMode(), limits)
}

// modulePart is a grammar module's TranslationPart. Every module owns its
// interface types, which have this shape, so a module's part converts to
// it field for field, and a module whose part changes shape fails to
// compile here.
type modulePart struct {
	Entry  string
	Source string
}

// partText is a module's part as alchemy's: nil for none, a render alchemy
// carries for a part with no source (json, csv), an alchemy file
// otherwise.
func partText(p *modulePart) *at.PartText {
	switch {
	case p == nil:
		return nil
	case p.Source == "":
		return at.CarriedPartText(p.Entry)
	}
	return at.NewPartText(p.Entry, p.Source)
}

// descriptor is a module's parts as alchemy's descriptor, named by the
// module's package name in diagnostics.
func descriptor(name, manifest string, lift, embed, render *modulePart) at.Descriptor {
	return at.Descriptor{
		Package:  name,
		Manifest: manifest,
		Lift:     partText(lift),
		Embed:    partText(embed),
		Render:   partText(render),
	}
}

// installed panics on the error a grammar module's plugin gives when it
// does not install: its documents are fixed, so only a defect of the
// module fails it, as the modules' own constructors panic.
func installed(name string, err error) {
	if err != nil {
		panic(name + ": the grammar does not install: " + err.Error())
	}
}

// module is a grammar module this command carries: its parts, and its
// reader or its tree (Format's).
type module struct {
	descriptor func() at.Descriptor
	reader     func(text string) *tt.ParserSource
	tree       func(text string) (any, *tt.Fail)
}

// modules is every grammar module this command carries, each with how its
// documents are read: the module's own parser, under its own options, or,
// where its parser's value is not yet the tree its parts declare, the tree
// its own API reads a document as.
func modules() []module {
	return []module{
		{descriptor: func() at.Descriptor {
			p := tabnaschess.Translate()
			return descriptor("tabnas-chess", p.Manifest, (*modulePart)(p.Lift), (*modulePart)(p.Embed), (*modulePart)(p.Render))
		}, tree: pgnTree},
		{descriptor: func() at.Descriptor {
			p := tabnascss.Translate()
			return descriptor("tabnas-css", p.Manifest, (*modulePart)(p.Lift), (*modulePart)(p.Embed), (*modulePart)(p.Render))
		}, reader: func(text string) *tt.ParserSource { return tt.NewParserSource(tabnascss.MakeJsonic(), text) }},
		{descriptor: func() at.Descriptor {
			p := tabnascsv.Translate()
			return descriptor("tabnas-csv", p.Manifest, (*modulePart)(p.Lift), (*modulePart)(p.Embed), (*modulePart)(p.Render))
		}, reader: func(text string) *tt.ParserSource {
			parser, err := tabnascsv.Make()
			installed("tabnas-csv", err)
			return tt.NewParserSource(parser, text)
		}},
		{descriptor: func() at.Descriptor {
			p := tabnasexpr.Translate()
			return descriptor("tabnas-expr", p.Manifest, (*modulePart)(p.Lift), (*modulePart)(p.Embed), (*modulePart)(p.Render))
		}, tree: exprTree},
		{descriptor: func() at.Descriptor {
			p := tabnasfeed.Translate()
			return descriptor("tabnas-feed", p.Manifest, (*modulePart)(p.Lift), (*modulePart)(p.Embed), (*modulePart)(p.Render))
		}, tree: feedTree},
		{descriptor: func() at.Descriptor {
			p := tabnasini.Translate()
			return descriptor("tabnas-ini", p.Manifest, (*modulePart)(p.Lift), (*modulePart)(p.Embed), (*modulePart)(p.Render))
		}, reader: func(text string) *tt.ParserSource { return tt.NewParserSource(tabnasini.MakeJsonic(), text) }},
		{descriptor: func() at.Descriptor {
			p := tabnasjson.Translate()
			return descriptor("tabnas-json", p.Manifest, (*modulePart)(p.Lift), (*modulePart)(p.Embed), (*modulePart)(p.Render))
		}, reader: func(text string) *tt.ParserSource { return tt.NewParserSource(tabnasjson.Make(), text) }},
		{descriptor: func() at.Descriptor {
			p := tabnasjson5.Translate()
			return descriptor("tabnas-json5", p.Manifest, (*modulePart)(p.Lift), (*modulePart)(p.Embed), (*modulePart)(p.Render))
		}, reader: func(text string) *tt.ParserSource {
			parser := tabnasjsonic.Make()
			installed("tabnas-json5", parser.UseDefaults(tabnasjson5.Json5, tabnasjson5.Defaults()))
			return tt.NewParserSource(parser, text)
		}},
		{descriptor: func() at.Descriptor {
			p := tabnasjsonc.Translate()
			return descriptor("tabnas-jsonc", p.Manifest, (*modulePart)(p.Lift), (*modulePart)(p.Embed), (*modulePart)(p.Render))
		}, reader: func(text string) *tt.ParserSource {
			parser := tabnasjsonic.Make()
			installed("tabnas-jsonc", parser.Use(tabnasjsonc.Jsonc))
			return tt.NewParserSource(parser, text)
		}},
		{descriptor: func() at.Descriptor {
			p := tabnasjsonic.Translate()
			return descriptor("tabnas-jsonic", p.Manifest, (*modulePart)(p.Lift), (*modulePart)(p.Embed), (*modulePart)(p.Render))
		}, reader: func(text string) *tt.ParserSource { return tt.NewParserSource(tabnasjsonic.Make(), text) }},
		{descriptor: func() at.Descriptor {
			p := tabnasjsonl.Translate()
			return descriptor("tabnas-jsonl", p.Manifest, (*modulePart)(p.Lift), (*modulePart)(p.Embed), (*modulePart)(p.Render))
		}, reader: func(text string) *tt.ParserSource { return tt.NewParserSource(tabnasjsonl.Make(), text) }},
		{descriptor: func() at.Descriptor {
			p := tabnasmarkdown.Translate()
			return descriptor("tabnas-markdown", p.Manifest, (*modulePart)(p.Lift), (*modulePart)(p.Embed), (*modulePart)(p.Render))
		}, reader: func(text string) *tt.ParserSource { return tt.NewParserSource(tabnasmarkdown.Make(), text) }},
		{descriptor: func() at.Descriptor {
			p := tabnasproto.Translate()
			return descriptor("tabnas-proto", p.Manifest, (*modulePart)(p.Lift), (*modulePart)(p.Embed), (*modulePart)(p.Render))
		}, tree: protoTree},
		{descriptor: func() at.Descriptor {
			p := tabnassemver.Translate()
			return descriptor("tabnas-semver", p.Manifest, (*modulePart)(p.Lift), (*modulePart)(p.Embed), (*modulePart)(p.Render))
		}, reader: func(text string) *tt.ParserSource { return tt.NewParserSource(tabnassemver.Make(), text) }},
		{descriptor: func() at.Descriptor {
			p := tabnastoml.Translate()
			return descriptor("tabnas-toml", p.Manifest, (*modulePart)(p.Lift), (*modulePart)(p.Embed), (*modulePart)(p.Render))
		}, reader: func(text string) *tt.ParserSource { return tt.NewParserSource(tabnastoml.MakeJsonic(), text) }},
		{descriptor: func() at.Descriptor {
			p := tabnasxml.Translate()
			return descriptor("tabnas-xml", p.Manifest, (*modulePart)(p.Lift), (*modulePart)(p.Embed), (*modulePart)(p.Render))
		}, reader: func(text string) *tt.ParserSource {
			// XML's plugin, outside its embed mode, reconfigures the jsonic
			// host it is installed on as a pure XML parser, jsonic's own
			// grammar and lexers unreachable.
			parser := tabnasjsonic.Make()
			installed("tabnas-xml", parser.UseDefaults(tabnasxml.Xml, tabnasxml.Defaults))
			return tt.NewParserSource(parser, text)
		}},
		{descriptor: func() at.Descriptor {
			p := tabnasyaml.Translate()
			return descriptor("tabnas-yaml", p.Manifest, (*modulePart)(p.Lift), (*modulePart)(p.Embed), (*modulePart)(p.Render))
		}, reader: func(text string) *tt.ParserSource { return tt.NewParserSource(tabnasyaml.MakeJsonic(), text) }},
		{descriptor: func() at.Descriptor {
			p := tabnaszon.Translate()
			return descriptor("tabnas-zon", p.Manifest, (*modulePart)(p.Lift), (*modulePart)(p.Embed), (*modulePart)(p.Render))
		}, reader: func(text string) *tt.ParserSource { return tt.NewParserSource(zonParser(), text) }},
	}
}

// bigKey is the member of the object that is ZON's reader's big integer.
const bigKey = "$big"

// zonParser is ZON's parser, with an integer no float64 holds exactly as
// the object {"$big": "<digits>"}, the digits after a minus sign when it is
// negative: the reader's big integer as ZON's translation part declares it
// and its Rust reader builds it. The Go reader builds a *big.Int, which no
// transduce event carries, so the token's value is replaced as it is read,
// before a rule takes it, and the value and the events hold the object.
func zonParser() *tabnas.Tabnas {
	parser := tabnaszon.MakeJsonic()
	parser.Sub(func(tkn *tabnas.Token, _ *tabnas.Rule, _ *tabnas.Context) {
		if n, ok := tkn.Val.(*big.Int); ok {
			tkn.Val = &tabnas.OrderedMap{Keys: []string{bigKey}, Vals: map[string]any{bigKey: n.String()}}
		}
	}, nil)
	return parser
}

// A tree reader's parser is built once and reused, as the modules' own
// one-call parses reuse theirs: building the grammar dominates a parse, a
// parse builds a fresh context and only reads the instance, and no source
// installs a budget or a subscriber on it.
var (
	protoParser = sync.OnceValue(func() *tabnas.Tabnas {
		history := 8192
		parser := tabnas.Make(tabnas.Options{Rewind: &tabnas.RewindOptions{History: &history}})
		installed("tabnas-proto", tabnasproto.Proto(parser))
		return parser
	})
	feedParser = sync.OnceValue(func() *tabnas.Tabnas {
		parser := tabnas.Make()
		installed("tabnas-feed", parser.UseDefaults(tabnasfeed.Feed, tabnasfeed.Defaults))
		return parser
	})
)

// exprTree is an expression as expr's module reads one for its shared
// fixtures, the tree its parts declare: each operation a list whose first
// element is the operator's source text (Simplify of its Parse). The
// parser's own value holds an operation's operator as an *Op, which no
// transduce event carries.
func exprTree(text string) (any, *tt.Fail) {
	value, err := tabnasexpr.Parse(text)
	if err != nil {
		return nil, engineFailure(err)
	}
	return tabnasexpr.Simplify(value), nil
}

// protoTree is a .proto file as proto's module reads one, the descriptor
// its parts declare (ToDescriptor of the parse, as its Parse builds it).
// The parser's own value is the grammar's syntax tree.
func protoTree(text string) (any, *tt.Fail) {
	cst, err := protoParser().Parse(text)
	if err != nil {
		return nil, engineFailure(err)
	}
	descriptor, err := tabnasproto.ToDescriptor(cst, nil)
	if err != nil {
		return nil, tt.InputFail(err.Error())
	}
	return plainTree(descriptor)
}

// feedTree is a feed as feed's module reads one by default, the Atom-shaped
// feed its parts declare: the parser builds it as a typed AtomFeed.
func feedTree(text string) (any, *tt.Fail) {
	feed, err := feedParser().Parse(text)
	if err != nil {
		return nil, engineFailure(err)
	}
	return plainTree(feed)
}

// pgnTree is a PGN database as chess's module reads one (its Parse), the
// tree its parts declare: the parser builds it as a typed Database.
func pgnTree(text string) (any, *tt.Fail) {
	database, err := tabnaschess.Parse(text)
	if err != nil {
		return nil, engineFailure(err)
	}
	return plainTree(database)
}

// plainTree is a module's typed value as the tree its JSON encoding names:
// encoding/json writes it, its fields' tags naming the members, in the
// fields' order (a map's in sorted key order), and the engine's ordered map
// reads it back, so that the walk keeps that order.
func plainTree(value any) (any, *tt.Fail) {
	text, err := json.Marshal(map[string]any{"tree": value})
	if err == nil {
		var tree tabnas.OrderedMap
		if err = json.Unmarshal(text, &tree); err == nil {
			return tree.Vals["tree"], nil
		}
	}
	return nil, tt.InputFail("the document's value has no tree: " + err.Error())
}

// engineFailure is an engine error as transduce's ParserSource reports it:
// the input's, with the engine's code and position, and a cancel, which no
// abort of this command's asks for, a guard of the grammar's own.
func engineFailure(err error) *tt.Fail {
	te, ok := err.(*tabnas.TabnasError)
	if !ok {
		return tt.InputFail(err.Error())
	}
	f := tt.FailFromTabnas(te)
	if te.Code == "cancel" {
		f.Message = fmt.Sprintf("the grammar stopped the parse with a guard of its own (%s: %s); a grammar "+
			"may refuse nesting or size below this crate's Limits", te.Code, strings.TrimRight(te.Detail, " \t\r\n"))
	}
	return f
}

var (
	registryOnce sync.Once
	registry     []*Format
)

// Formats is every format, by id, read once. The slice is the caller's.
func Formats() []*Format {
	registryOnce.Do(func() {
		for _, m := range modules() {
			part := at.PartFromDescriptor(m.descriptor())
			if part == nil {
				continue
			}
			registry = append(registry, &Format{Part: part, reader: m.reader, tree: m.tree})
		}
		sort.SliceStable(registry, func(i, j int) bool { return registry[i].Part.ID < registry[j].Part.ID })
	})
	return append([]*Format(nil), registry...)
}

// Named is the format a command line names, by its id, or nil.
func Named(id string) *Format {
	for _, f := range Formats() {
		if f.ID() == id {
			return f
		}
	}
	return nil
}

// Names is the formats' ids for a message: `csv, ini, ... or zon`.
func Names() string {
	formats := Formats()
	ids := make([]string, len(formats))
	for i, f := range formats {
		ids[i] = f.ID()
	}
	switch len(ids) {
	case 0:
		return ""
	case 1:
		return ids[0]
	}
	return strings.Join(ids[:len(ids)-1], ", ") + " or " + ids[len(ids)-1]
}

// Segment is one step of a Path: a member's key, or an element's index.
type Segment struct {
	Key     string
	Index   uint64
	IsIndex bool
}

// Path is a value below a document's root, as the segments --path names.
type Path struct {
	Segments []Segment
}

// String is the path as the JSON array it was given as.
func (p *Path) String() string {
	items := make([]tt.Datum, len(p.Segments))
	for i, s := range p.Segments {
		if s.IsIndex {
			items[i] = tt.NumberDatumLexeme(float64(s.Index), strconv.FormatUint(s.Index, 10))
		} else {
			items[i] = tt.StringDatum(s.Key)
		}
	}
	return tt.ArrayDatum(items...).String()
}

// find is the value at the path below d.
func (p *Path) find(d *tt.Datum) (*tt.Datum, bool) {
	here := d
	for _, s := range p.Segments {
		switch {
		case s.IsIndex && here.Kind == tt.DatumArray:
			if s.Index >= uint64(len(here.Items)) {
				return nil, false
			}
			here = &here.Items[s.Index]
		case !s.IsIndex && here.Kind == tt.DatumObject:
			next, ok := here.Get(s.Key)
			if !ok {
				return nil, false
			}
			here = next
		default:
			return nil, false
		}
	}
	return here, true
}

// Request is what a translation was asked.
type Request struct {
	// From is the format the input is read with.
	From *Format
	// To is the format it is written in.
	To *Format
	// Path is a value below the root to translate instead of the whole
	// document, nil for the whole document: a plain tree, whatever the
	// source's shapes.
	Path *Path
	// Options are what the host chooses: the key a root is wrapped under.
	Options at.Options
	// Program is a program, as its file name and its text, whose export
	// stands in the source's place: its JSON events a tree, its table
	// records. Nil for none.
	Program *alchemy.Source
	Limits  tt.Limits
}

// Compiled is the composition a request makes, and the program it compiles
// to: one compiled composition serves many inputs.
type Compiled struct {
	Composition *at.Composition
	Program     *alchemy.Program
}

// Compile is the composition a request makes, and the program it compiles
// to.
func Compile(r *Request) (*Compiled, *tt.Fail) {
	routers, renderers := tt.Routers(), tr.Renderers()
	if r.Program != nil {
		file, text := r.Program.File, r.Program.Text
		program, f := alchemy.Compile(text, file, routers, renderers)
		if f != nil {
			return nil, f
		}
		output := program.Output()
		if output == alchemy.OutputText {
			return nil, tt.NewFail(tt.CodeDSLTypeError, "bad_output: "+file+"'s export writes its own text, and "+
				"translate takes a program whose export answers JSON events or a table, which a format's "+
				"render then writes")
		}
		composition, f := at.ComposeProgram(output, r.To.Part, r.Options, "translate")
		if f != nil {
			return nil, f
		}
		compiled, f := composition.Compile(&alchemy.Source{File: file, Text: text}, routers, renderers)
		if f != nil {
			return nil, f
		}
		return &Compiled{Composition: composition, Program: compiled}, nil
	}
	source := r.From.Part
	if r.Path != nil {
		source = nil
	}
	composition, f := at.Compose(source, r.To.Part, r.Options, "translate")
	if f != nil {
		return nil, f
	}
	compiled, f := composition.Compile(nil, routers, renderers)
	if f != nil {
		return nil, f
	}
	return &Compiled{Composition: composition, Program: compiled}, nil
}

// unfollowed is whether the incremental source refused a document because
// the grammar's value is not what its events showed (a root wrapped or
// replaced after it streamed, a map rewritten, a repeated member that the
// grammar keeps once): the grammar's own value is the document, so it is
// read again whole.
func unfollowed(f *tt.Fail) bool {
	return f.Code == tt.CodeStreamabilityUnknown || f.Code == tt.CodeDuplicateMember
}

// Run runs a request over input, writing the document to out once the run
// has succeeded; metrics (a fresh set when nil) collects what the stages
// report, the output's bytes among it.
func Run(r *Request, input string, out io.Writer, metrics *tt.Metrics) *tt.Fail {
	compiled, f := Compile(r)
	if f != nil {
		return f
	}
	return RunCompiled(r, compiled, input, out, metrics)
}

// RunCompiled is Run, with the request's composition compiled already
// (Compile), so that one compiled composition serves many inputs.
//
// What a run writes is held until the run has succeeded, so that a run the
// incremental source gives up part way is run again from the start rather
// than written twice.
func RunCompiled(r *Request, compiled *Compiled, input string, out io.Writer, metrics *tt.Metrics) *tt.Fail {
	if metrics == nil {
		metrics = tt.NewMetrics()
	}
	attempt := func(mode tt.SourceMode, metrics *tt.Metrics) ([]byte, *tt.Fail) {
		var held bytes.Buffer
		sink, f := compiled.Program.Sink(&held, alchemy.RenderDefault, r.Limits, metrics)
		if f != nil {
			return nil, f
		}
		// The source's events reach a tree's render as the source made
		// them, so a stream that is no tree's is refused in front of it
		// rather than written half way.
		if compiled.Composition.Front == at.FrontTree {
			sink = tt.NewTreeContract(sink)
		}
		if r.Path == nil {
			_, f := r.From.run(input, mode, r.Limits, metrics, sink)
			if f != nil {
				return nil, f
			}
			return held.Bytes(), nil
		}
		value, f := read(r.From, input, tt.MaterializeMode(), r.Limits)
		if f != nil {
			return nil, f
		}
		selected, ok := r.Path.find(&value)
		if !ok {
			return nil, tt.InputFail("the path " + r.Path.String() + " names nothing in the document")
		}
		flow, f := tt.WalkDatum(selected, sink)
		if f != nil {
			return nil, f
		}
		if flow == tt.Continue {
			if _, f := sink.Event(tt.EvEnd()); f != nil {
				return nil, f
			}
		}
		return held.Bytes(), nil
	}
	var written []byte
	var f *tt.Fail
	if r.Path == nil && tt.Incremental(r.From.ID()) {
		prune := tt.Prune{}
		if selector, ok := compiled.Program.RowSelector(); ok {
			prune = tt.PruneUnderSelector(selector)
		}
		// The incremental attempt counts into metrics of its own, which
		// the caller's take on once it has succeeded: a run given up part
		// way counts nothing.
		own := tt.NewMetrics()
		written, f = attempt(tt.IncrementalMode(prune), own)
		if f != nil && unfollowed(f) {
			written, f = attempt(tt.MaterializeMode(), metrics)
		} else if f == nil {
			absorb(metrics, own)
		}
	} else {
		written, f = attempt(tt.MaterializeMode(), metrics)
	}
	if f != nil {
		return f
	}
	if len(written) == 0 {
		return nil
	}
	if n, err := out.Write(written); err != nil {
		f := tt.NewFail(tt.CodeOutputFailed, "the output could not be written: "+err.Error())
		// Bytes the writer took before it failed have left.
		if n > 0 {
			f.Committed()
		}
		return f
	}
	return nil
}

// absorb adds what an attempt counted to the caller's metrics: the counts
// summed and the high-water marks raised, so that the metrics a run hands
// back are those of the attempt whose output was written.
func absorb(into, from *tt.Metrics) {
	into.Events.Add(from.Events.Load())
	into.Keys.Add(from.Keys.Load())
	into.Scalars.Add(from.Scalars.Load())
	into.Rows.Add(from.Rows.Load())
	into.CapturedBytes.Add(from.CapturedBytes.Load())
	into.OutputBytes.Add(from.OutputBytes.Load())
	raise(&into.CapturedBytesHigh, from.CapturedBytesHigh.Load())
	raise(&into.RetainedBytesHigh, from.RetainedBytesHigh.Load())
}

// raise lifts a high-water mark to v when v is higher.
func raise(high *atomic.Uint64, v uint64) {
	for {
		now := high.Load()
		if v <= now || high.CompareAndSwap(now, v) {
			return
		}
	}
}

// collect is the events of a document, collected into a value.
type collect struct{ builder *tt.DatumBuilder }

func (c *collect) Event(ev tt.Event) (tt.Flow, *tt.Fail) {
	if ev.Kind != tt.End {
		if f := c.builder.Event(ev); f != nil {
			return tt.Continue, f
		}
	}
	return tt.Continue, nil
}

// read is a document read whole with a format's grammar, as a value. A
// repeated member keeps its last value, as the grammar's own value does.
func read(format *Format, input string, mode tt.SourceMode, limits tt.Limits) (tt.Datum, *tt.Fail) {
	c := &collect{builder: tt.NewDatumBuilder(math.MaxInt, "max_capture_bytes", tt.LastWins)}
	if _, f := format.run(input, mode, limits, tt.NewMetrics(), c); f != nil {
		return tt.Datum{}, f
	}
	value, ok := c.builder.Take()
	if !ok {
		return tt.Datum{}, tt.InputFail("the document holds no value")
	}
	return value, nil
}

// maxIndex is the largest index a path can name, 2^64 - 1, and twoTo64 the
// first whole number past it, as a float64.
const (
	maxIndex = math.MaxUint64
	twoTo64  = 18446744073709551616.0
)

// ParsePath is a path given as a JSON array of segments, each a string (a
// member's key) or a whole number (an element's index), the form alchemy's
// as-path validates.
func ParsePath(text string, limits tt.Limits) (*Path, *tt.Fail) {
	json := Named("json")
	if json == nil {
		return nil, tt.InputFail("no JSON grammar to read the path")
	}
	refuse := func() *tt.Fail {
		return tt.InputFail("--path takes a JSON array of keys and indexes, such as [\"people\",0], not " + text)
	}
	value, f := json.Read(text, limits)
	if f != nil || value.Kind != tt.DatumArray {
		return nil, refuse()
	}
	path := &Path{Segments: make([]Segment, 0, len(value.Items))}
	for _, item := range value.Items {
		switch {
		case item.Kind == tt.DatumString:
			path.Segments = append(path.Segments, Segment{Key: item.Text})
		case item.Kind == tt.DatumNumber && item.Value-math.Trunc(item.Value) == 0 &&
			item.Value >= 0 && item.Value <= twoTo64:
			// A whole number up to 2^64, the largest index as a float64
			// rounds to, saturating there.
			index := uint64(maxIndex)
			if item.Value < twoTo64 {
				index = uint64(item.Value)
			}
			path.Segments = append(path.Segments, Segment{Index: index, IsIndex: true})
		default:
			return nil, refuse()
		}
	}
	return path, nil
}

// FormatsJSON is the registry as JSON, one object per format, for `alchemy
// formats`: its id, the shapes it reads and writes, the root its render
// needs, its schema, why its documents are read whole where it says
// (whole, null where it does not), its parts' entries, and its loss
// sentences.
func FormatsJSON() string {
	text := func(s string) tt.Datum { return tt.StringDatum(s) }
	entry := func(a *at.Alc) tt.Datum {
		if a == nil {
			return tt.NullDatum()
		}
		return text(a.Entry)
	}
	formats := Formats()
	items := make([]tt.Datum, len(formats))
	for i, f := range formats {
		p := f.Part
		reads := make([]tt.Datum, len(p.Reads))
		for j, s := range p.Reads {
			reads[j] = text(s.String())
		}
		schema := tt.NullDatum()
		if p.Schema != "" {
			schema = text(p.Schema)
		}
		whole := tt.NullDatum()
		if p.Whole != "" {
			whole = text(p.Whole)
		}
		var render string
		switch p.Render.Kind {
		case at.RenderJSON:
			render = "json"
		case at.RenderCSV:
			render = "csv"
		default:
			render = p.Render.Alc.Entry
		}
		loss := make([]tt.Datum, len(p.Loss))
		for j, l := range p.Loss {
			loss[j] = text(l)
		}
		items[i] = tt.ObjectDatum(
			tt.Member{Key: "id", Value: text(p.ID)},
			tt.Member{Key: "reads", Value: tt.ArrayDatum(reads...)},
			tt.Member{Key: "writes", Value: text(p.Writes.String())},
			tt.Member{Key: "root", Value: text(p.Root.String())},
			tt.Member{Key: "schema", Value: schema},
			tt.Member{Key: "whole", Value: whole},
			tt.Member{Key: "lift", Value: entry(p.Lift)},
			tt.Member{Key: "embed", Value: entry(p.Embed)},
			tt.Member{Key: "render", Value: text(render)},
			tt.Member{Key: "loss", Value: tt.ArrayDatum(loss...)},
		)
	}
	return tt.ArrayDatum(items...).String()
}
