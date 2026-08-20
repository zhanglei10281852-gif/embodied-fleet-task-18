package apperr

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

// Logger wraps zerolog for structured logging across packages.
type Logger struct {
	log zerolog.Logger
}

// NewLogger creates a Logger at the given level. pretty=true uses ConsoleWriter.
func NewLogger(level string, pretty bool) *Logger {
	zerolog.TimeFieldFormat = time.RFC3339Nano
	var lvl zerolog.Level
	switch level {
	case "debug":
		lvl = zerolog.DebugLevel
	case "warn":
		lvl = zerolog.WarnLevel
	case "error":
		lvl = zerolog.ErrorLevel
	default:
		lvl = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(lvl)
	var w zerolog.ConsoleWriter
	if pretty {
		w = zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
	}
	var log zerolog.Logger
	if pretty {
		log = zerolog.New(w).With().Timestamp().Logger()
	} else {
		log = zerolog.New(os.Stderr).With().Timestamp().Logger()
	}
	return &Logger{log: log}
}

// Info logs an info message with optional fields.
func (l *Logger) Info(msg string, fields ...Field) {
	l.event("info", msg, fields)
}

// Warn logs a warning message.
func (l *Logger) Warn(msg string, fields ...Field) {
	l.event("warn", msg, fields)
}

// Error logs an error message.
func (l *Logger) Error(msg string, err error, fields ...Field) {
	f := append([]Field{{Key: "error", Value: err}}, fields...)
	l.event("error", msg, f)
}

// Debug logs a debug message.
func (l *Logger) Debug(msg string, fields ...Field) {
	l.event("debug", msg, fields)
}

// Field is a structured logging field.
type Field struct {
	Key   string
	Value any
}

// F is a shorthand for creating a Field.
func F(key string, val any) Field { return Field{Key: key, Value: val} }

func (l *Logger) event(level, msg string, fields []Field) {
	var evt *zerolog.Event
	switch level {
	case "info":
		evt = l.log.Info()
	case "warn":
		evt = l.log.Warn()
	case "error":
		evt = l.log.Error()
	case "debug":
		evt = l.log.Debug()
	default:
		evt = l.log.Info()
	}
	for _, f := range fields {
		switch v := f.Value.(type) {
		case string:
			evt = evt.Str(f.Key, v)
		case int:
			evt = evt.Int(f.Key, v)
		case int64:
			evt = evt.Int64(f.Key, v)
		case bool:
			evt = evt.Bool(f.Key, v)
		case error:
			evt = evt.AnErr(f.Key, v)
		default:
			evt = evt.Interface(f.Key, v)
		}
	}
	evt.Msg(msg)
}

// Child returns a logger with the given context fields.
func (l *Logger) Child(fields ...Field) *Logger {
	ctx := l.log.With()
	for _, f := range fields {
		switch v := f.Value.(type) {
		case string:
			ctx = ctx.Str(f.Key, v)
		case int:
			ctx = ctx.Int(f.Key, v)
		default:
			ctx = ctx.Interface(f.Key, v)
		}
	}
	return &Logger{log: ctx.Logger()}
}

// NopLogger returns a logger that discards all output.
func NopLogger() *Logger {
	return &Logger{log: zerolog.Nop()}
}
