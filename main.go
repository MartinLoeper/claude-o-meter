package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// AccountType represents the Claude account tier
type AccountType string

const (
	AccountTypePro     AccountType = "pro"
	AccountTypeMax     AccountType = "max"
	AccountTypeAPI     AccountType = "api"
	AccountTypeUnknown AccountType = "unknown"
)

// QuotaType represents the type of quota
type QuotaType string

const (
	QuotaTypeSession       QuotaType = "session"
	QuotaTypeWeekly        QuotaType = "weekly"
	QuotaTypeModelSpecific QuotaType = "model_specific"
)

// Quota represents a usage quota
type Quota struct {
	Type                 QuotaType `json:"type"`
	Model                string    `json:"model,omitempty"`
	PercentRemaining     float64   `json:"percent_remaining"`
	ResetsAt             *string   `json:"resets_at,omitempty"`
	ResetText            string    `json:"reset_text,omitempty"`
	TimeRemainingSeconds *int64    `json:"time_remaining_seconds,omitempty"`
	TimeRemainingHuman   string    `json:"time_remaining_human,omitempty"`
}

// CostUsage represents extra usage costs (Pro accounts)
type CostUsage struct {
	Spent     float64 `json:"spent,omitempty"`
	Budget    float64 `json:"budget,omitempty"`
	Unlimited bool    `json:"unlimited,omitempty"`
	ResetsAt  *string `json:"resets_at,omitempty"`
}

// UsageSnapshot represents the complete usage information
type UsageSnapshot struct {
	AccountType  AccountType `json:"account_type"`
	Email        string      `json:"email,omitempty"`
	Organization string      `json:"organization,omitempty"`
	Quotas       []Quota     `json:"quotas"`
	CostUsage    *CostUsage  `json:"cost_usage,omitempty"`
	CapturedAt   string      `json:"captured_at"`
	RawOutput    string      `json:"raw_output,omitempty"`
}

// ErrorResponse for JSON error output
type ErrorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}

// HyprPanelOutput represents the JSON format expected by HyprPanel custom modules
type HyprPanelOutput struct {
	Text    string `json:"text"`
	Alt     string `json:"alt"`
	Class   string `json:"class"`
	Tooltip string `json:"tooltip"`
}

var (
	// ANSI escape code pattern
	ansiPattern = regexp.MustCompile(`\x1B(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])`)

	// Account type patterns (case insensitive)
	proPattern = regexp.MustCompile(`(?i)·\s*claude\s+pro`)
	maxPattern = regexp.MustCompile(`(?i)·\s*claude\s+max`)
	apiPattern = regexp.MustCompile(`(?i)·\s*claude\s+api`)

	// Percentage pattern: "X% used" or "X% left"
	percentPattern = regexp.MustCompile(`(\d{1,3})\s*%\s*(used|left)`)

	// Time patterns for reset parsing (relative durations)
	daysPattern    = regexp.MustCompile(`(\d+)\s*d(?:ays?)?`)
	hoursPattern   = regexp.MustCompile(`(\d+)\s*h(?:ours?|r)?`)
	minutesPattern = regexp.MustCompile(`(\d+)\s*m(?:in(?:utes?)?)?`)

	// Absolute time patterns: "5:59am", "6am", "12:59pm", "6pm"
	timeOnlyPattern = regexp.MustCompile(`\b(\d{1,2})(?::(\d{2}))?(am|pm)\b`)

	// Full date pattern: "Jan 4, 2026, 12:59am" or "Jan 4, 2026, 1am"
	fullDatePattern = regexp.MustCompile(`\b(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+(\d{1,2}),?\s+(\d{4}),?\s+(\d{1,2})(?::(\d{2}))?(am|pm)\b`)

	// Timezone pattern to extract location
	timezonePattern = regexp.MustCompile(`\(([^)]+)\)`)

	// Email patterns
	emailHeaderPattern = regexp.MustCompile(`(?i)·\s*Claude\s+(?:Max|Pro)\s*·\s*([^\s@]+@[^\s@']+)`)
	emailLegacyPattern = regexp.MustCompile(`(?i)(?:Account|Email):\s*([^\s@]+@[^\s@]+)`)

	// Organization patterns
	orgHeaderPattern = regexp.MustCompile(`(?i)·\s*Claude\s+(?:Max|Pro)\s*·\s*(.+?)(?:\s*$|\n)`)
	orgLegacyPattern = regexp.MustCompile(`(?i)(?:Org|Organization):\s*(.+)`)

	// Cost pattern for extra usage
	costPattern = regexp.MustCompile(`\$?([\d,]+\.?\d*)\s*/\s*\$?([\d,]+\.?\d*)\s*spent`)
)

