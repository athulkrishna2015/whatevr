package notify

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/rs/zerolog"
	"net"
	"strings"
	"testing"
	"time"
)

func TestNativeTransportValidatesVersionAndSize(t *testing.T) {
	for _, raw := range []string{"{\"version\":2,\"type\":\"status\"}\n", "not json\n", strings.Repeat("x", (1<<20)+1) + "\n"} {
		if _, err := readNative(strings.NewReader(raw)); err == nil {
			t.Fatal("accepted invalid helper response")
		}
	}
	m, err := readNative(strings.NewReader("{\"version\":1,\"type\":\"status\",\"status\":\"denied\"}\n"))
	if err != nil || m.Status != "denied" {
		t.Fatalf("status: %+v %v", m, err)
	}
}
func TestNativeSessionRoutesClicksAndCancels(t *testing.T) {
	client, helper := net.Pipe()
	defer client.Close()
	defer helper.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clicked := make(chan string, 1)
	w := &Worker{namespace: "account", log: zerolog.Nop(), open: func(chat string) bool { clicked <- chat; return true }, queue: make(chan nativeMessage, 1)}
	ended := make(chan error, 1)
	go func() { _, err := w.session(ctx, client, nil); ended <- err }()
	reader := bufio.NewReader(helper)
	hello, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(hello, "hello") {
		t.Fatalf("handshake: %s %v", hello, err)
	}
	if err := json.NewEncoder(helper).Encode(nativeMessage{Version: 1, Type: "click", Namespace: "account", Chat: "chat-1"}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-clicked:
		if got != "chat-1" {
			t.Fatal(got)
		}
	case <-time.After(time.Second):
		t.Fatal("click not forwarded")
	}
	cancel()
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("session did not stop")
	}
}
