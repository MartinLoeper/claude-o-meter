# Architecture

## High-Level Overview

claude-o-meter uses a daemon/client architecture to efficiently provide usage metrics to status bar integrations without repeatedly querying the Claude CLI.

```mermaid
flowchart TB
    subgraph "Systemd User Service"
        daemon["claude-o-meter daemon"]
        file[("~/.cache/claude-o-meter/usage.json")]
        daemon -->|"writes periodically"| file
    end

    subgraph "Data Source"
        claude["claude /usage"]
    end

    subgraph "Clients"
        hyprpanel["HyprPanel Custom Module"]
        other["Other Status Bars / Scripts"]
    end

    daemon -->|"executes in PTY"| claude
    claude -->|"stdout with ANSI"| daemon

    hyprpanel -->|"claude-o-meter hyprpanel"| file
    other -->|"claude-o-meter hyprpanel"| file
```

### Components

| Component | Description |
|-----------|-------------|
| **Daemon** | Systemd user service running `claude-o-meter daemon`. Polls `claude /usage` at configurable intervals and writes JSON to a cache file. |
| **Cache File** | JSON file at `~/.cache/claude-o-meter/usage.json` containing the latest `UsageSnapshot`. |
| **Client Mode** | `claude-o-meter hyprpanel` reads the cache file and formats output for status bar consumption. |

### Modes

```
┌─────────────────────────────────────────────────────────────────┐
│  claude-o-meter query     → One-shot query, JSON to stdout      │
│  claude-o-meter daemon    → Background service, writes to file  │
│  claude-o-meter hyprpanel → Client reads file, formats output   │
└─────────────────────────────────────────────────────────────────┘
```

### Data Flow

1. **Daemon** spawns `claude /usage` in a PTY (via `script` command)
2. **Daemon** monitors output for completion patterns (`% used` or `% left`)
3. **Daemon** parses output, strips ANSI codes, extracts metrics
4. **Daemon** writes `UsageSnapshot` JSON to cache file
5. **Client** (e.g., HyprPanel module) invokes `claude-o-meter hyprpanel`
6. **Client mode** reads cache file, transforms to `HyprPanelOutput` format
7. **Status bar** displays the formatted metrics

### HyprPanel Integration Sequence

```mermaid
sequenceDiagram
    participant HP as HyprPanel
    participant CM as claude-o-meter hyprpanel
    participant CF as Cache File
    participant D as Daemon
    participant CLI as claude /usage

    Note over D,CLI: Background (every 60s)
    loop Daemon Poll Cycle
        D->>CLI: Execute in PTY
        CLI-->>D: ANSI output with usage data
        D->>D: Parse & strip ANSI
        D->>CF: Write UsageSnapshot JSON
    end

    Note over HP,CF: Foreground (every 6s)
    loop HyprPanel Poll Cycle
        HP->>CM: Invoke binary
        CM->>CF: Read JSON
        CF-->>CM: UsageSnapshot
        CM->>CM: Transform to HyprPanelOutput
        CM-->>HP: JSON {text, alt, class, tooltip}
        HP->>HP: Update status bar widget
    end
```

### Why This Architecture?

- **Efficiency**: The Claude CLI takes 2-3 seconds to return results. Running it on every status bar refresh would cause delays.
- **Decoupling**: Status bars poll frequently (every few seconds). The daemon polls infrequently (every 60s by default).
- **Reliability**: If the daemon fails, clients show stale data rather than hanging.

---

## Edge Cases

### Cache File Missing (First Startup)

When `claude-o-meter hyprpanel` is invoked before the daemon has written its first cache file, the client **blocks** until the file exists. This ensures the status bar receives valid data rather than a transient "loading" state.

```mermaid
sequenceDiagram
    participant HP as HyprPanel
    participant CM as claude-o-meter hyprpanel
    participant FS as Filesystem
    participant D as Daemon

    HP->>CM: Invoke binary
    CM->>FS: Check file exists?
    FS-->>CM: No

    loop Poll every 500ms
        CM->>FS: Check file exists?
        FS-->>CM: No
    end

    Note over D: Daemon completes first query
    D->>FS: Write cache file

    CM->>FS: Check file exists?
    FS-->>CM: Yes
    CM->>FS: Read JSON
    FS-->>CM: UsageSnapshot
    CM-->>HP: JSON {text, alt, class, tooltip}
```

**Behavior Summary:**

| Scenario | Client Behavior |
|----------|-----------------|
| Cache file exists | Read immediately, return formatted output |
| Cache file missing | Block, poll every 500ms until file appears |

**Rationale:** Blocking prevents the status bar from displaying a flickering "loading" indicator on every poll cycle during startup. The daemon typically writes its first result within 2-3 seconds, so the delay is minimal.

---

## Account Type Detection

The tool detects the Claude account type by parsing the header line from `claude /usage` output.

### Patterns

The CLI outputs a header line in the format:

```text
· claude <type> · user@email.com
```

Three regex patterns match this format (case-insensitive):

| Pattern | Matches | Account Type |
|---------|---------|--------------|
| `(?i)·\s*claude\s+pro` | `· claude pro` | `pro` |
| `(?i)·\s*claude\s+max` | `· claude max` | `max` |
| `(?i)·\s*claude\s+api` | `· claude api` | `api` |

### Detection Logic

Located in `detectAccountType()` (`main.go`):

1. Check patterns in order: pro → max → api
2. Return the first matching account type
3. If no pattern matches, return `unknown`

The function does **not** use fallback heuristics. If the header format is unrecognized (e.g., AWS Bedrock, Google Vertex, or future integrations), it returns `unknown` rather than guessing.

### Account Types

```go
const (
    AccountTypePro     = "pro"     // Claude Pro subscription
    AccountTypeMax     = "max"     // Claude Max subscription
    AccountTypeAPI     = "api"     // API access
    AccountTypeUnknown = "unknown" // Unrecognized format
)
```
