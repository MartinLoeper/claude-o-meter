# claude-o-meter

A CLI tool that extracts Claude usage metrics as JSON by parsing the output of `claude /usage`.

## Why?

Anthropic doesn't provide a public API for querying Claude usage metrics. The author was annoyed by not being able to display Claude usage in [HyprPanel](https://hyprpanel.com/) (a status bar for Hyprland). This tool solves that by scraping the metrics from the Claude CLI and outputting them as JSON, making it easy to integrate with status bars, scripts, and other tools.

## Built with Claude Code

This project is fully developed using [Claude Code](https://claude.ai/code) with Claude Opus 4.5. From the initial Go implementation to the Nix flake, Home Manager module, and documentation - every line was written through pair programming with Claude. It's genuinely remarkable what this model is capable of: understanding complex system interactions, writing idiomatic Go, crafting Nix expressions, and iterating on edge cases - all in a natural conversational flow.

## Compatibility

claude-o-meter scrapes output from the Claude CLI, so it depends on specific Claude Code versions. Our versioning follows Claude Code 1:1.

| claude-o-meter | Claude Code | Status    |
|----------------|-------------|-----------|
| 2.0.76         | 2.0.76      | Tested :white_check_mark: |

When using the Home Manager module, the correct Claude Code version is automatically included as a dependency via [claude-code-nix](https://github.com/sadjow/claude-code-nix).

**Note:** We are awaiting [upstream support for version pinning](https://github.com/sadjow/claude-code-nix/issues/144) to provide a Nix option for pinning specific Claude Code versions. Once available, we can always ship working defaults.

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
# Query once, output JSON to stdout
claude-o-meter

# Query once, output HyprPanel-compatible JSON
claude-o-meter query --hyprpanel-json

# Include raw CLI output in response
claude-o-meter --raw

# Run as daemon (writes to file periodically)
claude-o-meter daemon -i 60s -f ~/.cache/claude-o-meter.json

# Read daemon output and format for HyprPanel
claude-o-meter hyprpanel -f ~/.cache/claude-o-meter.json

# Show help
claude-o-meter --help
```

## Authentication States

claude-o-meter detects when the Claude CLI is not ready to provide usage data and returns structured error information:

| State | Error Code | Description |
|-------|------------|-------------|
| Setup required | `setup_required` | First-run setup screen (theme selection) |
| Not logged in | `not_logged_in` | User needs to authenticate |
| Token expired | `token_expired` | Session has expired, re-authentication needed |
| No subscription | `no_subscription` | User is on free tier without Pro/Max |

When an auth error is detected, the JSON output includes an `auth_error` field:

```json
{
  "account_type": "unknown",
  "quotas": null,
  "auth_error": {
    "Code": "setup_required",
    "Message": "Claude CLI setup required. Please run 'claude' to complete initial setup."
  },
  "captured_at": "2025-12-28T15:16:30+01:00"
}
```

In HyprPanel mode, auth errors display "!" with a descriptive tooltip.

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

![HyprPanel showing Claude usage metrics](assets/hyprpanel.png?)

### Step 1: Start the Daemon

The daemon polls Claude usage periodically and writes to a JSON file. Choose one of these options:

#### Option A: Home Manager (Recommended)

The flake provides a Home Manager module that runs claude-o-meter as a systemd user service.

```nix
{
  inputs.claude-o-meter.url = "github:MartinLoeper/claude-o-meter";

  # In your home-manager config:
  imports = [ inputs.claude-o-meter.homeManagerModules.default ];

  services.claude-o-meter = {
    enable = true;
  };
}
```

##### Home Manager Module Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enable` | bool | `false` | Enable the claude-o-meter daemon service |
| `package` | package | flake default | The claude-o-meter package to use |
| `interval` | string | `"60s"` | How often to query Claude usage metrics |
| `outputFile` | string | `~/.cache/claude-o-meter.json` | Path where the JSON output will be written |
| `debug` | bool | `false` | Print claude CLI output to journalctl for debugging |

Example with all options:

```nix
services.claude-o-meter = {
  enable = true;
  interval = "30s";
  outputFile = "/tmp/claude-usage.json";
  # debug = true;  # Enable to troubleshoot issues
};
```

The systemd service automatically includes all required dependencies in PATH (coreutils, procps, expect, util-linux, bash).

#### Option B: Manual

Run the daemon manually or create your own systemd service:

```bash
claude-o-meter daemon -i 60s -f ~/.cache/claude-o-meter.json
```

### Step 2: Add HyprPanel Module Config

Add to `~/.config/hyprpanel/modules.json`:

```json
{
    "custom/claude-usage": {
        "icon": {
          "low": "🟢",
          "medium": "🟡",
          "high": "🔴",
          "error": "⚫",
          "loading": "⏳",
          "setup_required": "🔧",
          "not_logged_in": "🔑",
          "token_expired": "⏰",
          "no_subscription": "💳"
        },
        "truncationSize": 0,
        "label": "{text}",
        "tooltip": "{tooltip}",
        "execute": "claude-o-meter hyprpanel -f ~/.cache/claude-o-meter.json",
        "actions": {
            "onLeftClick": "xdg-open https://claude.ai/settings/usage"
        },
        "interval": 6000,
        "hideOnEmpty": false
    }
}
```

### Step 3: Add the Module to Your Bar

Add `custom/claude-usage` to your bar layout in HyprPanel settings. The module won't appear automatically just by adding the config.

### Result

This displays:
- Session usage percentage with color indicator based on usage:
  - 🟢 **low** (green): 0-50% used
  - 🟡 **medium** (yellow): 51-80% used
  - 🔴 **high** (red): >80% used
- Loading indicator (hourglass) when the daemon hasn't written data yet
- Authentication state indicators:
  - 🔧 **setup_required**: Claude CLI needs initial setup
  - 🔑 **not_logged_in**: User needs to log in
  - ⏰ **token_expired**: Session expired, re-login needed
  - 💳 **no_subscription**: No Pro/Max subscription
- Tooltip with session time remaining, weekly usage, and extra usage info
- Click to open Claude usage settings

Check daemon logs: `journalctl --user -u claude-o-meter`

### Troubleshooting

When claude-o-meter encounters issues, HyprPanel displays the following states:

| Icon | Text | State | Cause | Solution |
|------|------|-------|-------|----------|
| 🔧 | Claude | `setup_required` | Claude CLI shows first-run setup screen | Run `claude` and complete the theme selection |
| 🔑 | Claude | `not_logged_in` | User is not authenticated | Run `claude` and sign in |
| ⏰ | Claude | `token_expired` | Session has expired | Run `claude` to re-authenticate |
| 💳 | Claude | `no_subscription` | No Pro/Max subscription | Upgrade to Claude Pro or Max |
| ⚫ | -- | `error` | Failed to fetch or parse usage data | Check daemon logs for details |
| ⏳ | ... | `loading` | Daemon hasn't written data yet | Wait for first poll or check if daemon is running |

All error states show a tooltip with a detailed message explaining the issue.

**Note:** After fixing an authentication issue (logging in, completing setup, etc.), restart the daemon to immediately fetch updated usage data:

```bash
systemctl --user restart claude-o-meter
```

Then wait a few seconds for the daemon to fetch new data, and restart HyprPanel to refresh the module:

```bash
systemctl --user restart hyprpanel.service
```

## How It Works

1. Runs `claude /usage` in a PTY environment via the `script` command
2. Polls for usage data patterns ("% used" / "% left")
3. Kills the process once data is captured
4. Strips ANSI escape codes from the output
5. Parses account type, quotas, reset times, and email
6. Outputs clean JSON to stdout (query mode) or file (daemon mode)

## Daemon Mode

The daemon mode is designed for integrations like status bars where calling the CLI on each poll would cause timeouts:

```bash
claude-o-meter daemon -i 60s -f /path/to/output.json
```

- Queries Claude usage at the specified interval
- Writes JSON atomically to the output file (temp file + rename)
- Logs to stderr (captured by journalctl when run as systemd service)
- Handles SIGTERM/SIGINT for graceful shutdown

## Credits

This project was inspired by and based on the parsing logic from [ClaudeBar](https://github.com/tddworks/ClaudeBar) by [tddworks](https://github.com/tddworks).

Specifically, the approach of executing `claude /usage` in a PTY and parsing the terminal output was derived from their Swift implementation:

- [ClaudeUsageProbe.swift](https://github.com/tddworks/ClaudeBar/blob/main/Sources/Infrastructure/CLI/ClaudeUsageProbe.swift)

ClaudeBar is a macOS menu bar application that monitors AI coding assistant usage quotas. Check it out if you're on macOS and want a GUI solution!

## License

MIT
