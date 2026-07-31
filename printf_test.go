package log_test

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	log "github.com/pod32g/simple-logger"
)

func TestFormattedMethods(t *testing.T) {
	cases := []struct {
		name  string
		emit  func(*log.Logger)
		level string
	}{
		{"Debugf", func(l *log.Logger) { l.Debugf("n=%d s=%s", 3, "x") }, "DEBUG"},
		{"Infof", func(l *log.Logger) { l.Infof("n=%d s=%s", 3, "x") }, "INFO"},
		{"Warnf", func(l *log.Logger) { l.Warnf("n=%d s=%s", 3, "x") }, "WARN"},
		{"Errorf", func(l *log.Logger) { l.Errorf("n=%d s=%s", 3, "x") }, "ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := log.NewLogger(&buf, log.DEBUG, &log.DefaultFormatter{})
			tc.emit(l)
			got := buf.String()
			if !strings.Contains(got, "n=3 s=x") {
				t.Errorf("message not formatted: %s", got)
			}
			if !strings.Contains(got, "["+tc.level+"]") {
				t.Errorf("wrong level, want %s: %s", tc.level, got)
			}
		})
	}
}

// The reason these methods exist: nothing is formatted when the entry is going
// to be dropped on level.
func TestFormattedMethodsSkipFormattingWhenDisabled(t *testing.T) {
	var buf bytes.Buffer
	l := log.NewLogger(&buf, log.ERROR, &log.DefaultFormatter{})

	formatted := 0
	arg := stringerFunc(func() string { formatted++; return "expensive" })

	l.Debugf("value=%s", arg)
	l.Infof("value=%s", arg)
	l.Warnf("value=%s", arg)
	if formatted != 0 {
		t.Errorf("argument was formatted %d times below the level threshold", formatted)
	}
	if buf.Len() != 0 {
		t.Errorf("unexpected output: %s", buf.String())
	}

	l.Errorf("value=%s", arg)
	if formatted != 1 {
		t.Errorf("argument formatted %d times at ERROR, want 1", formatted)
	}
	if !strings.Contains(buf.String(), "value=expensive") {
		t.Errorf("entry missing: %s", buf.String())
	}
}

// Formatted messages take the same path as any other message: bound fields,
// redaction and hooks all still apply.
func TestFormattedMessagesGoThroughTheNormalPath(t *testing.T) {
	var buf bytes.Buffer
	l := log.NewLogger(&buf, log.INFO, &log.JSONFormatter{}).With(log.String("component", "api"))
	l.SetRedactor(log.NewPatternScrubber(regexp.MustCompile(`secret-\w+`)))

	var hooked string
	l.AddHook(log.HookFunc(func(_ log.LogLevel, msg string, _ []log.Field) { hooked = msg }))

	l.Infof("token %s rejected", "secret-abc")

	got := buf.String()
	if !strings.Contains(got, "[REDACTED]") || strings.Contains(got, "secret-abc") {
		t.Errorf("redaction did not apply to a formatted message: %s", got)
	}
	if !strings.Contains(got, `"component":"api"`) {
		t.Errorf("bound fields missing: %s", got)
	}
	if !strings.Contains(hooked, "[REDACTED]") {
		t.Errorf("hook saw %q, want the redacted message", hooked)
	}
}

type stringerFunc func() string

func (f stringerFunc) String() string { return f() }

func TestFatalfExitsAfterLogging(t *testing.T) {
	if os.Getenv("FATALF_CHILD") == "1" {
		l := log.NewLogger(os.Stdout, log.INFO, &log.DefaultFormatter{})
		l.Fatalf("stopping after %d retries", 3)
		l.InfoString("unreachable")
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestFatalfExitsAfterLogging")
	cmd.Env = append(os.Environ(), "FATALF_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("expected a non-zero exit status")
	}
	got := string(out)
	if !strings.Contains(got, "stopping after 3 retries") {
		t.Errorf("fatal message missing or unformatted: %s", got)
	}
	if strings.Contains(got, "unreachable") {
		t.Errorf("execution continued past Fatalf: %s", got)
	}
}
