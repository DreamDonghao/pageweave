package network

import (
	"context"
	"net"
	"net/netip"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func TestPublic(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "172.16.1.1", "192.168.1.1", "169.254.1.1", "100.64.0.1", "224.0.0.1", "0.0.0.0", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "ff02::1", "2001:db8::1", "64:ff9b::7f00:1"} {
		if Public(netip.MustParseAddr(raw)) {
			t.Errorf("allowed %s", raw)
		}
	}
	for _, raw := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if !Public(netip.MustParseAddr(raw)) {
			t.Error(raw)
		}
	}
}
func TestURL(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "javascript:alert(1)", "/relative", "http://u:p@example.com", "https://example.com:99999", "http://[::1]"} {
		if raw == "http://[::1]" {
			if (Policy{}).Check(context.Background(), raw) == nil {
				t.Fatal(raw)
			}
		} else if _, e := Parse(raw); e == nil {
			t.Fatal(raw)
		}
	}
}
func TestFixtureExactOrigin(t *testing.T) {
	p := Policy{FixtureURL: "http://127.0.0.1:2345"}
	if e := p.Check(context.Background(), "http://127.0.0.1:2345/x"); e != nil {
		t.Fatal(e)
	}
	if e := p.Check(context.Background(), "http://127.0.0.1:2346/x"); e == nil {
		t.Fatal("wrong port allowed")
	}
}

func TestDNSAllAddressesAndCancellation(t *testing.T) {
	conn, e := net.ListenPacket("udp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	done := make(chan struct{})
	defer func() { conn.Close(); <-done }()
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, addr, e := conn.ReadFrom(buf)
			if e != nil {
				return
			}
			var query dnsmessage.Message
			if query.Unpack(buf[:n]) != nil {
				continue
			}
			reply := dnsmessage.Message{Header: dnsmessage.Header{ID: query.Header.ID, Response: true, RecursionAvailable: true}, Questions: query.Questions}
			for _, q := range query.Questions {
				if q.Type == dnsmessage.TypeA {
					for _, ip := range [][4]byte{{8, 8, 8, 8}, {127, 0, 0, 1}} {
						reply.Answers = append(reply.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: q.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 1}, Body: &dnsmessage.AResource{A: ip}})
					}
				}
			}
			data, e := reply.Pack()
			if e == nil {
				_, _ = conn.WriteTo(data, addr)
			}
		}
	}()
	resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", conn.LocalAddr().String())
	}}
	p := Policy{Resolver: resolver}
	if e = p.Check(context.Background(), "https://mixed.test/"); e == nil {
		t.Fatal("mixed public/private DNS allowed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e = p.Check(ctx, "https://cancelled.test/"); e == nil {
		t.Fatal("cancelled lookup succeeded")
	}
}
