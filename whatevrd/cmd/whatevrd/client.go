package main

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"syscall"

	"google.golang.org/protobuf/encoding/protodelim"

	"github.com/codelif/whatevr/platform"
	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/app"
)

// dialDaemon connects to socket, the default one when empty. a socket the
// login service holds starts the daemon on connect.
func dialDaemon(socket string) (net.Conn, error) {
	if socket == "" {
		paths, err := app.ResolvePaths()
		if err != nil {
			return nil, err
		}
		socket = paths.SocketPath
	}
	if err := platform.ValidateSocket(socket); err != nil {
		return nil, err
	}
	conn, err := net.Dial("unix", socket)
	if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
		return nil, fmt.Errorf("whatevrd is not running on %s\nstart it: whatevrd (or enable your platform service)", socket)
	}
	return conn, err
}

// daemonClient makes one request at a time and ignores events.
type daemonClient struct {
	conn net.Conn
	in   *bufio.Reader
	next uint64
}

func newDaemonClient(conn net.Conn, name string) (*daemonClient, error) {
	c := &daemonClient{conn: conn, in: bufio.NewReader(conn)}
	_, err := c.call(v2.Request_builder{Hello: v2.Hello_builder{Client: name, Protocol: 2}.Build()}.Build())
	return c, err
}

func (c *daemonClient) call(r *v2.Request) (*v2.Response, error) {
	c.next++
	r.SetId(c.next)
	if _, err := protodelim.MarshalTo(c.conn, v2.Frame_builder{Request: r}.Build()); err != nil {
		return nil, err
	}
	for {
		var f v2.Frame
		if err := protodelim.UnmarshalFrom(c.in, &f); err != nil {
			return nil, err
		}
		resp := f.GetResponse()
		if resp == nil {
			continue
		}
		// The daemon answers out-of-band errors under id 0 (pre-hello
		// rejections, oversized-frame close): surface them instead of
		// spinning until EOF past the server's message.
		if e := resp.GetError(); e != nil && resp.GetId() != c.next {
			return nil, fmt.Errorf("%s: %s", e.GetCode(), e.GetMessage())
		}
		if resp.GetId() != c.next {
			continue
		}
		if e := resp.GetError(); e != nil {
			return nil, fmt.Errorf("%s: %s", e.GetCode(), e.GetMessage())
		}
		return resp, nil
	}
}
