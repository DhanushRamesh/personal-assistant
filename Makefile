# Personal assistant
#
# Two places this runs, and they are not the same thing:
#
#   local        this machine, `env = dev`, plain HTTP on the loopback,
#                started and stopped by hand, config.ini in the repo
#
#   production   the server, `env = production`, behind Caddy and TLS,
#                run by systemd and restarted on failure,
#                config.ini in /opt/personal-assistant
#
# Local targets have no prefix. Production targets all start with `prod-`
# and act over SSH.

BINARY   := personal-assistant
PKG      := ./cmd/server

BUILD    := build
UNIT     := personal-assistant-local.service
LOGFILE  := personal-assistant.log

# The server. Override on the command line if it moves:
#   make prod-status PROD_HOST=1.2.3.4
PROD_HOST ?= 136.111.221.56
PROD_USER ?= dhanush-12514
# What the service, its unit, its directory and its OS user are called on the
# deployed machine. Renaming these on a running server is a migration, not an
# edit: see deployments/README.md.
PROD_NAME ?= personal-assistant
PROD_DIR  ?= /opt/$(PROD_NAME)
PROD_URL  ?= https://friday-server.duckdns.org
SSH       := ssh -o ConnectTimeout=20 $(PROD_USER)@$(PROD_HOST)

.DEFAULT_GOAL := help

# ############################# Help ######################################## #

.PHONY: help
help: ## List the targets
	@echo "local:"
	@grep -hE '^[a-z][a-z-]*:.*##' $(MAKEFILE_LIST) | grep -v '^prod-' | sort | awk -F':.*##' '{printf "  %-16s %s\n", $$1, $$2}'
	@echo
	@echo "production ($(PROD_HOST)):"
	@grep -hE '^prod-[a-z-]*:.*##' $(MAKEFILE_LIST) | sort | awk -F':.*##' '{printf "  %-16s %s\n", $$1, $$2}'

# ############################# Local ####################################### #

.PHONY: build
build: ## Build for this machine
	@go build -o $(BINARY) $(PKG)
	@echo "built ./$(BINARY)"

.PHONY: run
run: ## Run in the foreground, logs to the terminal (ctrl-c to stop)
	@go run $(PKG)

.PHONY: start
start: build ## Start it (systemd keeps it up)
	@systemctl --user start $(UNIT)
	@sleep 2
	@$(MAKE) --no-print-directory status

.PHONY: stop
stop: ## Stop it, and stop systemd putting it back
	@systemctl --user stop $(UNIT)
	@echo "stopped"
	@# Anything of ours left holding the port, from a crash or a run
	@# started by hand before this unit existed.
	@#
	@# Matched on the command line, not the process name. Linux
	@# truncates a name to fifteen characters, so -x personal-assistant
	@# never matched personal-assist and a server started by hand sat
	@# through every restart -- the symptom being "address already in
	@# use" after stop had just said it was done.
	@pkill -f '^\./$(BINARY)$$' 2>/dev/null && echo "  (also cleared a stray $(BINARY))" || true

.PHONY: restart
restart: build ## Rebuild and restart it
	@systemctl --user restart $(UNIT)
	@sleep 2
	@$(MAKE) --no-print-directory status

.PHONY: enable
enable: ## Start it at login as well as now
	@systemctl --user enable --now $(UNIT) && echo "enabled"

.PHONY: disable
disable: ## Stop starting it at login
	@systemctl --user disable $(UNIT) && echo "disabled"

.PHONY: status
status: ## Is it running, and is it answering
	@systemctl --user is-active $(UNIT) >/dev/null 2>&1 \
		&& echo "running (pid $$(systemctl --user show -p MainPID --value $(UNIT)))" \
		|| echo "not running ($$(systemctl --user is-active $(UNIT) 2>&1))"
	@# Restarts, not uptime. "Up 8 seconds" reads as healthy and can
	@# mean it has died four hundred times.
	@n=$$(systemctl --user show -p NRestarts --value $(UNIT) 2>/dev/null); \
		[ "$${n:-0}" != "0" ] && echo "  restarted $$n times since it was last started by hand" || true
	@ss -lntp 2>/dev/null | grep ':8080' | awk '{print "  listening " $$4}' || echo "  nothing on :8080"
	@curl -s -m 3 localhost:8080/health 2>/dev/null | sed 's/^/  health: /' || echo "  health: no answer"
	@curl -s -m 3 localhost:8080/ready  2>/dev/null | sed 's/^/  ready:  /' || true

