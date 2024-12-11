package udp

import (
	"bytes"
	"encoding/binary"
	"log"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	headerLen = 8
)

// Internet checksum hash.
type checksum uint

// AddBytes adds bytes to the checksum. If len(b) is odd it will be padded with
// an extra zero.
func (c *checksum) AddBytes(b []byte) {
	for i := 0; i+1 < len(b); i += 2 {
		*c += checksum(uint16(b[i])<<8) + checksum(b[i+1])
	}
	if len(b)%2 != 0 {
		*c += checksum(uint16(b[len(b)-1]) << 8)
	}
}

func (c *checksum) AddUint16(i uint16) {
	*c += checksum(i)
}

func (c *checksum) AddUint32(i uint32) {
	*c += checksum(i >> 16)
	*c += checksum(i & 0xffff)
}
func (c checksum) Sum() uint16 {
	sum := c&0xffff + c>>16
	return uint16(sum)
}

type udpHeader struct {
	SrcPort  uint16
	DstPort  uint16
	TotalLen uint16
	Checksum uint16
}

func (h *udpHeader) Encode() []byte {
	b := make([]byte, headerLen)
	_, err := binary.Encode(b, binary.BigEndian, h)
	if err != nil {
		log.Panicf("Err encoding UDP packet: %v\n(Packet: %#v)", err, *h)
	}
	return b
}

// CalcChecksumV4 calculates and sets the Checksum field.
func (h *udpHeader) CalcChecksumV4(ipHdr *ipv4.Header) {
	var ck checksum
	ck.AddBytes([]byte(ipHdr.Src.To4()))
	ck.AddBytes([]byte(ipHdr.Dst.To4()))
	ck.AddUint16(uint16(ipHdr.Protocol))
	ck.AddUint16(uint16(ipHdr.Len))
	h.addUDPFields(&ck)
}

// CalcChecksumV6 calculates and sets the Checksum field.
func (h *udpHeader) CalcChecksumV6(ipHdr *ipv6.Header) {
	var ck checksum
	ck.AddBytes([]byte(ipHdr.Src.To16()))
	ck.AddBytes([]byte(ipHdr.Dst.To16()))
	ck.AddUint32(uint32(ipHdr.PayloadLen))
	h.addUDPFields(&ck)
}

func (h *udpHeader) addUDPFields(ck *checksum) {
	ck.AddUint16(h.SrcPort)
	ck.AddUint16(h.DstPort)
	ck.AddUint16(h.TotalLen)
	h.Checksum = ck.Sum()
}

func parseUDPHeader(b []byte) (int, *udpHeader, error) {
	buf := bytes.NewBuffer(b)
	var hdr udpHeader
	err := binary.Read(buf, binary.BigEndian, &hdr)
	if err != nil {
		return -1, nil, err
	}
	return headerLen, &hdr, nil
}
