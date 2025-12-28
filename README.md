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
      "percent_remaining": 34,
      "reset_text": "Resets 4am (Europe/Berlin)"
    },
    {
      "type": "weekly",
      "percent_remaining": 85,
      "reset_text": "Resets Jan 1, 2026, 10pm (Europe/Berlin)"
    }
  ],
  "captured_at": "2025-12-28T00:53:16+01:00"
}
```

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
