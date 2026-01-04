package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/crypto/curve25519"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

type Peer struct {
	PublicKey string `json:"pub"`
	IP        string `json:"ip"`
}

type RegisterResponse struct {
	IP    string `json:"ip"`
	Peers []Peer `json:"peers"`
}

type Keys struct {
	Private string `json:"private_key"`
	Public  string `json:"public_key"`
}

func configureNetwork(iface, localIP string) {
	pureIP := strings.Split(localIP, "/")[0]
	log.Printf("Network Konfigürasyonu: %s (%s)", iface, localIP)

	switch runtime.GOOS {
	case "darwin":
		exec.Command("ifconfig", iface, pureIP, pureIP, "netmask", "255.255.255.0", "up").Run()
		exec.Command("route", "add", "-net", "10.0.0.0/24", "-interface", iface).Run()
	case "linux":
		// Hata almamak için önce temizle sonra ekle
		exec.Command("ip", "link", "set", iface, "up").Run()
		exec.Command("ip", "addr", "flush", "dev", iface).Run()
		exec.Command("ip", "addr", "add", localIP, "dev", iface).Run()
		// Rota çakışmasını önlemek için
		exec.Command("ip", "route", "del", "10.0.0.0/24").Run()
		err := exec.Command("ip", "route", "add", "10.0.0.0/24", "dev", iface).Run()
		if err != nil {
			log.Printf("Linux Route Error: %v", err)
		}
	case "windows":
		exec.Command("netsh", "interface", "ip", "set", "address", "name="+iface, "static", pureIP, "255.255.255.0").Run()
		exec.Command("route", "add", "10.0.0.0", "mask", "255.255.255.0", pureIP).Run()
		exec.Command("netsh", "interface", "ipv4", "set", "subinterface", iface, "mtu=1280", "store=persistent").Run()
	}
}

func decodeBase64ToHex(s64 string) string {
	decoded, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(s64))
	return hex.EncodeToString(decoded)
}

func getOrCreateKeys(filename string) (Keys, error) {
	var keys Keys
	if data, err := os.ReadFile(filename); err == nil {
		json.Unmarshal(data, &keys)
		return keys, nil
	}
	var priv [32]byte
	rand.Read(priv[:])
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64
	var pub [32]byte
	curve25519.ScalarBaseMult(&pub, &priv)
	keys.Private = base64.StdEncoding.EncodeToString(priv[:])
	keys.Public = base64.StdEncoding.EncodeToString(pub[:])
	data, _ := json.MarshalIndent(keys, "", "  ")
	os.WriteFile(filename, data, 0600)
	return keys, nil
}
func monitorHandshake(dev *device.Device, timeout time.Duration) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		state, _ := dev.IpcGet()

		if !strings.Contains(state, "last_handshake_time_sec") {
			continue
		}

		lines := strings.Split(state, "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "last_handshake_time_sec=") {
				secStr := strings.TrimPrefix(line, "last_handshake_time_sec=")
				sec, _ := strconv.ParseInt(secStr, 10, 64)
				if sec == 0 {
					continue
				}

				last := time.Unix(sec, 0)
				log.Println("Server ile son el sıkışma: ", time.Since(last))
				if time.Since(last) > timeout {
					log.Println("Server ile bağlantı koptu, VPN kapatılıyor")
					dev.Down()
					os.Exit(0)
				}
			}
		}
	}
}

func main() {
	_ = godotenv.Load()
	keys, _ := getOrCreateKeys("wg.key")

	regURL := os.Getenv("REGISTRATION_URL")
	serverEndpoint := os.Getenv("SERVER_ENDPOINT")

	ifaceName := "wg0"
	if runtime.GOOS == "darwin" {
		ifaceName = "utun7"
	}

	// 1. Register
	regData, _ := json.Marshal(map[string]string{"pub": keys.Public})
	resp, err := http.Post(regURL, "application/json", bytes.NewBuffer(regData))
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var regResponse RegisterResponse
	json.Unmarshal(body, &regResponse)
	host := regResponse.Peers[0]

	// 2. TUN & Device
	tunDevice, err := tun.CreateTUN(ifaceName, 1280)
	if err != nil {
		log.Fatal(err)
	}

	dev := device.NewDevice(tunDevice, conn.NewDefaultBind(), device.NewLogger(device.LogLevelVerbose, "WG: "))

	// AllowedIPs=0.0.0.0/0 her şeyi tünele basar, 10.0.0.0/24 sadece VPN'i.
	// Diğer istemcilere ping atmak için 10.0.0.0/24 olmalı.
	cfg := fmt.Sprintf("private_key=%s\npublic_key=%s\nendpoint=%s\nallowed_ip=10.0.0.0/24\npersistent_keepalive_interval=10\n",
		decodeBase64ToHex(keys.Private), decodeBase64ToHex(host.PublicKey), serverEndpoint)

	dev.IpcSet(cfg)
	dev.Up()

	// 3. Configure OS Routing
	configureNetwork(ifaceName, regResponse.IP)

	log.Printf("Bağlantı Hazır: %s", regResponse.IP)
	go monitorHandshake(dev, 6*time.Minute)
	select {}
}
