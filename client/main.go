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
	"runtime"
	"strings"

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

// WireGuard IPC için Base64 anahtarı Hex formatına çevirir
func decodeBase64ToHex(s64 string) string {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s64))
	if err != nil {
		return ""
	}
	return hex.EncodeToString(decoded)
}

func getOrCreateKeys(filename string) (Keys, error) {
	var keys Keys
	if _, err := os.Stat(filename); err == nil {
		data, err := os.ReadFile(filename)
		if err == nil {
			err = json.Unmarshal(data, &keys)
			if err == nil {
				return keys, nil
			}
		}
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

func main() {
	_ = godotenv.Load()
	keys, _ := getOrCreateKeys("wg.key")

	regURL := os.Getenv("REGISTRATION_URL")
	serverEndpoint := os.Getenv("SERVER_ENDPOINT")

	ifaceName := "wg0"
	if runtime.GOOS == "darwin" {
		ifaceName = "utun7"
	}

	// 1. Kayıt
	regData, _ := json.Marshal(map[string]string{"pub": keys.Public})
	resp, err := http.Post(regURL, "application/json", bytes.NewBuffer(regData))
	if err != nil {
		log.Fatal("Sunucu hatası:", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var regResponse RegisterResponse
	json.Unmarshal(body, &regResponse)

	host := regResponse.Peers[0]

	// 2. Tünel ve Logger
	tunDevice, err := tun.CreateTUN(ifaceName, 1280)
	if err != nil {
		log.Fatal(err)
	}

	logger := device.NewLogger(device.LogLevelVerbose, "WG: ")
	dev := device.NewDevice(tunDevice, conn.NewDefaultBind(), logger)

	// ÖNEMLİ: Anahtarları Hex formatına çeviriyoruz
	privHex := decodeBase64ToHex(keys.Private)
	pubHex := decodeBase64ToHex(host.PublicKey)

	// IPC Config (Hex formatında gönderilmeli)
	cfg := fmt.Sprintf("private_key=%s\npublic_key=%s\nendpoint=%s\nallowed_ip=0.0.0.0/0\npersistent_keepalive_interval=5\n",
		privHex, pubHex, serverEndpoint)

	if err := dev.IpcSet(cfg); err != nil {
		log.Fatalf("IPC hatası: %v", err)
	}

	dev.Up()
	log.Printf("Bağlantı aktif! IP: %s | Interface: %s", regResponse.IP, ifaceName)
	select {}
}
