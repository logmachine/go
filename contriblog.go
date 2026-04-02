// Package contriblog provides a collaborative, beautiful logging system for distributed developers.
// It wraps Go's standard log/slog library with colored terminal output, file logging,
// and optional log forwarding to a central server via HTTP or WebSocket (Socket.IO).
package contriblog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ANSI color and style codes used in terminal output.
const (
	colorDebug   = "\x1b[36m"
	colorInfo    = "\x1b[34m"
	colorWarning = "\x1b[33m"
	colorError   = "\x1b[31m"
	colorSuccess = "\x1b[32m"
	colorReset   = "\x1b[0m"
	colorBold    = "\x1b[1m"
)

// LevelSuccess is a custom slog level placed between INFO (0) and WARN (4),
// mirroring the Python implementation's SUCCESS = 25 (between INFO=20 and WARNING=30).
const LevelSuccess = slog.Level(2)

// getLogin returns the current OS user's login name.
func getLogin() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u := os.Getenv("USERNAME"); u != "" {
		return u
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return "unknown"
}

// CentralConfig holds the configuration for the central log-aggregation server.
type CentralConfig struct {
	// URL is the base URL of the central server (e.g. "https://logmachine.bufferpunk.com").
	URL string
	// Room is the logical group / organisation name used to bucket logs.
	Room string
	// Endpoint is the HTTP path for log POST requests (default: "/api/logs").
	Endpoint string
	// Headers contains extra HTTP headers to include in every request (e.g. Authorization).
	Headers map[string]string
	// SocketIO enables WebSocket (Socket.IO) transport instead of plain HTTP.
	SocketIO bool
	// SocketIOPath is the server-side Socket.IO mount path (default: "/socket.io/").
	SocketIOPath string
}

// Options configures a LogMachine instance.
type Options struct {
	// LogFile is the path to the general log file (default: "logs.log").
	LogFile string
	// ErrorFile is the path to the error-only log file (default: "errors.log").
	ErrorFile string
	// DebugLevel controls which log levels are printed to the console (0 = all, 1-7 = filtered).
	// See the allowedMap in the handler for the mapping.
	DebugLevel int
	// Verbose forces all levels through regardless of DebugLevel.
	Verbose bool
	// Central, if non-nil, enables log forwarding to a remote server.
	Central *CentralConfig
	// Attached, when true together with a Central config, uses Socket.IO instead of HTTP.
	Attached bool
}

// LogEntry represents a single parsed log record in structured form.
type LogEntry struct {
	User      string `json:"user"`
	Module    string `json:"module"`
	Level     string `json:"level"`
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
}

// Transporter is the interface satisfied by all log transport backends.
type Transporter interface {
	// Emit delivers a formatted log entry to the transport destination.
	Emit(formatted string) error
	// Close releases any resources held by the transporter.
	Close() error
}

// stdoutTransporter simply prints the formatted log to stdout.
type stdoutTransporter struct{}

func (t *stdoutTransporter) Emit(formatted string) error {
	fmt.Println(formatted)
	return nil
}

func (t *stdoutTransporter) Close() error { return nil }

// HTTPTransporter forwards log entries to a central server via HTTP POST.
type HTTPTransporter struct {
	parseLog func(string) *LogEntry
	central  *CentralConfig
	client   *http.Client
}