func stripANSI(text string) string {
	return ansiPattern.ReplaceAllString(text, "")
}

func detectAccountType(text string) AccountType {
	if proPattern.MatchString(text) {
		return AccountTypePro
	}
	if maxPattern.MatchString(text) {
		return AccountTypeMax
	}
	if apiPattern.MatchString(text) {
		return AccountTypeAPI
	}
	// Fallback: if we see quota-like content, assume max
	if strings.Contains(strings.ToLower(text), "current") && strings.Contains(text, "%") {
		return AccountTypeMax
	}
	return AccountTypeUnknown
}

func parsePercentage(text string) (float64, bool) {
	matches := percentPattern.FindStringSubmatch(text)
	if len(matches) < 3 {
		return 0, false
	}

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, false
	}

	// Convert "used" to remaining
	if strings.ToLower(matches[2]) == "used" {
		value = 100 - value
	}

	return value, true
}

// monthMap for parsing month names
var monthMap = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March,
	"apr": time.April, "may": time.May, "jun": time.June,
	"jul": time.July, "aug": time.August, "sep": time.September,
	"oct": time.October, "nov": time.November, "dec": time.December,
}

// parseAbsoluteTime attempts to parse absolute time from text and returns reset time and duration
func parseAbsoluteTime(text string) (*time.Time, *int64) {
	// Try to extract timezone location
	var loc *time.Location
	if tzMatches := timezonePattern.FindStringSubmatch(text); len(tzMatches) > 1 {
		tzName := tzMatches[1]
		if l, err := time.LoadLocation(tzName); err == nil {
			loc = l
		}
	}
	if loc == nil {
		loc = time.Local
	}

	now := time.Now().In(loc)

	// Try full date pattern first: "Jan 4, 2026, 12:59am" or "Jan 4, 2026, 1am"
	if matches := fullDatePattern.FindStringSubmatch(text); len(matches) > 6 {
		month := monthMap[strings.ToLower(matches[1])]
		day, _ := strconv.Atoi(matches[2])
		year, _ := strconv.Atoi(matches[3])
		hour, _ := strconv.Atoi(matches[4])
		min, _ := strconv.Atoi(matches[5]) // Will be 0 if minutes not specified
		ampm := strings.ToLower(matches[6])

		// Convert to 24-hour format
		if ampm == "pm" && hour != 12 {
			hour += 12
		} else if ampm == "am" && hour == 12 {
			hour = 0
		}

		resetTime := time.Date(year, month, day, hour, min, 0, 0, loc)
		duration := int64(resetTime.Sub(now).Seconds())
		if duration > 0 {
			return &resetTime, &duration
		}
		return &resetTime, nil
	}

	// Try time-only pattern: "5:59am" or "6am"
	if matches := timeOnlyPattern.FindStringSubmatch(text); len(matches) > 3 {
		hour, _ := strconv.Atoi(matches[1])
		min, _ := strconv.Atoi(matches[2]) // Will be 0 if minutes not specified
		ampm := strings.ToLower(matches[3])

		// Convert to 24-hour format
		if ampm == "pm" && hour != 12 {
			hour += 12
		} else if ampm == "am" && hour == 12 {
			hour = 0
		}

		// Create reset time for today
		resetTime := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, loc)

		// If the time has already passed today, it means tomorrow
		if resetTime.Before(now) {
			resetTime = resetTime.Add(24 * time.Hour)
		}

		duration := int64(resetTime.Sub(now).Seconds())
		if duration > 0 {
			return &resetTime, &duration
		}
		return &resetTime, nil
	}

	return nil, nil
}

