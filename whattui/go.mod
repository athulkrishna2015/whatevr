module whattui

go 1.26.0

// The fork carries the kitty graphics work whattui is built on: raw RGBA
// through shared memory, placement diffing by key, and the text sizing and
// z-index additions this project adds. Same fork pawbar uses.
replace go.rockorager.dev/vaxis => ./vaxis

require (
	github.com/go-text/typesetting v0.3.5
	go.rockorager.dev/vaxis v0.0.0-00010101000000-000000000000
	golang.org/x/image v0.46.0
)

require (
	github.com/rockorager/go-uucode v1.2.2 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/term v0.10.0 // indirect
)
