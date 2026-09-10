package awg

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	awgdevice "github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/sagernet/sing-box/adapter"
	M "github.com/sagernet/sing/common/metadata"
)

func waitDone(t *testing.T, done <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal(message)
	}
}

func TestDeviceCloseBeforeStart(t *testing.T) {
	for _, initialize := range []bool{false, true} {
		name := "constructed"
		if initialize {
			name = "initialized"
		}
		t.Run(name, func(t *testing.T) {
			osTun := newBlockingTun()
			d := &Device{tun: newTestSystemTun(osTun)}
			defer osTun.Close()
			if initialize {
				if err := d.Start(adapter.StartStateInitialize); err != nil {
					t.Fatal(err)
				}
			}
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Close panicked: %v", r)
				}
			}()
			for range 2 {
				if err := d.Close(); err != nil {
					t.Error(err)
				}
			}
			if osTun.closes.Load() != 1 {
				t.Errorf("underlying TUN close count=%d, want 1", osTun.closes.Load())
			}
			if err := d.Start(adapter.StartStateStart); !errors.Is(err, net.ErrClosed) {
				t.Errorf("Start after Close=%v", err)
			}
		})
	}
}

func TestDeviceNetworkTunCloseBeforeStart(t *testing.T) {
	d, err := NewDevice(context.Background(), nil, nil, "", DeviceOpts{Address: []netip.Prefix{netip.MustParsePrefix("10.0.0.1/32")}, MTU: 1408})
	if err != nil {
		t.Fatal(err)
	}
	// Release the real userspace TUN even when the old wrapper panics.
	defer func() {
		if r := recover(); r != nil {
			d.tun.Close()
			t.Errorf("Close panicked: %v", r)
		}
	}()
	if err := d.Start(adapter.StartStateInitialize); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	}
}

func TestDeviceStartFailureClosesCore(t *testing.T) {
	failure := errors.New("TUN startup failed")
	bindFailure := errors.New("socket startup failed")
	for _, tt := range []struct {
		name, config        string
		startErr, listenErr error
		want                string
		cause               error
	}{
		{name: "ipc", config: "private_key=invalid\n", want: "set ipc config"},
		{name: "tun", startErr: failure, want: "tun start", cause: failure},
		{name: "bind", listenErr: bindFailure, want: "socket startup failed", cause: bindFailure},
	} {
		t.Run(tt.name, func(t *testing.T) {
			osTun := newBlockingTun()
			osTun.startErr = tt.startErr
			d := &Device{tun: newTestSystemTun(osTun), ipcConfig: tt.config, logger: awgdevice.NewLogger(awgdevice.LogLevelSilent, ""), bind: newBind(packetDialer(func(M.Socksaddr) (net.PacketConn, error) {
				if tt.listenErr != nil {
					return nil, tt.listenErr
				}
				return newPacketConn(), nil
			}))}
			defer func() {
				osTun.Close()
				if d.awgDevice != nil {
					d.awgDevice.Close()
				}
			}()
			err := d.Start(adapter.StartStateStart)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("startup error=%v, want %q", err, tt.want)
			}
			if tt.cause != nil && !errors.Is(err, tt.cause) {
				t.Errorf("startup lost original cause: %v", err)
			}
			if tt.config != "" {
				var ipcErr *awgdevice.IPCError
				if !errors.As(err, &ipcErr) {
					t.Errorf("startup lost original IPC error: %v", err)
				}
			}
			if osTun.closes.Load() != 1 {
				t.Fatal("failed startup retained underlying TUN")
			}
			if d.awgDevice == nil {
				t.Fatal("test did not construct actual AWG core")
			}
			select {
			case <-d.awgDevice.Wait():
			default:
				t.Error("failed startup left actual core running")
			}
			for range 2 {
				if err := d.Close(); err != nil {
					t.Error(err)
				}
			}
		})
	}
}

func TestDeviceConcurrentLifecycle(t *testing.T) {
	for iteration := range 10 {
		osTun := newBlockingTun()
		d := &Device{tun: newTestSystemTun(osTun), logger: awgdevice.NewLogger(awgdevice.LogLevelSilent, ""), bind: newBind(packetDialer(func(M.Socksaddr) (net.PacketConn, error) { return newPacketConn(), nil }))}
		// Exercise races both before core creation and with active receive workers.
		if iteration%2 == 0 {
			if err := d.Start(adapter.StartStateStart); err != nil {
				t.Fatal(err)
			}
			waitDone(t, osTun.entered, "actual AWG TUN reader did not start")
		}
		gate := make(chan struct{})
		done := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			<-gate
			err := d.Start(adapter.StartStateStart)
			if err != nil && !errors.Is(err, net.ErrClosed) {
				t.Errorf("Start: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			<-gate
			for range 3 {
				if err := d.BindUpdate(); err != nil {
					t.Errorf("BindUpdate: %v", err)
				}
			}
		}()
		go func() {
			defer wg.Done()
			<-gate
			if err := d.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		}()
		close(gate)
		go func() { wg.Wait(); close(done) }()
		waitDone(t, done, "concurrent lifecycle did not complete")
		if err := d.Close(); err != nil {
			t.Error(err)
		}
		if osTun.closes.Load() != 1 {
			t.Errorf("TUN close count=%d", osTun.closes.Load())
		}
	}
}
