package logging

import (
	"log/slog"
	"os"
	"strings"
)

// Logger wraps slog for structured logging throughout SYNTHOS
type Logger struct {
	handler slog.Handler
	logger  *slog.Logger
}

// LogLevel represents logging levels
type LogLevel string

const (
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
)

// NewLogger creates a new structured logger with the given level
func NewLogger(level string) *Logger {
	logLevel := slog.LevelInfo
	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}

	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})

	return &Logger{
		handler: h,
		logger:  slog.New(h),
	}
}

// Info logs an informational message with attributes
func (l *Logger) Info(msg string, attrs ...slog.Attr) {
	l.logger.LogAttrs(nil, slog.LevelInfo, msg, attrs...)
}

// Debug logs a debug message with attributes
func (l *Logger) Debug(msg string, attrs ...slog.Attr) {
	l.logger.LogAttrs(nil, slog.LevelDebug, msg, attrs...)
}

// Warn logs a warning message with attributes
func (l *Logger) Warn(msg string, attrs ...slog.Attr) {
	l.logger.LogAttrs(nil, slog.LevelWarn, msg, attrs...)
}

// Error logs an error message with attributes
func (l *Logger) Error(msg string, attrs ...slog.Attr) {
	l.logger.LogAttrs(nil, slog.LevelError, msg, attrs...)
}

// WithGroup returns a new logger with the given group
func (l *Logger) WithGroup(name string) *Logger {
	newHandler := l.handler.WithGroup(name)
	return &Logger{
		handler: newHandler,
		logger:  slog.New(newHandler),
	}
}

// WithAttrs returns a new logger with the given attributes added to all logs
func (l *Logger) WithAttrs(attrs ...slog.Attr) *Logger {
	newHandler := l.handler.WithAttrs(attrs)
	return &Logger{
		handler: newHandler,
		logger:  slog.New(newHandler),
	}
}

// Global logger instance
var globalLogger *Logger

// init initializes the global logger (can be overridden)
func init() {
	globalLogger = NewLogger("info")
}

// SetGlobalLogger sets the global logger instance
func SetGlobalLogger(l *Logger) {
	globalLogger = l
}

// GetLogger returns the global logger instance
func GetLogger() *Logger {
	return globalLogger
}

// Convenience functions using global logger
func Info(msg string, attrs ...slog.Attr) {
	globalLogger.Info(msg, attrs...)
}

func Debug(msg string, attrs ...slog.Attr) {
	globalLogger.Debug(msg, attrs...)
}

func Warn(msg string, attrs ...slog.Attr) {
	globalLogger.Warn(msg, attrs...)
}

func Error(msg string, attrs ...slog.Attr) {
	globalLogger.Error(msg, attrs...)
}
