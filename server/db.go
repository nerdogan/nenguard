package main

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct{ *sql.DB }

func InitDB() *DB {
	db, _ := sql.Open("sqlite3", "peers.db")
	db.Exec(`
	CREATE TABLE IF NOT EXISTS peers(
		pub TEXT PRIMARY KEY,
		ip TEXT UNIQUE,
		last_seen DATETIME
	)`)
	return &DB{db}
}

func (db *DB) AssignPeer(pub, ip string) {
	db.Exec("INSERT OR REPLACE INTO peers(pub, ip, last_seen) VALUES(?, ?, CURRENT_TIMESTAMP)", pub, ip)
}
