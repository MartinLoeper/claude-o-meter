# Claude Code Statusline Integration

This guide explains how to display Claude usage metrics in Claude Code's statusline using claude-o-meter.

## What is the Claude Code Statusline?

Claude Code supports a customizable statusline that appears at the bottom of the terminal during conversations. You can configure it to show useful information like the current directory, git branch, model name, and—with claude-o-meter—your current usage metrics.

## Prerequisites

1. **claude-o-meter daemon running**: The daemon must be running and writing to a JSON file
2. **jq (optional but recommended)**: For reliable JSON parsing in the statusline script

## Setup

### Step 1: Ensure claude-o-meter daemon is running

If using Home Manager:

```nix
services.claude-o-meter = {
  enable = true;
  # stateFile defaults to ~/.cache/claude-o-meter.json
};
```

Or run manually:

```bash
claude-o-meter daemon -i 60s -f ~/.cache/claude-o-meter.json &
```

### Step 2: Create a statusline script

Create a file at `~/.claude/statusline.sh`:

```bash
#!/usr/bin/env bash
# See examples/statusline.sh for a complete example

input=$(cat)

# Parse claude-o-meter output
METER_FILE="$HOME/.cache/claude-o-meter.json"
if [ -f "$METER_FILE" ] && command -v claude-o-meter >/dev/null 2>&1; then
  meter_json=$(claude-o-meter hyprpanel -f "$METER_FILE" 2>/dev/null)
  meter_text=$(echo "$meter_json" | jq -r '.text // ""' 2>/dev/null)

  if [ -n "$meter_text" ]; then
    printf '⚡ %s\n' "$meter_text"
  fi
fi
```

Make it executable:

```bash
chmod +x ~/.claude/statusline.sh
```

### Step 3: Configure Claude Code

Add to your `~/.claude/settings.json`:

```json
{
  "statusline": {
    "script": "~/.claude/statusline.sh"
  }
}
```

Or with Home Manager:

```nix
programs.claude-code.settings = {
  statusline.script = "~/.claude/statusline.sh";
};
```

## Sample Script

A complete sample statusline script is provided at [`examples/statusline.sh`](examples/statusline.sh). It includes:

- **Directory**: Current working directory with home path abbreviation
- **Git branch**: Current git branch (if in a git repository)
- **Model name**: The Claude model being used
- **Claude Code version**: The version of Claude Code
- **Usage metrics**: Session usage percentage from claude-o-meter with color coding

### Features

- **Color coding**: Usage is displayed in green (low), orange (medium), or red (high) based on consumption
- **jq fallback**: Works without jq using bash-only JSON parsing (less reliable)
- **Logging**: Logs to `~/.claude/statusline.log` for debugging

### Output Example

```
📁 ~/projects/myapp  🌿 main  🤖 Claude Opus 4.5  📟 v2.0.76  ⚡ 45% Max
```

## Customization

### Changing colors

Edit the color functions in the script. Colors use ANSI 256-color codes:

```bash
meter_low_color() { printf '\033[38;5;82m'; }    # green
meter_medium_color() { printf '\033[38;5;214m'; } # orange
meter_high_color() { printf '\033[38;5;196m'; }   # red
```

### Changing the meter file location

If you configured a custom `stateFile` in claude-o-meter, update the script:

```bash
METER_FILE="/your/custom/path.json"
```

### Adding more information

The statusline script receives JSON input from Claude Code with session information. Use `jq` to extract additional fields:

```bash
# Extract context usage percentage
context_pct=$(echo "$input" | jq -r '.context.percent_used // ""' 2>/dev/null)

# Extract session cost
cost=$(echo "$input" | jq -r '.cost.total_usd // ""' 2>/dev/null)
```

## Troubleshooting

### Statusline not appearing

1. Check that the script is executable: `chmod +x ~/.claude/statusline.sh`
2. Check that the path in `settings.json` is correct
3. Test the script manually: `echo '{}' | ~/.claude/statusline.sh`

### Usage not showing

1. Verify the daemon is running: `systemctl --user status claude-o-meter`
2. Check that the JSON file exists: `cat ~/.cache/claude-o-meter.json`
3. Test claude-o-meter directly: `claude-o-meter hyprpanel -f ~/.cache/claude-o-meter.json`

### Debugging

Check the statusline log file:

```bash
tail -f ~/.claude/statusline.log
```

Check daemon logs:

```bash
journalctl --user -u claude-o-meter -f
```
