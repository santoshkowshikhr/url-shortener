package model

import (
	"database/sql"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func InsertURLMapping(longUrl, shortCode, customAlias string, expireAt *time.Time) error {
	_, err := db.Exec("INSERT INTO shortener (long_url, short_code, custom_alias, expire_at) VALUES (?, ?, ?, ?)", longUrl, shortCode, customAlias, expireAt)
	return err
}

func GetLongURLWithExpiry(shortCode string) (string, *time.Time, error) {
	row := db.QueryRow("SELECT long_url, expire_at FROM shortener WHERE short_code = ?", shortCode)

	var longURL string
	var expireAt sql.NullTime
	err := row.Scan(&longURL, &expireAt)
	if err != nil {
		return "", nil, err
	}

	if expireAt.Valid {
		return longURL, &expireAt.Time, nil
	}
	return longURL, nil, nil
}

func InitDB(path string) {
	var err error
	db, err = sql.Open("sqlite3", path)
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}

	createTable := `CREATE TABLE IF NOT EXISTS shortener (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		long_url TEXT NOT NULL,
		short_code TEXT NOT NULL UNIQUE,
		custom_alias TEXT UNIQUE,
		expire_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	if _, err := db.Exec(createTable); err != nil {
		log.Fatalf("create table failed: %v", err)
	}
}
