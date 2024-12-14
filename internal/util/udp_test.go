package util

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseUDPPacket(t *testing.T) {
	// Contains an invalid checksum; no checksum validation is done, or even
	// possible without an IP pseudo-header.
	b := []byte{1, 2, 3, 4, 0, 12, 5, 6, 7, 8, 9, 10}
	got, err := ParseUDPHeader(b)
	if err != nil {
		t.Errorf("Parse error: %v", err)
	}
	want := &UDPHeader{
		SrcPort:  0x0102,
		DstPort:  0x0304,
		TotalLen: uint16(len(b)),
		Checksum: 0x0506,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Wrong packet (-want, +got):\n%v", diff)
	}
	payload := b[UDPHeaderLen:]
	wantPayload := []byte{7, 8, 9, 10}
	if diff := cmp.Diff(wantPayload, payload); diff != "" {
		t.Errorf("Wrong payload (-want, +got):\n%v", diff)
	}
}
