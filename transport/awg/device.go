package awg

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"

	"github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/amnezia-vpn/amneziawg-go/v3/device"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/exceptions"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/network"
)

type DeviceOpts struct {
	UseIntegratedTun bool
	Address          []netip.Prefix
	AllowedIps       []netip.Prefix
	ExcludedIps      []netip.Prefix
	MTU              uint32
}

type Device struct {
	access    sync.Mutex
	closed    bool
	closeErr  error
	awgDevice *device.Device
	tun       tunAdapter
	bind      conn.Bind
	logger    *device.Logger
	ipcConfig string
}

func NewDevice(ctx context.Context, logger logger.ContextLogger, dial network.Dialer, ipcConfig string, opts DeviceOpts) (*Device, error) {
	var (
		tun tunAdapter
		err error
	)

	if opts.UseIntegratedTun {
		tun, err = newSystemTun(ctx, opts.Address, opts.AllowedIps, opts.ExcludedIps, opts.MTU, logger)
		if err != nil {
			return nil, exceptions.Cause(err, "create tunnel")
		}
	} else {
		tun, err = newNetworkTun(opts.Address, opts.MTU)
		if err != nil {
			return nil, err
		}
	}

	awgLogger := &device.Logger{
		Verbosef: func(format string, args ...interface{}) {
			logger.Debug(fmt.Sprintf(strings.ToLower(format), args...))
		},
		Errorf: func(format string, args ...interface{}) {
			logger.Error(fmt.Sprintf(strings.ToLower(format), args...))
		},
	}

	return &Device{
		tun:       tun,
		bind:      newBind(dial),
		logger:    awgLogger,
		ipcConfig: ipcConfig,
	}, nil
}

func (d *Device) Start(stage adapter.StartStage) (err error) {
	if stage != adapter.StartStateStart {
		return nil
	}

	d.access.Lock()
	defer d.access.Unlock()
	if d.closed {
		return net.ErrClosed
	}
	if d.awgDevice != nil {
		return nil
	}
	defer func() {
		if err != nil {
			d.closeLocked()
		}
	}()

	d.awgDevice = device.NewDevice(d.tun, d.bind, d.logger)
	if err := d.awgDevice.IpcSet(d.ipcConfig); err != nil {
		return E.Cause(err, "set ipc config")
	}

	if err := d.tun.Start(); err != nil {
		return E.Cause(err, "tun start")
	}

	return d.awgDevice.Up()
}

func (d *Device) BindUpdate() error {
	d.access.Lock()
	defer d.access.Unlock()
	if d.closed || d.awgDevice == nil {
		return nil
	}
	return d.awgDevice.BindUpdate()
}

func (d *Device) Close() error {
	d.access.Lock()
	defer d.access.Unlock()
	return d.closeLocked()
}

// closeLocked releases the TUN directly until the core takes ownership at Start.
func (d *Device) closeLocked() error {
	if d.closed {
		return d.closeErr
	}
	d.closed = true
	if d.awgDevice != nil {
		d.awgDevice.Close()
	} else if d.tun != nil {
		d.closeErr = d.tun.Close()
	}
	return d.closeErr
}

func (d *Device) DialContext(ctx context.Context, network string, destination metadata.Socksaddr) (net.Conn, error) {
	return d.tun.DialContext(ctx, network, destination)
}

func (d *Device) ListenPacket(ctx context.Context, destination metadata.Socksaddr) (net.PacketConn, error) {
	return d.tun.ListenPacket(ctx, destination)
}
