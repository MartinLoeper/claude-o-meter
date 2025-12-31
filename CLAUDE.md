# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

claude-o-meter is a Go CLI tool that extracts Claude usage metrics by parsing the output of `claude /usage`. It outputs JSON for integration with status bars like HyprPanel.

## Git Worktrees

This repository uses git worktrees. The main worktree lives in `claude-o-meter-worktrees/claude-o-meter/`.

**Creating new worktrees:** Always create worktrees in the parent directory (`claude-o-meter-worktrees/`) so Claude Code can navigate between them:

```bash
# From the main worktree, create a new worktree in the parent directory
git worktree add ../feature-branch-name feature-branch-name

# List all worktrees
git worktree list

# Remove a worktree
git worktree remove ../feature-branch-name
```

## Build Commands

```bash
# Build with version from VERSION file
go build -ldflags "-X main.Version=$(cat VERSION)" -o claude-o-meter .

# Run directly (dev version)
go run .

# Build with Nix (reads VERSION automatically)
nix build

# Enter dev shell (provides go, gopls, gotools)
nix develop
```

## Versioning

Version is stored in the `VERSION` file. See `VERSIONING.md` for the full scheme.

**When to update VERSION:**

- Parser changes for new Claude Code version → `<new-claude-version>-1`
- Bug fixes or new features → increment revision (`X.Y.Z-1` → `X.Y.Z-2`)
- Documentation only → no change needed

**Release process:**

1. Update `VERSION` file with new version
2. Commit the change
3. Create and push a git tag: `git tag v$(cat VERSION) && git push --tags`

**IMPORTANT:**

- Always update the `VERSION` file BEFORE creating a git tag. The tag must match the VERSION file contents.
- Only bump the version after meaningful changes to Nix sources (`flake.nix`, `nix/`) or Go sources (`*.go`). Documentation-only changes do not require a version bump.

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
