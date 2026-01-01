# TODO

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

## HyprPanel Menu Integration

Add instructions for setting up a HyprPanel menu for more sophisticated interactions:

### Use Cases
- Bind a keyboard shortcut to display time remaining until quota reset
- Show detailed quota breakdown in a popup menu
- Quick actions like opening Claude usage settings or refreshing data

### Documentation to Add
1. Example menu configuration for HyprPanel
2. Keyboard shortcut bindings (e.g., Super+C to show Claude usage popup)
3. Integration with notification systems for quota warnings

### Potential Features
- `claude-o-meter notify` subcommand for desktop notifications
- Machine-readable output format for scripting menu entries
- Support for different notification backends (dunst, mako, etc.)

## Claude Code Version Pinning

Once [upstream support](https://github.com/sadjow/claude-code-nix/issues/144) is available:

1. Add a `claudeCodeVersion` option to the Home Manager module
2. Default to the tested version from the compatibility matrix
3. Allow users to override if they need a different version
4. Document in README how to pin a specific version

This ensures users always get a working setup out of the box while retaining flexibility.

## Automated Compatibility Testing

Add GitHub CI to automatically test compatibility when a new Claude Code version is released upstream:

### Trigger
- Watch [claude-code-nix](https://github.com/sadjow/claude-code-nix) for new releases
- Automatically update flake input ref in CI and run tests

### Automated Tests
- Build claude-o-meter with the new Claude Code version
- Run unit tests for parsing logic
- Test CLI argument handling and output formatting

### Manual Tests
Some tests require a working Claude Pro/Max subscription, which is difficult in CI:
- Actual `claude /usage` output parsing
- Authentication state detection
- End-to-end daemon mode testing

Consider maintaining a set of recorded CLI outputs (snapshots) to test parsing without live authentication.

## Version Update Checklist

When a new Claude Code version is released:

1. Update the `claude-code` flake input to point to the new version
2. Update `version` in `flake.nix` to match Claude Code version
3. Test that parsing still works with `claude /usage` output
4. Update the compatibility matrix in README.md
5. Tag and release the new version
