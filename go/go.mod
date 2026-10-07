module github.com/tabnas/alchemy-cli/go

go 1.24.7

// The language, the stages its programs run on (transduce's routers and
// render's renderers), and the JSON grammar `run` reads its document with.
// The shared types, and the Routers and Renderers that transduce and
// render build on them, are in all three from v0.2.0.
require (
	github.com/tabnas/alchemy/go v0.2.2
	github.com/tabnas/json/go v0.5.15
	github.com/tabnas/render/go v0.2.2
	github.com/tabnas/transduce/go v0.2.3
)

require (
	github.com/tabnas/csv/go v0.6.4 // indirect
	github.com/tabnas/jsonic/go v0.7.6 // indirect
	github.com/tabnas/parser/go v0.12.10 // indirect
)
