package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"

	_ "github.com/mattn/go-sqlite3"
)

type Peer struct {
	PublicKey string `json:"pub"`
	IP        string `json:"ip"`
}

type DB struct {
	DB *sql.DB
}

// Initialize SQLite DB
func InitDB() *DB {
	db, err := sql.Open("sqlite3", "nenguard.db")
	if err != nil {
		log.Fatal(err)
	}

	db.Exec(`CREATE TABLE IF NOT EXISTS peers (
        pub TEXT PRIMARY KEY,
        ip TEXT UNIQUE
    )`)

	return &DB{DB: db}
}

// Assign a new IP to peer
func (db *DB) AssignPeer(pub string) (string, error) {
	ip := db.NextIP()
	if ip == "" {
		return "", fmt.Errorf("IP pool exhausted")
	}

	_, err := db.DB.Exec("INSERT INTO peers(pub, ip) VALUES(?, ?)", pub, ip)
	return ip, err
}

// Get next available IP in 10.0.0.0/24
func (db *DB) NextIP() string {
	used := map[int]bool{}
	rows, _ := db.DB.Query("SELECT ip FROM peers")
	defer rows.Close()
	for rows.Next() {
		var ip string
		rows.Scan(&ip)
		if parsed := net.ParseIP(ip).To4(); parsed != nil {
			used[int(parsed[3])] = true
		}
	}

	for i := 2; i <= 254; i++ {
		if !used[i] {
			return fmt.Sprintf("10.0.0.%d", i)
		}
	}
	return ""
}

// Get all peers except the one provided
func (db *DB) GetPeers(exclude string) ([]Peer, error) {
	rows, err := db.DB.Query("SELECT pub, ip FROM peers WHERE pub != ?", exclude)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var peers []Peer
	for rows.Next() {
		var p Peer
		rows.Scan(&p.PublicKey, &p.IP)
		peers = append(peers, p)
	}
	return peers, nil
}

// Add peer to WireGuard interface
func AddPeerToWG(pubKey, ip string) error {
	// Not: ip+"/24" tüm VPN ağını bu peer üzerinden yönlendirmeye çalışabilir.
	// Eğer istemciler arası ping sorunu olursa burayı ip+"/32" yapmayı dene.
	cmd := exec.Command("wg", "set", "wg0",
		"peer", pubKey,
		"allowed-ips", ip+"/32")
	return cmd.Run()
}

// YENİ: Sunucu açılışında peerları WG arayüzüne geri yükler
func (db *DB) RestorePeers() {
	rows, err := db.DB.Query("SELECT pub, ip FROM peers")
	if err != nil {
		log.Println("RestorePeers hatası:", err)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var pub, ip string
		if err := rows.Scan(&pub, &ip); err == nil {
			if err := AddPeerToWG(pub, ip); err == nil {
				count++
			}
		}
	}
	log.Printf("Veritabanından %d peer wg0 arayüzüne başarıyla geri yüklendi.", count)
}

func main() {
	db := InitDB()

	// Sunucu başlarken mevcut kayıtları wg0'a basıyoruz
	db.RestorePeers()

	http.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Pub string `json:"pub"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		if req.Pub == "" {
			http.Error(w, "Missing pub key", http.StatusBadRequest)
			return
		}

		var ip string
		err := db.DB.QueryRow("SELECT ip FROM peers WHERE pub=?", req.Pub).Scan(&ip)
		if err != nil { // peer not found → assign IP
			ip, err = db.AssignPeer(req.Pub)
			if err != nil {
				http.Error(w, "IP pool exhausted", http.StatusInternalServerError)
				return
			}

			// Add new peer to WireGuard interface
			if err := AddPeerToWG(req.Pub, ip); err != nil {
				log.Println("Failed to add peer to wg0:", err)
			} else {
				log.Println("Added new peer to wg0:", req.Pub, ip)
			}
		} else {
			// Mevcut peer yeniden kayıt olmaya çalışıyorsa (veya sunucu restart sonrası tüneli düşmüşse)
			// Arayüzde olup olmadığını garantiye alalım
			AddPeerToWG(req.Pub, ip)
		}

		peers, _ := db.GetPeers(req.Pub)
		resp := map[string]interface{}{
			"ip":    ip + "/24",
			"peers": peers,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	log.Println("Server running on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
