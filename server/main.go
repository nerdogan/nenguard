package main

import (
	"encoding/json"
	"log"
	"net/http"
)

func main() {
	db := InitDB()
	go StartDERP()

	http.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		pub := r.URL.Query().Get("pub")
		row := db.QueryRow("SELECT ip FROM peers WHERE pub=?", pub)

		var ip string
		if row.Scan(&ip) != nil {
			ip = NextIP(db.DB)
			db.AssignPeer(pub, ip)
		}

		json.NewEncoder(w).Encode(map[string]string{"ip": ip + "/32"})
	})

	log.Println("Server running on :8080")
	http.ListenAndServe(":8080", nil)
}