func parseResetTime(lines []string, startIdx int) (string, *time.Time, *int64) {
	// Look within next 14 lines for reset information
	endIdx := startIdx + 14
	if endIdx > len(lines) {
		endIdx = len(lines)
	}

	for i := startIdx; i < endIdx; i++ {
		line := strings.ToLower(lines[i])
		if strings.Contains(line, "reset") || strings.Contains(line, "renew") {
			// First try parsing relative duration components
			var totalSeconds int64

			if matches := daysPattern.FindStringSubmatch(lines[i]); len(matches) > 1 {
				days, _ := strconv.ParseInt(matches[1], 10, 64)
				totalSeconds += days * 24 * 60 * 60
			}
			if matches := hoursPattern.FindStringSubmatch(lines[i]); len(matches) > 1 {
				hours, _ := strconv.ParseInt(matches[1], 10, 64)
				totalSeconds += hours * 60 * 60
			}
			if matches := minutesPattern.FindStringSubmatch(lines[i]); len(matches) > 1 {
				mins, _ := strconv.ParseInt(matches[1], 10, 64)
				totalSeconds += mins * 60
			}

			if totalSeconds > 0 {
				resetTime := time.Now().Add(time.Duration(totalSeconds) * time.Second)
				return lines[i], &resetTime, &totalSeconds
			}

			// Fallback: try absolute time parsing
			resetTime, duration := parseAbsoluteTime(lines[i])
			if resetTime != nil {
				return lines[i], resetTime, duration
			}

			return lines[i], nil, nil
		}
	}
	return "", nil, nil
}

// formatDuration converts seconds to a human-readable duration string
func formatDuration(seconds int64) string {
	if seconds <= 0 {
		return "0m"
	}

	days := seconds / (24 * 60 * 60)
	seconds %= 24 * 60 * 60
	hours := seconds / (60 * 60)
	seconds %= 60 * 60
	minutes := seconds / 60

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}

	return strings.Join(parts, " ")
}

func parseQuotas(text string) []Quota {
	lines := strings.Split(text, "\n")
	var quotas []Quota

	quotaLabels := map[string]struct {
		qType QuotaType
		model string
	}{
		"current session":           {QuotaTypeSession, ""},
		"current week (all models)": {QuotaTypeWeekly, ""},
		"current week (opus)":       {QuotaTypeModelSpecific, "opus"},
		"current week (sonnet)":     {QuotaTypeModelSpecific, "sonnet"},
		"opus usage":                {QuotaTypeModelSpecific, "opus"},
		"sonnet usage":              {QuotaTypeModelSpecific, "sonnet"},
	}

	for i, line := range lines {
		lineLower := strings.ToLower(line)

		for label, info := range quotaLabels {
			if strings.Contains(lineLower, label) {
				// Look for percentage in this line and next few lines
				searchEnd := i + 5
				if searchEnd > len(lines) {
					searchEnd = len(lines)
				}

				for j := i; j < searchEnd; j++ {
					if percent, ok := parsePercentage(lines[j]); ok {
						resetText, resetTime, durationSeconds := parseResetTime(lines, j)

						quota := Quota{
							Type:             info.qType,
							Model:            info.model,
							PercentRemaining: percent,
							ResetText:        strings.TrimSpace(resetText),
						}

						if resetTime != nil {
							ts := resetTime.Format(time.RFC3339)
							quota.ResetsAt = &ts
						}

						if durationSeconds != nil {
							quota.TimeRemainingSeconds = durationSeconds
							quota.TimeRemainingHuman = formatDuration(*durationSeconds)
						}

						quotas = append(quotas, quota)
						break
					}
				}
				break
			}
		}
	}

	return quotas
}

func parseEmail(text string) string {
	// Try header format first
	if matches := emailHeaderPattern.FindStringSubmatch(text); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	// Try legacy format
	if matches := emailLegacyPattern.FindStringSubmatch(text); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

func parseOrganization(text string) string {
	// Look for the pattern: "email@domain.com's\nOrganization" or "email@domain.com's Organization"
	// The org name follows the email's possessive
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		// Look for email with 's at the end (possessive)
		if strings.Contains(line, "@") && strings.Contains(line, "'s") {
			// Check if "Organization" is on the same line
			if idx := strings.Index(line, "'s "); idx > 0 {
				org := strings.TrimSpace(line[idx+3:])
				// Clean up any box drawing characters
				org = strings.Trim(org, "│ \t")
				if org != "" && !strings.HasPrefix(org, "│") {
					// "Organization" is the default for personal accounts, omit it
					if strings.ToLower(org) == "organization" {
						return ""
					}
					return org
				}
			}
			// Check if "Organization" is on the next line
			if i+1 < len(lines) {
				nextLine := strings.TrimSpace(lines[i+1])
				nextLine = strings.Trim(nextLine, "│ \t")
				if nextLine != "" && !strings.Contains(nextLine, "│") && !strings.Contains(nextLine, "─") {
					// "Organization" is the default for personal accounts, omit it
					if strings.ToLower(nextLine) == "organization" {
						return ""
					}
					return nextLine
				}
			}
		}
	}

	// Try legacy format
	if matches := orgLegacyPattern.FindStringSubmatch(text); len(matches) > 1 {
		org := strings.TrimSpace(matches[1])
		if strings.ToLower(org) == "organization" {
			return ""
		}
		return org
	}
	return ""
}

