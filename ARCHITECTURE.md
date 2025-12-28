# Architecture

## Account Type Detection

The tool detects the Claude account type by parsing the header line from `claude /usage` output.

### Patterns

The CLI outputs a header line in the format:
```
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
