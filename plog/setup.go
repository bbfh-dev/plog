package plog

import (
	"log/slog"
)

func noReplaceAttr(groups []string, attr slog.Attr) slog.Attr {
	return attr
}

func SetupDefault(level slog.Level) {
	var opts = &slog.HandlerOptions{
		AddSource:   false,
		Level:       level,
		ReplaceAttr: noReplaceAttr,
	}

	slog.SetDefault(slog.New(NewHandler(opts)))
}
