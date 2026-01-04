package main

import (
	"testing"
)

func TestDetectAuthError(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantCode   AuthErrorCode
		wantNil    bool
	}{
		{
			name:     "token expired",
			input:    "Your token has expired. Please log in again.",
			wantCode: AuthErrorTokenExpired,
		},
		{
			name:     "session expired",
			input:    "Your session expired. Re-authenticate to continue.",
			wantCode: AuthErrorTokenExpired,
		},
		{
			name:     "authentication error underscore",
			input:    "authentication_error: invalid credentials",
			wantCode: AuthErrorNotLoggedIn,
		},
		{
			name:     "authentication failed",
			input:    "Authentication failed. Please try again.",
			wantCode: AuthErrorNotLoggedIn,
		},
		{
			name:     "not logged in explicit",
			input:    "You are not logged in. Please sign in to continue.",
			wantCode: AuthErrorNotLoggedIn,
		},
		{
			name:     "please log in",
			input:    "Please log in to use this feature.",
			wantCode: AuthErrorNotLoggedIn,
		},
		{
			name:     "login required",
			input:    "Login required to access usage metrics.",
			wantCode: AuthErrorNotLoggedIn,
		},
		{
			name:     "sign in to continue",
			input:    "Please sign in to continue using Claude.",
			wantCode: AuthErrorNotLoggedIn,
		},
		{
			name:     "login URL",
			input:    "Visit https://claude.ai/login to authenticate",
			wantCode: AuthErrorNotLoggedIn,
		},
		{
			name:     "auth URL",
			input:    "Go to https://anthropic.com/auth/signin to sign in",
			wantCode: AuthErrorNotLoggedIn,
		},
		{
			name:     "free tier",
			input:    "You are on the free tier. Upgrade to Pro for more features.",
			wantCode: AuthErrorNoSubscription,
		},
		{
			name:     "no subscription",
			input:    "No active subscription found.",
			wantCode: AuthErrorNoSubscription,
		},
		{
			name:     "upgrade to pro",
			input:    "Upgrade to Pro to access usage metrics.",
			wantCode: AuthErrorNoSubscription,
		},
		{
			name:     "setup required - let's get started",
			input:    "Let's get started.\n\n Choose the text style that looks best with your terminal",
			wantCode: AuthErrorSetupRequired,
		},
		{
			name:     "setup required - theme selection",
			input:    "Choose the text style that looks best\nTo change this later, run /theme",
			wantCode: AuthErrorSetupRequired,
		},
		{
			name:     "normal usage - no error",
			input:    "Current session: 50% used. Resets at 6am",
			wantNil:  true,
		},
		{
			name:     "quota data - no error",
			input:    "11% used\nResets 5:59pm (Europe/Berlin)",
			wantNil:  true,
		},
		{
			name:     "empty string - no error",
			input:    "",
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectAuthError(tt.input)
			if tt.wantNil {
				if got != nil {
					t.Errorf("detectAuthError() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Errorf("detectAuthError() = nil, want code %v", tt.wantCode)
				return
			}
			if got.Code != tt.wantCode {
				t.Errorf("detectAuthError().Code = %v, want %v", got.Code, tt.wantCode)
			}
			if got.Message == "" {
				t.Error("detectAuthError().Message should not be empty")
			}
		})
	}
}

func TestFormatHyprPanelAuthError(t *testing.T) {
	tests := []struct {
		name      string
		authError *AuthError
		wantText  string
		wantAlt   string
		wantClass string
	}{
		{
			name: "not logged in",
			authError: &AuthError{
				Code:    AuthErrorNotLoggedIn,
				Message: "Not logged in",
			},
			wantText:  "Claude",
			wantAlt:   "not_logged_in",
			wantClass: "auth_error",
		},
		{
			name: "token expired",
			authError: &AuthError{
				Code:    AuthErrorTokenExpired,
				Message: "Token expired",
			},
			wantText:  "Claude",
			wantAlt:   "token_expired",
			wantClass: "auth_error",
		},
		{
			name: "no subscription",
			authError: &AuthError{
				Code:    AuthErrorNoSubscription,
				Message: "No subscription",
			},
			wantText:  "Claude",
			wantAlt:   "no_subscription",
			wantClass: "auth_error",
		},
		{
			name: "setup required",
			authError: &AuthError{
				Code:    AuthErrorSetupRequired,
				Message: "Setup required",
			},
			wantText:  "Claude",
			wantAlt:   "setup_required",
			wantClass: "auth_error",
		},
		{
			name:      "nil error",
			authError: nil,
			wantText:  "--",
			wantAlt:   "error",
			wantClass: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatHyprPanelAuthError(tt.authError)
			if got.Text != tt.wantText {
				t.Errorf("formatHyprPanelAuthError().Text = %v, want %v", got.Text, tt.wantText)
			}
			if got.Alt != tt.wantAlt {
				t.Errorf("formatHyprPanelAuthError().Alt = %v, want %v", got.Alt, tt.wantAlt)
			}
			if got.Class != tt.wantClass {
				t.Errorf("formatHyprPanelAuthError().Class = %v, want %v", got.Class, tt.wantClass)
			}
		})
	}
}

