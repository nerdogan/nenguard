package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

const (
	API_SERVER = "http://192.168.1.104:8080"
	KEY_FILE   = "wg.key"
)

type Peer struct {
	PublicKey string `json:"pub"`
	IP        string `json:"ip"`
}

type RegisterResponse struct {
	IP    string `json:"ip"`
	Peers []Peer `json:"peers"`
}

func main() {
	log.Println("Starting client...")

	key := loadOrCreateKey()
	log.Println("Loaded WireGuard key:", key.PublicKey().String())

	ifName := "utun7"
	if runtime.GOOS == "windows" {
		ifName = "WGUTUN0"
	}

	// TUN device oluştur
	log.Println("Creating TUN device:", ifName)
	tunDev, err := tun.CreateTUN(ifName, 1420)
	if err != nil {
		log.Fatal("Failed to create TUN:", err)
	}
	log.Println("TUN device created:", ifName)

	// UDP bind
	raddr, err := net.ResolveUDPAddr("udp", "192.168.1.104:51820")
	if err != nil {
		log.Fatal("Failed to resolve server UDP:", err)
	}
	c, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		log.Fatal("Failed to create UDP connection:", err)
	}
	bind := &derpBind{conn: c}
	log.Println("UDP bind created to", raddr.String())

	// WireGuard device
	logger := device.NewLogger(device.LogLevelVerbose, "wg")
	wg := device.NewDevice(tunDev, bind, logger)
	log.Println("WireGuard device created, calling wg.Up()...")
	if err := safeUp(wg); err != nil {
		log.Fatal("wg.Up() failed:", err)
	}
	log.Println("WireGuard device is UP")

	// Register ve IP al
	log.Println("Registering to server with public key...")
	resp := register(key.PublicKey().String())
	log.Println("Received from server IP:", resp.IP, "Peers:", len(resp.Peers))

	// WireGuard konfig
	log.Println("Configuring WireGuard interface...")
	configureWG(wg, key, resp)
	log.Println("WireGuard configuration applied")

	// TUN interface IP ve route
	log.Println("Assigning IP and routes to TUN interface...")
	setupTun(ifName, resp.IP)

	log.Println("Client fully started. Assigned IP:", resp.IP)
	select {}
}

// wg.Up() sırasında panic kontrolü
func safeUp(dev *device.Device) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in wg.Up(): %v", r)
		}
	}()
	dev.Up()
	return nil
}

// Key yönetimi
func loadOrCreateKey() wgtypes.Key {
	if b, err := os.ReadFile(KEY_FILE); err == nil {
		k, _ := wgtypes.ParseKey(string(bytes.TrimSpace(b)))
		return k
	}
	k, _ := wgtypes.GeneratePrivateKey()
	_ = os.WriteFile(KEY_FILE, []byte(k.String()), 0600)
	return k
}

// Server register
func register(pub string) RegisterResponse {
	body, _ := json.Marshal(map[string]string{"pub": pub})

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(API_SERVER+"/register", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Fatal("Register failed:", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Fatal("Server returned non-200 status:", resp.Status)
	}

	var r RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		log.Fatal("JSON decode failed:", err)
	}
	if r.IP == "" {
		log.Fatal("Server returned empty IP")
	}
	return r
}

// WireGuard konfig
func configureWG(dev *device.Device, key wgtypes.Key, r RegisterResponse) {
	cfg := "private_key=" + hex.EncodeToString(key[:]) + "\nlisten_port=51820\n"

	for _, p := range r.Peers {
		cfg += "\n[peer]\n"
		cfg += "public_key=" + p.PublicKey + "\n"
		cfg += "allowed_ips=" + p.IP + "\n"
		cfg += "persistent_keepalive_interval=25\n"
	}

	if err := dev.IpcSet(cfg); err != nil {
		log.Fatal("Failed to configure WireGuard:", err)
	}
}

// TUN IP ve route ekleme
func setupTun(ifName, ip string) {
	log.Println("setupTun called with IP:", ip)
	if ip == "" {
		log.Println("setupTun: IP boş, atama yapılmıyor")
		return
	}

	switch runtime.GOOS {
	case "linux":
		parts := strings.Split(ip, "/")
		addr := parts[0]
		mask := "24"
		if len(parts) > 1 {
			mask = parts[1]
		}
		runCmd("sudo", "ip", "addr", "add", addr+"/"+mask, "dev", ifName)
		runCmd("sudo", "ip", "link", "set", "dev", ifName, "up")
		runCmd("sudo", "ip", "route", "add", "10.0.0.0/24", "dev", ifName)

	case "darwin":
		parts := strings.Split(ip, "/")
		addr := parts[0]
		runCmd("sudo", "ifconfig", ifName, addr, addr, "up")
		runCmd("sudo", "route", "-n", "add", "-net", "10.0.0.0/24", addr)

	case "windows":
		parts := strings.Split(ip, "/")
		addr := parts[0]
		runCmd("netsh", "interface", "ip", "set", "address", "name="+ifName, "static", addr, "255.255.255.0")
		runCmd("netsh", "interface", "ip", "add", "route", "10.0.0.0/24", ifName)

	default:
		log.Println("Unsupported OS for automatic TUN IP assignment")
	}
}

func runCmd(name string, args ...string) {
	log.Println("Executing:", name, strings.Join(args, " "))
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		log.Println("Command failed:", err, "Output:", string(out))
	}
}

// derpBind UDP wrap
type derpBind struct {
	conn *net.UDPConn
}

func (b *derpBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	recv := func(bufs [][]byte, sizes []int, eps []conn.Endpoint) (int, error) {
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
