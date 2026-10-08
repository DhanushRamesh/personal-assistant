package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"personal-assistant/internal/base/records/user"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"personal-assistant/internal/base/platform/auth"
	chatmysql "personal-assistant/internal/base/records/chat/mysql"
)

// createUser : Creates a person from the terminal.
//
// There is no endpoint for this. An endpoint that creates the first person must
// either be open, which lets a stranger claim the assistant, or be guarded by
// a shared secret, which is the same problem one level up. A command run by
// whoever already has the machine avoids both, and is needed exactly once.
func createUser(username string, db *storageDB) error {
	username = user.NormaliseUsername(username)

	password, err := readPassword()
	if err != nil {
		return err
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	person, err := user.NewUser(username, hash)
	if err != nil {
		if errors.Is(err, user.ErrInvalidUsername) {
			return fmt.Errorf("a username must be %d to %d characters of letters, digits, dots, dashes or underscores",
				user.MinUsernameLen, user.MaxUsernameLen)
		}
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repo := chatmysql.NewRepository(db)
	if err := repo.CreateUser(ctx, person); err != nil {
		if errors.Is(err, user.ErrUsernameTaken) {
			return fmt.Errorf("the username %q is already taken", username)
		}
		return err
	}

	fmt.Printf("created person %s (%s)\n", person.Username, person.ID)
	fmt.Println("log in from a client with:")
	fmt.Printf("  curl -X POST localhost:8080/v1/auth/login \\\n")
	fmt.Printf("    -H 'Content-Type: application/json' \\\n")
	fmt.Printf("    -d '{\"username\":%q,\"password\":\"...\",\"client_name\":\"my phone\"}'\n", person.Username)
	return nil
}

// readPassword : Reads a password twice from the terminal without echoing it.
func readPassword() (string, error) {
	if !term.IsTerminal(int(syscall.Stdin)) {
		return "", errors.New("a password must be typed at a terminal")
	}

	fmt.Print("password: ")
	first, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}

	fmt.Print("again: ")
	second, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}

	if !strings.EqualFold(string(first), string(second)) || string(first) != string(second) {
		return "", errors.New("the passwords do not match")
	}
	if err := auth.CheckPasswordPolicy(string(first)); err != nil {
		return "", err
	}
	return string(first), nil
}

// osExitOnError : Reports a command failure plainly and stops.
func osExitOnError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}
