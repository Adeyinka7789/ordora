package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env if present. In production, environment variables are set by systemd.
	_ = godotenv.Load()

	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		os.Getenv("ORDORA_DB_USER"),
		os.Getenv("ORDORA_DB_PASSWORD"),
		os.Getenv("ORDORA_DB_HOST"),
		os.Getenv("ORDORA_DB_PORT"),
		os.Getenv("ORDORA_DB_NAME"),
		os.Getenv("ORDORA_DB_SSLMODE"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	var version string
	if err := pool.QueryRow(ctx, "SELECT version()").Scan(&version); err != nil {
		log.Fatalf("failed to query version: %v", err)
	}

	var currentUser string
	if err := pool.QueryRow(ctx, "SELECT current_user").Scan(&currentUser); err != nil {
		log.Fatalf("failed to query current_user: %v", err)
	}

	fmt.Println("Connected successfully.")
	fmt.Println("  Postgres:", version)
	fmt.Println("  As user: ", currentUser)
}