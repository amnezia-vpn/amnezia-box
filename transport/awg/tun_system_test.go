package awg

import (
	"errors"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	awgdevice "github.com/amnezia-vpn/amneziawg-go/v3/device"
	awgtun "github.com/amnezia-vpn/amneziawg-go/v3/tun"
	singtun "github.com/sagernet/sing-tun"
)

// blockingTun supplies only OS resource behavior; the AWG device and adapter are real.
type blockingTun struct {
	entered, stopped    chan struct{}
	readOnce, closeOnce sync.Once
	closes, starts      atomic.Int32
	startErr, closeErr  error
}

func newBlockingTun() *blockingTun {
	return &blockingTun{entered: make(chan struct{}), stopped: make(chan struct{})}
}
func (*blockingTun) Name() (string, error)                    { return "test", nil }
func (f *blockingTun) Start() error                           { f.starts.Add(1); return f.startErr }
func (*blockingTun) UpdateRouteOptions(singtun.Options) error { return nil }
func (f *blockingTun) Read([]byte) (int, error) {
	f.readOnce.Do(func() { close(f.entered) })
	<-f.stopped
	return 0, os.ErrClosed
}
func (*blockingTun) Write(p []byte) (int, error) { return len(p), nil }
func (f *blockingTun) Close() error {
	f.closes.Add(1)
	f.closeOnce.Do(func() { close(f.stopped) })
	return f.closeErr
}
func newTestSystemTun(f *blockingTun) *systemTun {
	return &systemTun{mtu: 1408, singtun: f, events: make(chan awgtun.Event, 1)}
}

func TestSystemTunCloseUnblocksCore(t *testing.T) {
	f := newBlockingTun()
	tun := newTestSystemTun(f)
	core := awgdevice.NewDevice(tun, newBind(nil), awgdevice.NewLogger(awgdevice.LogLevelSilent, ""))
	done := make(chan struct{})
	defer func() { f.Close(); core.Close() }()
	waitDone(t, f.entered, "actual AWG TUN reader did not start")
	go func() { core.Close(); close(done) }()
	waitDone(t, done, "actual AWG Close blocked on underlying TUN read")
	if f.closes.Load() != 1 {
		t.Errorf("underlying TUN close count=%d", f.closes.Load())
	}
	if err := tun.Close(); err != nil {
		t.Error(err)
	}
	if f.closes.Load() != 1 {
		t.Errorf("repeated adapter close reached underlying TUN")
	}
}

func TestSystemTunStartAndClose(t *testing.T) {
	f := newBlockingTun()
	tun := newTestSystemTun(f)
	defer f.Close()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("lifecycle panicked: %v", r)
		}
	}()
	// Start must not require the core's event consumer to have been scheduled yet.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 2 {
			if err := tun.Start(); err != nil {
				t.Error(err)
			}
		}
	}()
	waitDone(t, done, "Start blocked waiting for event consumer or repeated Start")
	if event := <-tun.Events(); event != awgtun.EventUp {
		t.Errorf("startup event=%v", event)
	}
	if f.starts.Load() != 1 {
		t.Errorf("underlying Start count=%d", f.starts.Load())
	}
	for range 2 {
		if err := tun.Close(); err != nil {
			t.Error(err)
		}
	}
	if _, ok := <-tun.Events(); ok {
		t.Error("events remain open")
	}
	if err := tun.Start(); !errors.Is(err, net.ErrClosed) {
		t.Errorf("Start after Close=%v", err)
	}
}

func TestSystemTunClosePreservesError(t *testing.T) {
	f := newBlockingTun()
	failure := errors.New("TUN close failed")
	f.closeErr = failure
	tun := newTestSystemTun(f)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("repeated Close panicked: %v", r)
		}
	}()
	for range 2 {
		if err := tun.Close(); !errors.Is(err, failure) {
			t.Errorf("Close error=%v, want original cause", err)
		}
	}
	if f.closes.Load() != 1 {
		t.Errorf("underlying Close count=%d", f.closes.Load())
	}
}

// The core may close its TUN after a read error while the wrapper is starting it.
func TestSystemTunConcurrentStartClose(t *testing.T) {
	for range 20 {
		f := newBlockingTun()
		tun := newTestSystemTun(f)
		gate := make(chan struct{})
		done := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-gate
			if err := tun.Start(); err != nil && !errors.Is(err, net.ErrClosed) {
				t.Errorf("Start: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			<-gate
			if err := tun.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		}()
		close(gate)
		go func() { wg.Wait(); close(done) }()
		waitDone(t, done, "concurrent TUN startup and close did not finish")
		if f.closes.Load() != 1 {
			t.Errorf("underlying Close count=%d", f.closes.Load())
		}
		for event := range tun.Events() {
			if event != awgtun.EventUp {
				t.Errorf("unexpected event=%v", event)
			}
		}
	}
}
