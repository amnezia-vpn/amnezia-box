package main

import (
	"fmt"
	"log"

	"github.com/sagernet/sing-box/experimental/libbox"
)

func main() {
	// CheckConfig constructs and closes a userspace AWG endpoint. It does not
	// start a VPN connection or create an operating-system TUN interface.
	err := libbox.CheckConfig(`{
		"endpoints": [{
			"type": "awg",
			"tag": "awg",
			"private_key": "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=",
			"address": ["10.0.0.1/32"],
			"disable_cookies": true,
			"content_padding_addition": "0-32"
		}]
	}`)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("AWG consumer configuration accepted")
}
