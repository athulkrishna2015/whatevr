package main

import (
	"fmt"
	"io"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
)

// runOpen is `whatevrd open [url]`: hands a whatevr:// link to the daemon,
// starting it if the service holds the socket. what the desktop's link
// handler runs. no url brings a frontend up.
func runOpen(args []string, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		fmt.Fprintln(stderr, "usage: whatevrd open [whatevr://...]")
		return 2
	}
	url := "whatevr://"
	if len(args) == 1 {
		url = args[0]
	}
	if _, err := frontendCall(v2.Request_builder{LinkOpen: v2.LinkOpen_builder{Url: url}.Build()}.Build()); err != nil {
		fmt.Fprintf(stderr, "whatevrd open: %v\n", err)
		return 1
	}
	return 0
}
