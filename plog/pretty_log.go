// Modified version of: https://github.com/dusted-go/logging
package plog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
)

const (
	timeFormat = "[15:04:05]"

	reset = "\033[0m"

	// black        = 30
	// red          = 31
	// green        = 32
	// yellow       = 33
	// blue         = 34
	// magenta      = 35
	cyan      = 36
	lightGray = 37
	darkGray  = 90
	lightRed  = 91
	// lightGreen   = 92
	lightYellow  = 93
	lightBlue    = 94
	lightMagenta = 95
	// lightCyan    = 96
	white = 97
)

func colorizer(colorCode int, v string) string {
	return fmt.Sprintf("\033[%sm%s%s", strconv.Itoa(colorCode), v, reset)
}

type Handler struct {
	handler          slog.Handler
	record           func([]string, slog.Attr) slog.Attr
	buffer           *bytes.Buffer
	mutex            *sync.Mutex
	writer           io.Writer
	colorize         bool
	outputEmptyAttrs bool
}

func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{
		handler:  h.handler.WithAttrs(attrs),
		buffer:   h.buffer,
		record:   h.record,
		mutex:    h.mutex,
		writer:   h.writer,
		colorize: h.colorize,
	}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{
		handler:  h.handler.WithGroup(name),
		buffer:   h.buffer,
		record:   h.record,
		mutex:    h.mutex,
		writer:   h.writer,
		colorize: h.colorize,
	}
}

func (h *Handler) computeAttrs(
	ctx context.Context,
	r slog.Record,
) (map[string]any, error) {
	h.mutex.Lock()
	defer func() {
		h.buffer.Reset()
		h.mutex.Unlock()
	}()
	if err := h.handler.Handle(ctx, r); err != nil {
		return nil, fmt.Errorf("error when calling inner handler's Handle: %w", err)
	}

	var attrs map[string]any
	err := json.Unmarshal(h.buffer.Bytes(), &attrs)
	if err != nil {
		return nil, fmt.Errorf("error when unmarshaling inner handler's Handle result: %w", err)
	}
	return attrs, nil
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	colorize := func(code int, value string) string {
		return value
	}
	if h.colorize {
		colorize = colorizer
	}

	var level string
	levelAttr := slog.Attr{
		Key:   slog.LevelKey,
		Value: slog.AnyValue(r.Level),
	}
	if h.record != nil {
		levelAttr = h.record([]string{}, levelAttr)
	}

	if !levelAttr.Equal(slog.Attr{}) {
		level = levelAttr.Value.String() + ":"

		if r.Level <= slog.LevelDebug {
			level = colorize(lightGray, level)
		} else if r.Level <= slog.LevelInfo {
			level = colorize(cyan, level)
		} else if r.Level < slog.LevelWarn {
			level = colorize(lightBlue, level)
		} else if r.Level < slog.LevelError {
			level = colorize(lightYellow, level)
		} else if r.Level <= slog.LevelError+1 {
			level = colorize(lightRed, level)
		} else if r.Level > slog.LevelError+1 {
			level = colorize(lightMagenta, level)
		}
	}

	var timestamp string
	timeAttr := slog.Attr{
		Key:   slog.TimeKey,
		Value: slog.StringValue(r.Time.Format(timeFormat)),
	}
	if h.record != nil {
		timeAttr = h.record([]string{}, timeAttr)
	}
	if !timeAttr.Equal(slog.Attr{}) {
		timestamp = colorize(lightGray, timeAttr.Value.String())
	}

	var msg string
	msgAttr := slog.Attr{
		Key:   slog.MessageKey,
		Value: slog.StringValue(r.Message),
	}
	if h.record != nil {
		msgAttr = h.record([]string{}, msgAttr)
	}
	if !msgAttr.Equal(slog.Attr{}) {
		msg = colorize(white, msgAttr.Value.String())
	}

	attrs, err := h.computeAttrs(ctx, r)
	if err != nil {
		return err
	}

	var attrsAsBytes []byte
	if h.outputEmptyAttrs || len(attrs) > 0 {
		if len(attrs) >= 3 {
			attrsAsBytes, err = json.MarshalIndent(attrs, "", "    ")
		} else {
			attrsAsBytes, err = json.Marshal(attrs)
		}
		if err != nil {
			return fmt.Errorf("error when marshaling attrs: %w", err)
		}
	}

	out := strings.Builder{}
	if len(timestamp) > 0 {
		out.WriteString(timestamp)
		out.WriteString(" ")
	}
	if len(level) > 0 {
		out.WriteString(level)
		out.WriteString(" ")
	}
	if len(msg) > 0 {
		out.WriteString(msg)
		out.WriteString(" ")
	}
	if len(attrsAsBytes) > 0 {
		out.WriteString(colorize(darkGray, string(attrsAsBytes)))
	}

	_, err = io.WriteString(h.writer, out.String()+"\n")
	if err != nil {
		return err
	}

	return nil
}

func suppressDefaults(
	next func([]string, slog.Attr) slog.Attr,
) func([]string, slog.Attr) slog.Attr {
	return func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey ||
			a.Key == slog.LevelKey ||
			a.Key == slog.MessageKey {
			return slog.Attr{}
		}
		if next == nil {
			return a
		}
		return next(groups, a)
	}
}

func New(handlerOptions *slog.HandlerOptions, options ...option) *Handler {
	if handlerOptions == nil {
		handlerOptions = &slog.HandlerOptions{}
	}

	buf := &bytes.Buffer{}
	handler := &Handler{
		buffer: buf,
		handler: slog.NewJSONHandler(buf, &slog.HandlerOptions{
			Level:       handlerOptions.Level,
			AddSource:   handlerOptions.AddSource,
			ReplaceAttr: suppressDefaults(handlerOptions.ReplaceAttr),
		}),
		record: handlerOptions.ReplaceAttr,
		mutex:  &sync.Mutex{},
	}

	for _, opt := range options {
		opt(handler)
	}

	return handler
}

func NewHandler(opts *slog.HandlerOptions) *Handler {
	return New(opts, WithDestinationWriter(os.Stdout), WithColor(), WithOutputEmptyAttrs())
}

type option func(h *Handler)

func WithDestinationWriter(writer io.Writer) option {
	return func(h *Handler) {
		h.writer = writer
	}
}

func WithColor() option {
	return func(h *Handler) {
		h.colorize = true
	}
}

func WithOutputEmptyAttrs() option {
	return func(h *Handler) {
		h.outputEmptyAttrs = true
	}
}
