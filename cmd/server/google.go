package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	chatmysql "github.com/DhanushRamesh/personal-assistant/internal/chat/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/config"
	"github.com/DhanushRamesh/personal-assistant/internal/google"
	"github.com/DhanushRamesh/personal-assistant/internal/logging"
	"github.com/DhanushRamesh/personal-assistant/internal/storage"
)

// runGoogleCheck : Says whether the Google connection still works, and
// proves it rather than reporting what was last written down.
//
// A refresh token dies for reasons invisible from here and nothing
// notices until something needs it. Everything else in this server has
// had that shape of fault at least once, so there is a command to ask
// the question directly: it exchanges the stored token for a live one
// and says what happened.
//
// No token is printed, only its length and whether it worked.
func runGoogleCheck(username string) error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	logger, err := logging.New(os.Stderr, logging.Config{
		Level: "warn", Format: cfg.Log.Format, Service: serviceName,
	})
	if err != nil {
		return err
	}

	ctx := context.Background()
	db, err := storage.Open(ctx, cfg.Database, logger.Logger, storage.Options{})
	if err != nil {
		return err
	}
	defer db.Close()

	user, err := chat.Repository(chatmysql.NewRepository(db)).UserByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("finding %s: %w", username, err)
	}

	link := linkToGoogle(cfg, logger.Logger, db)
	if link == nil {
		return errors.New("no Google client is configured")
	}

	account, err := link.Account(ctx, user.ID)
	if errors.Is(err, google.ErrNotConnected) {
		fmt.Println("no Google account is connected")
		return nil
	}
	if err != nil {
		return err
	}

	fmt.Printf("account      %s\n", account.Email)
	fmt.Printf("connected    %s\n", account.ConnectedAt.Local().Format(time.RFC1123))
	fmt.Printf("scopes       %s\n", strings.Join(account.Scopes, "\n             "))
	if account.RefreshedAt != nil {
		fmt.Printf("last worked  %s\n", account.RefreshedAt.Local().Format(time.RFC1123))
	}
	if account.BrokenAt != nil {
		fmt.Printf("broken       %s\n             %s\n",
			account.BrokenAt.Local().Format(time.RFC1123), account.Broken)
	}

	// The part worth having: ask Google now rather than trust the row.
	source, err := link.TokenSource(ctx, user.ID)
	if err != nil {
		return err
	}
	token, err := source.Token()
	if err != nil {
		fmt.Printf("\nthe connection does NOT work: %v\n", err)
		return err
	}
	fmt.Printf("\nthe connection works: got an access token of %d characters, good until %s\n",
		len(token.AccessToken), token.Expiry.Local().Format(time.RFC1123))
	return nil
}
