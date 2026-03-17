package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
)

var db *sql.DB

func initDB() {
	host     := getenv("DB_HOST", "")
	user     := getenv("DB_USER", "")
	password := getenv("DB_PASSWORD", "")
	dbname   := getenv("DB_NAME", "kli_db")
	port     := getenv("DB_PORT", "5432")
	// DB_SSLMODE:
	//   "disable"      — local dev only
	//   "require"      — Kubernetes (internal traffic)
	//   "verify-full"  — production with CA cert
	sslmode := getenv("DB_SSLMODE", "disable")

	connStr := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, dbname, sslmode,
	)

	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("DB open error:", err)
	}

	// Connection pool limits — prevents exhausting PostgreSQL under load
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err = db.Ping(); err != nil {
		log.Fatal("DB ping failed:", err)
	}
	log.Printf("Connected to PostgreSQL (sslmode=%s)", sslmode)
}
