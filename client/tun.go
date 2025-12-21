package main

import "github.com/songgao/water"

func NewTUN() *water.Interface {
	cfg := water.Config{DeviceType: water.TUN}
	cfg.Name = "wg0"
	ifce, _ := water.New(cfg)
	return ifce
}