.PHONY: logs
logs: ## Follow the local log
	@tail -f $(LOGFILE)

.PHONY: test
test: ## Run the tests
	@go test ./... -race

.PHONY: evals-greeting
evals-greeting: ## Greet real arrivals sampled from the last four weeks (costs API calls)
	@go test ./internal/domain/greet/evals/ -tags evals -count=1 -v -timeout 20m $(ARGS)

.PHONY: evals-models
evals-models: ## Ask which models this gateway will accept (costs API calls)
	@go test ./internal/app/noticing/evals/ -tags evals -count=1 -v -timeout 10m \
		-run TestWhichModelsThisGatewayTakes $(ARGS)

.PHONY: evals-shape
evals-shape: ## Ask a real model the same question for the ear and the eye. ASK="..." for one of your own (costs API calls)
	@go test ./internal/domain/persona/evals/ -tags evals -count=1 -v -timeout 10m \
		-run TestTheSameQuestionIsShapedForTheEarAndTheEye $(ARGS)

.PHONY: evals-naming
evals-naming: ## Search his real reminders, rules and memories by name (no API calls)
	@go test ./internal/app/elsewhere/evals/ -tags evals -count=1 -v -timeout 3m $(ARGS)

.PHONY: evals-remarking
evals-remarking: ## What a rule says when it speaks, against its own words (costs API calls)
	@go test ./internal/app/noticing/evals/ -tags evals -count=1 -v -timeout 10m \
		-run TestAMorningRemarkGreetsAndSaysHowLong $(ARGS)

.PHONY: evals-refusing
evals-refusing: ## Ask how long a refusal runs (costs API calls)
	@go test ./internal/domain/persona/evals/ -tags evals -count=1 -v -timeout 10m \
		-run TestARefusalIsOneSentence $(ARGS)

.PHONY: evals-machinery
evals-machinery: ## Check the word "tool" never reaches the person (costs API calls)
	@go test ./internal/domain/persona/evals/ -tags evals -count=1 -v -timeout 10m \
		-run TestItNeverNamesItsOwnMachinery $(ARGS)

.PHONY: evals-clock
evals-clock: ## Ask the time against a history full of stale times (costs API calls)
	@go test ./internal/base/records/conversation/evals/ -tags evals -count=1 -v -timeout 10m \
		-run TestTheClockBeatsTheConversation $(ARGS)

.PHONY: evals-rhythm
evals-rhythm: ## Print the counted half of the description from the real database (no API calls)
	@go test ./internal/app/profile/evals/ -tags evals -count=1 -v -timeout 2m \
		-run TestWhatTheCountedHalfSays $(ARGS)

.PHONY: evals-wondering
evals-wondering: ## Ask the real judge what it would write, keeping nothing (costs API calls)
	@go test ./internal/app/noticing/evals/ -tags evals -count=1 -v -timeout 10m \
		-run TestWhatHeWouldWriteIfHeCould $(ARGS)

.PHONY: evals-tangled
evals-tangled: ## Ask whether a badly typed sentence still reaches the right tool (costs API calls)
	@# Twice per case, as typed and spelt correctly, so the spelling
	@# is the only thing the comparison measures.
	@go test ./internal/app/tool/evals/ -tags evals -count=1 -v -timeout 20m \
		-run TestASentenceSurvivesItsSpelling $(ARGS)

.PHONY: evals
evals: ## Ask a real model whether the tool descriptions work (costs API calls)
	@# Longer than go test's own ten minutes. Eighty-two cases paced
	@# at a second and a half, plus whatever the gateway is taking
	@# today, was always close to it and crossed it on 5 October: the
	@# suite panicked at 600s having reported nothing at all, which
	@# reads like a verdict and is not one.
	@go test ./internal/app/tool/evals/... ./internal/base/thinking/embed/evals/... ./internal/domain/memory/evals/... ./internal/domain/persona/evals/... ./internal/base/records/conversation/evals/... ./internal/domain/remind/evals/... -tags evals -count=1 -v -timeout 45m

