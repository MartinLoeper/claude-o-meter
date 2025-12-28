# TODO

## D-Bus Integration

Extend the daemon with D-Bus capabilities to allow external tools to trigger immediate queries:

### Use Case
When a user finishes a heavy Claude request, they want to immediately see the updated usage in the status bar rather than waiting up to 60 seconds for the next poll.

### Implementation
1. Add D-Bus service to the Go binary using a D-Bus library (e.g., `github.com/godbus/dbus`)
2. Register a session bus service (e.g., `com.github.MartinLoeper.ClaudeOMeter`)
3. Expose a method like `RefreshNow()` that:
   - Immediately triggers a query
   - Resets the interval timer (skip current wait cycle, wait full interval after refresh)
4. Add D-Bus service file to NixOS/Home Manager config for proper activation

### Claude Code Hook Integration
Users could register a Claude Code hook that calls the daemon over D-Bus when a request finishes:

```bash
# Example hook script
#!/usr/bin/env bash
dbus-send --session --dest=com.github.MartinLoeper.ClaudeOMeter \
  /com/github/MartinLoeper/ClaudeOMeter \
  com.github.MartinLoeper.ClaudeOMeter.RefreshNow
```

### NixOS Configuration
- Add D-Bus service file (`dbus.services` in Home Manager)
- Ensure proper permissions for session bus access

## Configurable Color Thresholds

Currently the color thresholds for HyprPanel output are hardcoded:
- 🟢 low: 0-50% used
- 🟡 medium: 51-80% used
- 🔴 high: >80% used

### Proposed Changes
1. Add CLI flags to `hyprpanel` subcommand:
   - `--threshold-medium` (default: 50)
   - `--threshold-high` (default: 80)
2. Add corresponding options to Home Manager module
3. Allow users to customize when they want to be warned about usage

## Version Update Checklist

When a new Claude Code version is released:

1. Update the `claude-code` flake input to point to the new version
2. Update `version` in `flake.nix` to match Claude Code version
3. Test that parsing still works with `claude /usage` output
4. Update the compatibility matrix in README.md
5. Tag and release the new version
