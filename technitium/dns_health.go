package technitium

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// CheckDNS probes the host-facing DNS listener, not the admin API or the VIP's
// current owner. A local cluster SOA works without public DNS or internet access
// and prevents an uninitialized resolver from becoming eligible. Both DNS
// transports must work. Each exchange is bounded even if the caller is not.
func CheckDNS(ctx context.Context, address, clusterDomain string) error {
	if clusterDomain == "" {
		clusterDomain = DefaultClusterDomain
	}
	for _, network := range []string{"udp", "tcp"} {
		if err := checkDNSExchange(ctx, network, address, clusterDomain); err != nil {
			return fmt.Errorf("DNS %s: %w", network, err)
		}
	}
	return nil
}

func checkDNSExchange(ctx context.Context, network, address, domain string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	name, err := dnsmessage.NewName(strings.TrimSuffix(domain, ".") + ".")
	if err != nil {
		return err
	}
	var randomID [2]byte
	if _, err := rand.Read(randomID[:]); err != nil {
		return err
	}
	id := binary.BigEndian.Uint16(randomID[:])
	query := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: id}, // authoritative local record; no recursion
		Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypeSOA, Class: dnsmessage.ClassINET}},
	}
	wire, err := query.Pack()
	if err != nil {
		return err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	// Cancel an exchange promptly when the agent is stopping.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if network == "tcp" {
		wire = append([]byte{byte(len(wire) >> 8), byte(len(wire))}, wire...)
	}
	if _, err := io.Copy(conn, strings.NewReader(string(wire))); err != nil {
		return err
	}
	var response []byte
	if network == "tcp" {
		var length [2]byte
		if _, err := io.ReadFull(conn, length[:]); err != nil {
			return err
		}
		response = make([]byte, binary.BigEndian.Uint16(length[:]))
		if _, err := io.ReadFull(conn, response); err != nil {
			return err
		}
	} else {
		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil {
			return err
		}
		response = buf[:n]
	}
	var message dnsmessage.Message
	if err := message.Unpack(response); err != nil {
		return err
	}
	if message.ID != id || !message.Response || !message.Authoritative || message.Truncated || message.RCode != dnsmessage.RCodeSuccess || len(message.Questions) != 1 || message.Questions[0] != query.Questions[0] {
		return fmt.Errorf("invalid or unsuccessful local SOA response")
	}
	for _, answer := range message.Answers {
		if answer.Header.Type == dnsmessage.TypeSOA && strings.EqualFold(answer.Header.Name.String(), name.String()) {
			return nil
		}
	}
	return fmt.Errorf("cluster SOA missing from response")
}
