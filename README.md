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

![HyprPanel showing Claude usage metrics](assets/hyprpanel.png)

### Option 1: Using Home Manager (Recommended)

The flake provides a Home Manager module that runs claude-o-meter as a systemd user service. This is the recommended approach as it handles polling in the background, avoiding timeout issues.

Add to your Home Manager configuration:

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

#### Home Manager Module Options

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

### Option 2: Manual Setup

#### 1. Start the daemon

You can run the daemon manually or create your own systemd service:

```bash
claude-o-meter daemon -i 60s -f ~/.cache/claude-o-meter.json
```

#### 2. Create a wrapper script

Save this as `~/.local/bin/claude-o-meter-wrapper`:

```bash
#!/usr/bin/env bash
# Wrapper script for HyprPanel - reads from daemon-written file
exec claude-o-meter hyprpanel -f "${HOME}/.cache/claude-o-meter.json"
```

Make it executable: `chmod +x ~/.local/bin/claude-o-meter-wrapper`

### 3. Add HyprPanel module config

Add to `~/.config/hyprpanel/modules.json`:

```json
{
    "custom/claude-usage": {
        "icon": {
          "low": "🟢",
          "medium": "🟡",
          "high": "🔴",
          "error": "⚫",
          "loading": "⏳"
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

### 4. Add the module to your bar

After adding the module config, you need to explicitly add `custom/claude-usage` to your bar layout in HyprPanel settings. The module won't appear automatically just by adding the config.

This displays:
- Session usage percentage with color indicator (green/yellow/red)
- Loading indicator (hourglass) when the daemon hasn't written data yet
- Tooltip with session time remaining, weekly usage, and extra usage info
- Click to open Claude usage settings

Check daemon logs: `journalctl --user -u claude-o-meter`

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
