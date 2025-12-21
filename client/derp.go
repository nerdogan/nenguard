package main

import (
	"net"

	"github.com/songgao/water"
)

type DERPClient struct {
	conn   *net.UDPConn
	nodeID [16]byte
	tun    *water.Interface
}

func StartDERP(tun *water.Interface, server string, nodeID [16]byte) *DERPClient {
	addr, _ := net.ResolveUDPAddr("udp", server)
	conn, _ := net.DialUDP("udp", nil, addr)

	c := &DERPClient{conn: conn, nodeID: nodeID, tun: tun}

	go func() {
		buf := make([]byte, 2048)
		for {
			n, _ := tun.Read(buf)
			pkt := append([]byte{1}, nodeID[:]...)
			pkt = append(pkt, buf[:n]...)
			conn.Write(pkt)
		}
	}()

	go func() {
		buf := make([]byte, 2048)
		for {
			n, _, _ := conn.ReadFromUDP(buf)
			tun.Write(buf[:n])
		}
	}()

	return c
}
