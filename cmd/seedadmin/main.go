package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/term"

	"github.com/Adeyinka7789/ordora/internal/auth"
	"github.com/Adeyinka7789/ordora/internal/config"
	"github.com/Adeyinka7789/ordora/internal/domain/platformadmin"
	"github.com/Adeyinka7789/ordora/internal/infra/id"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
)

func main() {
	_ = godotenv.Load()

	email := flag.String("email", "", "admin email")
	name := flag.String("name", "", "admin display name (defaults to email prefix)")
	passwordFlag := flag.String("password", "", "admin password (if empty, will prompt)")
	reset := flag.Bool("reset-password", false, "reset the password for an existing admin")
	flag.Parse()

	if *email == "" {
		fmt.Println("Usage: go run ./cmd/seedadmin --email you@yourdomain.com [--name \"Your Name\"]")
		os.Exit(1)
	}
	emailTrimmed := strings.TrimSpace(strings.ToLower(*email))
	if !strings.Contains(emailTrimmed, "@") {
		log.Fatalf("invalid email: %q", *email)
	}
	displayName := *name
	if displayName == "" {
		displayName = strings.SplitN(emailTrimmed, "@", 2)[0]
	}

	// Prompt for password (hidden).
	// Password: use flag if provided, otherwise prompt.
	var password string
	if *passwordFlag != "" {
		password = *passwordFlag
	} else {
		fmt.Print("Password: ")
		pwBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			log.Fatalf("read password: %v", err)
		}
		// Do NOT trim — the password is exactly what was typed.
		password = string(pwBytes)

		if len(password) < 10 {
			log.Fatal("password must be at least 10 characters")
		}

		fmt.Print("Confirm password: ")
		pw2Bytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			log.Fatalf("read password: %v", err)
		}
		if password != string(pw2Bytes) {
			log.Fatal("passwords do not match")
		}
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := postgres.Open(ctx, cfg.DB.DSN())
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer db.Close()

	svc := auth.NewAdminAuthService(auth.AdminAuthDeps{
		Admins:   postgres.NewPlatformAdminRepo(db),
		Sessions: postgres.NewAdminSessionRepo(db),
		IDs:      id.Generator{},
	})

	var admin *platformadmin.Admin
	if *reset {
		err = svc.ResetAdminPassword(ctx, emailTrimmed, password)
	} else {
		admin, err = svc.CreateAdmin(ctx, auth.CreateAdminInput{
			Email: emailTrimmed, Password: password, Name: displayName,
		})
	}
	if err != nil {
		log.Fatalf("create admin: %v", err)
	}
	if *reset {
		fmt.Printf("\n✓ Admin password reset for: %s\n", emailTrimmed)
		return
	}

	fmt.Printf("\n✓ Admin created:\n")
	fmt.Printf("  ID:    %s\n", admin.ID)
	fmt.Printf("  Email: %s\n", admin.Email)
	fmt.Printf("  Name:  %s\n", admin.Name)
	fmt.Printf("\nLog in at: %s%s/login\n", cfg.BaseURL, cfg.Admin.Path)

	// Keep the input bufio import used.
	_ = bufio.NewReader(os.Stdin)
}
