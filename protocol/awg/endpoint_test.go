package awg

import (
	"strings"
	"testing"

	"github.com/amnezia-vpn/amneziawg-go/v3/conn/bindtest"
	awgdevice "github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/amnezia-vpn/amneziawg-go/v3/tun/tuntest"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json"
)

func TestJSONToIPC(t *testing.T) {
	for _, test := range []struct {
		name string
		json string
		want string
	}{
		{
			name: "legacy",
			json: `{
				"private_key": "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=",
				"address": "10.0.0.1/32",
				"listen_port": 51820,
				"jc": 3, "jmin": 10, "jmax": 20,
				"s1": 16, "s2": 24,
				"h1": "101", "h2": "202", "h3": "303", "h4": "404",
				"peers": [{
					"address": "127.0.0.1", "port": 51821,
					"public_key": "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=",
					"preshared_key": "AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=",
					"allowed_ips": ["10.0.0.2/32", "fd00::2/128"],
					"persistent_keepalive_interval": 25
				}]
			}`,
			want: `private_key=0101010101010101010101010101010101010101010101010101010101010101
listen_port=51820
jc=3
jmin=10
jmax=20
s1=16
s2=24
h1=101
h2=202
h3=303
h4=404
public_key=0202020202020202020202020202020202020202020202020202020202020202
preshared_key=0303030303030303030303030303030303030303030303030303030303030303
endpoint=127.0.0.1:51821
persistent_keepalive_interval=25
allowed_ip=10.0.0.2/32
allowed_ip=fd00::2/128`,
		},
		{
			name: "awg_3_1",
			json: `{
				"private_key": "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=",
				"s1": 16, "s2": 24, "s3": 32, "s4": 40,
				"h1": "100-199", "h2": "200-299", "h3": "300-399", "h4": "400-499",
				"i1": "<b 0x01>", "i2": "<b 0x02>", "i3": "<b 0x03>", "i4": "<b 0x04>", "i5": "<b 0x05>",
				"header_protection_key": "BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ=",
				"content_padding_addition": "0-32",
				"rekey_after_time": "110-120", "rekey_timeout": "4-5",
				"reject_after_time": "170-180", "keepalive_timeout": "9-10",
				"max_handshake_attempts": "18-20",
				"random_trailers": true, "disable_cookies": true
			}`,
			want: `private_key=0101010101010101010101010101010101010101010101010101010101010101
s1=16
s2=24
s3=32
s4=40
h1=100-199
h2=200-299
h3=300-399
h4=400-499
i1=<b 0x01>
i2=<b 0x02>
i3=<b 0x03>
i4=<b 0x04>
i5=<b 0x05>
header_protection_key=0404040404040404040404040404040404040404040404040404040404040404
content_padding_addition=0-32
rekey_after_time=110-120
rekey_timeout=4-5
reject_after_time=170-180
keepalive_timeout=9-10
max_handshake_attempts=18-20
random_trailers=true
disable_cookies=true`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var options option.AwgEndpointOptions
			if err := json.Unmarshal([]byte(test.json), &options); err != nil {
				t.Fatal(err)
			}
			ipc, err := genIpcConfig(options)
			if err != nil {
				t.Fatal(err)
			}
			if ipc != test.want {
				t.Fatalf("unexpected IPC config:\ngot:\n%s\nwant:\n%s", ipc, test.want)
			}
			if err := newConfigTestDevice(t).IpcSet(ipc); err != nil {
				t.Fatalf("AWG rejected generated IPC: %v", err)
			}
		})
	}
}

func TestInvalidHeaderProtectionKey(t *testing.T) {
	for _, test := range []struct {
		name, key string
	}{
		{name: "base64", key: "not-base64!"},
		{name: "length", key: "AQ=="},
	} {
		t.Run(test.name, func(t *testing.T) {
			var options option.AwgEndpointOptions
			if err := json.Unmarshal([]byte(`{
				"private_key": "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=",
				"header_protection_key": "`+test.key+`"
			}`), &options); err != nil {
				t.Fatal(err)
			}
			ipc, err := genIpcConfig(options)
			if err == nil {
				err = newConfigTestDevice(t).IpcSet(ipc)
			}
			if err == nil || !strings.Contains(err.Error(), "header_protection_key") {
				t.Fatalf("invalid key was not rejected with its field name: %v", err)
			}
		})
	}
}

func newConfigTestDevice(t *testing.T) *awgdevice.Device {
	device := awgdevice.NewDevice(tuntest.NewChannelTUN().TUN(), bindtest.NewChannelBinds()[0], awgdevice.NewLogger(awgdevice.LogLevelSilent, ""))
	t.Cleanup(device.Close)
	return device
}
