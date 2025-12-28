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