func parseCostUsage(text string) *CostUsage {
	textLower := strings.ToLower(text)

	// Check if extra usage is mentioned
	if !strings.Contains(textLower, "extra usage") {
		return nil
	}

	// Check if it's disabled
	if strings.Contains(textLower, "extra usage not enabled") {
		return nil
	}

	// Find the extra usage section and look for cost pattern or unlimited
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), "extra usage") {
			// Search within next 10 lines
			endIdx := i + 10
			if endIdx > len(lines) {
				endIdx = len(lines)
			}

			for j := i; j < endIdx; j++ {
				lineLower := strings.ToLower(lines[j])

				// Check for unlimited
				if strings.Contains(lineLower, "unlimited") {
					return &CostUsage{
						Unlimited: true,
					}
				}

				// Check for spent/budget pattern
				if matches := costPattern.FindStringSubmatch(lines[j]); len(matches) > 2 {
					spent, _ := strconv.ParseFloat(strings.ReplaceAll(matches[1], ",", ""), 64)
					budget, _ := strconv.ParseFloat(strings.ReplaceAll(matches[2], ",", ""), 64)

					return &CostUsage{
						Spent:  spent,
						Budget: budget,
					}
				}
			}
		}
	}

	return nil
}

func executeClaudeCLI(ctx context.Context, timeout time.Duration, debug bool) (string, error) {
	// Use expect to handle interactive prompts properly
	// It waits for the prompt before sending input
	expectScript := `
set timeout 30
spawn claude --dangerously-skip-permissions /usage
expect {
    "Yes, I accept" {
        send "2\r"
        exp_continue
    }
    "Yes, continue" {
        send "1\r"
        exp_continue
    }
    "% used" {
        # Got usage data, wait a bit for full output
        sleep 0.3
    }
    "% left" {
        sleep 0.3
    }
    timeout {
        exit 1
    }
    eof
}
`
	cmd := exec.CommandContext(ctx, "expect", "-c", expectScript)

	var stdout bytes.Buffer
	if debug {
		// In debug mode, tee output to stderr so we can see it in real-time
		cmd.Stdout = io.MultiWriter(&stdout, os.Stderr)
		cmd.Stderr = io.MultiWriter(&stdout, os.Stderr)
	} else {
		cmd.Stdout = &stdout
		cmd.Stderr = &stdout // Capture stderr to ensure consistent PTY behavior
	}

	// Set environment to ensure PTY works without a controlling terminal
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	// Create a new session so script works without a controlling terminal,
	// and set process group so we can kill all children on timeout
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil

	// Start the command
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start claude CLI: %w", err)
	}

	// Create a channel to signal completion
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// Poll for usage data and kill when we have it
	checkInterval := 500 * time.Millisecond
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Kill the entire process group to ensure script and its children die
			if cmd.Process != nil {
				syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			// Check if we got data before timing out
			output := stdout.String()
			if strings.Contains(output, "% used") || strings.Contains(output, "% left") {
				return output, nil
			}
			return "", fmt.Errorf("command timed out after %v", timeout)

		case err := <-done:
			// Command finished on its own
			output := stdout.String()
			if strings.Contains(output, "% used") || strings.Contains(output, "% left") {
				return output, nil
			}
			if err != nil {
				return "", fmt.Errorf("failed to execute claude CLI: %w", err)
			}
			return output, nil

		case <-ticker.C:
			// Check if we have usage data yet
			output := stdout.String()
			if strings.Contains(output, "% used") || strings.Contains(output, "% left") {
				// Give it a moment to finish rendering, then kill the process group
				time.Sleep(300 * time.Millisecond)
				if cmd.Process != nil {
					syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}
				return stdout.String(), nil
			}
		}
	}
}

