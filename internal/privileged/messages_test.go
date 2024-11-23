package privileged

import (
	"bytes"
	"log"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pcekm/graphping/internal/backend"
)

// Makes a raw message that is as long as it can possibly be. (About 64k).
func makeEncodedMaximalMessage() []byte {
	msg := []byte{254, 255}
	for range 255 {
		msg = append(msg, 255)
		msg = append(msg, bytes.Repeat([]byte{0}, 255)...)
	}
	return msg
}

// Makes a parsed message that should match makeEncodedMaximalMessage.
func makeDecodedMaximalMessage() RawMessage {
	msg := RawMessage{Type: 254}
	for range 255 {
		msg.Args = append(msg.Args, bytes.Repeat([]byte{0}, 255))
	}
	return msg
}

func marshalRawMsg(msg RawMessage) []byte {
	var buf bytes.Buffer
	if _, err := msg.WriteTo(&buf); err != nil {
		log.Panicf("WriteTo err: %v", err)
	}
	return buf.Bytes()
}

func TestReadMessage(t *testing.T) {
	cases := []struct {
		Name    string
		Encoded []byte
		Want    Message
		WantErr bool
	}{
		{Name: "Empty", Encoded: []byte{}, WantErr: true},
		{Name: "MissingArgCount", Encoded: []byte{1}, WantErr: true},
		{Name: "MissingArgLen", Encoded: []byte{1, 1}, WantErr: true},
		{Name: "MissingMessage", Encoded: []byte{1, 1, 1}, WantErr: true},
		{Name: "InvalidMsgType", Encoded: []byte{254, 0}, Want: RawMessage{Type: 254}},
		{Name: "Shutdown", Encoded: []byte{byte(msgShutdown), 0}, Want: Shutdown{}},
		{Name: "Shutdown/ExtraArgs", Encoded: []byte{byte(msgShutdown), 1, 0}, WantErr: true},
		{Name: "PrivilegeDrop", Encoded: []byte{byte(msgPrivilegeDrop), 0}, Want: PrivilegeDrop{}},
		{Name: "OpenConnection", Encoded: []byte{byte(msgOpenConnection), 0}, Want: OpenConnection{}},
		{
			Name:    "OpenConnectionReply",
			Encoded: []byte{byte(msgOpenConnectionReply), 1, 3, 102, 111, 111},
			Want:    OpenConnectionReply{ID: "foo"},
		},
		{
			Name:    "OpenConnectionReply/MissingConnectionID",
			Encoded: []byte{byte(msgOpenConnectionReply), 0},
			WantErr: true,
		},
		{
			Name:    "OpenConnectionReply/ExtraArgs",
			Encoded: marshalRawMsg(RawMessage{Type: msgOpenConnectionReply, Args: [][]byte{{0}, {}}}),
			WantErr: true,
		},
		{
			Name:    "CloseConnection",
			Encoded: []byte{byte(msgCloseConnection), 1, 3, 98, 97, 114},
			Want:    CloseConnection{ID: "bar"},
		},
		{
			Name:    "CloseConnection/TooManyArgs",
			Encoded: []byte{byte(msgCloseConnection), 2, 3, 98, 97, 114, 0},
			WantErr: true,
		},
		{
			Name:    "SendPing",
			Encoded: []byte{byte(msgSendPing), 2, 1, 88, 7, 1, 2, 3, 3, 4, 5, 6},
			Want: SendPing{
				ID: "X",
				Packet: backend.Packet{
					Type:    backend.PacketReply,
					Seq:     0x0203,
					Payload: []byte{4, 5, 6},
				},
			},
		},
		{
			Name:    "SendPing/MissingArg",
			Encoded: marshalRawMsg(RawMessage{Type: msgSendPing, Args: [][]byte{{0}}}),
			WantErr: true,
		},
		{
			Name:    "SendPing/MissingPacketType",
			Encoded: marshalRawMsg(RawMessage{Type: msgSendPing, Args: [][]byte{{0}, {}}}),
			WantErr: true,
		},
		{
			Name:    "SendPing/MissingSequence",
			Encoded: marshalRawMsg(RawMessage{Type: msgSendPing, Args: [][]byte{{0}, {0}}}),
			WantErr: true,
		},
		{
			Name:    "SendPing/MissingPayloadLen",
			Encoded: marshalRawMsg(RawMessage{Type: msgSendPing, Args: [][]byte{{0}, {0, 1, 2}}}),
			WantErr: true,
		},
		{
			Name:    "SendPing/MissingPayload",
			Encoded: marshalRawMsg(RawMessage{Type: msgSendPing, Args: [][]byte{{0}, {0, 1, 2, 3}}}),
			WantErr: true,
		},
		{
			Name:    "SendPing/ShortPayload",
			Encoded: marshalRawMsg(RawMessage{Type: msgSendPing, Args: [][]byte{{0}, {0, 1, 2, 3, 0, 0}}}),
			WantErr: true,
		},
		{
			Name:    "SendPing/CruftAtEnd",
			Encoded: marshalRawMsg(RawMessage{Type: msgSendPing, Args: [][]byte{{0}, {0, 1, 2, 3, 0, 0, 0, 9}}}),
			WantErr: true,
		},
		{
			Name:    "PingReply",
			Encoded: []byte{byte(msgPingReply), 2, 1, 89, 9, 2, 3, 4, 5, 5, 6, 7, 8, 9},
			Want: PingReply{
				ID: "Y",
				Packet: backend.Packet{
					Type:    backend.PacketTimeExceeded,
					Seq:     0x0304,
					Payload: []byte{5, 6, 7, 8, 9},
				},
			},
		},
		{
			Name:    "PingReply/MissingArg",
			Encoded: marshalRawMsg(RawMessage{Type: msgPingReply, Args: [][]byte{{0}}}),
			WantErr: true,
		},
		{
			Name:    "PingReply/MissingPacketType",
			Encoded: marshalRawMsg(RawMessage{Type: msgPingReply, Args: [][]byte{{0}, {}}}),
			WantErr: true,
		},
		{
			Name:    "PingReply/MissingSequence",
			Encoded: marshalRawMsg(RawMessage{Type: msgPingReply, Args: [][]byte{{0}, {0}}}),
			WantErr: true,
		},
		{
			Name:    "PingReply/MissingPayloadLen",
			Encoded: marshalRawMsg(RawMessage{Type: msgPingReply, Args: [][]byte{{0}, {0, 1, 2}}}),
			WantErr: true,
		},
		{
			Name:    "PingReply/MissingPayload",
			Encoded: marshalRawMsg(RawMessage{Type: msgPingReply, Args: [][]byte{{0}, {0, 1, 2, 3}}}),
			WantErr: true,
		},
		{
			Name:    "PingReply/ShortPayload",
			Encoded: marshalRawMsg(RawMessage{Type: msgPingReply, Args: [][]byte{{0}, {0, 1, 2, 3, 0, 0}}}),
			WantErr: true,
		},
		{
			Name:    "PingReply/CruftAtEnd",
			Encoded: marshalRawMsg(RawMessage{Type: msgPingReply, Args: [][]byte{{0}, {0, 1, 2, 3, 0, 0, 0, 9}}}),
			WantErr: true,
		},
		{Name: "OneEmptyArg", Encoded: []byte{254, 1, 0}, Want: RawMessage{Type: 254, Args: [][]byte{{}}}},
		{
			Name:    "OneNonemptyArg",
			Encoded: []byte{254, 1, 2, 3, 4},
			Want: RawMessage{
				Type: 254,
				Args: [][]byte{{3, 4}},
			},
		},
		{
			Name:    "TwoNonemptyArgs",
			Encoded: []byte{254, 2, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			Want: RawMessage{
				Type: 254,
				Args: [][]byte{
					{3, 4},
					{6, 7, 8, 9, 10},
				},
			},
		},
		{
			Name:    "MaximalMessage",
			Encoded: makeEncodedMaximalMessage(),
			Want:    makeDecodedMaximalMessage(),
		},
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			msg, err := ReadMessage(bytes.NewBuffer(c.Encoded))
			if (err != nil) != c.WantErr {
				t.Errorf("Wrong error returned: %v (WantErr=%v)", err, c.WantErr)
			}
			if diff := cmp.Diff(c.Want, msg, cmp.AllowUnexported(RawMessage{})); err == nil && diff != "" {
				t.Errorf("Wrong message read (-want, +got):\n%v", diff)
			}
		})
	}
}

