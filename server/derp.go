package main

import (
	"net"
	"sync"
)

var peers = sync.Map{} // nodeID -> addr

func StartDERP() {
	addr, _ := net.ResolveUDPAddr("udp", ":3478")
	conn, _ := net.ListenUDP("udp", addr)
	buf := make([]byte, 2048)

	for {
		n, src, _ := conn.ReadFromUDP(buf)
		if n < 17 {
			continue
		}
		var nodeID [16]byte
		copy(nodeID[:], buf[1:17])
		peers.Store(nodeID, src)
		if dst, ok := peers.Load(nodeID); ok {
			conn.WriteToUDP(buf[17:n], dst.(*net.UDPAddr))
		}
	}
}
