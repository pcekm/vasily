package privsep

import (
	"bufio"
	"io"
	"net"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/privsep/messages"
	"github.com/pcekm/graphping/internal/util"
)

type serverHarness struct {
	t       *testing.T
	srv     *Server
	srvDone chan any
	out     io.WriteCloser
	in      io.ReadCloser
	inb     *bufio.Reader
}

func newServerHarness(t *testing.T) *serverHarness {
	deadline := time.Now().Add(5 * time.Second)
	fromServer, toServer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Error creating pipe: %v", err)
	}
	fromServer.SetDeadline(deadline)
	toServer.SetDeadline(deadline)
	fromClient, toClient, err := os.Pipe()
	if err != nil {
		t.Fatalf("Error creating pipe: %v", err)
	}
	fromClient.SetDeadline(deadline)
	toClient.SetDeadline(deadline)
	srv := newServer()
	srv.in = fromClient
	srv.out = toServer
	srvDone := make(chan any)
	return &serverHarness{
		t:       t,
		srv:     srv,
		srvDone: srvDone,
		in:      fromServer,
		inb:     bufio.NewReader(fromServer),
		out:     toClient,
	}
}

func (h *serverHarness) Run() {
	h.srv.run()
	close(h.srvDone)
}

// Closes the output pipe, and waits for the server to exit.
func (h *serverHarness) DoneWriting() {
	if h.out == nil {
		return
	}
	if err := h.out.Close(); err != nil {
		h.t.Errorf("Error closing out pipe: %v", err)
	}
	h.out = nil
	select {
	case <-h.srvDone:
	case <-time.After(5 * time.Second):
		h.t.Errorf("Timed out waiting for server to exit.")
	}
}

func (h *serverHarness) Close() {
	h.DoneWriting()
	if err := h.srv.Close(); err != nil {
		h.t.Errorf("Error closing server: %v", err)
	}
	if err := h.in.Close(); err != nil {
		h.t.Errorf("Error closing in pipe: %v", err)
	}
}

func (h *serverHarness) Write(msg messages.Message) {
	if _, err := msg.WriteTo(h.out); err != nil {
		h.t.Errorf("Error sending message: %v", err)
	}
}

func (h *serverHarness) Read() messages.Message {
	msg, err := messages.ReadMessage(h.inb)
	if err != nil {
		h.t.Errorf("Error reading message: %v", err)
	}
	return msg
}

// Captures a panic, or returns nil if no panic.
func capturePanic(f func()) (r any) {
	defer func() {
		r = recover()
	}()
	f()
	return nil
}

func TestShutdown(t *testing.T) {
	h := newServerHarness(t)
	defer h.Close()

	var exitcode *int
	h.srv.osExit = func(x int) {
		exitcode = &x
	}
	go func() {
		h.Write(messages.Shutdown{})
		h.DoneWriting()
	}()

	h.Run()
	if exitcode == nil || *exitcode != 0 {
		t.Errorf("Shutdown did not call sys.Exit")
	}

}

// The privilege-related tests are smoke tests. In the sense that they _pass_ if
// they emit smoke. :-) Testing them properly will require an integration test
// in a VM. (Dependency injection is another idea, but the added complication
// gives me pause. Ideally this code should be extremely easy to visually
// inspect, and indirection could obscure things.)

func TestPrivilegeDrop_SmokeTest(t *testing.T) {
	h := newServerHarness(t)
	defer h.Close()

	go func() {
		h.Write(messages.PrivilegeDrop{})
		h.DoneWriting()
	}()
	h.Run()
}

// A real ping test of the loopback address. Only works on Darwin since it
// doesn't require privileges.
func TestPingLoopback(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skipf("Unsupported OS: %v", runtime.GOOS)
	}

	cases := []struct {
		Ver  util.IPVersion
		Addr net.IP
	}{
		{Ver: util.IPv4, Addr: net.ParseIP("127.0.0.1")},
		{Ver: util.IPv6, Addr: net.ParseIP("::1")},
	}
	for _, c := range cases {
		t.Run(c.Ver.String(), func(t *testing.T) {
			h := newServerHarness(t)
			defer h.Close()

			var id messages.ConnectionID
			go func() {
				defer h.DoneWriting()
				h.Write(messages.OpenConnection{IPVer: c.Ver})
				msg := h.Read()
				ocr, ok := msg.(messages.OpenConnectionReply)
				if !ok {
					t.Errorf("Expected OpenConnectionReply, got: %#v", msg)
					return
				}
				id = ocr.ID
				if _, ok := h.srv.conns[id]; !ok {
					t.Error("No connection opened.")
					return
				}

				h.Write(messages.SendPing{
					ID:     id,
					Packet: backend.Packet{Seq: 1, Payload: []byte("8675309")},
					Addr:   c.Addr,
				})

				msg = h.Read()
				pingRepl, ok := msg.(messages.PingReply)
				if !ok {
					t.Errorf("Expected PingReply, got %#v", msg)
					return
				}

				want := messages.PingReply{
					ID:     id,
					Packet: backend.Packet{Type: backend.PacketReply, Seq: 1, Payload: []byte("8675309")},
					Peer:   c.Addr,
				}
				if diff := cmp.Diff(want, pingRepl); diff != "" {
					t.Errorf("Wrong ping reply (-want, +got):\n%v", diff)
				}

				h.Write(messages.CloseConnection{ID: id})
			}()

			h.Run()

			if _, ok := h.srv.conns[id]; ok {
				t.Error("Connection not removed from server connection list after close.")
				return
			}

		})
	}
}
