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

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

// Version is set at build time via ldflags
var Version = "dev"

// D-Bus service constants
const (
	dbusServiceName = "com.github.MartinLoeper.ClaudeOMeter"
	dbusObjectPath  = "/com/github/MartinLoeper/ClaudeOMeter"
	dbusInterface   = "com.github.MartinLoeper.ClaudeOMeter"
)

// DBusService exposes methods over D-Bus for external control
type DBusService struct {
	refreshChan chan struct{}
}

// RefreshNow triggers an immediate usage query
func (s *DBusService) RefreshNow() *dbus.Error {
	select {
	case s.refreshChan <- struct{}{}:
		log.Printf("D-Bus: RefreshNow called, triggering immediate refresh")
	default:
		log.Printf("D-Bus: RefreshNow called, refresh already pending")
	}
	return nil
}

// AccountType represents the Claude account tier
type AccountType string

const (
	AccountTypePro     AccountType = "pro"
	AccountTypeMax     AccountType = "max"
	AccountTypeAPI     AccountType = "api"
	AccountTypeUnknown AccountType = "unknown"
)

// AuthErrorCode represents specific authentication error types
type AuthErrorCode string

const (
	AuthErrorNone           AuthErrorCode = ""
	AuthErrorNotLoggedIn    AuthErrorCode = "not_logged_in"
	AuthErrorTokenExpired   AuthErrorCode = "token_expired"
	AuthErrorNoSubscription AuthErrorCode = "no_subscription"
	AuthErrorSetupRequired  AuthErrorCode = "setup_required"
)

// AuthError represents an authentication-related error
type AuthError struct {
	Code    AuthErrorCode
	Message string
}

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
	AccountType  AccountType    `json:"account_type"`
	Email        string         `json:"email,omitempty"`
	Organization string         `json:"organization,omitempty"`
	Quotas       []Quota        `json:"quotas"`
	CostUsage    *CostUsage     `json:"cost_usage,omitempty"`
	AuthError    *AuthError     `json:"auth_error,omitempty"`
	CapturedAt   string         `json:"captured_at"`
	RawOutput    string         `json:"raw_output,omitempty"`
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

	// Authentication error patterns
	// Login prompt patterns - these indicate the user needs to authenticate
	loginPromptPattern = regexp.MustCompile(`(?i)(sign\s*in|log\s*in|authenticate)\s*(to\s+continue|required|to\s+use)`)
	loginURLPattern    = regexp.MustCompile(`(?i)https?://[^\s]*(?:login|auth|signin)[^\s]*`)

	// Token/session expiration patterns
	tokenExpiredPattern = regexp.MustCompile(`(?i)(token|session)\s*(has\s+)?expired`)
	authErrorPattern    = regexp.MustCompile(`(?i)authentication[_\s]*(error|failed|required)`)

	// No subscription patterns - user is logged in but doesn't have Pro/Max
	noSubscriptionPattern = regexp.MustCompile(`(?i)(free\s+tier|no\s+(active\s+)?subscription|upgrade\s+to\s+(pro|max)|subscribe\s+to)`)

	// Generic not logged in indicators
	notLoggedInPattern = regexp.MustCompile(`(?i)(not\s+logged\s+in|please\s+(log|sign)\s*in|login\s+required)`)

	// First-run setup screen pattern - "Let's get started" with theme selection
	// Note: Handle various apostrophe types and be lenient with whitespace
	setupRequiredPattern  = regexp.MustCompile(`(?i)let.?s\s+get\s+started`)
	themeSelectionPattern = regexp.MustCompile(`(?i)(choose\s+(the\s+)?text\s+style|run\s+/theme|dark\s+mode|light\s+mode)`)
)

func stripANSI(text string) string {
	return ansiPattern.ReplaceAllString(text, "")
}

