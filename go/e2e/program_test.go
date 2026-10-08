// Copyright (c) 2026 tabnas, MIT License

package e2e

// program_test.go: the API a host embeds (rs/src/program.rs's tests): the
// sink a program writes through, on render's writer. From alchemy's
// go/program_test.go, whose tests of the compiled program alone stay
// there.

import (
	"bytes"
	"testing"

	. "github.com/tabnas/alchemy/go"
	tt "github.com/tabnas/transduce/go"
)

// rowSelectorText is a program's row selector as text, or <none>.
func rowSelectorText(p *Program) string {
	s, ok := p.RowSelector()
	if !ok {
		return "<none>"
	}
	return s.String()
}

func TestTheSinkWritesThroughAWriter(t *testing.T) {
	p := mustCompile(t, workedExample, "t.alc")
	var buffer bytes.Buffer
	sink, f := p.Sink(&buffer, RenderDefault, tt.DefaultLimits(), tt.NewMetrics())
	if f != nil {
		t.Fatal(f)
	}
	d, err := tt.DatumFromJSON(records)
	if err != nil {
		t.Fatal(err)
	}
	if _, f := tt.WalkDatum(&d, sink); f != nil {
		t.Fatal(f)
	}
	if _, f := sink.Event(tt.EvEnd()); f != nil {
		t.Fatal(f)
	}
	if buffer.String() != expectedCSV {
		t.Errorf("%q", buffer.String())
	}
}
