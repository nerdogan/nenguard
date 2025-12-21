package main

import (
	"database/sql"
	"fmt"
)

func NextIP(db *sql.DB) string {
	for i := 2; i < 254; i++ {
		ip := fmt.Sprintf("10.10.0.%d", i)
		row := db.QueryRow("SELECT 1 FROM peers WHERE ip=?", ip)
		var x int
		if row.Scan(&x) != nil {
			continue
		}
		return ip
	}
	panic("IP pool exhausted")
}
