#!/bin/bash
set -e

shopt -s nullglob

# Install newest generator candidate, if present.
genfiles=("$HOME"/Downloads/marineplacesgen-*.go)

if [ ${#genfiles[@]} -gt 0 ]; then
    latest_gen=$(ls -t "${genfiles[@]}" | head -1)
    echo "Installing $(basename "$latest_gen")"
    mv -f "$latest_gen" ./cmd/marineplacesgen.go
else
    echo "No new generator found; using existing ./cmd/marineplacesgen.go"
fi

# Install newest corrected marine restaurant dataset, if present.
restaurant_files=(
    "$HOME"/Downloads/marine_restaurants-corrected-*.json
    "$HOME"/Downloads/marine_restaurants-correct-*.json
)

if [ ${#restaurant_files[@]} -gt 0 ]; then
    latest_restaurants=$(ls -t "${restaurant_files[@]}" | head -1)
    echo "Installing $(basename "$latest_restaurants")"
    mv -f "$latest_restaurants" ./assets/marine_restaurants.json
else
    echo "No new corrected restaurant file found; using existing ./assets/marine_restaurants.json"
fi

go run ./cmd/marineplacesgen.go "$@"