func TestMessage_WriteTo(t *testing.T) {
	cases := []struct {
		Name    string
		Msg     Message
		Want    []byte
		WantErr bool
	}{
		{Name: "Empty", Msg: RawMessage{}, Want: []byte{0, 0}},

		{Name: "Shutdown", Msg: Shutdown{}, Want: []byte{byte(msgShutdown), 0}},
		{Name: "PrivilegeDrop", Msg: PrivilegeDrop{}, Want: []byte{byte(msgPrivilegeDrop), 0}},
		{Name: "OpenConnection", Msg: OpenConnection{}, Want: []byte{byte(msgOpenConnection), 0}},
		{
			Name: "OpenConnectionReply",
			Msg:  OpenConnectionReply{ID: "foo"},
			Want: []byte{byte(msgOpenConnectionReply), 1, 3, 102, 111, 111},
		},
		{
			Name: "CloseConnection",
			Msg:  CloseConnection{ID: "bar"},
			Want: []byte{byte(msgCloseConnection), 1, 3, 98, 97, 114},
		},
		{
			Name: "SendPing",
			Msg: SendPing{
				ID: "X", Packet: backend.Packet{
					Type:    backend.PacketTimeExceeded,
					Seq:     0x0203,
					Payload: []byte{4, 5},
				},
			},
			Want: []byte{byte(msgSendPing), 2, 1, 88, 6, 2, 2, 3, 2, 4, 5},
		},
		{
			Name: "PingReply",
			Msg: PingReply{
				ID: "P", Packet: backend.Packet{
					Type:    backend.PacketReply,
					Seq:     0x0405,
					Payload: []byte{6, 7, 8},
				},
			},
			Want: []byte{byte(msgPingReply), 2, 1, 80, 7, 1, 4, 5, 3, 6, 7, 8},
		},

		{Name: "TooManyArgs", Msg: RawMessage{Args: make([][]byte, 256)}, WantErr: true},
		{Name: "ArgTooLong", Msg: RawMessage{Args: [][]byte{make([]byte, 256)}}, WantErr: true},
		{Name: "NoArgs", Msg: RawMessage{Type: msgShutdown}, Want: []byte{byte(msgShutdown), 0}},
		{Name: "OneEmptyArg", Msg: RawMessage{Type: msgShutdown, Args: [][]byte{{}}}, Want: []byte{byte(msgShutdown), 1, 0}},
		{
			Name: "OneNonemptyArg",
			Msg: RawMessage{
				Type: msgShutdown,
				Args: [][]byte{{3, 4}},
			},
			Want: []byte{byte(msgShutdown), 1, 2, 3, 4},
		},
		{
			Name: "TwoNonemptyArgs",
			Msg: RawMessage{
				Type: msgSendPing,
				Args: [][]byte{
					{3, 4},
					{6, 7, 8, 9, 10},
				},
			},
			Want: []byte{byte(msgSendPing), 2, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
		{
			Name: "MaximalMessage",
			Msg:  makeDecodedMaximalMessage(),
			Want: makeEncodedMaximalMessage(),
		},
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			var buf bytes.Buffer
			n, err := c.Msg.WriteTo(&buf)
			if (err != nil) != c.WantErr {
				t.Errorf("Wrong error returned: %v (WantErr=%v)", err, c.WantErr)
			}
			got := buf.Bytes()
			if len(got) != int(n) {
				t.Errorf("Wrong number of bytes read: %d (want %d)", n, len(got))
			}
			if diff := cmp.Diff(c.Want, got); diff != "" {
				t.Errorf("Wrong bytes written (-want, +got):\n%v", diff)
			}
		})
	}
}

