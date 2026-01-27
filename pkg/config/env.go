// Package config provides configuration helpers for msg2agent.
package config

import (
	"os"
	"strconv"
	"time"
)

// EnvPrefix is the prefix for all msg2agent environment variables.
const EnvPrefix = "MSG2AGENT_"

// GetEnv returns the value of the environment variable with the MSG2AGENT_ prefix.
// Returns empty string if not set.
func GetEnv(key string) string {
	return os.Getenv(EnvPrefix + key)
}

// GetEnvOrDefault returns the value of the environment variable with the MSG2AGENT_ prefix,
// or the default value if not set.
func GetEnvOrDefault(key, defaultVal string) string {
	if val := GetEnv(key); val != "" {
		return val
	}
	return defaultVal
}

// GetEnvInt returns the value of the environment variable as an int.
// Returns the default value if not set or not a valid integer.
func GetEnvInt(key string, defaultVal int) int {
	val := GetEnv(key)
	if val == "" {
		return defaultVal
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return i
}

// GetEnvFloat returns the value of the environment variable as a float64.
// Returns the default value if not set or not a valid float.
func GetEnvFloat(key string, defaultVal float64) float64 {
	val := GetEnv(key)
	if val == "" {
		return defaultVal
	}
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return defaultVal
	}
	return f
}

// GetEnvBool returns the value of the environment variable as a bool.
// Accepts "true", "1", "yes" as true values (case insensitive).
// Returns the default value if not set.
func GetEnvBool(key string, defaultVal bool) bool {
	val := GetEnv(key)
	if val == "" {
		return defaultVal
	}
	switch val {
	case "true", "True", "TRUE", "1", "yes", "Yes", "YES":
		return true
	case "false", "False", "FALSE", "0", "no", "No", "NO":
		return false
	}
	return defaultVal
}

// GetEnvDuration returns the value of the environment variable as a time.Duration.
// The value should be a valid Go duration string (e.g., "10s", "5m", "1h").
// Returns the default value if not set or not a valid duration.
func GetEnvDuration(key string, defaultVal time.Duration) time.Duration {
	val := GetEnv(key)
	if val == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(val)
	if err != nil {
		return defaultVal
	}
	return d
}

// FlagOrEnv returns the flag value if set (non-empty/non-zero), otherwise the env value.
// This allows env vars to provide defaults that flags can override.
func FlagOrEnv(flagVal, envKey, defaultVal string) string {
	if flagVal != "" {
		return flagVal
	}
	return GetEnvOrDefault(envKey, defaultVal)
}

// FlagOrEnvInt returns the flag value if different from default, otherwise the env value.
func FlagOrEnvInt(flagVal int, flagDefault int, envKey string, defaultVal int) int {
	if flagVal != flagDefault {
		return flagVal
	}
	return GetEnvInt(envKey, defaultVal)
}

// FlagOrEnvBool returns the flag value if true, otherwise the env value.
func FlagOrEnvBool(flagVal bool, envKey string, defaultVal bool) bool {
	if flagVal {
		return true
	}
	return GetEnvBool(envKey, defaultVal)
}

// FlagOrEnvFloat returns the flag value if different from default, otherwise the env value.
func FlagOrEnvFloat(flagVal float64, flagDefault float64, envKey string, defaultVal float64) float64 {
	if flagVal != flagDefault {
		return flagVal
	}
	return GetEnvFloat(envKey, defaultVal)
}
