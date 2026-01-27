package config

import (
	"os"
	"testing"
	"time"
)

func TestGetEnv(t *testing.T) {
	t.Setenv("MSG2AGENT_TEST_KEY", "test_value")

	if got := GetEnv("TEST_KEY"); got != "test_value" {
		t.Errorf("GetEnv() = %q, want %q", got, "test_value")
	}

	if got := GetEnv("NONEXISTENT_KEY"); got != "" {
		t.Errorf("GetEnv() = %q, want empty string", got)
	}
}

func TestGetEnvOrDefault(t *testing.T) {
	t.Setenv("MSG2AGENT_TEST_KEY", "set_value")

	tests := []struct {
		name       string
		key        string
		defaultVal string
		want       string
	}{
		{"env set", "TEST_KEY", "default", "set_value"},
		{"env not set", "UNSET_KEY", "default", "default"},
		{"empty default", "UNSET_KEY", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetEnvOrDefault(tt.key, tt.defaultVal); got != tt.want {
				t.Errorf("GetEnvOrDefault() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetEnvInt(t *testing.T) {
	tests := []struct {
		name       string
		envVal     string
		defaultVal int
		want       int
	}{
		{"valid int", "42", 0, 42},
		{"negative int", "-10", 0, -10},
		{"invalid int", "not_a_number", 99, 99},
		{"empty", "", 123, 123},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVal != "" {
				t.Setenv("MSG2AGENT_INT_KEY", tt.envVal)
			} else {
				os.Unsetenv("MSG2AGENT_INT_KEY")
			}

			if got := GetEnvInt("INT_KEY", tt.defaultVal); got != tt.want {
				t.Errorf("GetEnvInt() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetEnvFloat(t *testing.T) {
	tests := []struct {
		name       string
		envVal     string
		defaultVal float64
		want       float64
	}{
		{"valid float", "3.14", 0, 3.14},
		{"integer as float", "42", 0, 42.0},
		{"negative float", "-1.5", 0, -1.5},
		{"invalid float", "not_a_number", 99.9, 99.9},
		{"empty", "", 1.23, 1.23},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVal != "" {
				t.Setenv("MSG2AGENT_FLOAT_KEY", tt.envVal)
			} else {
				os.Unsetenv("MSG2AGENT_FLOAT_KEY")
			}

			if got := GetEnvFloat("FLOAT_KEY", tt.defaultVal); got != tt.want {
				t.Errorf("GetEnvFloat() = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestGetEnvBool(t *testing.T) {
	tests := []struct {
		name       string
		envVal     string
		defaultVal bool
		want       bool
	}{
		{"true lowercase", "true", false, true},
		{"True mixed", "True", false, true},
		{"TRUE uppercase", "TRUE", false, true},
		{"1", "1", false, true},
		{"yes", "yes", false, true},
		{"Yes mixed", "Yes", false, true},
		{"YES uppercase", "YES", false, true},
		{"false lowercase", "false", true, false},
		{"False mixed", "False", true, false},
		{"FALSE uppercase", "FALSE", true, false},
		{"0", "0", true, false},
		{"no", "no", true, false},
		{"No mixed", "No", true, false},
		{"NO uppercase", "NO", true, false},
		{"invalid value", "maybe", true, true},
		{"empty default true", "", true, true},
		{"empty default false", "", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVal != "" {
				t.Setenv("MSG2AGENT_BOOL_KEY", tt.envVal)
			} else {
				os.Unsetenv("MSG2AGENT_BOOL_KEY")
			}

			if got := GetEnvBool("BOOL_KEY", tt.defaultVal); got != tt.want {
				t.Errorf("GetEnvBool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetEnvDuration(t *testing.T) {
	tests := []struct {
		name       string
		envVal     string
		defaultVal time.Duration
		want       time.Duration
	}{
		{"seconds", "10s", 0, 10 * time.Second},
		{"minutes", "5m", 0, 5 * time.Minute},
		{"hours", "2h", 0, 2 * time.Hour},
		{"complex", "1h30m", 0, 90 * time.Minute},
		{"invalid", "not_duration", 30 * time.Second, 30 * time.Second},
		{"empty", "", 1 * time.Minute, 1 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVal != "" {
				t.Setenv("MSG2AGENT_DUR_KEY", tt.envVal)
			} else {
				os.Unsetenv("MSG2AGENT_DUR_KEY")
			}

			if got := GetEnvDuration("DUR_KEY", tt.defaultVal); got != tt.want {
				t.Errorf("GetEnvDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFlagOrEnv(t *testing.T) {
	t.Setenv("MSG2AGENT_FLAG_KEY", "env_value")

	tests := []struct {
		name       string
		flagVal    string
		envKey     string
		defaultVal string
		want       string
	}{
		{"flag set", "flag_value", "FLAG_KEY", "default", "flag_value"},
		{"flag empty, env set", "", "FLAG_KEY", "default", "env_value"},
		{"flag empty, env not set", "", "UNSET_KEY", "default", "default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FlagOrEnv(tt.flagVal, tt.envKey, tt.defaultVal); got != tt.want {
				t.Errorf("FlagOrEnv() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFlagOrEnvInt(t *testing.T) {
	t.Setenv("MSG2AGENT_INT_FLAG", "100")

	tests := []struct {
		name        string
		flagVal     int
		flagDefault int
		envKey      string
		defaultVal  int
		want        int
	}{
		{"flag changed", 50, 0, "INT_FLAG", 10, 50},
		{"flag default, env set", 0, 0, "INT_FLAG", 10, 100},
		{"flag default, env not set", 0, 0, "UNSET_INT", 10, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FlagOrEnvInt(tt.flagVal, tt.flagDefault, tt.envKey, tt.defaultVal); got != tt.want {
				t.Errorf("FlagOrEnvInt() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFlagOrEnvBool(t *testing.T) {
	t.Setenv("MSG2AGENT_BOOL_FLAG", "true")

	tests := []struct {
		name       string
		flagVal    bool
		envKey     string
		defaultVal bool
		want       bool
	}{
		{"flag true", true, "BOOL_FLAG", false, true},
		{"flag false, env true", false, "BOOL_FLAG", false, true},
		{"flag false, env not set", false, "UNSET_BOOL", false, false},
		{"flag false, env not set, default true", false, "UNSET_BOOL", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FlagOrEnvBool(tt.flagVal, tt.envKey, tt.defaultVal); got != tt.want {
				t.Errorf("FlagOrEnvBool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFlagOrEnvFloat(t *testing.T) {
	t.Setenv("MSG2AGENT_FLOAT_FLAG", "99.9")

	tests := []struct {
		name        string
		flagVal     float64
		flagDefault float64
		envKey      string
		defaultVal  float64
		want        float64
	}{
		{"flag changed", 50.5, 0, "FLOAT_FLAG", 10.0, 50.5},
		{"flag default, env set", 0, 0, "FLOAT_FLAG", 10.0, 99.9},
		{"flag default, env not set", 0, 0, "UNSET_FLOAT", 10.0, 10.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FlagOrEnvFloat(tt.flagVal, tt.flagDefault, tt.envKey, tt.defaultVal); got != tt.want {
				t.Errorf("FlagOrEnvFloat() = %f, want %f", got, tt.want)
			}
		})
	}
}
