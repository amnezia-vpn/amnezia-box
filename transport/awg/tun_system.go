package awg

import (
	"context"
	"net"
	"net/netip"
	"os"
	"sync"

	awgTun "github.com/amnezia-vpn/amneziawg-go/v3/tun"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/option"
	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

type systemTun struct {
	access   sync.Mutex
	started  bool
	closed   bool
	closeErr error
	mtu      uint32
	singtun  tun.Tun
	events   chan awgTun.Event
	name     string
	dialer   network.Dialer
}

func newSystemTun(ctx context.Context, address []netip.Prefix, allowedIps []netip.Prefix, excludedIps []netip.Prefix, mtu uint32, logger logger.Logger) (tunAdapter, error) {
	networkManager := service.FromContext[adapter.NetworkManager](ctx)
	name := tun.CalculateInterfaceName("")
	// A single startup event must not wait for the core event reader.
	events := make(chan awgTun.Event, 1)

	dial, err := dialer.NewDefault(ctx, option.DialerOptions{
		BindInterface: name,
	})
	if err != nil {
		return nil, exceptions.Cause(err, "get in-tunnel dialer")
	}

	singtun, err := tun.New(tun.Options{
		Name: name,
		GSO:  true,
		MTU:  uint32(mtu),
		Inet4Address: common.Filter(address, func(it netip.Prefix) bool {
			return it.Addr().Is4()
		}),
		Inet6Address: common.Filter(address, func(it netip.Prefix) bool {
			return it.Addr().Is6()
		}),
		InterfaceMonitor: networkManager.InterfaceMonitor(),
		InterfaceFinder:  networkManager.InterfaceFinder(),
		Inet4RouteAddress: common.Filter(allowedIps, func(it netip.Prefix) bool {
			return it.Addr().Is4()
		}),
		Inet6RouteAddress: common.Filter(allowedIps, func(it netip.Prefix) bool {
			return it.Addr().Is6()
		}),
		Inet4RouteExcludeAddress: common.Filter(excludedIps, func(it netip.Prefix) bool {
			return it.Addr().Is4()
		}),
		Inet6RouteExcludeAddress: common.Filter(excludedIps, func(it netip.Prefix) bool {
			return it.Addr().Is6()
		}),
		Logger: logger,
	})
	if err != nil {
		return nil, exceptions.Cause(err, "create tunnel")
	}

	return &systemTun{
		mtu:     mtu,
		events:  events,
		singtun: singtun,
		name:    name,
		dialer:  dial,
	}, nil
}

func (t *systemTun) Start() error {
	t.access.Lock()
	defer t.access.Unlock()
	if t.closed {
		return net.ErrClosed
	}
	if t.started {
		return nil
	}
	if err := t.singtun.Start(); err != nil {
		return exceptions.Cause(err, "start tunnel")
	}

	t.started = true
	t.events <- awgTun.EventUp
	return nil
}

func (t *systemTun) File() *os.File {
	return nil
}

func (t *systemTun) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	n, err := t.singtun.Read(bufs[0][offset-tun.PacketOffset:])
	if err != nil {
		return 0, err
	}
	sizes[0] = n
	return 1, nil
}

func (t *systemTun) Write(bufs [][]byte, offset int) (int, error) {
	for _, buf := range bufs {
		common.ClearArray(buf[offset-tun.PacketOffset : offset])
		tun.PacketFillHeader(buf[offset-tun.PacketOffset:], tun.PacketIPVersion(buf[offset:]))

		if _, err := t.singtun.Write(buf[offset-tun.PacketOffset:]); err != nil {
			return 0, err
		}
	}
	return len(bufs), nil
}

func (t *systemTun) MTU() (int, error) {
	return int(t.mtu), nil
}

func (t *systemTun) Name() (string, error) {
	return t.name, nil
}

func (t *systemTun) Events() <-chan awgTun.Event {
	return t.events
}

func (t *systemTun) Close() error {
	t.access.Lock()
	defer t.access.Unlock()
	if t.closed {
		return t.closeErr
	}
	t.closed = true
	// Closing the OS TUN releases the core's blocking Read before it waits for exit.
	t.closeErr = t.singtun.Close()
	close(t.events)
	return t.closeErr
}

func (t *systemTun) BatchSize() int {
	return 1
}

func (t *systemTun) DialContext(ctx context.Context, network string, destination metadata.Socksaddr) (net.Conn, error) {
	return t.dialer.DialContext(ctx, network, destination)
}

func (t *systemTun) ListenPacket(ctx context.Context, destination metadata.Socksaddr) (net.PacketConn, error) {
	return t.dialer.ListenPacket(ctx, destination)
}
