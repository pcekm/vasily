package udppkt

import (
	"bytes"
	"encoding/binary"
	"log"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	UDPHeaderLen = 8 // Length of a UDP header.
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

// UDPHeader is a header for a UDP message.
type UDPHeader struct {
	SrcPort  uint16
	DstPort  uint16
	TotalLen uint16
	Checksum uint16
}

// Marshal encodes a UDP header. The psh field is the ip header and may be
// either ipv4.Header or ipv6.Header. Anything else will silently produce an
// incorrect checksum.
func (h *UDPHeader) Marshal(psh any) []byte {
	switch psh := psh.(type) {
	case *ipv4.Header:
		h.calcChecksumV4(psh)
	case ipv4.Header:
		h.calcChecksumV4(&psh)
	case *ipv6.Header:
		h.calcChecksumV6(psh)
	case ipv6.Header:
		h.calcChecksumV6(&psh)
	}
	b := make([]byte, UDPHeaderLen)
	_, err := binary.Encode(b, binary.BigEndian, h)
	if err != nil {
		log.Panicf("Err encoding UDP packet: %v\n(Packet: %#v)", err, *h)
	}
	return b
}

// CalcChecksumV4 calculates and sets the Checksum field.
func (h *UDPHeader) calcChecksumV4(ipHdr *ipv4.Header) {
	var ck checksum
	ck.AddBytes([]byte(ipHdr.Src.To4()))
	ck.AddBytes([]byte(ipHdr.Dst.To4()))
	ck.AddUint16(uint16(ipHdr.Protocol))
	ck.AddUint16(uint16(ipHdr.Len))
	h.addUDPFields(&ck)
}

// CalcChecksumV6 calculates and sets the Checksum field.
func (h *UDPHeader) calcChecksumV6(ipHdr *ipv6.Header) {
	var ck checksum
	ck.AddBytes([]byte(ipHdr.Src.To16()))
	ck.AddBytes([]byte(ipHdr.Dst.To16()))
	ck.AddUint32(uint32(ipHdr.PayloadLen))
	h.addUDPFields(&ck)
}

func (h *UDPHeader) addUDPFields(ck *checksum) {
	ck.AddUint16(h.SrcPort)
	ck.AddUint16(h.DstPort)
	ck.AddUint16(h.TotalLen)
	h.Checksum = ck.Sum()
}

// ParseUDPHeader parses a UDP header. A UDP header is always [UDPHeaderLen]
// bytes long.
func ParseUDPHeader(b []byte) (*UDPHeader, error) {
	buf := bytes.NewBuffer(b)
	var hdr UDPHeader
	err := binary.Read(buf, binary.BigEndian, &hdr)
	if err != nil {
		return nil, err
	}
	return &hdr, nil
}
