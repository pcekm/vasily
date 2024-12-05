// TODO: There's a lot of code here. Is it really safer than a third party
// library? Safer than protobufs? Safer than strings and Scanf? Simpler than
// either?

// Package messages contains messages that are passed between the privsep client
// and server and functions for encoding and decoding them.
package messages

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/util"
)

const (
	maxMessageLen = 2 + 255*(1+255)
)

var (
	// ErrInvalidMessageType is returned when an unrecognized message type is read
	// while decoding a message.
	ErrInvalidMessageType = errors.New("invalid message type")
)

// Used in a panic to communicate an error back up to the top level decode
// operation. This deliberately doesn't implement error. It's meant to be
// unpacked and the original error returned.
type caughtErr struct {
	Err error
}

// Panics with the given error message.
func panicMsg(msg string) {
	panic(caughtErr{Err: errors.New(msg)})
}

// Panics with the given format and args.
func panicMsgf(s string, args ...any) {
	panic(caughtErr{Err: fmt.Errorf(s, args...)})
}

// Panics with the given error.
func panicErr(err error) {
	panic(caughtErr{Err: err})
}

// Catches panics sent with panicErr and panicErrf and sets err. Other panic
// values are re-panicked.
func catchError(err *error) {
	if e := recover(); e != nil {
		if e, ok := e.(caughtErr); ok {
			*err = e.Err
			return
		}
		panic(e)
	}
}

// Type of message send between the client and server.
type messageType byte

// Message types.
const (
	// RequestShutdown is a message to shutdown the privsep server.
	msgShutdown messageType = iota

	// msgPrivilegeDrop is a request to drop privileges.
	msgPrivilegeDrop

	// msgLog contains a log message.
	msgLog

	// msgOpenConnection is a request message to create a new connection.
	msgOpenConnection

	// msgOpenConnectionReply is a reply message to a new connection open
	// message.
	msgOpenConnectionReply

	// msgCloseConnection is a request message to close a connection.
	msgCloseConnection

	// msgCloseConnectionReply is a request message to close a connection.
	msgCloseConnectionReply

	// msgSendPing is a request message to send a ping.
	msgSendPing

	// msgPingReply is a reply message containing a ping reply.
	msgPingReply
)

func (t messageType) String() string {
	switch t {
	case msgShutdown:
		return "msgShutdown"
	case msgPrivilegeDrop:
		return "msgPrivilegeDrop"
	case msgLog:
		return "msgLog"
	case msgOpenConnection:
		return "msgOpenConnection"
	case msgOpenConnectionReply:
		return "msgOpenConnectionReply"
	case msgCloseConnection:
		return "msgCloseConnection"
	case msgCloseConnectionReply:
		return "msgCloseConnectionReply"
	case msgSendPing:
		return "msgSendPing"
	case msgPingReply:
		return "msgPingReply"
	default:
		return fmt.Sprintf("(unknown:%d)", t)
	}
}

// Message holds a protocol message.
type Message interface {
	io.WriterTo
}

// ReadMessage reads and decodes a message.
func ReadMessage(r io.ByteReader) (msg Message, err error) {
	defer catchError(&err)
	raw, err := readRawMessage(r)
	if err != nil {
		return nil, err
	}
	switch raw.Type {
	case msgShutdown:
		msg = raw.asShutdown()
	case msgPrivilegeDrop:
		msg = raw.asPrivilegeDrop()
	case msgLog:
		msg = raw.asLog()
	case msgOpenConnection:
		msg = raw.asOpenConnection()
	case msgOpenConnectionReply:
		msg = raw.asOpenConnectionReply()
	case msgCloseConnection:
		msg = raw.asCloseConnection()
	case msgCloseConnectionReply:
		msg = raw.asCloseConnectionReply()
	case msgSendPing:
		msg = raw.asSendPing()
	case msgPingReply:
		msg = raw.asPingReply()
	default:
		msg = raw
	}
	return msg, err
}

// ConnectionID is an identifier for an open connection.
type ConnectionID int

func (n ConnectionID) encode() []byte {
	return encodeInt(int(n))
}

