package utils

import (
	"fmt"
	"os"
	"time"
)

type Logger struct {
	writeFile *os.File
	readFile  *os.File
}

func NewLogger(logFile string) *Logger {
	logger := &Logger{}
	if logFile != "" {
		var err error
		logger.writeFile, err = os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			fmt.Printf("Error opening log file: %v\n", err)
			return logger
		}
		logger.readFile, err = os.Open(logFile)
		if err != nil {
			fmt.Printf("Error opening log file for reading: %v\n", err)
		}
	}
	return logger
}

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

func (l *Logger) Read() string {
	if l.readFile == nil {
		return ""
	}
	buf := make([]byte, 1024)
	n, _ := l.readFile.Read(buf)
	return string(buf[:n])
}

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