// formatHyprPanelOutput converts a UsageSnapshot to HyprPanel JSON format
func formatHyprPanelOutput(snapshot *UsageSnapshot) *HyprPanelOutput {
	if snapshot == nil || len(snapshot.Quotas) == 0 {
		return &HyprPanelOutput{
			Text:    "--",
			Alt:     "error",
			Class:   "error",
			Tooltip: "Error fetching usage",
		}
	}

	// Calculate session usage percentage (used, not remaining)
	sessionUsed := 100 - snapshot.Quotas[0].PercentRemaining
	sessionTime := "unknown"
	if snapshot.Quotas[0].TimeRemainingHuman != "" {
		sessionTime = snapshot.Quotas[0].TimeRemainingHuman
	}

	// Calculate weekly usage if available
	weeklyUsed := 0.0
	weeklyTime := "unknown"
	if len(snapshot.Quotas) > 1 {
		weeklyUsed = 100 - snapshot.Quotas[1].PercentRemaining
		if snapshot.Quotas[1].TimeRemainingHuman != "" {
			weeklyTime = snapshot.Quotas[1].TimeRemainingHuman
		}
	}

	// Determine level based on session usage
	var level string
	switch {
	case sessionUsed > 80:
		level = "high"
	case sessionUsed > 50:
		level = "medium"
	default:
		level = "low"
	}

	// Build tooltip
	tooltipLines := []string{
		fmt.Sprintf("Session: %.0f%% used (%s left)", sessionUsed, sessionTime),
		fmt.Sprintf("Weekly: %.0f%% used (%s left)", weeklyUsed, weeklyTime),
	}

	// Add extra usage info if available
	if snapshot.CostUsage != nil {
		if snapshot.CostUsage.Unlimited {
			tooltipLines = append(tooltipLines, "Extra: Unlimited")
		} else if snapshot.CostUsage.Budget > 0 {
			tooltipLines = append(tooltipLines, fmt.Sprintf("Extra: $%.2f / $%.0f", snapshot.CostUsage.Spent, snapshot.CostUsage.Budget))
		}
	}

	return &HyprPanelOutput{
		Text:    fmt.Sprintf("%.0f%%", sessionUsed),
		Alt:     level,
		Class:   level,
		Tooltip: strings.Join(tooltipLines, "\\n"),
	}
}

// formatHyprPanelError returns an error HyprPanelOutput
func formatHyprPanelError(message string) *HyprPanelOutput {
	return &HyprPanelOutput{
		Text:    "--",
		Alt:     "error",
		Class:   "error",
		Tooltip: message,
	}
}

// formatHyprPanelLoading returns a loading HyprPanelOutput (for daemon startup)
func formatHyprPanelLoading() *HyprPanelOutput {
	return &HyprPanelOutput{
		Text:    "...",
		Alt:     "loading",
		Class:   "loading",
		Tooltip: "Waiting for daemon...",
	}
}

func parseClaudeOutput(rawOutput string, includeRaw bool) *UsageSnapshot {
	cleanOutput := stripANSI(rawOutput)

	snapshot := &UsageSnapshot{
		AccountType:  detectAccountType(cleanOutput),
		Email:        parseEmail(cleanOutput),
		Organization: parseOrganization(cleanOutput),
		Quotas:       parseQuotas(cleanOutput),
		CostUsage:    parseCostUsage(cleanOutput),
		CapturedAt:   time.Now().Format(time.RFC3339),
	}

	if includeRaw {
		snapshot.RawOutput = cleanOutput
	}

	return snapshot
}

// runQuery executes a single query and returns the snapshot or error
func runQuery(includeRaw bool, timeout time.Duration, debug bool) (*UsageSnapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	rawOutput, err := executeClaudeCLI(ctx, timeout, debug)
	if err != nil {
		return nil, err
	}

	return parseClaudeOutput(rawOutput, includeRaw), nil
}

