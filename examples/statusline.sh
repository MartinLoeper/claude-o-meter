#!/usr/bin/env bash
# Claude Code statusline script with claude-o-meter integration
# See STATUSLINE.md for setup instructions

input=$(cat)

# Get the directory where this statusline script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOG_FILE="${SCRIPT_DIR}/statusline.log"
TIMESTAMP=$(date '+%Y-%m-%d %H:%M:%S')

# ---- check jq availability ----
HAS_JQ=0
if command -v jq >/dev/null 2>&1; then
  HAS_JQ=1
fi

# ---- logging ----
{
  echo "[$TIMESTAMP] Status line triggered"
  echo "[$TIMESTAMP] Input:"
  if [ "$HAS_JQ" -eq 1 ]; then
    echo "$input" | jq . 2>/dev/null || echo "$input"
  else
    echo "$input"
    echo "[$TIMESTAMP] WARNING: jq not found, using bash fallback for JSON parsing"
  fi
  echo "---"
} >> "$LOG_FILE" 2>/dev/null

# ---- color helpers (force colors for Claude Code) ----
use_color=1
[ -n "$NO_COLOR" ] && use_color=0

# ---- modern sleek colors ----
dir_color() { if [ "$use_color" -eq 1 ]; then printf '\033[38;5;117m'; fi; }    # sky blue
model_color() { if [ "$use_color" -eq 1 ]; then printf '\033[38;5;147m'; fi; }  # light purple
cc_version_color() { if [ "$use_color" -eq 1 ]; then printf '\033[38;5;249m'; fi; } # light gray
git_color() { if [ "$use_color" -eq 1 ]; then printf '\033[38;5;150m'; fi; }  # soft green
rst() { if [ "$use_color" -eq 1 ]; then printf '\033[0m'; fi; }

# ---- claude-o-meter colors ----
meter_low_color() { if [ "$use_color" -eq 1 ]; then printf '\033[38;5;82m'; fi; }    # green
meter_medium_color() { if [ "$use_color" -eq 1 ]; then printf '\033[38;5;214m'; fi; } # orange
meter_high_color() { if [ "$use_color" -eq 1 ]; then printf '\033[38;5;196m'; fi; }   # red

# ---- JSON extraction ----
if [ "$HAS_JQ" -eq 1 ]; then
  current_dir=$(echo "$input" | jq -r '.workspace.current_dir // .cwd // "unknown"' 2>/dev/null | sed "s|^$HOME|~|g")
  model_name=$(echo "$input" | jq -r '.model.display_name // "Claude"' 2>/dev/null)
  cc_version=$(echo "$input" | jq -r '.version // ""' 2>/dev/null)
else
  # Bash fallback for JSON extraction
  current_dir=$(echo "$input" | grep -o '"workspace"[[:space:]]*:[[:space:]]*{[^}]*"current_dir"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"current_dir"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/' | sed 's/\\\\/\//g')
  if [ -z "$current_dir" ] || [ "$current_dir" = "null" ]; then
    current_dir=$(echo "$input" | grep -o '"cwd"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"cwd"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/' | sed 's/\\\\/\//g')
  fi
  [ -z "$current_dir" ] && current_dir="unknown"
  current_dir=$(echo "$current_dir" | sed "s|^$HOME|~|g")

  model_name=$(echo "$input" | grep -o '"model"[[:space:]]*:[[:space:]]*{[^}]*"display_name"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"display_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')
  [ -z "$model_name" ] && model_name="Claude"

  cc_version=$(echo "$input" | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')
fi

# ---- git branch ----
git_branch=""
if git rev-parse --git-dir >/dev/null 2>&1; then
  git_branch=$(git branch --show-current 2>/dev/null || git rev-parse --short HEAD 2>/dev/null)
fi

# ---- claude-o-meter integration ----
meter_text=""
meter_class=""
METER_FILE="$HOME/.cache/claude-o-meter.json"
if [ -f "$METER_FILE" ] && command -v claude-o-meter >/dev/null 2>&1; then
  meter_json=$(claude-o-meter hyprpanel -f "$METER_FILE" 2>/dev/null)
  if [ -n "$meter_json" ]; then
    if [ "$HAS_JQ" -eq 1 ]; then
      meter_text=$(echo "$meter_json" | jq -r '.text // ""' 2>/dev/null)
      meter_class=$(echo "$meter_json" | jq -r '.class // ""' 2>/dev/null)
    else
      meter_text=$(echo "$meter_json" | grep -o '"text"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"text"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')
      meter_class=$(echo "$meter_json" | grep -o '"class"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"class"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')
    fi
  fi
fi

# ---- render statusline ----
printf '📁 %s%s%s' "$(dir_color)" "$current_dir" "$(rst)"

if [ -n "$git_branch" ]; then
  printf '  🌿 %s%s%s' "$(git_color)" "$git_branch" "$(rst)"
fi

printf '  🤖 %s%s%s' "$(model_color)" "$model_name" "$(rst)"

if [ -n "$cc_version" ] && [ "$cc_version" != "null" ]; then
  printf '  📟 %sv%s%s' "$(cc_version_color)" "$cc_version" "$(rst)"
fi

if [ -n "$meter_text" ]; then
  case "$meter_class" in
    low)    meter_color="$(meter_low_color)" ;;
    medium) meter_color="$(meter_medium_color)" ;;
    high)   meter_color="$(meter_high_color)" ;;
    *)      meter_color="" ;;
  esac
  printf '  ⚡ %s%s%s' "$meter_color" "$meter_text" "$(rst)"
fi

printf '\n'
