package awg

import (
	"bytes"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/binary"
	"net/netip"
	"runtime"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/amnezia-vpn/amneziawg-go/v3/conn/bindtest"
	awgdevice "github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/amnezia-vpn/amneziawg-go/v3/tun/tuntest"
	"github.com/sagernet/sing-box/option"
)

func TestHandshake(t *testing.T) {
	for _, test := range []struct {
		name           string
		disableCookies bool
		underLoad      bool
	}{
		{name: "default"},
		{name: "cookies_disabled", disableCookies: true},
		{name: "cookies_disabled_under_load", disableCookies: true, underLoad: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				binds := bindtest.NewChannelBinds()
				tuns := [2]*tuntest.ChannelTUN{tuntest.NewChannelTUN(), tuntest.NewChannelTUN()}
				var devices [2]*awgdevice.Device
				var keys [2]*ecdh.PrivateKey
				for i := range keys {
					var err error
					keys[i], err = ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{byte(i + 1)}, 32))
					if err != nil {
						t.Fatal(err)
					}
				}

				// Hold invalid packets at the public logger boundary so the receive
				// queue can fill without exposing or modifying AWG's private state.
				releaseNoise := make(chan struct{})
				unblockNoise := sync.OnceFunc(func() { close(releaseNoise) })
				defer func() {
					unblockNoise()
					for _, device := range devices {
						if device != nil {
							device.Close()
						}
					}
				}()
				for i := range devices {
					logger := awgdevice.NewLogger(awgdevice.LogLevelSilent, "")
					if i == 0 && test.underLoad {
						logger.Verbosef = func(format string, args ...any) {
							if format == "Received packet with invalid mac1" {
								<-releaseNoise
							}
						}
					}
					devices[i] = awgdevice.NewDevice(tuns[i].TUN(), binds[i], logger)
					other := 1 - i
					ipc, err := genIpcConfig(option.AwgEndpointOptions{
						PrivateKey:     base64.StdEncoding.EncodeToString(keys[i].Bytes()),
						DisableCookies: test.disableCookies,
						Peers: []option.AwgPeerOptions{{
							Address:    "127.0.0.1",
							Port:       uint16(i + 1),
							PublicKey:  base64.StdEncoding.EncodeToString(keys[other].PublicKey().Bytes()),
							AllowedIPs: []netip.Prefix{netip.PrefixFrom(netip.AddrFrom4([4]byte{10, 0, 0, byte(other + 1)}), 32)},
						}},
					})
					if err != nil {
						t.Fatal(err)
					}
					if err := devices[i].IpcSet(ipc); err != nil {
						t.Fatal(err)
					}
					if err := devices[i].Up(); err != nil {
						t.Fatal(err)
					}
				}
				synctest.Wait()

				if test.underLoad {
					noise := make([]byte, awgdevice.MessageInitiationSize)
					binary.LittleEndian.PutUint32(noise, awgdevice.MessageInitiationType)
					endpoint, err := binds[1].ParseEndpoint("127.0.0.1:2")
					if err != nil {
						t.Fatal(err)
					}
					for range awgdevice.QueueHandshakeSize + runtime.NumCPU() {
						if err := binds[1].Send([][]byte{noise}, endpoint); err != nil {
							t.Fatal(err)
						}
					}
					synctest.Wait()
					if !devices[0].IsUnderLoad() {
						t.Fatal("receive queue did not trigger AWG's load protection")
					}
					unblockNoise()
					synctest.Wait()
				}

				packet := tuntest.Ping(netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("10.0.0.2"))
				tuns[1].Outbound <- packet
				// Drain runnable work without advancing time past the load window
				// or allowing a later handshake retry to hide a dropped initiation.
				synctest.Wait()
				if test.underLoad && !devices[0].IsUnderLoad() {
					t.Fatal("load protection expired before the handshake was checked")
				}
				select {
				case received := <-tuns[0].Inbound:
					if !bytes.Equal(received, packet) {
						t.Fatalf("packet changed in transit: got %x, want %x", received, packet)
					}
				default:
					t.Fatal("handshake did not deliver the queued packet")
				}
			})
		})
	}
}
