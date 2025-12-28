package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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

func executeClaudeCLI(ctx context.Context, timeout time.Duration) (string, error) {
	// Create command using script to provide a PTY environment
	// The -q flag suppresses the "Script started" message
	// We use /dev/null as the typescript file since we capture stdout directly
	cmd := exec.CommandContext(ctx, "script", "-q", "-c", "claude /usage", "/dev/null")

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stdout // Capture stderr to ensure consistent PTY behavior

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

func main() {
	// Parse flags
	includeRaw := false
	timeout := 30 * time.Second

	for _, arg := range os.Args[1:] {
		switch arg {
		case "-d", "--debug":
			includeRaw = true
		case "-r", "--raw":
			includeRaw = true
		case "-h", "--help":
			fmt.Println(`claude-o-meter - Get Claude usage metrics as JSON

Usage: claude-o-meter [options]

Options:
  -d, --debug    Enable debug mode (includes raw output)
  -r, --raw      Include raw CLI output in JSON
  -h, --help     Show this help message

Requires the 'claude' CLI to be installed and authenticated.

Output: JSON object with usage quotas, account info, and reset times.`)
			os.Exit(0)
		}
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Execute CLI
	rawOutput, err := executeClaudeCLI(ctx, timeout)
	if err != nil {
		errResp := ErrorResponse{
			Error:   "Failed to get usage data",
			Details: err.Error(),
		}
		jsonBytes, _ := json.MarshalIndent(errResp, "", "  ")
		fmt.Fprintln(os.Stderr, string(jsonBytes))
		os.Exit(1)
	}

	// Parse output
	snapshot := parseClaudeOutput(rawOutput, includeRaw)

	// Output JSON
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
