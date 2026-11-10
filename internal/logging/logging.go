// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

// Package logging provides the shared slog setup for the operator.
package logging

import (
	"context"
	"io"
	"log/slog"
	"strings"
)

var levelVar = new(slog.LevelVar) // default: Info

// Setup initialises the global slog logger. Call once at startup.
// Component loggers created via For automatically pick up the new handler
// because they delegate to slog.Default() at call time.
func Setup(w io.Writer, levelStr string) {
	levelVar.Set(parseLevel(levelStr))
	h := slog.NewTextHandler(w, &slog.HandlerOptions{Level: levelVar})
	slog.SetDefault(slog.New(h))
}

// SetLevel changes the active log level at runtime without restart.
func SetLevel(levelStr string) {
	levelVar.Set(parseLevel(levelStr))
}

// Logger is a thin wrapper that always delegates to the current slog.Default(),
// so package-level vars created before Setup() still pick up the configured handler.
type Logger struct {
	component string
}

// For returns a Logger for the given component. Safe to call at package init time.
func For(component string) *Logger {
	return &Logger{component: component}
}

func (l *Logger) log(level slog.Level, msg string, args ...any) {
	slog.Default().With("component", l.component).Log(context.Background(), level, msg, args...)
}

func (l *Logger) Debug(msg string, args ...any) { l.log(slog.LevelDebug, msg, args...) }
func (l *Logger) Info(msg string, args ...any)  { l.log(slog.LevelInfo, msg, args...) }
func (l *Logger) Warn(msg string, args ...any)  { l.log(slog.LevelWarn, msg, args...) }
func (l *Logger) Error(msg string, args ...any) { l.log(slog.LevelError, msg, args...) }

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
