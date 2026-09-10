package awg

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
)

// packetConn models a socket's blocking read and net.ErrClosed on repeated Close.
type packetConn struct {
	done     chan struct{}
	once     sync.Once
	closes   atomic.Int32
	closeErr error
}

func newPacketConn() *packetConn                                { return &packetConn{done: make(chan struct{})} }
func (c *packetConn) ReadFrom([]byte) (int, net.Addr, error)    { <-c.done; return 0, nil, net.ErrClosed }
func (c *packetConn) WriteTo(p []byte, _ net.Addr) (int, error) { return len(p), nil }
func (c *packetConn) Close() error {
	n := c.closes.Add(1)
	c.once.Do(func() { close(c.done) })
	if n > 1 {
		return net.ErrClosed
	}
	return c.closeErr
}
func (*packetConn) LocalAddr() net.Addr              { return &net.UDPAddr{} }
func (*packetConn) SetDeadline(time.Time) error      { return nil }
func (*packetConn) SetReadDeadline(time.Time) error  { return nil }
func (*packetConn) SetWriteDeadline(time.Time) error { return nil }

type packetDialer func(M.Socksaddr) (net.PacketConn, error)

func (f packetDialer) ListenPacket(_ context.Context, addr M.Socksaddr) (net.PacketConn, error) {
	return f(addr)
}
func (packetDialer) DialContext(context.Context, string, M.Socksaddr) (net.Conn, error) {
	return nil, errors.ErrUnsupported
}

func TestBindOpenRollsBackIPv4(t *testing.T) {
	var packets []*packetConn
	failIPv6 := true
	b := newBind(packetDialer(func(addr M.Socksaddr) (net.PacketConn, error) {
		if addr.Addr.Is6() && failIPv6 {
			return nil, syscall.EADDRNOTAVAIL
		}
		p := newPacketConn()
		packets = append(packets, p)
		return p, nil
	}))
	defer b.Close()
	fns, _, err := b.Open(51820)
	if !errors.Is(err, syscall.EADDRNOTAVAIL) {
		t.Fatalf("Open error = %v, want IPv6 failure", err)
	}
	if len(fns) != 0 {
		t.Errorf("failed Open returned %d receivers", len(fns))
	}
	if packets[0].closes.Load() != 1 {
		t.Errorf("failed Open leaked IPv4 socket")
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	failIPv6 = false
	fns, port, err := b.Open(51820)
	if err != nil || len(fns) != 2 || port != 51820 {
		t.Fatalf("retry: receivers=%d, port=%d, error=%v", len(fns), port, err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	for i, p := range packets {
		if p.closes.Load() != 1 {
			t.Errorf("socket %d close count = %d", i, p.closes.Load())
		}
	}
}

func TestBindOpenUnsupportedFamily(t *testing.T) {
	for _, ipv4 := range []bool{true, false} {
		t.Run(fmt.Sprintf("ipv4_unsupported_%t", ipv4), func(t *testing.T) {
			p := newPacketConn()
			b := newBind(packetDialer(func(addr M.Socksaddr) (net.PacketConn, error) {
				if addr.Addr.Is4() == ipv4 {
					return nil, syscall.EAFNOSUPPORT
				}
				return p, nil
			}))
			defer b.Close()
			fns, _, err := b.Open(51820)
			if err != nil || len(fns) != 1 {
				t.Fatalf("receivers=%d, error=%v", len(fns), err)
			}
			if err := b.Close(); err != nil {
				t.Fatal(err)
			}
			if p.closes.Load() != 1 {
				t.Fatal("supported socket was not released")
			}
		})
	}
}

func TestBindClosePreservesOtherSocketError(t *testing.T) {
	failure := errors.New("socket close failed")
	for _, closed4 := range []bool{true, false} {
		t.Run(fmt.Sprintf("ipv4_already_closed_%t", closed4), func(t *testing.T) {
			p4, p6 := newPacketConn(), newPacketConn()
			if closed4 {
				p4.Close()
				p6.closeErr = failure
			} else {
				p6.Close()
				p4.closeErr = failure
			}
			b := &bind_adapter{conn4: p4, conn6: p6}
			err := b.Close()
			if !errors.Is(err, failure) {
				t.Errorf("lost real close failure: %v", err)
			}
			if errors.Is(err, net.ErrClosed) {
				t.Errorf("already-closed socket should be ignored: %v", err)
			}
			if err := b.Close(); err != nil {
				t.Errorf("repeated Close: %v", err)
			}
		})
	}
}
