//go:build spike

// Command spike-shim serves OpenAI's chat completions over this
// server's gateway, so that a library expecting OpenAI can be pointed
// at Zoho Platform AI without knowing it.
//
// Written for the Mem0 spike, which needed a model to pull facts out
// of text at write time -- the half of Mem0 that is not just a vector
// search. It is useful past that: anything speaking OpenAI can be
// measured against this gateway through it.
//
// Not a general shim. It carries the messages, the system prompt and
// the text of the reply, and nothing else.
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/base/thinking/environment/platformai"
)

// Addr : Where it listens. Loopback only: it answers with the
// gateway's credentials and must not leave this machine.
const Addr = "127.0.0.1:8099"

type chatRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	ResponseFormat *struct {
		Type string `json:"type"`
	} `json:"response_format"`
}

func main() {
	cfg, err := config.Load("config.ini", os.LookupEnv)
	must(err)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	env, err := platformai.New(platformai.Config{
		ClientID: cfg.PlatformAI.ClientID, ClientSecret: cfg.PlatformAI.ClientSecret,
		RefreshToken: cfg.PlatformAI.RefreshToken, PortalID: cfg.PlatformAI.PortalID,
		TokenURL: cfg.PlatformAI.TokenURL, ChatURL: cfg.PlatformAI.ChatURL,
		Scope: cfg.PlatformAI.Scope, RedirectURI: cfg.PlatformAI.RedirectURI,
		Vendor: cfg.PlatformAI.Vendor, Model: cfg.PlatformAI.Model,
		Thinking: cfg.PlatformAI.Thinking, Timeout: cfg.PlatformAI.Timeout,
	}, logger)
	must(err)

	var served int
	http.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var in chatRequest
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		req := environment.Request{Purpose: environment.PurposeChat}
		var system []string
		for _, m := range in.Messages {
			switch m.Role {
			case "system":
				system = append(system, m.Content)
			case "assistant":
				req.History = append(req.History, environment.Turn{
					Role: environment.RoleAssistant, Text: m.Content})
			default:
				req.History = append(req.History, environment.Turn{
					Role: environment.RoleUser, Text: m.Content})
			}
		}
		// JSON mode is an instruction here rather than a setting: the
		// gateway has no field for it.
		if in.ResponseFormat != nil && strings.Contains(in.ResponseFormat.Type, "json") {
			system = append(system, "Reply with JSON and nothing else. No prose, no code fences.")
		}
		req.SystemPrompt = strings.Join(system, "\n\n")
		if n := len(req.History); n > 0 && req.History[n-1].Role == environment.RoleUser {
			req.Prompt = req.History[n-1].Text
			req.History = req.History[:n-1]
		}

		stream, err := env.Run(r.Context(), req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		var final environment.Message
		for msg := range stream {
			if msg.Kind.Terminal() {
				final = msg
			}
		}
		if final.Kind == environment.KindError {
			http.Error(w, final.Code+": "+final.Detail, http.StatusBadGateway)
			return
		}

		served++
		fmt.Printf("  %3d  %d messages in, %d characters out\n", served, len(in.Messages), len(final.Text))
		json.NewEncoder(w).Encode(map[string]any{
			"id": "shim", "object": "chat.completion", "model": in.Model,
			"created": time.Now().Unix(),
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]string{"role": "assistant", "content": fence(final.Text)},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
		})
	})

	fmt.Println("openai over the gateway, on", Addr)
	must(http.ListenAndServe(Addr, nil))
}

// fence : The reply with any code fence taken off, because a model
// asked for JSON often wraps it and a caller parsing JSON will not.
func fence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
}

func must(err error) {
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