func newHTTPTransporter(parseLog func(string) *LogEntry, central *CentralConfig) *HTTPTransporter {
	return &HTTPTransporter{
		parseLog: parseLog,
		central:  central,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Emit prints the log locally and then POSTs it to the central server.
func (t *HTTPTransporter) Emit(formatted string) error {
	fmt.Println(formatted)

	if t.central == nil {
		return nil
	}
	if t.central.Room == "" {
		return fmt.Errorf("central config must include 'Room' for log transport. Example: &CentralConfig{URL: \"http://central-server/\", Room: \"my_organisation\"}")
	}

	logData := t.parseLog(formatted)
	if logData == nil {
		return nil
	}

	endpoint := t.central.Endpoint
	if endpoint == "" {
		endpoint = "/api/logs"
	}
	url := fmt.Sprintf("%s%s?room=%s", strings.TrimRight(t.central.URL, "/"), endpoint, t.central.Room)

	body, err := json.Marshal(logData)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.central.Headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to send log to central: %s", string(respBody))
	}
	return nil
}

func (t *HTTPTransporter) Close() error { return nil }

// WebSocketTransporter forwards log entries via a Socket.IO v4 WebSocket connection.
type WebSocketTransporter struct {
	parseLog func(string) *LogEntry
	central  *CentralConfig
	conn     *websocket.Conn
	mu       sync.Mutex
}

func newWebSocketTransporter(parseLog func(string) *LogEntry, central *CentralConfig) (*WebSocketTransporter, error) {
	t := &WebSocketTransporter{
		parseLog: parseLog,
		central:  central,
	}
	if err := t.connect(); err != nil {
		// Return transporter even if initial connect fails so local logging still works.
		return t, fmt.Errorf("websocket connect: %w", err)
	}
	return t, nil
}

// connect establishes the Socket.IO v4 WebSocket connection.
func (t *WebSocketTransporter) connect() error {
	socketPath := t.central.SocketIOPath
	if socketPath == "" {
		socketPath = "/socket.io/"
	}

	base := strings.TrimRight(t.central.URL, "/")
	wsURL := strings.Replace(base, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL = fmt.Sprintf("%s%s?EIO=4&transport=websocket", wsURL, socketPath)

	header := http.Header{}
	for k, v := range t.central.Headers {
		header.Set(k, v)
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		return err
	}

	// Read the EIO OPEN packet (starts with "0").
	_, _, err = conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("reading open packet: %w", err)
	}

	// Send Socket.IO namespace connect packet "40".
	if err := conn.WriteMessage(websocket.TextMessage, []byte("40")); err != nil {
		conn.Close()
		return fmt.Errorf("sending namespace connect: %w", err)
	}

	// Read the namespace connect acknowledgement "40" or "40{}".
	_, _, err = conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("reading namespace ack: %w", err)
	}

	t.conn = conn
	return nil
}

// Emit prints the log locally and then emits it via Socket.IO.
func (t *WebSocketTransporter) Emit(formatted string) error {
	fmt.Println(formatted)

	if t.central == nil || t.central.Room == "" {
		return nil
	}

	logData := t.parseLog(formatted)
	if logData == nil {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.conn == nil {
		return fmt.Errorf("websocket not connected")
	}

	// Socket.IO v4 event format: 42["event", payload]
	payload := map[string]interface{}{
		"room": t.central.Room,
		"data": logData,
	}
	eventData, err := json.Marshal([]interface{}{"log", payload})
	if err != nil {
		return err
	}
	packet := append([]byte("42"), eventData...)

	return t.conn.WriteMessage(websocket.TextMessage, packet)
}

func (t *WebSocketTransporter) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.conn != nil {
		return t.conn.Close()
	}
	return nil
}

// contribHandler is the core slog.Handler implementation.
type contribHandler struct {
	mu           sync.Mutex
	opts         Options
	logWriter    io.WriteCloser
	errorWriter  io.WriteCloser
	transporter  Transporter
	customLevels map[slog.Level]string // level value → level name
	levelColors  map[string]string     // level name → ANSI color
	allowedMap   map[int][]string      // debugLevel → allowed level names
	attrs        []slog.Attr
	groups       []string
}