.PHONY: check
check: ## Everything that must pass before committing
	@gofmt -l .
	@go vet ./...
	@# The evals too, compiled and not run. They are behind a build
	@# tag, so `go vet ./...` never looks at them -- which is how
	@# the rule suite came to be unbuildable from 8 October and
	@# nobody knew: `make evals` went on reporting cleanly on
	@# everything else and printed "[build failed]" for the one
	@# suite that measures tool descriptions, in the middle of
	@# forty lines of PASS.
	@#
	@# Compiling them here costs a second and means a signature
	@# change that strands an eval is caught by the thing that is
	@# run before every commit, rather than by whoever next spends
	@# real money running them.
	@go vet -tags evals ./... 2>&1 | grep -v "^#" || true
	@go test ./... -race

.PHONY: clean
createuser: ## Create a user locally (prompts for a password)
	@go run $(PKG) createuser $(USER_NAME)

.PHONY: createuser

.PHONY: logs-clear
logs-clear: ## Empty the local log
	@: > $(LOGFILE)
	@echo "cleared $(LOGFILE)"

clean: stop ## Stop and remove build output
	@rm -rf $(BUILD) $(BINARY) $(LOGFILE)
	@echo "cleaned"

# ############################# Production ################################## #

.PHONY: deploy-build
deploy-build: ## Build the binary the server runs (linux/amd64)
	@mkdir -p $(BUILD)
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o $(BUILD)/$(BINARY) $(PKG)
	@echo "$(BUILD)/$(BINARY): $$(du -h $(BUILD)/$(BINARY) | cut -f1)"

.PHONY: prod-deploy
prod-deploy: check deploy-build ## Test, build, upload and restart the server
	@scp -q $(BUILD)/$(BINARY) $(PROD_USER)@$(PROD_HOST):/tmp/$(PROD_NAME).new
	@$(SSH) 'sudo systemctl stop $(PROD_NAME) \
		&& sudo mv /tmp/$(PROD_NAME).new $(PROD_DIR)/$(PROD_NAME) \
		&& sudo chown assistant:assistant $(PROD_DIR)/$(PROD_NAME) \
		&& sudo chmod 750 $(PROD_DIR)/$(PROD_NAME) \
		&& sudo systemctl start $(PROD_NAME)'
	@sleep 4
	@$(MAKE) --no-print-directory prod-status

.PHONY: prod-start
prod-start: ## Start it on the server
	@$(SSH) 'sudo systemctl start $(PROD_NAME)' && echo "started"
	@sleep 3
	@$(MAKE) --no-print-directory prod-status

.PHONY: prod-stop
prod-stop: ## Stop it on the server (drains first)
	@$(SSH) 'sudo systemctl stop $(PROD_NAME)' && echo "stopped"

.PHONY: prod-restart
prod-restart: ## Restart it on the server
	@$(SSH) 'sudo systemctl restart $(PROD_NAME)' && echo "restarted"
	@sleep 3
	@$(MAKE) --no-print-directory prod-status

.PHONY: prod-status
prod-status: ## What the server is doing
	@$(SSH) 'printf "  $(PROD_NAME): %s   mysql: %s   caddy: %s\n" \
		"$$(systemctl is-active $(PROD_NAME))" "$$(systemctl is-active mysql)" "$$(systemctl is-active caddy)"; \
		free -m | awk "/Mem:/ {printf \"  memory: %s MB used, %s MB free\\n\", \$$3, \$$7}"' 2>/dev/null || echo "  unreachable"
	@printf "  %s/health -> " "$(PROD_URL)"
	@curl -s -m 10 -o /dev/null -w "%{http_code}\n" $(PROD_URL)/health 2>/dev/null || echo "no answer"
	@curl -s -m 10 $(PROD_URL)/ready 2>/dev/null | sed 's/^/  ready: /' || true

.PHONY: prod-logs
prod-logs: ## Follow the server's log
	@$(SSH) -t 'sudo journalctl -u $(PROD_NAME) -f -n 40'

.PHONY: prod-ssh
prod-ssh: ## Open a shell on the server
	@$(SSH) -t

.PHONY: prod-createuser
prod-createuser: ## Create a user on the server (prompts for a password)
	@$(SSH) -t 'sudo -u assistant $(PROD_DIR)/$(PROD_NAME) createuser $(USER_NAME)'

.PHONY: prod-backup
prod-backup: ## Run a database backup on the server now
	@$(SSH) 'sudo systemctl start $(PROD_NAME)-backup.service && sleep 5 && sudo ls -lh /var/backups/$(PROD_NAME)/ | tail -3'
