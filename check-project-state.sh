#!/usr/bin/env bash
set -u

README_FILE="${1:-README.md}"

if [[ ! -f "$README_FILE" ]]; then
  echo "ERROR: README checkpoint file not found: $README_FILE" >&2
  exit 2
fi

if command -v shasum >/dev/null 2>&1; then
  hash_file() {
    shasum -a 256 "$1" | awk '{print $1}'
  }
elif command -v sha256sum >/dev/null 2>&1; then
  hash_file() {
    sha256sum "$1" | awk '{print $1}'
  }
else
  echo "ERROR: neither 'shasum' nor 'sha256sum' is available." >&2
  exit 2
fi

extract_state_value() {
  local label="$1"
  awk -v label="$label" '
    BEGIN { in_state = 0 }
    /^<!-- PROJECT-STATE:BEGIN -->$/ {
      in_state = 1
      next
    }
    /^<!-- PROJECT-STATE:END -->$/ {
      in_state = 0
      next
    }
    in_state && index($0, "- " label ": **") == 1 {
      line = $0
      sub("^- " label ": \\*\\*", "", line)
      sub("\\*\\*[[:space:]]*$", "", line)
      print line
      exit
    }
  ' "$README_FILE"
}

expected_app_version="$(extract_state_value "Public app version")"
expected_build_version="$(extract_state_value "Generated source build")"
next_build_version="$(extract_state_value "Next generated source build")"

if [[ -z "$expected_app_version" || -z "$expected_build_version" ]]; then
  echo "ERROR: README Development State is missing app/build version fields." >&2
  exit 2
fi

found=0
matched=0
mismatched=0
missing=0

printf '%-26s %-10s %s\n' "FILE" "STATUS" "DETAIL"
printf '%-26s %-10s %s\n' "--------------------------" "----------" "----------------------------------------"

while IFS=$'\t' read -r file expected; do
  [[ -z "$file" || -z "$expected" ]] && continue
  found=$((found + 1))

  if [[ ! -f "$file" ]]; then
    printf '%-26s %-10s %s\n' "$file" "MISSING" "file not found"
    missing=$((missing + 1))
    continue
  fi

  actual="$(hash_file "$file")"

  if [[ "$actual" == "$expected" ]]; then
    printf '%-26s %-10s %s\n' "$file" "MATCH" "$actual"
    matched=$((matched + 1))
  else
    printf '%-26s %-10s %s\n' "$file" "DIFFERS" "expected $expected"
    printf '%-26s %-10s %s\n' "" "" "actual   $actual"
    mismatched=$((mismatched + 1))
  fi
done < <(
  awk '
    function trim(s) {
      sub(/^[ \t]+/, "", s)
      sub(/[ \t]+$/, "", s)
      return s
    }

    function stripticks(s) {
      gsub(/`/, "", s)
      return s
    }

    BEGIN { in_state = 0 }

    /^<!-- PROJECT-STATE:BEGIN -->$/ {
      in_state = 1
      next
    }

    /^<!-- PROJECT-STATE:END -->$/ {
      in_state = 0
      next
    }

    in_state && /^\| `/ {
      n = split($0, col, "|")
      if (n >= 4) {
        file = stripticks(trim(col[2]))
        digest = tolower(stripticks(trim(col[3])))

        if (file ~ /^[A-Za-z0-9_.\/-]+$/ &&
            digest ~ /^[0-9a-f]{64}$/) {
          print file "\t" digest
        }
      }
    }
  ' "$README_FILE"
)

if (( found == 0 )); then
  echo
  echo "ERROR: no managed-file SHA-256 checkpoints found in README Development State." >&2
  exit 2
fi

version_errors=0
actual_app_version=""
actual_build_version=""

if [[ -f "main.go" ]]; then
  actual_app_version="$(
    sed -n 's/^[[:space:]]*appVersion[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' main.go |
      head -n 1
  )"
  actual_build_version="$(
    sed -n 's/^[[:space:]]*buildVersion[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' main.go |
      head -n 1
  )"
fi

echo
printf '%-26s %-10s %s\n' "IDENTITY" "STATUS" "DETAIL"
printf '%-26s %-10s %s\n' "--------------------------" "----------" "----------------------------------------"

if [[ "$actual_app_version" == "$expected_app_version" ]]; then
  printf '%-26s %-10s %s\n' "appVersion" "MATCH" "$actual_app_version"
else
  printf '%-26s %-10s %s\n' "appVersion" "DIFFERS" "README $expected_app_version / main.go ${actual_app_version:-missing}"
  version_errors=$((version_errors + 1))
fi

if [[ "$actual_build_version" == "$expected_build_version" ]]; then
  printf '%-26s %-10s %s\n' "buildVersion" "MATCH" "$actual_build_version"
else
  printf '%-26s %-10s %s\n' "buildVersion" "DIFFERS" "README $expected_build_version / main.go ${actual_build_version:-missing}"
  version_errors=$((version_errors + 1))
fi

echo
echo "Checkpoint: $README_FILE"
echo "Public:     $expected_app_version"
echo "Build:      $expected_build_version"
if [[ -n "$next_build_version" ]]; then
  echo "Next build: $next_build_version"
fi
echo "Checked:    $found"
echo "Matched:    $matched"
echo "Different:  $mismatched"
echo "Missing:    $missing"

if (( mismatched > 0 || missing > 0 || version_errors > 0 )); then
  exit 1
fi

exit 0
