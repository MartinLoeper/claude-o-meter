# TODO

## Login Handling

Currently we assume the user is logged in and on the Pro or Max plan. We should:

1. Check this precondition before attempting to parse usage data
2. Handle the case where the user is not logged in:
   - Clearly log an error message (e.g., "User not logged in to Claude CLI")
   - Return a proper error response instead of failing silently
   - In HyprPanel mode, show an appropriate error state with tooltip explaining the issue
3. Consider detecting the login prompt output pattern from `claude /usage` and reporting it appropriately

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
