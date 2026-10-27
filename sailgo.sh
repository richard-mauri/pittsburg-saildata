#!/bin/bash

# pmset disablesleep 1
# pgrep -fl 'sailing-go|cloudflared'
# curl -fsS https://testweather.themauris.org/health

# Start sailing-go and its Cloudflare Tunnel
# without disturbing Mauri's Stock Quotes.

HOME=/Users/richardmauri
cd "$HOME/projects/sailing-go" || exit 1

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

printf 'Started SailGo and its Cloudflare Tunnel.\\n'
"$HOME/projects/sailing-go/sailing-go" --version

