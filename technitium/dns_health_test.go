package technitium

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func dnsTestServer(t *testing.T, mutate func(string, *dnsmessage.Message)) string {
	t.Helper()
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	udp, err := net.ListenPacket("udp", tcp.Addr().String())
	if err != nil {
		tcp.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { tcp.Close(); udp.Close() })
	response := func(network string, wire []byte) []byte {
		var query dnsmessage.Message
		if err := query.Unpack(wire); err != nil {
			return nil
		}
		query.Header = dnsmessage.Header{ID: query.ID, Response: true, Authoritative: true}
		query.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: query.Questions[0].Name, Type: dnsmessage.TypeSOA, Class: dnsmessage.ClassINET}, Body: &dnsmessage.SOAResource{NS: query.Questions[0].Name, MBox: query.Questions[0].Name}}}
		mutate(network, &query)
		out, _ := query.Pack()
		return out
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := udp.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = udp.WriteTo(response("udp", buf[:n]), addr)
		}
	}()
	go func() {
		for {
			conn, err := tcp.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(time.Second))
				var length [2]byte
				if _, err := io.ReadFull(conn, length[:]); err != nil {
					return
				}
				query := make([]byte, binary.BigEndian.Uint16(length[:]))
				if _, err := io.ReadFull(conn, query); err != nil {
					return
				}
				wire := response("tcp", query)
				binary.BigEndian.PutUint16(length[:], uint16(len(wire)))
				_, _ = conn.Write(append(length[:], wire...))
			}()
		}
	}()
	return tcp.Addr().String()
}

func TestCheckDNSRequiresAuthoritativeClusterSOAOverBothTransports(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mutate    func(string, *dnsmessage.Message)
		wantError bool
	}{
		{"healthy", func(string, *dnsmessage.Message) {}, false},
		{"wrong ID", func(_ string, m *dnsmessage.Message) { m.ID++ }, true},
		{"uninitialized", func(_ string, m *dnsmessage.Message) { m.Answers = nil }, true},
		{"not authoritative", func(_ string, m *dnsmessage.Message) { m.Authoritative = false }, true},
		{"servfail", func(_ string, m *dnsmessage.Message) { m.RCode = dnsmessage.RCodeServerFailure }, true},
		{"TCP broken", func(network string, m *dnsmessage.Message) {
			if network == "tcp" {
				m.Answers = nil
			}
		}, true},
		{"UDP broken", func(network string, m *dnsmessage.Message) {
			if network == "udp" {
				m.Answers = nil
			}
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			address := dnsTestServer(t, tc.mutate)
			err := CheckDNS(context.Background(), address, DefaultClusterDomain)
			if (err != nil) != tc.wantError {
				t.Fatalf("CheckDNS error = %v, want error %v", err, tc.wantError)
			}
		})
	}
}

func TestCheckDNSHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := CheckDNS(ctx, "127.0.0.1:53", DefaultClusterDomain); err == nil {
		t.Fatal("cancelled probe succeeded")
	}
}
