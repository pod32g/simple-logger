package log

import "testing"

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		in       string
		expected LogLevel
	}{
		{"DEBUG", DEBUG},
		{"info", INFO},
		{"Warn", WARN},
		{"error", ERROR},
		{"fatal", FATAL},
		{"unknown", INFO},
	}
	for _, tt := range tests {
		if got := parseLogLevel(tt.in); got != tt.expected {
			t.Errorf("parseLogLevel(%s)=%v, want %v", tt.in, got, tt.expected)
		}
	}
}
