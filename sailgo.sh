#!/bin/bash

# pmset disablesleep 1
# pgrep -fl 'sailing-go|cloudflared'
# curl -fsS https://testweather.themauris.org/health

# Start sailing-go and its Cloudflare Tunnel
# without disturbing Mauri's Stock Quotes.

HOME=/Users/richardmauri
cd "$HOME/projects/sailing-go" || exit 1

# Report the checked-out source version before launching the existing binary.
# The binary may be older if it has not been rebuilt after a source update.
SOURCE_FILE="$HOME/projects/sailing-go/main.go"
if [[ -r "$SOURCE_FILE" ]]; then
    APP_VERSION=$(awk -F '"' '/^[[:space:]]*appVersion[[:space:]]*=/ {print $2; exit}' "$SOURCE_FILE")
    BUILD_VERSION=$(awk -F '"' '/^[[:space:]]*buildVersion[[:space:]]*=/ {print $2; exit}' "$SOURCE_FILE")
    if [[ -n "$APP_VERSION" && -n "$BUILD_VERSION" ]]; then
        printf 'SailGo source: Version %s · Build %s (compiled binary may differ)\n' "$APP_VERSION" "$BUILD_VERSION"
    else
        printf 'SailGo source version unavailable in main.go\n' >&2
    fi
else
    printf 'SailGo source version unavailable: main.go not readable\n' >&2
fi

# Stop only the sailing Go application.
pkill -x sailing-go 2>/dev/null || true

# Stop only the sailing Cloudflare Tunnel.
SAILING_TOKEN="$HOME/projects/sailing-go/.cloudflared/tunnel_token"

ps -axo pid=,command= | while read -r pid command; do
    if [[ "$command" == *"cloudflared tunnel run"* &&
          "$command" == *"$SAILING_TOKEN"* ]]; then
        kill "$pid"
    fi
done

# Start sailing-go.
nohup "$HOME/projects/sailing-go/sailing-go" -server \
  > "$HOME/projects/sailing-go/sailing-go.log" 2>&1 < /dev/null &

# Start only the sailing tunnel.
nohup "$HOME/Applications/cloudflared" tunnel run \
  --token-file "$SAILING_TOKEN" \
  > "$HOME/projects/sailing-go/.cloudflared/tunnel.log" 2>&1 < /dev/null &

disown -a

