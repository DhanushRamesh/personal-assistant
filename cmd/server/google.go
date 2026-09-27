package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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

// aliases : Short names Google returns alongside the full scope URLs.
//
// "email" and "profile" come back beside their userinfo equivalents and
// mean the same thing. Counted as extras they made every healthy
// connection report a mismatch, which is a warning that trains somebody
// to ignore warnings.
var aliases = map[string]string{
	"email":   "https://www.googleapis.com/auth/userinfo.email",
	"profile": "https://www.googleapis.com/auth/userinfo.profile",
}

// absent : Which recorded scopes Google does not actually allow.
//
// One direction only. Google allowing more than was written down is
// harmless and usually just an alias; allowing less is the fault worth
// hearing about, because a tool will then fail on a permission the
// record says it has.
func absent(live, recorded []string) []string {
	allowed := map[string]bool{}
	for _, s := range live {
		allowed[s] = true
		if full, ok := aliases[s]; ok {
			allowed[full] = true
		}
	}

	var missing []string
	for _, s := range recorded {
		if !allowed[s] {
			missing = append(missing, s)
		}
	}
	return missing
}

// liveScopes : What Google says an access token is allowed to do.
func liveScopes(ctx context.Context, access string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://oauth2.googleapis.com/tokeninfo?access_token="+url.QueryEscape(access), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var said struct {
		Scope string `json:"scope"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&said); err != nil {
		return nil, err
	}
	return strings.Fields(said.Scope), nil
}

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

	// What Google says the token is actually good for, which is not
	// always what was written down: the scope list in a token response
	// can be shorter than what was granted, and a stored list that
	// disagrees with reality sends somebody to reconnect something
	// that works, or lets a tool call something it cannot.
	live, err := liveScopes(ctx, token.AccessToken)
	if err != nil {
		fmt.Printf("could not ask Google what it allows: %v\n", err)
		return nil
	}
	fmt.Printf("\nGoogle says the token allows:\n  %s\n", strings.Join(live, "\n  "))
	if missing := absent(live, account.Scopes); len(missing) > 0 {
		fmt.Printf("\nrecorded here but not allowed by Google:\n  %s\n",
			strings.Join(missing, "\n  "))
	}
	return nil
}