// RawMessage is a basic message.
type RawMessage struct {
	// Type is the type of message.
	Type messageType

	// Args contains the raw message Args.
	Args [][]byte
}

// readRawMessage reads a message.
func readRawMessage(r io.ByteReader) (RawMessage, error) {
	msg := RawMessage{}

	// MessageType.
	b, err := r.ReadByte()
	if err != nil {
		return RawMessage{}, err
	}
	msg.Type = messageType(b)

	// Number of args.
	numArgs, err := r.ReadByte()
	if err != nil {
		return RawMessage{}, err
	}

	// Read args.
	for range numArgs {
		argLen, err := r.ReadByte()
		if err != nil {
			return RawMessage{}, err
		}
		arg := make([]byte, argLen)
		for i := range argLen {
			arg[i], err = r.ReadByte()
			if err != nil {
				return RawMessage{}, err
			}
		}
		msg.Args = append(msg.Args, arg)
	}

	return msg, nil
}

// Write outputs the message.
func (m RawMessage) WriteTo(w io.Writer) (int64, error) {
	if len(m.Args) > math.MaxUint8 {
		return 0, fmt.Errorf("too many args: %d", len(m.Args))
	}
	buf := []byte{byte(m.Type), byte(len(m.Args))}
	for _, arg := range m.Args {
		if len(arg) > math.MaxUint8 {
			return 0, fmt.Errorf("arg too long (%d): %v", len(arg), arg)
		}
		buf = append(buf, byte(len(arg)))
		buf = append(buf, arg...)
	}
	n, err := w.Write(buf)
	return int64(n), err
}

// Checks the argument count and panics with an error if it's wrong.
func (m RawMessage) checkNArgs(want int) {
	if len(m.Args) != want {
		panicMsgf("unexpected argument count: %d (want %d)", m.Args, want)
	}
}

// Checks the argument exists and panics with an error if it doesn't.
func (m RawMessage) checkArgExists(i int) {
	if len(m.Args) <= i {
		panicMsgf("arg %d not found", i)
	}
}

// Checks the argument exists and has the given length and panics with an error
// if something is wrong.
func (m RawMessage) checkArgLen(i, wantLen int) {
	m.checkArgExists(i)
	if len(m.Args[i]) != wantLen {
		panicMsgf("arg %d is %d bytes (want %d)", i, len(m.Args[i]), wantLen)
	}
}

// Checks the type of a raw message and panics if it's unexpected.
// This is a bug if it happens, so no panic recovery for this.
func (m RawMessage) checkType(want messageType) {
	if m.Type != want {
		log.Panicf("Wrong message type: %v (want %v)", m.Type, want)
	}
}

// Gets a string arg at position i.
func (m RawMessage) argString(i int) string {
	m.checkArgExists(i)
	return string(m.Args[i])
}

// Gets a byte arg at position i.
func (m RawMessage) argByte(i int) byte {
	m.checkArgLen(i, 1)
	return m.Args[i][0]
}

// Gets a big-endian uint16 arg at position i.
func (m RawMessage) argUint16(i int) uint16 {
	m.checkArgLen(i, 2)
	return uint16(m.Args[i][0])<<8 | uint16(m.Args[i][1])
}

// Gets a big-endian 32-bit int arg at position i.
func (m RawMessage) argInt(i int) int {
	m.checkArgLen(i, 4)
	return int(m.Args[i][0])<<24 | int(m.Args[i][1])<<16 | int(m.Args[i][2])<<8 | int(m.Args[i][3])
}

// Gets a []byte arg at position i.
func (m RawMessage) argBytes(i int) []byte {
	m.checkArgExists(i)
	return m.Args[i]
}

// Decodes a connection id at argument index i.
func (m RawMessage) argConnectionID(i int) ConnectionID {
	return ConnectionID(m.argInt(i))
}

// Gets an IPVersion arg at position i.
func (m RawMessage) argIPVersion(i int) util.IPVersion {
	return util.IPVersion(m.argByte(i))
}

