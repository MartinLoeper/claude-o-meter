# claude-o-meter

A CLI tool that extracts Claude usage metrics as JSON by parsing the output of `claude /usage`.

## Why?

Anthropic doesn't provide a public API for querying Claude usage metrics. The author was annoyed by not being able to display Claude usage in [HyprPanel](https://hyprpanel.com/) (a status bar for Hyprland). This tool solves that by scraping the metrics from the Claude CLI and outputting them as JSON, making it easy to integrate with status bars, scripts, and other tools.

## Installation

### Nix (recommended for NixOS users)

```bash
# Run directly
nix run github:MartinLoeper/claude-o-meter

# Or install to profile
nix profile install github:MartinLoeper/claude-o-meter
```

In a flake-based NixOS configuration:

```nix
{
  inputs.claude-o-meter.url = "github:MartinLoeper/claude-o-meter";

  # Then in your packages:
  environment.systemPackages = [ inputs.claude-o-meter.packages.${system}.default ];
}
```

### Go

```bash
go install github.com/MartinLoeper/claude-o-meter@latest
```

### Build from source

```bash
git clone https://github.com/MartinLoeper/claude-o-meter.git
cd claude-o-meter
go build -o claude-o-meter .
```

## Requirements

- The [Claude Code CLI](https://docs.anthropic.com/en/docs/claude-code) must be installed and authenticated
- Linux/macOS (uses `script` command for PTY)

## Usage

```bash
# Get usage as JSON
claude-o-meter

# Include raw CLI output in response
claude-o-meter --raw

# Show help
claude-o-meter --help
```

## Example Output

```json
{
  "account_type": "pro",
  "email": "user@example.com",
  "quotas": [
    {
      "type": "session",
      "percent_remaining": 80,
      "resets_at": "2025-12-28T06:00:00+01:00",
      "reset_text": "Resets 6am (Europe/Berlin)",
      "time_remaining_seconds": 12600,
      "time_remaining_human": "3h 30m"
    },
    {
      "type": "weekly",
      "percent_remaining": 98,
      "resets_at": "2026-01-04T01:00:00+01:00",
      "reset_text": "Resets Jan 4, 2026, 1am (Europe/Berlin)",
      "time_remaining_seconds": 597600,
      "time_remaining_human": "6d 22h"
    }
  ],
  "cost_usage": {
    "spent": 49.49,
    "budget": 100
  },
  "captured_at": "2025-12-28T02:30:00+01:00"
}
```

## HyprPanel Integration

Here's how to display Claude usage in [HyprPanel](https://hyprpanel.com/):

### 1. Create a wrapper script

Save this as `~/.local/bin/claude-o-meter-wrapper`:

```bash
#!/usr/bin/env bash
# Wrapper script for HyprPanel - with caching and error handling

CACHE="/tmp/claude-o-meter-cache.json"

# Try to fetch fresh data
RESULT=$(claude-o-meter 2>&1)

# Check if we got valid data or an error
if echo "$RESULT" | grep -q '"quotas"'; then
    # Success - update cache
    echo "$RESULT" > "$CACHE"
elif echo "$RESULT" | grep -q '"error"'; then
    # Error occurred - log to journalctl and fall back to cache
    logger -t claude-o-meter "Failed to fetch usage: $(echo "$RESULT" | jq -r '.details // .error')"
fi

# Output from cache or error
if [[ -f "$CACHE" ]] && grep -q '"quotas"' "$CACHE"; then
    jq -r 'if .quotas then
        (100 - .quotas[0].percent_remaining) as $session_used |
        (100 - .quotas[1].percent_remaining) as $weekly_used |
        (.quotas[0].time_remaining_human // "unknown") as $session_time |
        (.quotas[1].time_remaining_human // "unknown") as $weekly_time |
        (if .cost_usage then (if .cost_usage.unlimited then "Extra: Unlimited" else "Extra: $\(.cost_usage.spent) / $\(.cost_usage.budget)" end) else null end) as $extra |
        (if $session_used > 80 then "high" elif $session_used > 50 then "medium" else "low" end) as $level |
        (["Session: \($session_used | floor)% used (\($session_time) left)", "Weekly: \($weekly_used | floor)% used (\($weekly_time) left)"] + (if $extra then [$extra] else [] end) | join("\\n")) as $tooltip |
        "{ \"text\": \"\($session_used | floor)%\", \"alt\": \"\($level)\", \"class\": \"\($level)\", \"tooltip\": \"\($tooltip)\" }"
    else "{ \"text\": \"--\", \"alt\": \"error\", \"class\": \"error\", \"tooltip\": \"Error fetching usage\" }" end' < "$CACHE"
else
    echo '{ "text": "--", "alt": "error", "tooltip": "Error fetching usage" }'
fi
```

Make it executable: `chmod +x ~/.local/bin/claude-o-meter-wrapper`

### 2. Add HyprPanel module config

Add to `~/.config/hyprpanel/modules.json`:

```json
{
    "custom/claude-usage": {
        "icon": {
          "low": "🟢",
          "medium": "🟡",
          "high": "🔴",
          "error": "⚫"
        },
        "truncationSize": 0,
        "label": "{text} Claude",
        "tooltip": "{tooltip}",
        "execute": "~/.local/bin/claude-o-meter-wrapper",
        "actions": {
            "onLeftClick": "xdg-open https://claude.ai/settings/usage"
        },
        "interval": 60000,
        "hideOnEmpty": false
    }
}
```

This displays:
- Session usage percentage with color indicator (green/yellow/red)
- Tooltip with session time remaining, weekly usage, and extra usage info
- Click to open Claude usage settings

Check for errors: `journalctl -t claude-o-meter`

## How It Works

1. Runs `claude /usage` in a PTY environment via the `script` command
2. Polls for usage data patterns ("% used" / "% left")
3. Kills the process once data is captured
4. Strips ANSI escape codes from the output
5. Parses account type, quotas, reset times, and email
6. Outputs clean JSON to stdout

## Credits

This project was inspired by and based on the parsing logic from [ClaudeBar](https://github.com/tddworks/ClaudeBar) by [tddworks](https://github.com/tddworks).

Specifically, the approach of executing `claude /usage` in a PTY and parsing the terminal output was derived from their Swift implementation:

- [ClaudeUsageProbe.swift](https://github.com/tddworks/ClaudeBar/blob/main/Sources/Infrastructure/CLI/ClaudeUsageProbe.swift)

ClaudeBar is a macOS menu bar application that monitors AI coding assistant usage quotas. Check it out if you're on macOS and want a GUI solution!

## License

MIT
