module whatevrd

go 1.26.0

// our fork in a submodule, kept current by merging upstream main into it.
replace go.mau.fi/whatsmeow => ./whatsmeow

require (
	github.com/coder/websocket v1.8.15
	github.com/coreos/go-systemd/v22 v22.7.0
	github.com/godbus/dbus/v5 v5.2.2
	github.com/mattn/go-isatty v0.0.24
	github.com/mattn/go-sqlite3 v1.14.52
	github.com/nyaruka/phonenumbers v1.8.1
	github.com/rs/zerolog v1.35.1
	go.mau.fi/libsignal v0.2.2
	go.mau.fi/whatsmeow v0.0.0-20260929112325-8b41cfe6d9c4
	golang.org/x/crypto v0.57.0
	golang.org/x/sys v0.48.0
	google.golang.org/protobuf v1.36.12
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
	rsc.io/qr v0.2.0
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/beeper/argo-go v1.1.2 // indirect
	github.com/codelif/whatevr/proto v0.0.0
	github.com/elliotchance/orderedmap/v3 v3.1.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/petermattis/goid v0.0.0-20260820044319-269ab09b5261 // indirect
	github.com/vektah/gqlparser/v2 v2.5.37 // indirect
	go.mau.fi/util v0.10.1 // indirect
	golang.org/x/exp v0.0.0-20260908205506-85c1c2202aba // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

replace github.com/codelif/whatevr/proto => ../proto

replace go.mau.fi/util => git.sr.ht/~codelif/go-util v0.10.2-0.20261003185047-cb7dec371f08