// Gets an IP address arg at position i.
func (m RawMessage) argIP(i int) net.IP {
	ip := net.IP(m.argBytes(i))
	if len(ip) != 4 && len(ip) != 16 {
		panicMsgf("wrong IP length: %d", len(ip))
	}
	return ip
}

// Decodes a [backend.Packet] at index i.
// Packets are encoded as:
//
//	<type><seq><payloadLen><payload>
//
//	<type>:       1 byte; maps to payload.PacketType
//	<seq>:        2 bytes; unsigned, big endian sequence number
//	<payloadLen>: 1 byte; length of payload
//	<payload>:    sequence of payloadLen bytes
func (m RawMessage) decodePacket(i int) backend.Packet {
	m.checkArgExists(i)
	buf := bytes.NewBuffer(m.Args[i])
	tp, err := buf.ReadByte()
	if err != nil {
		panicMsgf("error reading packet type: %v", err)
	}
	var seq uint16
	if err := binary.Read(buf, binary.BigEndian, &seq); err != nil {
		panicMsgf("error reading sequence number: %#v", err)
	}
	plen, err := buf.ReadByte()
	if err != nil {
		panicMsgf("error reading payload len: %v", err)
	}
	payload := make([]byte, plen)
	n, err := buf.Read(payload)
	if err != nil {
		panicMsgf("error reading payload: %v", err)
	}
	if n != int(plen) {
		panicMsgf("short payload: %d bytes (want %d)", n, plen)
	}
	if buf.Len() != 0 {
		panicMsgf("unused %d extra bytes at end of payload", buf.Len())
	}
	return backend.Packet{
		Type:    backend.PacketType(tp),
		Seq:     int(seq),
		Payload: payload,
	}
}

// Encodes a packet. Silently truncates a payload that's too long.
func encodePacket(pkt backend.Packet) []byte {
	var buf bytes.Buffer
	// Errors are always going to be nil on a bytes.Buffer, so there's no reason
	// to check them.
	buf.WriteByte(byte(pkt.Type))
	binary.Write(&buf, binary.BigEndian, uint16(pkt.Seq))
	payload := pkt.Payload
	if len(payload) > math.MaxUint8 {
		payload = payload[:math.MaxUint8]
	}
	buf.WriteByte(byte(len(payload)))
	buf.Write(payload)
	return buf.Bytes()
}

// Encodes a 32-bit signed int in big-endian order.
func encodeInt(n int) []byte {
	return []byte{
		byte(n >> 24),
		byte(n >> 16),
		byte(n >> 8),
		byte(n),
	}
}

// Shutdown is a message sent to the server telling it to exit.
type Shutdown struct{}

func (Shutdown) WriteTo(w io.Writer) (int64, error) {
	raw := RawMessage{Type: msgShutdown}
	return raw.WriteTo(w)
}

func (m RawMessage) asShutdown() (msg Shutdown) {
	m.checkType(msgShutdown)
	m.checkNArgs(0)
	return msg
}

// PrivilegeDrop is a message sent to the server telling it to drop privileges.
// This should be done when there are no more connections to create. Once sent,
// privileges cannot be regained without restarting the program.
type PrivilegeDrop struct{}

func (PrivilegeDrop) WriteTo(w io.Writer) (int64, error) {
	raw := RawMessage{Type: msgPrivilegeDrop}
	return raw.WriteTo(w)
}

func (m RawMessage) asPrivilegeDrop() (msg PrivilegeDrop) {
	m.checkType(msgPrivilegeDrop)
	m.checkNArgs(0)
	return msg
}

// Log is a log message.
type Log struct {
	// Msg is the logging message.
	Msg string
}

func (l Log) WriteTo(w io.Writer) (int64, error) {
	raw := RawMessage{
		Type: msgLog,
		Args: [][]byte{[]byte(l.Msg)},
	}
	return raw.WriteTo(w)
}

func (m RawMessage) asLog() (msg Log) {
	m.checkType(msgLog)
	return Log{Msg: m.argString(0)}
}

// OpenConnection is a message to open a new ICMP connection.
type OpenConnection struct {
	IPVer util.IPVersion
}