func TestIsQuotaSectionMarker(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{
			name: "current session marker",
			line: "Current session",
			want: true,
		},
		{
			name: "current week all models",
			line: "Current week (all models)",
			want: true,
		},
		{
			name: "current week opus",
			line: "Current week (opus)",
			want: true,
		},
		{
			name: "opus usage",
			line: "Opus usage",
			want: true,
		},
		{
			name: "sonnet usage",
			line: "Sonnet usage",
			want: true,
		},
		{
			name: "reset line - not a marker",
			line: "Resets 5d 3h",
			want: false,
		},
		{
			name: "percentage line - not a marker",
			line: "50% used",
			want: false,
		},
		{
			name: "empty line - not a marker",
			line: "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isQuotaSectionMarker(tt.line)
			if got != tt.want {
				t.Errorf("isQuotaSectionMarker(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestParseResetTime_StopsAtQuotaBoundary(t *testing.T) {
	// This test verifies that parseResetTime stops searching when it encounters
	// another quota section marker, preventing it from matching the wrong reset time.
	lines := []string{
		"Current session",           // 0
		"0% used",                    // 1 - startIdx
		"",                           // 2 - no reset info for session
		"Current week (all models)", // 3 - quota boundary, should stop here
		"50% used",                   // 4
		"Resets 5d 3h",               // 5 - this should NOT be matched for session
	}

	resetText, resetTime, duration := parseResetTime(lines, 1)

	// Should return empty since no reset was found before the quota boundary
	if resetText != "" {
		t.Errorf("parseResetTime should return empty resetText when stopped by quota boundary, got %q", resetText)
	}
	if resetTime != nil {
		t.Errorf("parseResetTime should return nil resetTime when stopped by quota boundary, got %v", resetTime)
	}
	if duration != nil {
		t.Errorf("parseResetTime should return nil duration when stopped by quota boundary, got %v", duration)
	}
}

func TestParseResetTime_FindsResetBeforeBoundary(t *testing.T) {
	// This test verifies that parseResetTime still finds reset times
	// that appear before a quota boundary.
	lines := []string{
		"Current session",           // 0
		"50% used",                   // 1 - startIdx
		"Resets 2h 30m",              // 2 - reset info for session
		"",                           // 3
		"Current week (all models)", // 4 - quota boundary
		"50% used",                   // 5
		"Resets 5d 3h",               // 6 - weekly reset
	}

	resetText, resetTime, duration := parseResetTime(lines, 1)

	if resetText == "" {
		t.Error("parseResetTime should find reset text before quota boundary")
	}
	if resetTime == nil {
		t.Error("parseResetTime should find reset time before quota boundary")
	}
	if duration == nil {
		t.Error("parseResetTime should find duration before quota boundary")
	} else {
		// 2h 30m = 9000 seconds
		expectedSeconds := int64(2*60*60 + 30*60)
		// Allow some tolerance for time passing during test
		if *duration < expectedSeconds-5 || *duration > expectedSeconds+5 {
			t.Errorf("parseResetTime duration = %d, want ~%d", *duration, expectedSeconds)
		}
	}
}

func TestParseQuotas_SessionResetNotMatchedFromWeekly(t *testing.T) {
	// This test simulates the bug scenario: session at 0% with no reset time,
	// followed by weekly quota with a reset time.
	// The session quota should NOT get the weekly reset time.
	input := `· Claude Max · user@example.com
│
│  Current session
│  0% used
│
│  Current week (all models)
│  50% used
│  Resets 5d 3h
│`

	quotas := parseQuotas(input)

	if len(quotas) < 2 {
		t.Fatalf("expected at least 2 quotas, got %d", len(quotas))
	}

	// Find session quota
	var sessionQuota *Quota
	var weeklyQuota *Quota
	for i := range quotas {
		if quotas[i].Type == QuotaTypeSession {
			sessionQuota = &quotas[i]
		}
		if quotas[i].Type == QuotaTypeWeekly {
			weeklyQuota = &quotas[i]
		}
	}

	if sessionQuota == nil {
		t.Fatal("session quota not found")
	}
	if weeklyQuota == nil {
		t.Fatal("weekly quota not found")
	}

	// Session should have 100% remaining (0% used)
	if sessionQuota.PercentRemaining != 100 {
		t.Errorf("session PercentRemaining = %v, want 100", sessionQuota.PercentRemaining)
	}

	// Session should NOT have a reset time (since there was none in its section)
	if sessionQuota.TimeRemainingSeconds != nil {
		t.Errorf("session TimeRemainingSeconds should be nil (no reset in section), got %v", *sessionQuota.TimeRemainingSeconds)
	}

	// Weekly should have the reset time
	if weeklyQuota.TimeRemainingSeconds == nil {
		t.Error("weekly TimeRemainingSeconds should not be nil")
	} else {
		// 5d 3h = 5*24*60*60 + 3*60*60 = 442800 seconds
		expectedSeconds := int64(5*24*60*60 + 3*60*60)
		if *weeklyQuota.TimeRemainingSeconds < expectedSeconds-5 || *weeklyQuota.TimeRemainingSeconds > expectedSeconds+5 {
			t.Errorf("weekly TimeRemainingSeconds = %d, want ~%d", *weeklyQuota.TimeRemainingSeconds, expectedSeconds)
		}
	}
}
