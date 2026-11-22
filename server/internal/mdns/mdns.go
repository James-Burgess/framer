package mdns

import (
	"context"
	"encoding/binary"
	"log"
	"net"
	"strings"
	"syscall"
	"time"
)

const (
	mdnsAddr = "224.0.0.251:5353"
	qtypeA   = 1
	qtypePTR = 12
	qtypeTXT = 16
	qtypeSRV = 33
	qtypeANY = 255
)

type Responder struct {
	host    string // e.g. "frame"
	port    uint16
	service string // e.g. "frame._http._tcp.local"
}

func New(hostname string, port int) *Responder {
	h := strings.TrimSuffix(strings.TrimSpace(strings.ToLower(hostname)), ".local")
	if h == "" {
		h = "frame"
	}
	return &Responder{
		host:    h,
		port:    uint16(port),
		service: h + "._http._tcp.local",
	}
}

// Run starts listening for mDNS queries on 224.0.0.251:5353 until ctx is cancelled.
func (r *Responder) Run(ctx context.Context) {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var sockErr error
			_ = c.Control(func(fd uintptr) {
				sockErr = setReuseAddrPort(fd)
			})
			return sockErr
		},
	}

	pc, err := lc.ListenPacket(ctx, "udp4", ":5353")
	if err != nil {
		log.Printf("mdns: failed to bind :5353: %v", err)
		return
	}
	defer pc.Close()

	udpConn, ok := pc.(*net.UDPConn)
	if !ok {
		log.Printf("mdns: unexpected packet conn type")
		return
	}

	joinAllInterfaces(udpConn)
	mcastDst, _ := net.ResolveUDPAddr("udp4", mdnsAddr)

	// Announce on startup and periodically re-join interfaces (handles Wi-Fi reconnects)
	go func() {
		r.announce(udpConn, mcastDst)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				joinAllInterfaces(udpConn)
				r.announce(udpConn, mcastDst)
			}
		}
	}()

	go func() {
		<-ctx.Done()
		_ = udpConn.Close()
	}()

	log.Printf("mdns: advertising %s.local:%d on LAN", r.host, r.port)
	buf := make([]byte, 4096)
	for {
		n, src, err := udpConn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		if n < 12 {
			continue
		}
		ip := currentLANIPv4()
		if ip == nil {
			continue
		}
		resp, unicast := r.HandleQuery(buf[:n], ip)
		if len(resp) == 0 {
			continue
		}
		if unicast || src.Port != 5353 {
			_, _ = udpConn.WriteToUDP(resp, src)
		} else {
			_, _ = udpConn.WriteToUDP(resp, mcastDst)
		}
	}
}

func (r *Responder) announce(conn *net.UDPConn, dst *net.UDPAddr) {
	ip := currentLANIPv4()
	if ip == nil || dst == nil {
		return
	}
	pkt := r.buildResponse(0, true, true, ip)
	if len(pkt) > 0 {
		_, _ = conn.WriteToUDP(pkt, dst)
	}
}

// HandleQuery parses an incoming mDNS query packet and returns a response packet
// if it asks for our hostname or _http._tcp.local service.
func (r *Responder) HandleQuery(pkt []byte, ip net.IP) ([]byte, bool) {
	if len(pkt) < 12 {
		return nil, false
	}
	flags := binary.BigEndian.Uint16(pkt[2:4])
	// Ignore responses (QR bit set)
	if flags&0x8000 != 0 {
		return nil, false
	}
	qdCount := int(binary.BigEndian.Uint16(pkt[4:6]))
	if qdCount == 0 {
		return nil, false
	}

	txID := binary.BigEndian.Uint16(pkt[0:2])
	off := 12
	wantHost := false
	wantService := false
	unicast := false

	hostFQDN := r.host + ".local"

	for i := 0; i < qdCount && off < len(pkt); i++ {
		name, nextOff, ok := readDNSName(pkt, off)
		if !ok || nextOff+4 > len(pkt) {
			break
		}
		qtype := binary.BigEndian.Uint16(pkt[nextOff : nextOff+2])
		qclass := binary.BigEndian.Uint16(pkt[nextOff+2 : nextOff+4])
		off = nextOff + 4

		if qclass&0x8000 != 0 {
			unicast = true
		}

		lname := strings.ToLower(strings.TrimSuffix(name, "."))
		if lname == hostFQDN && (qtype == qtypeA || qtype == qtypeANY) {
			wantHost = true
		} else if lname == "_http._tcp.local" && (qtype == qtypePTR || qtype == qtypeANY) {
			wantService = true
		} else if lname == r.service && (qtype == qtypeSRV || qtype == qtypeTXT || qtype == qtypeANY) {
			wantService = true
			wantHost = true
		}
	}

	if !wantHost && !wantService {
		return nil, false
	}
	return r.buildResponse(txID, wantHost, wantService, ip), unicast
}