// writeSnapshotToFile atomically writes a snapshot to the given file path
func writeSnapshotToFile(snapshot *UsageSnapshot, outputFile string) error {
	jsonBytes, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(outputFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write to temp file first
	tmpFile := outputFile + ".tmp"
	if err := os.WriteFile(tmpFile, jsonBytes, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpFile, outputFile); err != nil {
		os.Remove(tmpFile) // Clean up on failure
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// runDaemon runs the query in a loop, writing results to the output file
func runDaemon(interval time.Duration, outputFile string, timeout time.Duration, debug bool) {
	log.Printf("Starting daemon: interval=%s, output=%s, debug=%v", interval, outputFile, debug)

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on start
	doQuery := func() {
		snapshot, err := runQuery(false, timeout, debug)
		if err != nil {
			log.Printf("Query failed: %v", err)
			// Write error response to file so consumers know there was an issue
			errResp := &UsageSnapshot{
				AccountType: AccountTypeUnknown,
				CapturedAt:  time.Now().Format(time.RFC3339),
			}
			if writeErr := writeSnapshotToFile(errResp, outputFile); writeErr != nil {
				log.Printf("Failed to write error state: %v", writeErr)
			}
			return
		}

		if err := writeSnapshotToFile(snapshot, outputFile); err != nil {
			log.Printf("Failed to write snapshot: %v", err)
			return
		}

		log.Printf("Query successful: %s quota at %.0f%%",
			snapshot.AccountType,
			100-snapshot.Quotas[0].PercentRemaining)
	}

	doQuery()

	for {
		select {
		case <-ticker.C:
			doQuery()
		case sig := <-sigChan:
			log.Printf("Received signal %v, shutting down...", sig)
			return
		}
	}
}

func printUsage() {
	fmt.Println(`claude-o-meter - Get Claude usage metrics as JSON

Usage: claude-o-meter <command> [options]

Commands:
  query     Query usage once and output to stdout (default if no command given)
  daemon    Run as a daemon, periodically querying and writing to file
  hyprpanel Read from file and output HyprPanel-compatible JSON

Query options:
  -d, --debug           Enable debug mode (includes raw output)
  -r, --raw             Include raw CLI output in JSON
  --hyprpanel-json      Output in HyprPanel module format

Daemon options:
  -i, --interval   Query interval (default: 60s)
  -f, --file       Output file path (required)
  --debug          Print claude CLI output in real-time

HyprPanel options:
  -f, --file       Input file path (required)

Examples:
  claude-o-meter                           # Query once, output to stdout
  claude-o-meter query                     # Same as above
  claude-o-meter query --raw               # Include raw CLI output
  claude-o-meter query --hyprpanel-json    # Output for HyprPanel (one-shot)
  claude-o-meter daemon -i 60s -f /tmp/claude.json
  claude-o-meter hyprpanel -f /tmp/claude.json  # Read file, output HyprPanel JSON

Requires the 'claude' CLI to be installed and authenticated.`)
}

func main() {
	if len(os.Args) < 2 {
		// Default to query command
		runQueryCommand(os.Args[1:])
		return
	}

	switch os.Args[1] {
	case "query":
		runQueryCommand(os.Args[2:])
	case "daemon":
		runDaemonCommand(os.Args[2:])
	case "hyprpanel":
		runHyprPanelCommand(os.Args[2:])
	case "-h", "--help", "help":
		printUsage()
		os.Exit(0)
	default:
		// Check if it's a flag for query command
		if strings.HasPrefix(os.Args[1], "-") {
			runQueryCommand(os.Args[1:])
		} else {
			fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
			printUsage()
			os.Exit(1)
		}
	}
}

func runQueryCommand(args []string) {
	queryFlags := flag.NewFlagSet("query", flag.ExitOnError)
	debug := queryFlags.Bool("d", false, "Enable debug mode")
	debugLong := queryFlags.Bool("debug", false, "Enable debug mode")
	raw := queryFlags.Bool("r", false, "Include raw output")
	rawLong := queryFlags.Bool("raw", false, "Include raw output")
	hyprpanelJSON := queryFlags.Bool("hyprpanel-json", false, "Output in HyprPanel format")
	help := queryFlags.Bool("h", false, "Show help")
	helpLong := queryFlags.Bool("help", false, "Show help")

	queryFlags.Parse(args)

	if *help || *helpLong {
		printUsage()
		os.Exit(0)
	}

	includeRaw := *debug || *debugLong || *raw || *rawLong
	timeout := 30 * time.Second

	snapshot, err := runQuery(includeRaw, timeout, false)
	if err != nil {
		if *hyprpanelJSON {
			output := formatHyprPanelError(err.Error())
			jsonBytes, _ := json.Marshal(output)
			fmt.Println(string(jsonBytes))
			os.Exit(0) // Don't exit with error for HyprPanel
		}
		errResp := ErrorResponse{
			Error:   "Failed to get usage data",
			Details: err.Error(),
		}
		jsonBytes, _ := json.MarshalIndent(errResp, "", "  ")
		fmt.Fprintln(os.Stderr, string(jsonBytes))
		os.Exit(1)
	}

	if *hyprpanelJSON {
		output := formatHyprPanelOutput(snapshot)
		jsonBytes, _ := json.Marshal(output)
		fmt.Println(string(jsonBytes))
		return
	}

	jsonBytes, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		errResp := ErrorResponse{
			Error:   "Failed to encode JSON",
			Details: err.Error(),
		}
		jsonBytes, _ := json.MarshalIndent(errResp, "", "  ")
		fmt.Fprintln(os.Stderr, string(jsonBytes))
		os.Exit(1)
	}

	fmt.Println(string(jsonBytes))
}

func runDaemonCommand(args []string) {
	daemonFlags := flag.NewFlagSet("daemon", flag.ExitOnError)
	interval := daemonFlags.Duration("i", 60*time.Second, "Query interval")
	intervalLong := daemonFlags.Duration("interval", 60*time.Second, "Query interval")
	outputFile := daemonFlags.String("f", "", "Output file path (required)")
	outputFileLong := daemonFlags.String("file", "", "Output file path (required)")
	debug := daemonFlags.Bool("debug", false, "Print claude CLI output in real-time")
	help := daemonFlags.Bool("h", false, "Show help")
	helpLong := daemonFlags.Bool("help", false, "Show help")

	daemonFlags.Parse(args)

	if *help || *helpLong {
		printUsage()
		os.Exit(0)
	}

	// Determine which flags were used
	actualInterval := *interval
	if *intervalLong != 60*time.Second {
		actualInterval = *intervalLong
	}

	actualOutputFile := *outputFile
	if *outputFileLong != "" {
		actualOutputFile = *outputFileLong
	}

	if actualOutputFile == "" {
		fmt.Fprintln(os.Stderr, "Error: -f/--file is required for daemon mode")
		os.Exit(1)
	}

	timeout := 30 * time.Second
	runDaemon(actualInterval, actualOutputFile, timeout, *debug)
}

func runHyprPanelCommand(args []string) {
	hyprFlags := flag.NewFlagSet("hyprpanel", flag.ExitOnError)
	inputFile := hyprFlags.String("f", "", "Input file path (required)")
	inputFileLong := hyprFlags.String("file", "", "Input file path (required)")
	help := hyprFlags.Bool("h", false, "Show help")
	helpLong := hyprFlags.Bool("help", false, "Show help")

	hyprFlags.Parse(args)

	if *help || *helpLong {
		printUsage()
		os.Exit(0)
	}

	actualInputFile := *inputFile
	if *inputFileLong != "" {
		actualInputFile = *inputFileLong
	}

	if actualInputFile == "" {
		fmt.Fprintln(os.Stderr, "Error: -f/--file is required for hyprpanel mode")
		os.Exit(1)
	}

	// Check if file exists
	if _, err := os.Stat(actualInputFile); os.IsNotExist(err) {
		// File doesn't exist - daemon hasn't written yet
		output := formatHyprPanelLoading()
		jsonBytes, _ := json.Marshal(output)
		fmt.Println(string(jsonBytes))
		return
	}

	// Read and parse the file
	data, err := os.ReadFile(actualInputFile)
	if err != nil {
		output := formatHyprPanelError("Failed to read file: " + err.Error())
		jsonBytes, _ := json.Marshal(output)
		fmt.Println(string(jsonBytes))
		return
	}

	var snapshot UsageSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		output := formatHyprPanelError("Failed to parse JSON: " + err.Error())
		jsonBytes, _ := json.Marshal(output)
		fmt.Println(string(jsonBytes))
		return
	}

	// Check if the snapshot has valid data
	if len(snapshot.Quotas) == 0 {
		output := formatHyprPanelError("No quota data available")
		jsonBytes, _ := json.Marshal(output)
		fmt.Println(string(jsonBytes))
		return
	}

	output := formatHyprPanelOutput(&snapshot)
	jsonBytes, _ := json.Marshal(output)
	fmt.Println(string(jsonBytes))
}