func (c OpenConnection) WriteTo(w io.Writer) (int64, error) {
	raw := RawMessage{
		Type: msgOpenConnection,
		Args: [][]byte{{byte(c.IPVer)}},
	}
	return raw.WriteTo(w)
}

func (m RawMessage) asOpenConnection() OpenConnection {
	m.checkType(msgOpenConnection)
	return OpenConnection{IPVer: m.argIPVersion(0)}
}

// OpenConnectionReply is a message to open a new ICMP connection.
type OpenConnectionReply struct {
	// ID holds the identifier for the opened connection.
	ID ConnectionID
}

func (o OpenConnectionReply) WriteTo(w io.Writer) (int64, error) {
	raw := RawMessage{
		Type: msgOpenConnectionReply,
		Args: [][]byte{o.ID.encode()},
	}
	return raw.WriteTo(w)
}

func (m RawMessage) asOpenConnectionReply() (msg OpenConnectionReply) {
	m.checkType(msgOpenConnectionReply)
	m.checkNArgs(1)
	msg.ID = m.argConnectionID(0)
	return msg
}

// CloseConnection is a message to close an existing ICMP connection.
type CloseConnection struct {
	// ID holds the identifier of the connection to close.
	ID ConnectionID
}

func (c CloseConnection) WriteTo(w io.Writer) (int64, error) {
	raw := RawMessage{
		Type: msgCloseConnection,
		Args: [][]byte{c.ID.encode()},
	}
	return raw.WriteTo(w)
}

func (m RawMessage) asCloseConnection() (msg CloseConnection) {
	m.checkType(msgCloseConnection)
	m.checkNArgs(1)
	msg.ID = m.argConnectionID(0)
	return msg
}

// CloseConnectionReply is a response to a close message request.
type CloseConnectionReply struct {
	ID ConnectionID
}

func (c CloseConnectionReply) WriteTo(w io.Writer) (int64, error) {
	raw := RawMessage{
		Type: msgCloseConnectionReply,
		Args: [][]byte{c.ID.encode()},
	}
	return raw.WriteTo(w)
}

func (m RawMessage) asCloseConnectionReply() (msg CloseConnectionReply) {
	m.checkType(msgCloseConnectionReply)
	msg.ID = m.argConnectionID(0)
	return msg
}

// SendPing is a message to send a ping.
type SendPing struct {
	// ID holds the identifier of the connection to send the message over.
	ID ConnectionID

	// Packet is the ping message to send. The message type _must_ be
	// PacketRequest.
	Packet backend.Packet

	// Addr is the address to ping.
	Addr net.IP

	// TTL is the time to live for the outgoing packet. Zero means use the
	// default.
	TTL int
}

func (s SendPing) WriteTo(w io.Writer) (int64, error) {
	raw := RawMessage{
		Type: msgSendPing,
		Args: [][]byte{
			s.ID.encode(),
			encodePacket(s.Packet),
			[]byte(s.Addr),
			encodeInt(s.TTL),
		},
	}
	return raw.WriteTo(w)
}

func (m RawMessage) asSendPing() SendPing {
	m.checkType(msgSendPing)
	m.checkNArgs(4)
	return SendPing{
		ID:     m.argConnectionID(0),
		Packet: m.decodePacket(1),
		Addr:   m.argIP(2),
		TTL:    m.argInt(3),
	}
}

// PingReply is a message with the response to a ping.
// type PingReply
type PingReply struct {
	// ID holds the identifier of the connection that received the message.
	ID ConnectionID

	// Packet is the ping message received.
	Packet backend.Packet

	// Peer is the host the packet was received from.
	Peer net.IP
}

func (p PingReply) WriteTo(w io.Writer) (int64, error) {
	raw := RawMessage{
		Type: msgPingReply,
		Args: [][]byte{
			p.ID.encode(),
			encodePacket(p.Packet),
			[]byte(p.Peer),
		},
	}
	return raw.WriteTo(w)
}
func (m RawMessage) asPingReply() PingReply {
	m.checkType(msgPingReply)
	m.checkNArgs(3)
	return PingReply{
		ID:     m.argConnectionID(0),
		Packet: m.decodePacket(1),
		Peer:   m.argIP(2),
	}
}