func (r *Responder) buildResponse(txID uint16, includeA, includeSvc bool, ip net.IP) []byte {
	ip4 := ip.To4()
	if ip4 == nil {
		return nil
	}

	var answers []byte
	var anCount uint16

	hostFQDN := r.host + ".local"

	if includeA {
		// frame.local. 120 IN A <ip4> (cache-flush 0x8001)
		answers = appendRR(answers, hostFQDN, qtypeA, 0x8001, 120, ip4)
		anCount++
	}
	if includeSvc {
		// _http._tcp.local. 120 IN PTR frame._http._tcp.local.
		answers = appendRR(answers, "_http._tcp.local", qtypePTR, 0x0001, 120, encodeDNSName(r.service))
		anCount++

		// frame._http._tcp.local. 120 IN SRV 0 0 <port> frame.local.
		srv := make([]byte, 6)
		binary.BigEndian.PutUint16(srv[4:6], r.port)
		srv = append(srv, encodeDNSName(hostFQDN)...)
		answers = appendRR(answers, r.service, qtypeSRV, 0x8001, 120, srv)
		anCount++

		// frame._http._tcp.local. 120 IN TXT "path=/manage"
		txt := []byte{12}
		txt = append(txt, []byte("path=/manage")...)
		answers = appendRR(answers, r.service, qtypeTXT, 0x8001, 120, txt)
		anCount++

		if !includeA {
			answers = appendRR(answers, hostFQDN, qtypeA, 0x8001, 120, ip4)
			anCount++
		}
	}

	hdr := make([]byte, 12)
	binary.BigEndian.PutUint16(hdr[0:2], txID)
	binary.BigEndian.PutUint16(hdr[2:4], 0x8400) // QR=1, AA=1
	binary.BigEndian.PutUint16(hdr[6:8], anCount)
	return append(hdr, answers...)
}

func appendRR(dst []byte, name string, rtype, rclass uint16, ttl uint32, rdata []byte) []byte {
	dst = append(dst, encodeDNSName(name)...)
	var meta [10]byte
	binary.BigEndian.PutUint16(meta[0:2], rtype)
	binary.BigEndian.PutUint16(meta[2:4], rclass)
	binary.BigEndian.PutUint32(meta[4:8], ttl)
	binary.BigEndian.PutUint16(meta[8:10], uint16(len(rdata)))
	dst = append(dst, meta[:]...)
	dst = append(dst, rdata...)
	return dst
}

func encodeDNSName(name string) []byte {
	var out []byte
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		if len(label) == 0 || len(label) > 63 {
			continue
		}
		out = append(out, byte(len(label)))
		out = append(out, []byte(label)...)
	}
	out = append(out, 0)
	return out
}

func readDNSName(pkt []byte, off int) (string, int, bool) {
	var labels []string
	origNext := -1
	jumped := 0
	for off < len(pkt) {
		l := int(pkt[off])
		if l == 0 {
			off++
			if origNext == -1 {
				origNext = off
			}
			return strings.Join(labels, "."), origNext, true
		}
		if l&0xC0 == 0xC0 {
			if off+1 >= len(pkt) {
				return "", 0, false
			}
			ptr := int(binary.BigEndian.Uint16(pkt[off:off+2]) & 0x3FFF)
			if origNext == -1 {
				origNext = off + 2
			}
			off = ptr
			jumped++
			if jumped > 10 {
				return "", 0, false
			}
			continue
		}
		off++
		if off+l > len(pkt) {
			return "", 0, false
		}
		labels = append(labels, string(pkt[off:off+l]))
		off += l
	}
	return "", 0, false
}

func currentLANIPv4() net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.To4() == nil {
				continue
			}
			ip4 := ip.To4()
			// Skip Tailscale CGNAT 100.64.0.0/10
			if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
				continue
			}
			return ip4
		}
	}
	return nil
}

func joinAllInterfaces(conn *net.UDPConn) {
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return
	}
	mcast := [4]byte{224, 0, 0, 251}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagMulticast == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok || ipNet.IP.To4() == nil {
				continue
			}
			ip4 := ipNet.IP.To4()
			if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
				continue
			}
			mreq := &syscall.IPMreq{
				Multiaddr: mcast,
				Interface: [4]byte{ip4[0], ip4[1], ip4[2], ip4[3]},
			}
			_ = rawConn.Control(func(fd uintptr) {
				_ = syscall.SetsockoptIPMreq(int(fd), syscall.IPPROTO_IP, syscall.IP_ADD_MEMBERSHIP, mreq)
			})
		}
	}
}