// TODO: I'm not sure how useful these fuzzing tests are.
// They end up skipping a lot or they trigger expected errors.

func FuzzRawMessage(f *testing.F) {
	f.Fuzz(func(t *testing.T, mType byte, arg1, arg2 []byte) {
		if len(arg1) > 255 || len(arg2) > 255 {
			t.Skip("Args too long")
		}
		msg := RawMessage{Type: messageType(mType), Args: [][]byte{arg1, arg2}}
		var out bytes.Buffer
		n, err := msg.WriteTo(&out)
		if err != nil {
			t.Fatalf("WriteTo error: %v", err)
		}
		if int(n) != out.Len() {
			t.Errorf("WriteTo returned wrong length: %d (want %d)", n, out.Len())
		}
		got, err := readRawMessage(bytes.NewBuffer(out.Bytes()))
		if err != nil {
			t.Fatalf("ReadMessage error: %v", err)
		}
		if diff := cmp.Diff(msg, got); diff != "" {
			t.Fatalf("Message didn't decode to the same thing (-want, +got):\n%v", diff)
		}
	})
}

func FuzzReadMessage(f *testing.F) {
	for _, seed := range [][]byte{
		{0, 0},
		{1, 1, 0},
		{1, 1, 1, 0},
		{1, 2, 0, 0},
		{1, 2, 1, 0, 2, 0, 0},
		makeEncodedMaximalMessage(),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		msg, err := ReadMessage(bytes.NewBuffer(in))
		if err != nil {
			t.SkipNow()
		}
		var out bytes.Buffer
		n, err := msg.WriteTo(&out)
		if err != nil {
			t.Fatalf("WriteTo error: %v", err)
		}
		if int(n) != out.Len() {
			t.Errorf("WriteTo returned wrong length: %d (want %d)", n, out.Len())
		}
		if !bytes.Equal(in[:n], out.Bytes()) {
			t.Fatalf("Input != Output:\nInput: %v\nOutput: %v", in, out.Bytes())
		}
	})
}
