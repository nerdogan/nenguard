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

// İşletim sistemine göre ağ yapılandırması yapar
func configureNetwork(iface, localIP string) {
	// localIP genellikle "10.0.0.5/24" formatında gelir.
	// Bazı OS komutları maskesiz IP bekler.
	pureIP := strings.Split(localIP, "/")[0]

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// macOS: IP ata ve route ekle
		exec.Command("ifconfig", iface, pureIP, pureIP, "netmask", "255.255.255.0", "up").Run()
		cmd = exec.Command("route", "add", "-net", "10.0.0.0/24", "-interface", iface)
	case "linux":
		// Linux: IP ata ve linki ayağa kaldır
		exec.Command("ip", "addr", "add", localIP, "dev", iface).Run()
		exec.Command("ip", "link", "set", iface, "up").Run()
		cmd = exec.Command("ip", "route", "add", "10.0.0.0/24", "dev", iface)
	case "windows":
		// Windows: netsh ile IP yapılandır (Yönetici yetkisi gerekir)
		// Not: Windows'ta interface ismi tam eşleşmelidir.
		exec.Command("netsh", "interface", "ip", "set", "address", "name="+iface, "static", pureIP, "255.255.255.0").Run()
		cmd = exec.Command("route", "add", "10.0.0.0", "mask", "255.255.255.0", pureIP)
	}

	if cmd != nil {
		if err := cmd.Run(); err != nil {
			log.Printf("Yönlendirme hatası (Routing Error): %v", err)
		} else {
			log.Printf("Ağ yapılandırıldı: %s (%s)", iface, localIP)
		}
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

func main() {
	_ = godotenv.Load()
	keys, _ := getOrCreateKeys("wg.key")

	regURL := os.Getenv("REGISTRATION_URL")
	serverEndpoint := os.Getenv("SERVER_ENDPOINT")

	ifaceName := "wg0"
	if runtime.GOOS == "darwin" {
		ifaceName = "utun7"
	}

	// 1. Sunucuya Kayıt
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

	// 2. TUN Cihazı Oluştur
	tunDevice, err := tun.CreateTUN(ifaceName, 1280)
	if err != nil {
		log.Fatal("TUN oluşturulamadı:", err)
	}

	dev := device.NewDevice(tunDevice, conn.NewDefaultBind(), device.NewLogger(device.LogLevelVerbose, "WG: "))

	// 3. Konfigürasyon ve Handshake
	cfg := fmt.Sprintf("private_key=%s\npublic_key=%s\nendpoint=%s\nallowed_ip=0.0.0.0/0\npersistent_keepalive_interval=5\n",
		decodeBase64ToHex(keys.Private), decodeBase64ToHex(host.PublicKey), serverEndpoint)

	if err := dev.IpcSet(cfg); err != nil {
		log.Fatal(err)
	}
	dev.Up()

	// 4. İŞLETİM SİSTEMİNE GÖRE ROUTE EKLEME
	configureNetwork(ifaceName, regResponse.IP)

	log.Printf("Bağlantı aktif! IP: %s | Cihaz: %s", regResponse.IP, ifaceName)
	select {}
}