func newContribHandler(opts Options, transporter Transporter) (*contribHandler, error) {
	lf, err := os.OpenFile(opts.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %q: %w", opts.LogFile, err)
	}

	ef, err := os.OpenFile(opts.ErrorFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		lf.Close()
		return nil, fmt.Errorf("failed to open error file %q: %w", opts.ErrorFile, err)
	}

	return &contribHandler{
		opts:        opts,
		logWriter:   lf,
		errorWriter: ef,
		transporter: transporter,
		customLevels: map[slog.Level]string{
			LevelSuccess: "SUCCESS",
		},
		levelColors: map[string]string{
			"DEBUG":   colorDebug,
			"INFO":    colorInfo,
			"WARN":    colorWarning,
			"ERROR":   colorError,
			"SUCCESS": colorSuccess,
		},
		allowedMap: map[int][]string{
			1: {"ERROR"},
			2: {"SUCCESS"},
			3: {"WARN"},
			4: {"INFO"},
			5: {"ERROR", "WARN"},
			6: {"INFO", "SUCCESS"},
			7: {"ERROR", "WARN", "INFO"},
		},
	}, nil
}

// Enabled reports whether the handler handles records at the given level.
func (h *contribHandler) Enabled(_ context.Context, level slog.Level) bool {
	return true // level gating is handled in Handle via isAllowed
}

// Handle formats and dispatches a log record.
// File writes are never filtered — all levels are persisted to disk.
// DebugLevel filtering applies only to the transporter (console/central output),
// mirroring the Python implementation where only the console handler is filtered.
func (h *contribHandler) Handle(_ context.Context, r slog.Record) error {
	levelName := h.levelName(r.Level)

	// Resolve caller's source file for the module directory.
	parentDir := "stdin"
	if r.PC != 0 {
		frames := runtime.CallersFrames([]uintptr{r.PC})
		frame, _ := frames.Next()
		if frame.File != "" {
			parentDir = filepath.Base(filepath.Dir(frame.File))
		}
	}

	formatted := h.formatLog(levelName, r.Message, parentDir, r.Time)

	// Always write to files regardless of DebugLevel.
	h.mu.Lock()
	fmt.Fprintln(h.logWriter, formatted)
	if levelName == "ERROR" {
		fmt.Fprintln(h.errorWriter, formatted)
	}
	h.mu.Unlock()

	// Apply DebugLevel filter only to the transporter (console/central output).
	h.mu.Lock()
	allowed := h.isAllowed(levelName)
	h.mu.Unlock()

	if !allowed {
		return nil
	}

	if err := h.transporter.Emit(formatted); err != nil {
		fmt.Fprintf(os.Stderr, "[contriblog] transport error: %v\n", err)
	}
	return nil
}

// WithAttrs returns a new handler with the given attributes pre-set.
func (h *contribHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h2 := h.clone()
	h2.attrs = append(h2.attrs, attrs...)
	return h2
}

// WithGroup returns a new handler with the given group name pre-set.
func (h *contribHandler) WithGroup(name string) slog.Handler {
	h2 := h.clone()
	h2.groups = append(h2.groups, name)
	return h2
}

// clone returns a shallow copy of the handler with a fresh mutex.
func (h *contribHandler) clone() *contribHandler {
	h.mu.Lock()
	defer h.mu.Unlock()
	return &contribHandler{
		opts:         h.opts,
		logWriter:    h.logWriter,
		errorWriter:  h.errorWriter,
		transporter:  h.transporter,
		customLevels: h.customLevels,
		levelColors:  h.levelColors,
		allowedMap:   h.allowedMap,
		attrs:        append([]slog.Attr(nil), h.attrs...),
		groups:       append([]string(nil), h.groups...),
	}
}

// levelName converts a slog.Level to a human-readable name.
func (h *contribHandler) levelName(level slog.Level) string {
	if name, ok := h.customLevels[level]; ok {
		return name
	}
	switch {
	case level < slog.LevelInfo:
		return "DEBUG"
	case level < slog.LevelWarn:
		return "INFO"
	case level < slog.LevelError:
		return "WARN"
	default:
		return "ERROR"
	}
}

// isAllowed reports whether the given level name passes the current DebugLevel filter.
func (h *contribHandler) isAllowed(levelName string) bool {
	if h.opts.DebugLevel == 0 || h.opts.Verbose {
		return true
	}
	allowed, ok := h.allowedMap[h.opts.DebugLevel]
	if !ok {
		return true
	}
	for _, a := range allowed {
		if a == levelName {
			return true
		}
	}
	return false
}

