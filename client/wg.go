package main

import (
	"log"

	"github.com/songgao/water"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

func StartWG(t *water.Interface) *device.Device {
	td, err := tun.FromFile(t.File())
	if err != nil {
		log.Fatal(err)
	}
	wg := device.NewDevice(td, device.NewLogger(device.LogLevelVerbose, "wg> "), nil)
	wg.Up()
	return wg
}
