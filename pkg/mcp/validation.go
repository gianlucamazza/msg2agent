package mcp

import (
	"fmt"
	"regexp"
	"strings"
)

// Input validation limits to prevent DoS attacks.
const (
	MaxDIDLength        = 2048
	MaxParamsLength     = 1 << 20 // 1MB
	MaxCapabilityLength = 256
	MaxMethodLength     = 128
	MaxTaskIDLength     = 128
	MaxCapabilities     = 100 // Max capabilities in a query
)

// DID format: did:<method>:<identifier>[:<path>]*
// Examples:
//   - did:wba:example.com:agent:alice
//   - did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK
//   - did:web:example.com
var didRegex = regexp.MustCompile(`^did:[a-z0-9]+:[a-zA-Z0-9._:%-]+$`)

// ValidateDID validates a DID string format.
func ValidateDID(did string) error {
	if did == "" {
		return ErrDIDEmpty
	}
	if len(did) > MaxDIDLength {
		return fmt.Errorf("%w: %d > %d", ErrDIDTooLong, len(did), MaxDIDLength)
	}
	if !didRegex.MatchString(did) {
		return fmt.Errorf("%w: %s", ErrDIDInvalidFormat, did)
	}
	return nil
}

// ValidateMethod validates a method name.
func ValidateMethod(method string) error {
	if method == "" {
		return ErrMethodEmpty
	}
	if len(method) > MaxMethodLength {
		return fmt.Errorf("%w: %d > %d", ErrMethodTooLong, len(method), MaxMethodLength)
	}
	// Method should be alphanumeric with dots, slashes, and underscores
	for _, c := range method {
		if !isMethodChar(c) {
			return fmt.Errorf("%w: invalid character '%c'", ErrMethodInvalidFormat, c)
		}
	}
	return nil
}

func isMethodChar(c rune) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '.' || c == '/' || c == '_' || c == '-'
}

// ValidateParams validates a JSON params string.
func ValidateParams(params string) error {
	if len(params) > MaxParamsLength {
		return fmt.Errorf("%w: %d > %d", ErrParamsTooLarge, len(params), MaxParamsLength)
	}
	return nil
}

// ValidateTaskID validates a task ID (typically a UUID).
func ValidateTaskID(taskID string) error {
	if taskID == "" {
		return ErrTaskIDEmpty
	}
	if len(taskID) > MaxTaskIDLength {
		return fmt.Errorf("%w: %d > %d", ErrTaskIDTooLong, len(taskID), MaxTaskIDLength)
	}
	return nil
}

// ValidateCapability validates a single capability name.
func ValidateCapability(cap string) error {
	if cap == "" {
		return ErrCapabilityEmpty
	}
	if len(cap) > MaxCapabilityLength {
		return fmt.Errorf("%w: %d > %d", ErrCapabilityTooLong, len(cap), MaxCapabilityLength)
	}
	return nil
}

// ValidateCapabilities validates a comma-separated list of capabilities.
func ValidateCapabilities(capsStr string) ([]string, error) {
	if capsStr == "" {
		return nil, ErrCapabilitiesEmpty
	}

	caps := strings.Split(capsStr, ",")
	if len(caps) > MaxCapabilities {
		return nil, fmt.Errorf("%w: %d > %d", ErrTooManyCapabilities, len(caps), MaxCapabilities)
	}

	result := make([]string, 0, len(caps))
	for _, cap := range caps {
		cap = strings.TrimSpace(cap)
		if cap == "" {
			continue
		}
		if err := ValidateCapability(cap); err != nil {
			return nil, err
		}
		result = append(result, cap)
	}

	if len(result) == 0 {
		return nil, ErrCapabilitiesEmpty
	}

	return result, nil
}