// detectAuthError checks the CLI output for authentication-related errors
// Returns nil if no auth error is detected
func detectAuthError(text string) *AuthError {
	textLower := strings.ToLower(text)

	// Check for first-run setup screen (Let's get started / theme selection)
	if setupRequiredPattern.MatchString(text) || themeSelectionPattern.MatchString(text) {
		return &AuthError{
			Code:    AuthErrorSetupRequired,
			Message: "Claude CLI setup required. Please run 'claude' to complete initial setup.",
		}
	}

	// Check for token expiration first (most specific)
	if tokenExpiredPattern.MatchString(text) {
		return &AuthError{
			Code:    AuthErrorTokenExpired,
			Message: "Claude CLI session has expired. Please run 'claude' to re-authenticate.",
		}
	}

	// Check for authentication errors
	if authErrorPattern.MatchString(text) {
		return &AuthError{
			Code:    AuthErrorNotLoggedIn,
			Message: "Authentication error. Please run 'claude' to log in.",
		}
	}

	// Check for explicit not logged in messages
	if notLoggedInPattern.MatchString(text) {
		return &AuthError{
			Code:    AuthErrorNotLoggedIn,
			Message: "Not logged in to Claude CLI. Please run 'claude' to authenticate.",
		}
	}

	// Check for login prompts (sign in, log in, etc.)
	if loginPromptPattern.MatchString(text) || loginURLPattern.MatchString(text) {
		return &AuthError{
			Code:    AuthErrorNotLoggedIn,
			Message: "Login required. Please run 'claude' to authenticate.",
		}
	}

	// Check for no subscription (user is logged in but doesn't have Pro/Max)
	if noSubscriptionPattern.MatchString(text) {
		return &AuthError{
			Code:    AuthErrorNoSubscription,
			Message: "No active Claude Pro or Max subscription. Usage metrics require a paid plan.",
		}
	}

	// Additional heuristic: if we see "claude" mentioned but no percentage data,
	// and there's mention of "account" or "subscription", it might be a subscription issue
	if strings.Contains(textLower, "account") || strings.Contains(textLower, "subscription") {
		if !strings.Contains(text, "% used") && !strings.Contains(text, "% left") {
			// Only flag this if we have some indication it's about authentication
			if strings.Contains(textLower, "verify") || strings.Contains(textLower, "confirm") {
				return &AuthError{
					Code:    AuthErrorNotLoggedIn,
					Message: "Authentication verification required. Please run 'claude' to verify your account.",
				}
			}
		}
	}

	return nil
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

// recalculateTimeRemaining recalculates time remaining from a ResetsAt timestamp
func recalculateTimeRemaining(resetsAt *string) string {
	if resetsAt == nil {
		return "unknown"
	}
	resetTime, err := time.Parse(time.RFC3339, *resetsAt)
	if err != nil {
		return "unknown"
	}
	seconds := int64(time.Until(resetTime).Seconds())
	if seconds <= 0 {
		return "0m"
	}
	return formatDuration(seconds)
}

// calculateNextResetRefresh finds the earliest quota reset time and returns
// a duration for when to schedule the next refresh (60 seconds after reset).
// Returns nil if no valid reset times are found.
func calculateNextResetRefresh(quotas []Quota) *time.Duration {
	var minSeconds int64 = -1

	for _, q := range quotas {
		if q.TimeRemainingSeconds != nil && *q.TimeRemainingSeconds > 0 {
			if minSeconds < 0 || *q.TimeRemainingSeconds < minSeconds {
				minSeconds = *q.TimeRemainingSeconds
			}
		}
	}

	if minSeconds < 0 {
		return nil
	}

	// Schedule refresh 60 seconds after the reset
	refreshDelay := time.Duration(minSeconds+60) * time.Second
	return &refreshDelay
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
	// Run claude from /tmp to avoid permission prompts for home directory
	// Use script command for PTY
	cmd := exec.CommandContext(ctx, "script", "-q", "-c", "claude /usage", "/dev/null")
	cmd.Dir = "/tmp"

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

	// Helper to check if output contains usage data
	hasUsageData := func(output string) bool {
		return strings.Contains(output, "% used") || strings.Contains(output, "% left")
	}

	// Helper to check if output indicates an auth error (so we can stop waiting)
	hasAuthError := func(output string) bool {
		cleanOutput := stripANSI(output)
		return detectAuthError(cleanOutput) != nil
	}

	for {
		select {
		case <-ctx.Done():
			// Kill the entire process group to ensure script and its children die
			if cmd.Process != nil {
				syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			// Check if we got data before timing out
			output := stdout.String()
			if hasUsageData(output) || hasAuthError(output) {
				return output, nil
			}
			return output, fmt.Errorf("command timed out after %v", timeout)

		case err := <-done:
			// Command finished on its own
			output := stdout.String()
			if hasUsageData(output) || hasAuthError(output) {
				return output, nil
			}
			if err != nil {
				return "", fmt.Errorf("failed to execute claude CLI: %w", err)
			}
			return output, nil

		case <-ticker.C:
			// Check if we have usage data or auth error yet
			output := stdout.String()
			if hasUsageData(output) {
				// Give it a moment to finish rendering, then kill the process group
				time.Sleep(300 * time.Millisecond)
				if cmd.Process != nil {
					syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}
				return stdout.String(), nil
			}
			// Also check for auth errors - no point waiting for usage data if not logged in
			if hasAuthError(output) {
				// Give it a moment to capture the full error message
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
	// Check for auth errors first
	if snapshot != nil && snapshot.AuthError != nil {
		return formatHyprPanelAuthError(snapshot.AuthError)
	}

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
	// Recalculate time remaining from ResetsAt to avoid stale values
	sessionTime := recalculateTimeRemaining(snapshot.Quotas[0].ResetsAt)

	// Calculate weekly usage if available
	weeklyUsed := 0.0
	weeklyTime := "unknown"
	if len(snapshot.Quotas) > 1 {
		weeklyUsed = 100 - snapshot.Quotas[1].PercentRemaining
		weeklyTime = recalculateTimeRemaining(snapshot.Quotas[1].ResetsAt)
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

	// Determine account label for display
	accountLabel := "Claude"
	switch snapshot.AccountType {
	case AccountTypeMax:
		accountLabel = "Max"
	case AccountTypePro:
		accountLabel = "Pro"
	}

	return &HyprPanelOutput{
		Text:    fmt.Sprintf("%.0f%% %s", sessionUsed, accountLabel),
		Alt:     level,
		Class:   level,
		Tooltip: strings.Join(tooltipLines, "\n"),
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

// formatHyprPanelAuthError returns an auth error HyprPanelOutput with appropriate styling
func formatHyprPanelAuthError(authErr *AuthError) *HyprPanelOutput {
	if authErr == nil {
		return formatHyprPanelError("Unknown error")
	}

	// Use different alt/class based on error type for potential icon customization
	alt := "auth_error"
	switch authErr.Code {
	case AuthErrorNotLoggedIn:
		alt = "not_logged_in"
	case AuthErrorTokenExpired:
		alt = "token_expired"
	case AuthErrorNoSubscription:
		alt = "no_subscription"
	case AuthErrorSetupRequired:
		alt = "setup_required"
	}

	return &HyprPanelOutput{
		Text:    "Claude",
		Alt:     alt,
		Class:   "auth_error",
		Tooltip: authErr.Message,
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
		AuthError:    detectAuthError(cleanOutput),
		CapturedAt:   time.Now().Format(time.RFC3339),
	}

	if includeRaw {
		snapshot.RawOutput = cleanOutput
	}

	// If we have an auth error and no quotas, ensure account type reflects the issue
	if snapshot.AuthError != nil && len(snapshot.Quotas) == 0 {
		snapshot.AccountType = AccountTypeUnknown
	}

	return snapshot
}

// runQuery executes a single query and returns the snapshot, raw CLI output, and error.
// The raw output is always returned (even on error) for debugging purposes.
func runQuery(includeRaw bool, timeout time.Duration, debug bool) (*UsageSnapshot, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	rawOutput, err := executeClaudeCLI(ctx, timeout, debug)
	if err != nil {
		return nil, rawOutput, err
	}

	return parseClaudeOutput(rawOutput, includeRaw), rawOutput, nil
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

// startDBusService registers the D-Bus service and blocks forever
func startDBusService(refreshChan chan struct{}) {
	conn, err := dbus.SessionBus()
	if err != nil {
		log.Printf("Failed to connect to session bus: %v", err)
		return
	}

	service := &DBusService{refreshChan: refreshChan}

	// Export the service methods
	err = conn.Export(service, dbus.ObjectPath(dbusObjectPath), dbusInterface)
	if err != nil {
		log.Printf("Failed to export D-Bus service: %v", err)
		return
	}

	// Export introspection data for dbus-send compatibility
	introNode := &introspect.Node{
		Name: dbusObjectPath,
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			{
				Name: dbusInterface,
				Methods: []introspect.Method{
					{
						Name: "RefreshNow",
					},
				},
			},
		},
	}
	err = conn.Export(introspect.NewIntrospectable(introNode), dbus.ObjectPath(dbusObjectPath),
		"org.freedesktop.DBus.Introspectable")
	if err != nil {
		log.Printf("Failed to export introspection: %v", err)
		return
	}

	// Request the service name
	reply, err := conn.RequestName(dbusServiceName, dbus.NameFlagDoNotQueue)
	if err != nil {
		log.Printf("Failed to request D-Bus name: %v", err)
		return
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		log.Printf("D-Bus name %s already taken", dbusServiceName)
		return
	}

	log.Printf("D-Bus service registered: %s", dbusServiceName)
	select {} // Block forever, methods are called in separate goroutines
}

// sendNotification sends a desktop notification via D-Bus (org.freedesktop.Notifications)
func sendNotification(summary, body, iconPath string, timeoutMs int32) error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return fmt.Errorf("failed to connect to session bus: %w", err)
	}

	obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	call := obj.Call("org.freedesktop.Notifications.Notify", 0,
		"claude-o-meter",              // app_name
		uint32(0),                     // replaces_id (0 = new notification)
		iconPath,                      // app_icon
		summary,                       // summary
		body,                          // body
		[]string{},                    // actions (empty for simple notification)
		map[string]dbus.Variant{},     // hints (empty for basic notification)
		timeoutMs,                     // expire_timeout (-1 = server default, 0 = never, >0 = ms)
	)

	if call.Err != nil {
		return fmt.Errorf("failed to send notification: %w", call.Err)
	}

	return nil
}

// NotifyConfig holds notification configuration for the daemon
type NotifyConfig struct {
	Threshold int    // Percentage threshold (0-100), 0 = disabled
	TimeoutMs int32  // Notification timeout in milliseconds (-1 = server default, 0 = never)
	IconPath  string // Path to icon file
}

// runDaemon runs the query in a loop, writing results to the output file
func runDaemon(interval time.Duration, outputFile string, timeout time.Duration, debug bool, enableDbus bool, notifyConfig *NotifyConfig) {
	log.Printf("Starting daemon: interval=%s, output=%s, debug=%v, dbus=%v", interval, outputFile, debug, enableDbus)
	if notifyConfig != nil && notifyConfig.Threshold > 0 {
		log.Printf("Notifications enabled: threshold=%d%%, timeout=%dms, icon=%s",
			notifyConfig.Threshold, notifyConfig.TimeoutMs, notifyConfig.IconPath)
	}

	// Create refresh channel for D-Bus triggers
	refreshChan := make(chan struct{}, 1)

	// Start D-Bus service if enabled
	if enableDbus {
		go startDBusService(refreshChan)
	}

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Reset timer for auto-refresh when quota resets
	var resetTimer *time.Timer
	var resetTimerChan <-chan time.Time

	// scheduleResetRefresh calculates and schedules the next reset-based refresh
	scheduleResetRefresh := func(quotas []Quota) {
		// Stop existing timer if any and drain channel if it already fired
		if resetTimer != nil {
			if !resetTimer.Stop() {
				select {
				case <-resetTimer.C:
				default:
				}
			}
		}

		delay := calculateNextResetRefresh(quotas)
		if delay == nil {
			resetTimerChan = nil
			return
		}

		resetTimer = time.NewTimer(*delay)
		resetTimerChan = resetTimer.C
		log.Printf("Scheduled reset refresh in %s", delay)
	}

	// Track notification state to avoid spamming
	// Reset when usage drops below threshold
	notificationSent := false

	// Track query success for retry behavior.
	// On failure, retry at a fixed 1-minute interval until success.
	lastQuerySucceeded := true
	retryInterval := 1 * time.Minute

	// Run immediately on start
	doQuery := func() bool {
		snapshot, rawOutput, err := runQuery(false, timeout, debug)
		if err != nil {
			log.Printf("Query failed: %v", err)
			// Log raw CLI output for debugging
			if rawOutput != "" {
				log.Printf("Raw CLI output:\n%s", stripANSI(rawOutput))
			}
			// Write error response to file so consumers know there was an issue
			errResp := &UsageSnapshot{
				AccountType: AccountTypeUnknown,
				CapturedAt:  time.Now().Format(time.RFC3339),
			}
			if writeErr := writeSnapshotToFile(errResp, outputFile); writeErr != nil {
				log.Printf("Failed to write error state: %v", writeErr)
			}
			return false
		}

		// Check for authentication errors
		if snapshot.AuthError != nil {
			log.Printf("Authentication error: %s - %s", snapshot.AuthError.Code, snapshot.AuthError.Message)
		}

		if err := writeSnapshotToFile(snapshot, outputFile); err != nil {
			log.Printf("Failed to write snapshot: %v", err)
			return false
		}

		if snapshot.AuthError != nil {
			// Already logged above, just note the write succeeded
			log.Printf("Auth error state written to file")
		} else if len(snapshot.Quotas) > 0 {
			log.Printf("Query successful: %s quota at %.0f%%",
				snapshot.AccountType,
				100-snapshot.Quotas[0].PercentRemaining)

			// Check if notification threshold is exceeded (session quota only)
			if notifyConfig != nil && notifyConfig.Threshold > 0 {
				sessionUsed := 100 - snapshot.Quotas[0].PercentRemaining
				if sessionUsed >= float64(notifyConfig.Threshold) {
					if !notificationSent {
						err := sendNotification(
							"Claude Usage High",
							fmt.Sprintf("Session usage at %.0f%% (threshold: %d%%)", sessionUsed, notifyConfig.Threshold),
							notifyConfig.IconPath,
							notifyConfig.TimeoutMs,
						)
						if err != nil {
							log.Printf("Failed to send notification: %v", err)
						} else {
							log.Printf("Notification sent: session usage at %.0f%%", sessionUsed)
							notificationSent = true
						}
					}
				} else {
					// Reset notification state when usage drops below threshold
					if notificationSent {
						log.Printf("Usage dropped below threshold, notification reset")
					}
					notificationSent = false
				}
			}
		} else {
			log.Printf("Query returned no quota data")
		}

		// Schedule next reset-based refresh
		scheduleResetRefresh(snapshot.Quotas)
		return true
	}

	lastQuerySucceeded = doQuery()
	if !lastQuerySucceeded {
		ticker.Reset(retryInterval)
		log.Printf("Initial query failed, retrying in %s", retryInterval)
	}

	for {
		select {
		case <-ticker.C:
			wasSuccessful := lastQuerySucceeded
			lastQuerySucceeded = doQuery()
			if !lastQuerySucceeded && wasSuccessful {
				// Just failed - switch to retry interval
				ticker.Reset(retryInterval)
				log.Printf("Switching to retry interval: %s", retryInterval)
			} else if lastQuerySucceeded && !wasSuccessful {
				// Just recovered - switch back to normal interval
				ticker.Reset(interval)
				log.Printf("Query recovered, resuming normal interval: %s", interval)
			}
		case <-refreshChan:
			log.Printf("D-Bus refresh requested")
			wasSuccessful := lastQuerySucceeded
			lastQuerySucceeded = doQuery()
			if lastQuerySucceeded {
				ticker.Reset(interval) // Reset timer after successful manual refresh
				if !wasSuccessful {
					log.Printf("Query recovered, resuming normal interval: %s", interval)
				}
			} else {
				// Failed via D-Bus trigger - ensure retry interval is applied/refreshed
				ticker.Reset(retryInterval)
				if wasSuccessful {
					log.Printf("Switching to retry interval: %s", retryInterval)
				}
			}
		case <-resetTimerChan:
			log.Printf("Quota reset timer fired, refreshing...")
			wasSuccessful := lastQuerySucceeded
			lastQuerySucceeded = doQuery()
			if lastQuerySucceeded {
				ticker.Reset(interval) // Reset regular ticker after successful reset refresh
				if !wasSuccessful {
					log.Printf("Query recovered, resuming normal interval: %s", interval)
				}
			} else {
				// Failed via reset trigger - ensure retry interval is applied/refreshed
				ticker.Reset(retryInterval)
				if wasSuccessful {
					log.Printf("Switching to retry interval: %s", retryInterval)
				}
			}
		case sig := <-sigChan:
			log.Printf("Received signal %v, shutting down...", sig)
			if resetTimer != nil {
				resetTimer.Stop()
			}
			return
		}
	}
}

func printUsage() {
	fmt.Printf(`claude-o-meter %s - Get Claude usage metrics as JSON

Usage: claude-o-meter <command> [options]

Commands:
  query     Query usage once and output to stdout (default if no command given)
  daemon    Run as a daemon, periodically querying and writing to file
  hyprpanel Read from file and output HyprPanel-compatible JSON
  refresh   Trigger immediate daemon refresh via D-Bus

Global options:
  -v, --version         Show version
  -h, --help            Show help

Query options:
  -d, --debug           Enable debug mode (includes raw output)
  -r, --raw             Include raw CLI output in JSON
  --hyprpanel-json      Output in HyprPanel module format

Daemon options:
  -i, --interval        Query interval (default: 60s)
  -f, --file            Output file path (required)
  -b, --dbus            Enable D-Bus service for external refresh triggers
  --debug               Print claude CLI output in real-time
  -t, --notify-threshold  Notify when session usage >= this %% (0 = disabled)
  --notify-timeout      Notification display timeout (e.g., 5s; 0 = never)
  --notify-icon         Path to notification icon (PNG/SVG)

HyprPanel options:
  -f, --file       Input file path (required)

Refresh options:
  -d, --debug      Print confirmation message

Examples:
  claude-o-meter                           # Query once, output to stdout
  claude-o-meter query                     # Same as above
  claude-o-meter query --raw               # Include raw CLI output
  claude-o-meter query --hyprpanel-json    # Output for HyprPanel (one-shot)
  claude-o-meter daemon -i 60s -f /tmp/claude.json -b
  claude-o-meter hyprpanel -f /tmp/claude.json  # Read file, output HyprPanel JSON
  claude-o-meter refresh                        # Trigger daemon to refresh now

Requires the 'claude' CLI to be installed and authenticated.
`, Version)
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
	case "refresh":
		runRefreshCommand(os.Args[2:])
	case "-h", "--help", "help":
		printUsage()
		os.Exit(0)
	case "-v", "--version", "version":
		fmt.Printf("claude-o-meter %s\n", Version)
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
	debugMode := *debug || *debugLong
	timeout := 30 * time.Second

	snapshot, rawOutput, err := runQuery(includeRaw, timeout, debugMode)
	if err != nil {
		// Print raw CLI output for debugging (mimics --debug behavior on failure)
		if rawOutput != "" {
			fmt.Fprintln(os.Stderr, "--- Raw CLI Output ---")
			fmt.Fprintln(os.Stderr, stripANSI(rawOutput))
			fmt.Fprintln(os.Stderr, "---")
		}
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
	enableDbus := daemonFlags.Bool("b", false, "Enable D-Bus service for external refresh triggers")
	enableDbusLong := daemonFlags.Bool("dbus", false, "Enable D-Bus service for external refresh triggers")
	debug := daemonFlags.Bool("debug", false, "Print claude CLI output in real-time")
	notifyThreshold := daemonFlags.Int("t", 0, "Notify when session usage >= this percentage (0 = disabled)")
	notifyThresholdLong := daemonFlags.Int("notify-threshold", 0, "Notify when session usage >= this percentage (0 = disabled)")
	notifyTimeout := daemonFlags.Duration("notify-timeout", 0, "Notification display timeout (0 = never auto-close, default = server decides)")
	notifyIcon := daemonFlags.String("notify-icon", "", "Path to notification icon (PNG/SVG)")
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

	actualEnableDbus := *enableDbus || *enableDbusLong

	// Determine notification threshold
	actualNotifyThreshold := *notifyThreshold
	if *notifyThresholdLong != 0 {
		actualNotifyThreshold = *notifyThresholdLong
	}

	// Validate threshold
	if actualNotifyThreshold < 0 || actualNotifyThreshold > 100 {
		fmt.Fprintln(os.Stderr, "Error: --notify-threshold must be between 0 and 100")
		os.Exit(1)
	}

	if actualOutputFile == "" {
		fmt.Fprintln(os.Stderr, "Error: -f/--file is required for daemon mode")
		os.Exit(1)
	}

	// Build notification config if threshold is set
	var notifyConfig *NotifyConfig
	if actualNotifyThreshold > 0 {
		// Convert timeout duration to milliseconds
		// 0 duration means "never auto-close" (0 in DBus)
		// If user didn't set it, use -1 to let server decide
		var timeoutMs int32 = -1 // server default
		if *notifyTimeout > 0 {
			timeoutMs = int32(notifyTimeout.Milliseconds())
		} else if *notifyTimeout == 0 && daemonFlags.Lookup("notify-timeout").Value.String() != "0s" {
			// User didn't specify, use server default
			timeoutMs = -1
		} else {
			// User explicitly set 0, means never auto-close
			timeoutMs = 0
		}

		notifyConfig = &NotifyConfig{
			Threshold: actualNotifyThreshold,
			TimeoutMs: timeoutMs,
			IconPath:  *notifyIcon,
		}
	}

	timeout := 30 * time.Second
	runDaemon(actualInterval, actualOutputFile, timeout, *debug, actualEnableDbus, notifyConfig)
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

	// Wait for file to exist (blocks until daemon has written)
	for {
		if _, err := os.Stat(actualInputFile); err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
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

	// Check for auth errors first
	if snapshot.AuthError != nil {
		output := formatHyprPanelAuthError(snapshot.AuthError)
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

func runRefreshCommand(args []string) {
	refreshFlags := flag.NewFlagSet("refresh", flag.ExitOnError)
	debug := refreshFlags.Bool("d", false, "Enable debug output")
	debugLong := refreshFlags.Bool("debug", false, "Enable debug output")
	help := refreshFlags.Bool("h", false, "Show help")
	helpLong := refreshFlags.Bool("help", false, "Show help")

	refreshFlags.Parse(args)

	if *help || *helpLong {
		printUsage()
		os.Exit(0)
	}

	debugMode := *debug || *debugLong

	conn, err := dbus.SessionBus()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to session bus: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	obj := conn.Object(dbusServiceName, dbusObjectPath)
	call := obj.Call(dbusInterface+".RefreshNow", 0)
	if call.Err != nil {
		fmt.Fprintf(os.Stderr, "Failed to call RefreshNow: %v\n", call.Err)
		os.Exit(1)
	}

	if debugMode {
		fmt.Println("Refresh triggered successfully")
	}
}