// formatLog produces the LogMachine-style terminal string for a log entry.
func (h *contribHandler) formatLog(levelName, msg, parentDir string, t time.Time) string {
	username := os.Getenv("CL_USERNAME")
	if username == "" {
		username = getLogin()
	}

	timestamp := t.Format("2006-01-02T15:04:05-07:00")

	color := h.levelColors[levelName]
	if color == "" {
		color = colorInfo
	}

	levelFmt := fmt.Sprintf("%s%s[ %s ]%s", colorBold, color, levelName, colorReset)

	return fmt.Sprintf(
		"%s(%s%s @ %s%s%s) 🤌 CL Timing: %s[ %s ]%s\n%s %s\n🏁",
		colorDebug,
		username, colorReset,
		colorWarning, parentDir, colorReset,
		color, timestamp, colorReset,
		levelFmt, msg,
	)
}

// LogMachine wraps slog.Logger with LogMachine-specific functionality.
type LogMachine struct {
	*slog.Logger
	handler *contribHandler
}

// New creates a new LogMachine instance.
//
// If opts.Central is non-nil, the logger resolves (and caches) the username
// from the central server and enables log forwarding.
func New(opts Options) (*LogMachine, error) {
	if opts.LogFile == "" {
		opts.LogFile = "logs.log"
	}
	if opts.ErrorFile == "" {
		opts.ErrorFile = "errors.log"
	}

	// Resolve username when a central server is configured.
	if opts.Central != nil {
		resolveUsername(opts.Central.URL)
	}

	// Build the transporter.
	var transporter Transporter
	if opts.Central != nil {
		useSocketIO := opts.Attached || opts.Central.SocketIO
		if useSocketIO {
			t, err := newWebSocketTransporter(parseLog, opts.Central)
			if err != nil {
				// Fall back to stdout; don't hard-fail on connection errors.
				fmt.Fprintf(os.Stderr, "[contriblog] websocket connect failed: %v\n", err)
				transporter = &stdoutTransporter{}
			} else {
				transporter = t
			}
		} else {
			transporter = newHTTPTransporter(parseLog, opts.Central)
		}
	} else {
		transporter = &stdoutTransporter{}
	}

	handler, err := newContribHandler(opts, transporter)
	if err != nil {
		return nil, err
	}

	logger := slog.New(handler)
	return &LogMachine{Logger: logger, handler: handler}, nil
}

// resolveUsername fetches (or loads from cache) the CL_USERNAME for the given server URL.
func resolveUsername(serverURL string) {
	clFile := filepath.Join(homeDir(), ".cl_username")

	if _, err := os.Stat(clFile); err == nil {
		// Already cached on disk.
		data, err := os.ReadFile(clFile)
		if err == nil {
			os.Setenv("CL_USERNAME", strings.TrimSpace(string(data)))
			return
		}
	}

	// Fetch from server.
	login := getLogin()
	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("%s/api/get_username?base=%s", strings.TrimRight(serverURL, "/"), login)
	resp, err := client.Get(url)
	if err != nil {
		os.Setenv("CL_USERNAME", "unknown")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		os.Setenv("CL_USERNAME", "unknown")
		return
	}

	var result struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || result.Username == "" {
		os.Setenv("CL_USERNAME", "unknown")
		return
	}

	username := result.Username
	if username == "unknown" {
		os.Setenv("CL_USERNAME", "unknown")
		return
	}

	os.Setenv("CL_USERNAME", username)
	// Persist to disk.
	_ = os.WriteFile(clFile, []byte(username), 0600)
}

// homeDir returns the current user's home directory.
func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	if u, err := user.Current(); err == nil {
		return u.HomeDir
	}
	return "."
}

// Success logs at the custom SUCCESS level.
func (c *LogMachine) Success(msg string, args ...any) {
	c.Logger.Log(context.Background(), LevelSuccess, msg, args...)
}

