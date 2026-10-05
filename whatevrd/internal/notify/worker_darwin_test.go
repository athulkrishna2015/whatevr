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
	for _, raw := range []string{"{\"version\":1,\"type\":\"status\"}\n", "not json\n", strings.Repeat("x", (1<<20)+1) + "\n"} {
		if _, err := readNative(strings.NewReader(raw)); err == nil {
			t.Fatal("accepted invalid helper response")
		}
	}
	m, err := readNative(strings.NewReader("{\"version\":2,\"type\":\"status\",\"status\":\"denied\"}\n"))
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
	clicked := make(chan string, 3)
	w := &Worker{namespace: "account", socket: "/tmp/d.sock", log: zerolog.Nop(), queue: make(chan nativeMessage, 1), open: Handlers{
		Chat:     func(chat string) bool { clicked <- chat; return true },
		Link:     func(url string) { clicked <- url },
		Activate: func() { clicked <- "activate" },
	}}
	ended := make(chan error, 1)
	go func() { _, err := w.session(ctx, client, nil); ended <- err }()
	reader := bufio.NewReader(helper)
	hello, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(hello, "hello") || !strings.Contains(hello, "/tmp/d.sock") {
		t.Fatalf("handshake: %s %v", hello, err)
	}
	for _, m := range []nativeMessage{
		{Version: 2, Type: "link", Namespace: "other", URL: "whatevr://wrong"},
		{Version: 2, Type: "click", Namespace: "account", Chat: "chat-1"},
		{Version: 2, Type: "link", Namespace: "account", URL: "whatevr://chat/x"},
		{Version: 2, Type: "activate", Namespace: "account"},
	} {
		if err := json.NewEncoder(helper).Encode(m); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"chat-1", "whatevr://chat/x", "activate"} {
		select {
		case got := <-clicked:
			if got != want {
				t.Fatalf("got %s, want %s", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s not forwarded", want)
		}
	}
	cancel()
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("session did not stop")
	}
}
