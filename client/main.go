package main

func main() {
	tun := NewTUN()
	wg := StartWG(tun)
	_ = wg

	var nodeID [16]byte
	copy(nodeID[:], []byte("NODE123456789012"))

	StartDERP(tun, "SERVER_IP:3478", nodeID)

	select {}
}
