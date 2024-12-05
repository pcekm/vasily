package client

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/backend/test"
	"github.com/pcekm/graphping/internal/privsep/messages"
	"github.com/pcekm/graphping/internal/util"
)

type messageHandler func(messages.Message) messages.Message

var runComplete = messages.RawMessage{Type: 254}

type fakeServer struct {
	in  io.ReadCloser
	inb *bufio.Reader
	out io.WriteCloser

	handler messageHandler
}

func newFakeServer(in io.ReadCloser, out io.WriteCloser, handler messageHandler) *fakeServer {
	return &fakeServer{
		in:      in,
		inb:     bufio.NewReader(in),
		out:     out,
		handler: handler,
	}
}

func (s *fakeServer) Close() error {
	return errors.Join(
		s.in.Close(),
		s.out.Close(),
	)
}

func (s *fakeServer) Run() {
	for {
		in, err := messages.ReadMessage(s.inb)
		if err != nil {
			log.Printf("ReadMessage: %v", err)
			return
		}
		out := s.handler(in)
		if out != nil {
			_, err := out.WriteTo(s.out)
			if err != nil {
				log.Printf("WriteTo: %v", err)
				return
			}
		}
	}
}

// Makes a connected client/server pair.
func makeCSPair(t *testing.T, handler messageHandler) (*Client, *fakeServer) {
	fromClient, toServer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Error creating pipe: %v", err)
	}
	fromClient.SetDeadline(time.Now().Add(5 * time.Second))
	toServer.SetDeadline(time.Now().Add(5 * time.Second))
	fromServer, toClient, err := os.Pipe()
	if err != nil {
		t.Fatalf("Error creating pipe: %v", err)
	}
	fromServer.SetDeadline(time.Now().Add(5 * time.Second))
	toClient.SetDeadline(time.Now().Add(5 * time.Second))

	client := New(fromServer, toServer)
	server := newFakeServer(fromClient, toClient, handler)
	return client, server
}

func TestClientOpenClose(t *testing.T) {
	handler := func(msg messages.Message) messages.Message {
		switch msg := msg.(type) {
		case messages.OpenConnection:
			return messages.OpenConnectionReply{ID: 1234}
		case messages.CloseConnection:
			if msg.ID != 1234 {
				// Only reply to expected ID.
				return nil
			}
			return messages.CloseConnectionReply{ID: msg.ID}
		default:
			return nil
		}
	}
	client, server := makeCSPair(t, handler)
	go server.Run()

	conn, err := client.NewConn(util.IPv6)
	if err != nil {
		t.Fatalf("NewConn error: %v", err)
	}

	if conn.ID() != 1234 {
		t.Errorf("Wrong connection ID: %v (want %v)", conn.ID(), 1234)
	}

	if err := conn.Close(); err != nil {
		t.Errorf("Error closing connection: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Errorf("Error closing client: %v", err)
	}
}

func TestReadFrom(t *testing.T) {
	ctx, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	sent := messages.PingReply{
		ID: 1234,
		Packet: backend.Packet{
			Type:    backend.PacketReply,
			Seq:     2,
			Payload: []byte("payload"),
		},
		Peer: net.ParseIP("10.0.8.2").To4(),
	}
	handler := func(msg messages.Message) messages.Message {
		switch msg := msg.(type) {
		case messages.OpenConnection:
			return messages.OpenConnectionReply{ID: 1234}
		case messages.CloseConnection:
			if msg.ID != 1234 {
				// Only reply to expected ID.
				return nil
			}
			return messages.CloseConnectionReply{ID: msg.ID}
		case messages.SendPing:
			return sent
		default:
			return nil
		}
	}
	client, server := makeCSPair(t, handler)
	go server.Run()

	conn, err := client.NewConn(util.IPv4)
	if err != nil {
		t.Errorf("NewConn error: %v", err)
	}

	if err := conn.WriteTo(&backend.Packet{}, test.LoopbackV4); err != nil {
		t.Errorf("WriteTo error: %v", err)
	}

	pkt, peer, err := conn.ReadFrom(ctx)
	if err != nil {
		t.Errorf("ReadFrom error: %v", err)
	}
	if diff := cmp.Diff(&net.UDPAddr{IP: sent.Peer}, peer); diff != "" {
		t.Errorf("Wrong peer (-want, +got):\n%v", diff)
	}
	if diff := cmp.Diff(&sent.Packet, pkt); diff != "" {
		t.Errorf("Wrong packet (-want, +got):\n%v", diff)
	}

	if err := conn.Close(); err != nil {
		t.Errorf("Error closing connection: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Errorf("Error closing client: %v", err)
	}
}

func TestWriteTo(t *testing.T) {
	var gotMsg messages.SendPing // Don't test until after client.Close() to avoid race.
	handler := func(msg messages.Message) messages.Message {
		switch msg := msg.(type) {
		case messages.OpenConnection:
			return messages.OpenConnectionReply{ID: 1234}
		case messages.CloseConnection:
			if msg.ID != 1234 {
				// Only reply to expected ID.
				return nil
			}
			return messages.CloseConnectionReply{ID: msg.ID}
		case messages.SendPing:
			gotMsg = msg
			return nil
		default:
			return nil
		}
	}
	client, server := makeCSPair(t, handler)
	go server.Run()

	conn, err := client.NewConn(util.IPv4)
	if err != nil {
		t.Errorf("NewConn error: %v", err)
	}

	sent := &backend.Packet{
		Seq:     2,
		Payload: []byte("stuff"),
	}
	if err := conn.WriteTo(sent, test.LoopbackV4, backend.TTLOption{TTL: 5}); err != nil {
		t.Errorf("WriteTo error: %v", err)
	}

	if err := conn.Close(); err != nil {
		t.Errorf("Error closing connection: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Errorf("Error closing client: %v", err)
	}

	want := messages.SendPing{
		ID:     1234,
		Packet: *sent,
		Addr:   test.LoopbackV4.IP,
		TTL:    5,
	}
	if diff := cmp.Diff(want, gotMsg); diff != "" {
		t.Errorf("Wrong packet received by server (-want, +got):\n%v", diff)
	}
}

func TestLog(t *testing.T) {
	handler := func(msg messages.Message) messages.Message {
		switch msg := msg.(type) {
		case messages.Log:
			return msg
		}
		return nil
	}
	client, server := makeCSPair(t, handler)
	go server.Run()
	client.inputTap = make(chan messages.Message, 1)

	var lb strings.Builder

	log.SetFlags(0)
	defer log.SetFlags(log.LstdFlags)
	log.SetOutput(&lb)
	defer log.SetOutput(os.Stderr)

	if err := client.sendMessage(messages.Log{Msg: "foo"}); err != nil {
		t.Errorf("sendMessage error: %v", err)
	}

	// Wait for the message to be handled.
	<-client.inputTap

	if diff := cmp.Diff("foo\n", lb.String()); diff != "" {
		t.Errorf("Wrong log output (-want, +got):\n%v", diff)
	}
}
