package mdns

import (
	"encoding/binary"
	"net"
	"testing"
)

func buildQuery(name string, qtype uint16) []byte {
	hdr := make([]byte, 12)
	binary.BigEndian.PutUint16(hdr[4:6], 1) // QDCOUNT=1
	q := encodeDNSName(name)
	var tail [4]byte
	binary.BigEndian.PutUint16(tail[0:2], qtype)
	binary.BigEndian.PutUint16(tail[2:4], 1) // IN
	return append(append(hdr, q...), tail[:]...)
}

func TestHandleQueryFrameLocal(t *testing.T) {
	r := New("frame", 8080)
	ip := net.ParseIP("10.0.0.131")

	resp, _ := r.HandleQuery(buildQuery("frame.local", qtypeA), ip)
	if len(resp) < 16 {
		t.Fatalf("expected A response, got %d bytes", len(resp))
	}
	// Last 4 bytes must be 10.0.0.131
	gotIP := net.IPv4(resp[len(resp)-4], resp[len(resp)-3], resp[len(resp)-2], resp[len(resp)-1])
	if !gotIP.Equal(ip) {
		t.Fatalf("expected IP %s, got %s", ip, gotIP)
	}

	// Unrelated host should return nil
	other, _ := r.HandleQuery(buildQuery("printer.local", qtypeA), ip)
	if other != nil {
		t.Fatalf("expected nil for unrelated host")
	}

	// _http._tcp.local PTR should return 4 answers (PTR + SRV + TXT + A)
	svcResp, _ := r.HandleQuery(buildQuery("_http._tcp.local", qtypePTR), ip)
	if len(svcResp) < 12 || binary.BigEndian.Uint16(svcResp[6:8]) != 4 {
		t.Fatalf("expected 4 answers for _http._tcp.local, got %d", binary.BigEndian.Uint16(svcResp[6:8]))
	}
}
