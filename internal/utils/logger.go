package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Logger writes timestamped lines to a log file (and allows reading recent data).
type Logger struct {
	writeFile *os.File
	readFile  *os.File
}

// defaultLogPath returns the path to the default SDSM log file using the
// same naming as Paths.LogFile(), rooted next to the running executable.
func defaultLogPath() string {
	exe, err := os.Executable()
	if err == nil {
		if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil && resolved != "" {
			exe = resolved
		}
		execDir := filepath.Dir(exe)
		// Use the same convention as utils.Paths.LogFile()
		return NewPaths(execDir).LogFile()
	}
	// Fallback to a safe temp location
	return NewPaths(filepath.Join(os.TempDir(), "sdsm")).LogFile()
}

// writeToDefaultLog attempts to write a single timestamped line to the default
// SDSM log. If it fails, it falls back to stderr.
func writeToDefaultLog(message string) {
	path := defaultLogPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		// Last resort: stderr
		fmt.Fprintf(os.Stderr, "%s: %s\n", time.Now().Format("2006-01-02 15:04:05"), message)
		return
	}
	defer f.Close()
	ts := time.Now().Format("2006-01-02 15:04:05")
	_, _ = f.WriteString(fmt.Sprintf("%s: %s\n", ts, message))
}

// NewLogger opens the given log file for appending and a parallel read handle.
// If the file cannot be opened, logs will be written to stdout.
func NewLogger(logFile string) *Logger {
	logger := &Logger{}
	// Ensure we always have a target path; prefer provided path, else default.
	if logFile == "" {
		logFile = defaultLogPath()
	}

	// Try to ensure directory exists first
	_ = os.MkdirAll(filepath.Dir(logFile), 0o755)

	var err error
	logger.writeFile, err = os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		writeToDefaultLog(fmt.Sprintf("Error opening log file (%s): %v", logFile, err))
		// Return a logger that will fall back to stdout on Write()
		return logger
	}
	logger.readFile, err = os.Open(logFile)
	if err != nil {
		writeToDefaultLog(fmt.Sprintf("Error opening log file for reading (%s): %v", logFile, err))
	}
	return logger
}

// Write appends a timestamped message to the log (or stdout when no file).
func (l *Logger) Write(message string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logMessage := fmt.Sprintf("%s: %s\n", timestamp, message)
	if l.writeFile != nil {
		l.writeFile.WriteString(logMessage)
		l.writeFile.Sync()
	} else {
		fmt.Print(logMessage)
	}
}

// Read reads up to 1 KiB from the current read handle for quick previews.
func (l *Logger) Read() string {
	if l.readFile == nil {
		return ""
	}
	buf := make([]byte, 1024)
	n, _ := l.readFile.Read(buf)
	return string(buf[:n])
}

// Close flushes and closes underlying file handles.
func (l *Logger) Close() {
	if l.writeFile != nil {
		l.writeFile.Close()
	}
	if l.readFile != nil {
		l.readFile.Close()
	}
}

// File returns the underlying write file handle when available.
func (l *Logger) File() *os.File {
	if l == nil {
		return nil
	}
	return l.writeFile
}
