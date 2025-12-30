# Versioning

claude-o-meter follows a **dependency-tracking version scheme** that couples its version to the Claude Code CLI it parses.

## Format

```
<claude-code-version>-<revision>
```

| Component | Description |
|-----------|-------------|
| `claude-code-version` | The Claude Code version this release is tested and compatible with |
| `revision` | Incrementing number for claude-o-meter changes (resets to 1 for new Claude Code versions) |

## Examples

| Version | Meaning |
|---------|---------|
| `2.0.76-1` | Initial release compatible with Claude Code 2.0.76 |
| `2.0.76-2` | Bug fix or feature in claude-o-meter (same Claude Code compatibility) |
| `2.0.80-1` | Updated for Claude Code 2.0.80 (parser changes, new fields, etc.) |

## Rationale

claude-o-meter parses the output of `claude /usage`. When Claude Code changes its output format, claude-o-meter may need parser updates. This versioning scheme:

- **Communicates compatibility**: Users know which Claude Code version is supported
- **Tracks tool changes**: The revision number tracks claude-o-meter-specific changes
- **Simplifies debugging**: Version mismatches are immediately apparent

## When to Increment

| Change Type | Action |
|-------------|--------|
| Claude Code updated, parser changes needed | Bump to `<new-claude-version>-1` |
| Bug fix in claude-o-meter | Increment revision (`X.Y.Z-1` → `X.Y.Z-2`) |
| New feature in claude-o-meter | Increment revision |
| Documentation only | No version change required |

## Version File

The current version is stored in the `VERSION` file at the repository root. All build tooling reads from this single source of truth.
