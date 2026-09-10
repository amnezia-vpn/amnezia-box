//go:build with_conntrack

package awg

import (
	"net"
	"testing"

	awgdevice "github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/amnezia-vpn/amneziawg-go/v3/tun/tuntest"
	"github.com/sagernet/sing-box/common/conntrack"
	M "github.com/sagernet/sing/common/metadata"
)

func TestDeviceBindUpdateAfterConntrackReset(t *testing.T) {
	// conntrack.Close operates on a global registry; do not run this test in parallel.
	var packets []*packetConn
	dial := packetDialer(func(M.Socksaddr) (net.PacketConn, error) {
		p := newPacketConn()
		packets = append(packets, p)
		return conntrack.NewPacketConn(p)
	})
	core := awgdevice.NewDevice(tuntest.NewChannelTUN().TUN(), newBind(dial), awgdevice.NewLogger(awgdevice.LogLevelSilent, ""))
	defer core.Close()
	d := &Device{awgDevice: core}
	if err := core.Up(); err != nil {
		t.Fatal(err)
	}
	if len(packets) != 2 {
		t.Fatalf("initial sockets=%d, want 2", len(packets))
	}
	for reset := 1; reset <= 3; reset++ {
		conntrack.Close()
		if err := d.BindUpdate(); err != nil {
			t.Errorf("reset %d: %v", reset, err)
		}
		if len(packets) != 2*(reset+1) {
			t.Fatalf("reset %d: socket opens=%d, want %d", reset, len(packets), 2*(reset+1))
		}
		for _, p := range packets[:len(packets)-2] {
			select {
			case <-p.done:
			default:
				t.Error("old socket still open")
			}
		}
	}
}
