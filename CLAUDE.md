# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

claude-o-meter is a Go CLI tool that extracts Claude usage metrics by parsing the output of `claude /usage`. It outputs JSON for integration with status bars like HyprPanel.

## Build Commands

```bash
# Build
go build -o claude-o-meter .

# Run directly
go run .

# Build with Nix
nix build

# Enter dev shell (provides go, gopls, gotools)
nix develop
```

## Testing

This project currently has no tests. The codebase is a single `main.go` file.

## Architecture

**Single-file Go application** (`main.go`) with three main modes:
- `query` - One-shot query, outputs JSON to stdout
- `daemon` - Runs in a loop, writes JSON to file periodically
- `hyprpanel` - Reads daemon output file, formats for HyprPanel

**Core flow:**
1. `executeClaudeCLI()` - Runs `claude /usage` in a PTY via the `script` command, polls for "% used" or "% left" patterns, then kills the process
2. `parseClaudeOutput()` - Strips ANSI codes, detects account type (pro/max/api), parses quotas, email, organization, and cost usage
3. Output formatting - Either raw `UsageSnapshot` JSON or `HyprPanelOutput` for status bar integration

**Key data types:**
- `UsageSnapshot` - Complete usage info (account type, quotas, cost usage)
- `Quota` - Individual quota with type (session/weekly/model_specific), percentage remaining, reset time
- `HyprPanelOutput` - HyprPanel module format with text, alt, class, tooltip

**Nix integration:**
- `flake.nix` - Builds the Go module, provides dev shell
- `nix/hm-module.nix` - Home Manager module for running as a systemd service

## Parsing Details

The tool uses regex patterns to parse Claude CLI output. Key patterns are defined at the top of `main.go`:
- Account type: `·\s*claude\s+(pro|max|api)`
- Percentages: `(\d{1,3})\s*%\s*(used|left)`
- Reset times: Relative durations (`5d 3h`) and absolute times (`Jan 4, 2026, 1am`)
- Cost: `\$?([\d,]+\.?\d*)\s*/\s*\$?([\d,]+\.?\d*)\s*spent`
