package main

import (
	"context"
	"fmt"
	"os"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	chatmysql "github.com/DhanushRamesh/personal-assistant/internal/chat/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/config"
	"github.com/DhanushRamesh/personal-assistant/internal/logging"
	memorymysql "github.com/DhanushRamesh/personal-assistant/internal/memory/mysql"
	remindmysql "github.com/DhanushRamesh/personal-assistant/internal/remind/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/storage"
	"github.com/DhanushRamesh/personal-assistant/internal/vocabulary"
)

// runVocabulary : Prints the word list speech-to-text should be primed
// with, and nothing else.
//
// A command rather than an endpoint. The caller is the script that starts
// the containers, on this machine, before the server is necessarily up --
// so an HTTP call would need the server running to configure the thing the
// server talks through, and a token besides.
//
// Only the list goes to standard output, so the caller can put it straight
// into the argument without parsing anything.
func runVocabulary(username string) error {
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

	chats := chatmysql.NewRepository(db)
	user, err := chat.Repository(chats).UserByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("finding %s: %w", username, err)
	}

	said, err := vocabulary.Sources{
		Memories:      memorymysql.New(db),
		Conversations: chats,
		Reminders:     remindmysql.New(db),
	}.Gather(ctx, user.ID)
	if err != nil {
		return err
	}

	fmt.Println(vocabulary.Prompt(vocabulary.Core, vocabulary.Found(said)))
	return nil
}
