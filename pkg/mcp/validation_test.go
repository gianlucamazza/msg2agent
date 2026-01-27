package mcp

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateDID(t *testing.T) {
	tests := []struct {
		name    string
		did     string
		wantErr error
	}{
		{
			name:    "valid did:wba",
			did:     "did:wba:example.com:agent:alice",
			wantErr: nil,
		},
		{
			name:    "valid did:key",
			did:     "did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK",
			wantErr: nil,
		},
		{
			name:    "valid did:web",
			did:     "did:web:example.com",
			wantErr: nil,
		},
		{
			name:    "valid did with dashes and dots",
			did:     "did:example:user-123.test",
			wantErr: nil,
		},
		{
			name:    "empty DID",
			did:     "",
			wantErr: ErrDIDEmpty,
		},
		{
			name:    "missing did prefix",
			did:     "wba:example.com:agent:alice",
			wantErr: ErrDIDInvalidFormat,
		},
		{
			name:    "invalid method (uppercase)",
			did:     "did:WBA:example.com:agent:alice",
			wantErr: ErrDIDInvalidFormat,
		},
		{
			name:    "too short",
			did:     "did:a:",
			wantErr: ErrDIDInvalidFormat,
		},
		{
			name:    "DID too long",
			did:     "did:wba:" + strings.Repeat("a", MaxDIDLength),
			wantErr: ErrDIDTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDID(tt.did)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateDID(%q) = %v, want nil", tt.did, err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidateDID(%q) = nil, want error containing %v", tt.did, tt.wantErr)
				} else if !errors.Is(err, tt.wantErr) && !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Errorf("ValidateDID(%q) = %v, want error containing %v", tt.did, err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidateMethod(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		wantErr error
	}{
		{
			name:    "simple method",
			method:  "ping",
			wantErr: nil,
		},
		{
			name:    "method with dots",
			method:  "relay.discover",
			wantErr: nil,
		},
		{
			name:    "method with slashes",
			method:  "tasks/get",
			wantErr: nil,
		},
		{
			name:    "method with underscores",
			method:  "get_status",
			wantErr: nil,
		},
		{
			name:    "method with dashes",
			method:  "get-status",
			wantErr: nil,
		},
		{
			name:    "empty method",
			method:  "",
			wantErr: ErrMethodEmpty,
		},
		{
			name:    "method too long",
			method:  strings.Repeat("a", MaxMethodLength+1),
			wantErr: ErrMethodTooLong,
		},
		{
			name:    "invalid character space",
			method:  "get status",
			wantErr: ErrMethodInvalidFormat,
		},
		{
			name:    "invalid character bang",
			method:  "get!status",
			wantErr: ErrMethodInvalidFormat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMethod(tt.method)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateMethod(%q) = %v, want nil", tt.method, err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidateMethod(%q) = nil, want error containing %v", tt.method, tt.wantErr)
				} else if !errors.Is(err, tt.wantErr) && !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Errorf("ValidateMethod(%q) = %v, want error containing %v", tt.method, err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidateParams(t *testing.T) {
	tests := []struct {
		name    string
		params  string
		wantErr error
	}{
		{
			name:    "empty params",
			params:  "",
			wantErr: nil,
		},
		{
			name:    "valid JSON",
			params:  `{"key": "value"}`,
			wantErr: nil,
		},
		{
			name:    "params at max size",
			params:  strings.Repeat("a", MaxParamsLength),
			wantErr: nil,
		},
		{
			name:    "params too large",
			params:  strings.Repeat("a", MaxParamsLength+1),
			wantErr: ErrParamsTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateParams(tt.params)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateParams() = %v, want nil", err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidateParams() = nil, want error containing %v", tt.wantErr)
				} else if !errors.Is(err, tt.wantErr) && !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Errorf("ValidateParams() = %v, want error containing %v", err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidateTaskID(t *testing.T) {
	tests := []struct {
		name    string
		taskID  string
		wantErr error
	}{
		{
			name:    "valid UUID",
			taskID:  "550e8400-e29b-41d4-a716-446655440000",
			wantErr: nil,
		},
		{
			name:    "valid short ID",
			taskID:  "task-123",
			wantErr: nil,
		},
		{
			name:    "empty task ID",
			taskID:  "",
			wantErr: ErrTaskIDEmpty,
		},
		{
			name:    "task ID too long",
			taskID:  strings.Repeat("a", MaxTaskIDLength+1),
			wantErr: ErrTaskIDTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTaskID(tt.taskID)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateTaskID(%q) = %v, want nil", tt.taskID, err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidateTaskID(%q) = nil, want error containing %v", tt.taskID, tt.wantErr)
				} else if !errors.Is(err, tt.wantErr) && !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Errorf("ValidateTaskID(%q) = %v, want error containing %v", tt.taskID, err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidateCapability(t *testing.T) {
	tests := []struct {
		name    string
		cap     string
		wantErr error
	}{
		{
			name:    "valid capability",
			cap:     "math",
			wantErr: nil,
		},
		{
			name:    "valid capability with dashes",
			cap:     "text-processing",
			wantErr: nil,
		},
		{
			name:    "empty capability",
			cap:     "",
			wantErr: ErrCapabilityEmpty,
		},
		{
			name:    "capability too long",
			cap:     strings.Repeat("a", MaxCapabilityLength+1),
			wantErr: ErrCapabilityTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCapability(tt.cap)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateCapability(%q) = %v, want nil", tt.cap, err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidateCapability(%q) = nil, want error containing %v", tt.cap, tt.wantErr)
				} else if !errors.Is(err, tt.wantErr) && !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Errorf("ValidateCapability(%q) = %v, want error containing %v", tt.cap, err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidateCapabilities(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr error
	}{
		{
			name:    "single capability",
			input:   "math",
			want:    []string{"math"},
			wantErr: nil,
		},
		{
			name:    "multiple capabilities",
			input:   "math,text,code",
			want:    []string{"math", "text", "code"},
			wantErr: nil,
		},
		{
			name:    "with whitespace",
			input:   " math , text , code ",
			want:    []string{"math", "text", "code"},
			wantErr: nil,
		},
		{
			name:    "empty string",
			input:   "",
			want:    nil,
			wantErr: ErrCapabilitiesEmpty,
		},
		{
			name:    "only whitespace and commas",
			input:   " , , ",
			want:    nil,
			wantErr: ErrCapabilitiesEmpty,
		},
		{
			name:    "too many capabilities",
			input:   strings.Repeat("cap,", MaxCapabilities+1) + "cap",
			want:    nil,
			wantErr: ErrTooManyCapabilities,
		},
		{
			name:    "capability too long in list",
			input:   "math," + strings.Repeat("a", MaxCapabilityLength+1),
			want:    nil,
			wantErr: ErrCapabilityTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateCapabilities(tt.input)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateCapabilities(%q) error = %v, want nil", tt.input, err)
				}
				if len(got) != len(tt.want) {
					t.Errorf("ValidateCapabilities(%q) = %v, want %v", tt.input, got, tt.want)
				}
				for i := range got {
					if got[i] != tt.want[i] {
						t.Errorf("ValidateCapabilities(%q)[%d] = %v, want %v", tt.input, i, got[i], tt.want[i])
					}
				}
			} else {
				if err == nil {
					t.Errorf("ValidateCapabilities(%q) = nil error, want error containing %v", tt.input, tt.wantErr)
				} else if !errors.Is(err, tt.wantErr) && !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Errorf("ValidateCapabilities(%q) error = %v, want error containing %v", tt.input, err, tt.wantErr)
				}
			}
		})
	}
}

func TestMCPError(t *testing.T) {
	err := NewValidationError("test error")
	if err.Code != CodeValidation {
		t.Errorf("NewValidationError().Code = %d, want %d", err.Code, CodeValidation)
	}
	if err.Retryable {
		t.Error("NewValidationError().Retryable = true, want false")
	}
	if err.Error() != "test error" {
		t.Errorf("NewValidationError().Error() = %q, want %q", err.Error(), "test error")
	}

	err = NewNotFoundError("not found")
	if err.Code != CodeNotFound {
		t.Errorf("NewNotFoundError().Code = %d, want %d", err.Code, CodeNotFound)
	}

	err = NewInternalError("internal")
	if err.Code != CodeInternal {
		t.Errorf("NewInternalError().Code = %d, want %d", err.Code, CodeInternal)
	}
	if !err.Retryable {
		t.Error("NewInternalError().Retryable = false, want true")
	}

	err = NewUnavailableError("unavailable")
	if err.Code != CodeUnavailable {
		t.Errorf("NewUnavailableError().Code = %d, want %d", err.Code, CodeUnavailable)
	}
	if !err.Retryable {
		t.Error("NewUnavailableError().Retryable = false, want true")
	}
}
