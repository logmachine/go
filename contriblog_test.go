package contriblog_test

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	contriblog "github.com/bufferpunk/contriblog"
)

// tempOpts returns Options pointing to temporary log files that are cleaned up after the test.
func tempOpts(t *testing.T) contriblog.Options {
	t.Helper()
	dir := t.TempDir()
	return contriblog.Options{
		LogFile:   filepath.Join(dir, "logs.log"),
		ErrorFile: filepath.Join(dir, "errors.log"),
	}
}

func TestNew(t *testing.T) {
	opts := tempOpts(t)
	logger, err := contriblog.New(opts)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer logger.Close()
}

func TestBasicLevels(t *testing.T) {
	opts := tempOpts(t)
	logger, err := contriblog.New(opts)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer logger.Close()

	// These should not panic or return errors.
	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")
	logger.Success("success message")
}

func TestLogFilesAreWritten(t *testing.T) {
	opts := tempOpts(t)
	logger, err := contriblog.New(opts)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	logger.Info("hello world")
	logger.Error("something went wrong")
	logger.Close()

	// Both messages should appear in the main log file.
	logData, err := os.ReadFile(opts.LogFile)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	if !strings.Contains(string(logData), "hello world") {
		t.Errorf("log file missing INFO message; got:\n%s", logData)
	}
	if !strings.Contains(string(logData), "something went wrong") {
		t.Errorf("log file missing ERROR message; got:\n%s", logData)
	}

	// Only the error should appear in the error file.
	errData, err := os.ReadFile(opts.ErrorFile)
	if err != nil {
		t.Fatalf("reading error file: %v", err)
	}
	if !strings.Contains(string(errData), "something went wrong") {
		t.Errorf("error file missing ERROR message; got:\n%s", errData)
	}
	if strings.Contains(string(errData), "hello world") {
		t.Errorf("error file should not contain INFO message; got:\n%s", errData)
	}
}

func TestSuccessLevel(t *testing.T) {
	opts := tempOpts(t)
	logger, err := contriblog.New(opts)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer logger.Close()

	logger.Success("operation completed")

	logData, err := os.ReadFile(opts.LogFile)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	if !strings.Contains(string(logData), "operation completed") {
		t.Errorf("log file missing SUCCESS message; got:\n%s", logData)
	}
	if !strings.Contains(string(logData), "SUCCESS") {
		t.Errorf("log file should contain SUCCESS level tag; got:\n%s", logData)
	}
}

func TestNewLevel(t *testing.T) {
	opts := tempOpts(t)
	logger, err := contriblog.New(opts)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer logger.Close()

	trace := logger.NewLevel("TRACE", slog.Level(-8))
	trace("entering function")

	logData, err := os.ReadFile(opts.LogFile)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	if !strings.Contains(string(logData), "TRACE") {
		t.Errorf("log file should contain TRACE level; got:\n%s", logData)
	}
}

func TestParseLog(t *testing.T) {
	opts := tempOpts(t)
	logger, err := contriblog.New(opts)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer logger.Close()

	// Write a known log entry and read it back.
	logger.Info("test message for parsing")
	logger.Close()

	logData, err := os.ReadFile(opts.LogFile)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}

	// Re-open for further use.
	logger, err = contriblog.New(opts)
	if err != nil {
		t.Fatalf("re-opening logger: %v", err)
	}
	defer logger.Close()

	entry := logger.ParseLog(string(logData))
	if entry == nil {
		t.Fatalf("ParseLog() returned nil for:\n%s", logData)
	}
	if entry.Level != "INFO" {
		t.Errorf("ParseLog().Level = %q, want %q", entry.Level, "INFO")
	}
	if entry.Message != "test message for parsing" {
		t.Errorf("ParseLog().Message = %q, want %q", entry.Message, "test message for parsing")
	}
	if entry.Timestamp == "" {
		t.Error("ParseLog().Timestamp should not be empty")
	}
	if entry.User == "" {
		t.Error("ParseLog().User should not be empty")
	}
}

