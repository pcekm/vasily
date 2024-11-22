package test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pcekm/graphping/internal/backend"
	"go.uber.org/mock/gomock"
)

func TestMockPingExchange(t *testing.T) {
	ctrl := gomock.NewController(t)
	conn := NewMockConn(ctrl)

	payload := []byte("the payload")
	conn.MockPingExchange(NewPingExchange(2).SetPayload(payload))

	sentPkt := &backend.Packet{Seq: 2, Payload: payload}
	if err := conn.WriteTo(sentPkt, LoopbackV4); err != nil {
		t.Errorf("WriteTo error: %v", err)
	}
	gotPkt, peer, err := conn.ReadFrom()
	if err != nil {
		t.Errorf("ReadFrom error: %v", err)
	}
	if diff := cmp.Diff(LoopbackV4, peer); diff != "" {
		t.Errorf("Wrong peer (-want, +got):\n%v", diff)
	}
	wantPkt := &backend.Packet{Type: backend.PacketReply, Seq: 2, Payload: payload}
	if diff := cmp.Diff(wantPkt, gotPkt); diff != "" {
		t.Errorf("Wrong packet received (-want, +got):\n%v", diff)
	}

	ctrl.Finish()
}

func BenchmarkPingExchange_Overall(b *testing.B) {
	b.StopTimer()
	ctrl := gomock.NewController(b)
	conn := NewMockConn(ctrl)
	for i := range b.N {
		conn.MockPingExchange(NewPingExchange(i))
	}

	b.StartTimer()
	for i := range b.N {
		sentPkt := backend.Packet{Seq: i}
		if err := conn.WriteTo(&sentPkt, LoopbackV4); err != nil {
			b.Errorf("WriteTo error: %v", err)
		}
		_, _, err := conn.ReadFrom()
		if err != nil {
			b.Errorf("ReadFrom error: %v", err)
		}
	}
	ctrl.Finish()
}

func BenchmarkPingExchange_WriteTo(b *testing.B) {
	b.StopTimer()
	ctrl := gomock.NewController(b)
	conn := NewMockConn(ctrl)
	for i := range b.N {
		conn.MockPingExchange(NewPingExchange(i))
	}

	for i := range b.N {
		sentPkt := backend.Packet{Seq: i}
		b.StartTimer()
		if err := conn.WriteTo(&sentPkt, LoopbackV4); err != nil {
			b.StopTimer()
			b.Errorf("WriteTo error: %v", err)
		}
		_, _, err := conn.ReadFrom()
		if err != nil {
			b.Errorf("ReadFrom error: %v", err)
		}
	}
	ctrl.Finish()
}

func BenchmarkPingExchange_ReadFrom(b *testing.B) {
	b.StopTimer()
	ctrl := gomock.NewController(b)
	conn := NewMockConn(ctrl)
	for i := range b.N {
		conn.MockPingExchange(NewPingExchange(i))
	}

	for i := range b.N {
		sentPkt := backend.Packet{Seq: i}
		if err := conn.WriteTo(&sentPkt, LoopbackV4); err != nil {
			b.Errorf("WriteTo error: %v", err)
		}
		b.StartTimer()
		_, _, err := conn.ReadFrom()
		b.StopTimer()
		if err != nil {
			b.Errorf("ReadFrom error: %v", err)
		}
	}
	ctrl.Finish()
}
