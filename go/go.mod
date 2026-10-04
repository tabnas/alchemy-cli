module github.com/tabnas/alchemy-cli/go

go 1.24.7

// The language, the stages its programs run on (transduce's routers and
// render's renderers), and the JSON grammar `run` reads its document with.
// The shared types are in no release of alchemy yet, nor are the Routers
// and Renderers that transduce and render build on them: a go.work over
// the sibling checkouts resolves all three until their releases do.
require (
	github.com/tabnas/alchemy/go v0.1.3
	github.com/tabnas/json/go v0.5.13
	github.com/tabnas/render/go v0.1.2
	github.com/tabnas/transduce/go v0.1.2
)

require (
	github.com/tabnas/csv/go v0.6.2 // indirect
	github.com/tabnas/jsonic/go v0.7.4 // indirect
	github.com/tabnas/parser/go v0.12.9 // indirect
)