func TestJsonifier(t *testing.T) {
	opts := tempOpts(t)
	logger, err := contriblog.New(opts)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	logger.Info("entry one")
	logger.Error("entry two")
	logger.Close()

	// Re-create logger (file already exists, append mode).
	logger, err = contriblog.New(opts)
	if err != nil {
		t.Fatalf("re-opening logger: %v", err)
	}
	defer logger.Close()

	entries, err := logger.Jsonifier()
	if err != nil {
		t.Fatalf("Jsonifier() error = %v", err)
	}
	if len(entries) < 2 {
		t.Errorf("Jsonifier() returned %d entries, want ≥2", len(entries))
	}

	// Each entry should be valid JSON.
	for _, e := range entries {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(e), &m); err != nil {
			t.Errorf("Jsonifier() entry is not valid JSON: %s", e)
		}
	}
}

func TestDebugLevelFilter(t *testing.T) {
	tests := []struct {
		debugLevel int
		level      string
		wantLog    bool
	}{
		{1, "ERROR", true},
		{1, "INFO", false},
		{2, "SUCCESS", true},
		{2, "INFO", false},
		{3, "WARN", true},
		{3, "DEBUG", false},
		{4, "INFO", true},
		{4, "DEBUG", false},
		{5, "ERROR", true},
		{5, "WARN", true},
		{5, "INFO", false},
		{6, "INFO", true},
		{6, "SUCCESS", true},
		{6, "WARN", false},
		{7, "ERROR", true},
		{7, "WARN", true},
		{7, "INFO", true},
		{0, "DEBUG", true},
		{0, "INFO", true},
		{0, "ERROR", true},
	}

	for _, tc := range tests {
		t.Run(tc.level+"_dl"+strconv.Itoa(tc.debugLevel), func(t *testing.T) {
			dir := t.TempDir()
			opts := contriblog.Options{
				LogFile:    filepath.Join(dir, "logs.log"),
				ErrorFile:  filepath.Join(dir, "errors.log"),
				DebugLevel: tc.debugLevel,
			}
			logger, err := contriblog.New(opts)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			sentinel := "sentinel_" + tc.level

			switch tc.level {
			case "DEBUG":
				logger.Debug(sentinel)
			case "INFO":
				logger.Info(sentinel)
			case "WARN":
				logger.Warn(sentinel)
			case "ERROR":
				logger.Error(sentinel)
			case "SUCCESS":
				logger.Success(sentinel)
			}
			logger.Close()

			// File writing is not filtered by DebugLevel — only console (transporter) output is.
			// This mirrors the Python implementation where only the console handler is filtered.
			// We verify that the file always receives the message regardless of DebugLevel.
			logData, err := os.ReadFile(opts.LogFile)
			if err != nil {
				t.Fatalf("reading log file: %v", err)
			}
			if !strings.Contains(string(logData), sentinel) {
				t.Errorf("log file missing %q (DebugLevel=%d does not filter file writes); got:\n%s",
					sentinel, tc.debugLevel, logData)
			}
		})
	}
}

func TestCLUsernameEnvVar(t *testing.T) {
	os.Setenv("CL_USERNAME", "testuser")
	t.Cleanup(func() { os.Unsetenv("CL_USERNAME") })

	opts := tempOpts(t)
	logger, err := contriblog.New(opts)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer logger.Close()

	logger.Info("checking username")
	logger.Close()

	logData, err := os.ReadFile(opts.LogFile)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	if !strings.Contains(string(logData), "testuser") {
		t.Errorf("expected username 'testuser' in log output; got:\n%s", logData)
	}
}

func TestLogFormat(t *testing.T) {
	opts := tempOpts(t)
	logger, err := contriblog.New(opts)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer logger.Close()

	logger.Info("format check")
	logger.Close()

	logData, err := os.ReadFile(opts.LogFile)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}

	raw := string(logData)

	// The format must contain the emoji landmarks.
	if !strings.Contains(raw, "🤌 CL Timing:") {
		t.Errorf("log missing '🤌 CL Timing:' marker; got:\n%s", raw)
	}
	if !strings.Contains(raw, "🏁") {
		t.Errorf("log missing '🏁' end marker; got:\n%s", raw)
	}
	if !strings.Contains(raw, "[ INFO ]") {
		t.Errorf("log missing '[ INFO ]' level tag; got:\n%s", raw)
	}
	if !strings.Contains(raw, "format check") {
		t.Errorf("log missing message body; got:\n%s", raw)
	}
}
