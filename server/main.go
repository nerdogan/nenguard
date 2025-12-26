package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
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
	_ = godotenv.Load()
	db := InitDB()

	serverKey := strings.TrimSpace(os.Getenv("SERVER_PUB_KEY"))
	iface := os.Getenv("WG_INTERFACE")
	serverWGIP := os.Getenv("SERVER_WG_IP")
	port := os.Getenv("LISTEN_PORT")

	http.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]string
		json.Unmarshal(body, &req)
		clientPub := strings.TrimSpace(req["pub"])

		var ip string
		err := db.QueryRow("SELECT ip FROM peers WHERE pub=?", clientPub).Scan(&ip)
		if err != nil {
			ip = NextIP(db)
			db.Exec("INSERT INTO peers (pub, ip) VALUES (?, ?)", clientPub, ip)
			wgSetPeer(iface, clientPub, ip+"/32")
		}

		peers := []Peer{{PublicKey: serverKey, IP: serverWGIP}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(RegisterResponse{IP: ip + "/24", Peers: peers})
	})

	log.Printf("Sunucu %s portunda aktif.", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func InitDB() *sql.DB {
	db, _ := sql.Open("sqlite3", "nenguard.db")
	db.Exec(`CREATE TABLE IF NOT EXISTS peers (pub TEXT PRIMARY KEY, ip TEXT UNIQUE)`)
	return db
}

func NextIP(db *sql.DB) string {
	var lastIP string
	db.QueryRow("SELECT ip FROM peers ORDER BY ip DESC LIMIT 1").Scan(&lastIP)
	if lastIP == "" {
		return "10.0.0.2"
	}
	parts := strings.Split(lastIP, ".")
	var n int
	fmt.Sscanf(parts[3], "%d", &n)
	return fmt.Sprintf("%s.%s.%s.%d", parts[0], parts[1], parts[2], n+1)
}

func wgSetPeer(iface, pubKey, ip string) {
	exec.Command("wg", "set", iface, "peer", pubKey, "allowed-ips", ip, "persistent-keepalive", "5").Run()
}
