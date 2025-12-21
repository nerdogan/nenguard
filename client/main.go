package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

const (
	API_SERVER  = "http://192.168.1.106:8080"
	DERP_SERVER = "192.168.1.106:3478"
	KEY_FILE    = "wg.key"
)

type RegisterResponse struct {
	IP    string `json:"ip"`
	Peers []Peer `json:"peers"`
}

type Peer struct {
	PublicKey string `json:"pub"`
	IP        string `json:"ip"`
}

func main() {
	key := loadOrCreateKey()

	tunDev, err := tun.CreateTUN("utun7", 1420)
	if err != nil {
		log.Fatal(err)
	}
	bind := newDERPBind(DERP_SERVER)

	logger := device.NewLogger(device.LogLevelSilent, "wg")
	wg := device.NewDevice(tunDev, bind, logger)
	wg.Up()

	resp := register(key.PublicKey().String())
	configureWG(wg, key, resp)

	log.Println("nenguard client running")
	select {}
}

//////////////////// KEY ////////////////////

func loadOrCreateKey() wgtypes.Key {
	if b, err := os.ReadFile(KEY_FILE); err == nil {
		k, _ := wgtypes.ParseKey(string(bytes.TrimSpace(b)))
		return k
	}
	k, _ := wgtypes.GeneratePrivateKey()
	_ = os.WriteFile(KEY_FILE, []byte(k.String()), 0600)
	return k
}

//////////////////// CONTROL PLANE ////////////////////

func register(pub string) RegisterResponse {
	body, _ := json.Marshal(map[string]string{"pub": pub})
	resp, err := http.Post(API_SERVER+"/register", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	var r RegisterResponse
	_ = json.NewDecoder(resp.Body).Decode(&r)
	return r
}

//////////////////// WG CONFIG ////////////////////

func configureWG(dev *device.Device, key wgtypes.Key, r RegisterResponse) {
	cfg := "private_key=" + hex.EncodeToString(key[:]) + "\nlisten_port=0\n"

	for _, p := range r.Peers {
		cfg += "\n[peer]\n"
		cfg += "public_key=" + p.PublicKey + "\n"
		cfg += "allowed_ip=" + p.IP + "\n"
		cfg += "persistent_keepalive_interval=25\n"
	}

	if err := dev.IpcSet(cfg); err != nil {
		log.Fatal(err)
	}

	log.Println("WireGuard configured, IP:", r.IP)
}

//////////////////// DERP BIND ////////////////////

type derpBind struct {
	conn *net.UDPConn
}

func newDERPBind(addr string) conn.Bind {
	raddr, _ := net.ResolveUDPAddr("udp", addr)
	c, _ := net.DialUDP("udp", nil, raddr)
	return &derpBind{conn: c}
}

func (b *derpBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	recv := func(bufs [][]byte, sizes []int, eps []conn.Endpoint) (n int, err error) {
		return 0, nil
	}

	return []conn.ReceiveFunc{recv}, port, nil
}

func (b *derpBind) Send(pkts [][]byte, _ conn.Endpoint) error {
	for _, p := range pkts {
		_, _ = b.conn.Write(p)
	}
	return nil
}

func (b *derpBind) Close() error         { return b.conn.Close() }
func (b *derpBind) BatchSize() int       { return 1 }
func (b *derpBind) SetMark(uint32) error { return nil }
func (b *derpBind) ParseEndpoint(string) (conn.Endpoint, error) {
	return nil, nil
}
