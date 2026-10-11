package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

type errorRecords struct {
	mu       sync.Mutex
	messages []string
}

func (r *errorRecords) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.messages...)
}

// The pin logs deferred Finish/Submit failures, rather than returning them to
// OnDraw. Observe both the public GoGPU logger and package-level slog default.
// Any error record makes this diagnostic fail, even if stale pixels matched.
type errorHandler struct {
	base    slog.Handler
	errors  *errorRecords
	context []slog.Attr
}

func (h errorHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= slog.LevelError || h.base.Enabled(ctx, level)
}
func (h errorHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Level >= slog.LevelError {
		var text strings.Builder
		text.WriteString(record.Message)
		appendAttr := func(attr slog.Attr) bool {
			fmt.Fprintf(&text, " %s=%v", attr.Key, attr.Value)
			return true
		}
		for _, attr := range h.context {
			appendAttr(attr)
		}
		record.Attrs(appendAttr)
		h.errors.mu.Lock()
		h.errors.messages = append(h.errors.messages, text.String())
		h.errors.mu.Unlock()
	}
	return h.base.Handle(ctx, record)
}
func (h errorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return errorHandler{base: h.base.WithAttrs(attrs), errors: h.errors,
		context: append(append([]slog.Attr(nil), h.context...), attrs...)}
}
func (h errorHandler) WithGroup(name string) slog.Handler {
	return errorHandler{base: h.base.WithGroup(name), errors: h.errors, context: h.context}
}
