// Command example demonstrates the LogMachine logger package for Go.
// Run with: go run ./example
package main

import (
	"fmt"
	"log/slog"

	logmachine "github.com/logmachine/go"
)

func main() {
	// Basic logger without central forwarding.
	logger, err := logmachine.New(logmachine.Options{
		LogFile:    "logs.log",
		ErrorFile:  "errors.log",
		DebugLevel: 0,
		Verbose:    false,
	})
	if err != nil {
		fmt.Printf("failed to create logger: %v\n", err)
		return
	}
	defer logger.Close()

	logger.Info("Hello, world!")
	logger.Error("An error occurred!")
	logger.Success("Operation completed successfully!")
	logger.Debug("Debugging information here.")
	logger.Warn("This is a warning message.")

	// Add a custom log level.
	criticalHack := logger.NewLevel("CRITICAL_HACK", slog.Level(16))
	criticalHack("Zero day found!")

	// Parse and inspect a log entry.
	entries, err := logger.Jsonifier()
	if err != nil {
		fmt.Printf("jsonifier error: %v\n", err)
		return
	}
	fmt.Println("\n--- JSON log entries ---")
	for _, e := range entries {
		fmt.Println(e)
	}

	// Logger with central HTTP forwarding.
	centralLogger, err := logmachine.New(logmachine.Options{
		LogFile:   "central_logs.log",
		ErrorFile: "central_errors.log",
		Central: &logmachine.CentralConfig{
			URL:  "https://logmachine.bufferpunk.com",
			Room: "public",
		},
	})
	if err != nil {
		fmt.Printf("failed to create central logger: %v\n", err)
		return
	}
	defer centralLogger.Close()

	centralLogger.Info("Central logging is working!")
}
