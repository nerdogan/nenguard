package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net"
	"net/http"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	DB *sql.DB
}

func InitDB() *DB {
	sqliteDB, err := sql.Open("sqlite3", "nenguard.db")
	if err != nil {
		log.Fatal(err)
	}

	sqliteDB.Exec(`CREATE TABLE IF NOT EXISTS peers (
		pub TEXT PRIMARY KEY,
		ip TEXT UNIQUE
	)`)

	return &DB{DB: sqliteDB}
}

func StartDERP() {
	// DERP server implementation
	log.Println("DERP server started")
}

func NextIP(db *sql.DB) string {
	// Simple IP allocation logic
	var lastIP string
	row := db.QueryRow("SELECT ip FROM peers ORDER BY ip DESC LIMIT 1")
	if row.Scan(&lastIP) != nil {
		return "10.0.0.1"
	}

	// Parse and increment IP
	ip := net.ParseIP(lastIP)
	if ip == nil {
		return "10.0.0.1"
	}
	ip = ip.To4()
	ip[3]++
	return ip.String()
}

func (db *DB) AssignPeer(pub, ip string) error {
	_, err := db.DB.Exec("INSERT INTO peers (pub, ip) VALUES (?, ?)", pub, ip)
	return err
}

func main() {
	db := InitDB()
	go StartDERP()

	http.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req map[string]string
		json.NewDecoder(r.Body).Decode(&req)
		pub := req["pub"]

		row := db.DB.QueryRow("SELECT ip FROM peers WHERE pub=?", pub)

		var ip string
		if row.Scan(&ip) != nil {
			ip = NextIP(db.DB)
			db.AssignPeer(pub, ip)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"ip": ip + "/32"})
	})

	log.Println("Server running on :8080")
	http.ListenAndServe(":8080", nil)
}