// NewLevel registers a new custom log level and returns a logging function for it.
//
//	level := c.NewLevel("TRACE", slog.Level(-8))
//	level("entering function foo")
func (c *LogMachine) NewLevel(name string, level slog.Level) func(msg string, args ...any) {
	c.handler.mu.Lock()
	c.handler.customLevels[level] = name
	if _, exists := c.handler.levelColors[name]; !exists {
		c.handler.levelColors[name] = colorInfo // default color
	}
	c.handler.mu.Unlock()

	return func(msg string, args ...any) {
		c.Logger.Log(context.Background(), level, msg, args...)
	}
}

// ParseLog parses a LogMachine-formatted string into a LogEntry.
// Returns nil if the string does not match the expected format.
func parseLog(text string) *LogEntry {
	text = strings.TrimSpace(text)

	// Strip ANSI escape codes.
	ansiRe := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	clean := ansiRe.ReplaceAllString(text, "")

	// Match the header: "(user @ module) 🤌 CL Timing: [ timestamp ]"
	headerRe := regexp.MustCompile(`\((.*?) @ (.*?)\) 🤌 CL Timing: \[ (.*?) \]`)
	headerMatch := headerRe.FindStringSubmatch(clean)
	if headerMatch == nil {
		return nil
	}

	username := headerMatch[1]
	module := headerMatch[2]
	timestamp := headerMatch[3]

	// Match "[ LEVEL ] message" from the second line.
	lines := strings.SplitN(clean, "\n", 3)
	levelLine := ""
	if len(lines) > 1 {
		levelLine = strings.TrimSpace(lines[1])
	}

	levelRe := regexp.MustCompile(`\[\s?(\w+)\s?\]\s?(.*)`)
	levelMatch := levelRe.FindStringSubmatch(levelLine)

	level := "UNKNOWN"
	message := ""
	if levelMatch != nil {
		level = strings.TrimSpace(levelMatch[1])
		message = strings.TrimSpace(levelMatch[2])
	}

	// Remove trailing 🏁 from message if present.
	flagRe := regexp.MustCompile(`🏁`)
	message = strings.TrimSpace(flagRe.ReplaceAllString(message, ""))

	return &LogEntry{
		User:      username,
		Module:    module,
		Level:     level,
		Timestamp: timestamp,
		Message:   message,
	}
}

// ParseLog is the exported wrapper around the internal parseLog function.
func (c *LogMachine) ParseLog(text string) *LogEntry {
	return parseLog(text)
}

// Jsonifier reads the log file and returns a slice of JSON-encoded LogEntry strings,
// one per parsed log record. This mirrors the Python implementation's jsonifier() method.
func (c *LogMachine) Jsonifier() ([]string, error) {
	data, err := os.ReadFile(c.handler.opts.LogFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read log file: %w", err)
	}

	var entries []string
	for _, block := range strings.Split(string(data), "\n🏁\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		entry := parseLog(block)
		if entry == nil {
			continue
		}
		b, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		entries = append(entries, string(b))
	}
	return entries, nil
}

// Close releases resources held by the logger (open file handles, network connections).
func (c *LogMachine) Close() error {
	c.handler.mu.Lock()
	defer c.handler.mu.Unlock()

	var errs []string
	if c.handler.logWriter != nil {
		if err := c.handler.logWriter.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if c.handler.errorWriter != nil {
		if err := c.handler.errorWriter.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if c.handler.transporter != nil {
		if err := c.handler.transporter.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("close errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// DefaultLogger is a ready-to-use LogMachine instance that forwards to the public
// logmachine server (room "public"), mirroring the Python module-level default_logger.
var DefaultLogger = func() *LogMachine {
	logger, err := New(Options{
		DebugLevel: 0,
		Verbose:    false,
		Central: &CentralConfig{
			URL:  "https://logmachine.bufferpunk.com",
			Room: "public",
		},
	})
	if err != nil {
		// If we cannot create the default logger (e.g. can't open log files),
		// fall back to a plain slog logger to avoid a panic at init time.
		fmt.Fprintf(os.Stderr, "[contriblog] default logger init failed: %v\n", err)
		return nil
	}
	return logger
}()
