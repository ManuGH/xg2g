package log

import (
	"io"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Config captures options for configuring the global logger.
type Config struct {
	Level   string    // optional log level ("debug", "info", etc.)
	Output  io.Writer // optional writer (defaults to os.Stdout)
	Service string    // optional service name attached to every log entry
}

var (
	once sync.Once
	base zerolog.Logger
)

// Configure initialises the global zerolog logger exactly once.
func Configure(cfg Config) {
	once.Do(func() {
		level := zerolog.InfoLevel
		if cfg.Level != "" {
			if parsed, err := zerolog.ParseLevel(cfg.Level); err == nil {
				level = parsed
			}
		} else if env := os.Getenv("LOG_LEVEL"); env != "" {
			if parsed, err := zerolog.ParseLevel(env); err == nil {
				level = parsed
			}
		}
		zerolog.SetGlobalLevel(level)
		zerolog.TimeFieldFormat = time.RFC3339

		writer := cfg.Output
		if writer == nil {
			writer = os.Stdout
		}

		service := cfg.Service
		if service == "" {
			service = os.Getenv("LOG_SERVICE")
			if service == "" {
				service = "xg2g"
			}
		}

		base = zerolog.New(writer).With().
			Timestamp().
			Str("service", service).
			Str("version", os.Getenv("VERSION")).
			Logger()
	})
}

func logger() zerolog.Logger {
	Configure(Config{})
	return base
}

// Base returns the configured base logger instance.
func Base() zerolog.Logger {
	return logger()
}

// WithComponent returns a child logger annotated with the given component name.
func WithComponent(component string) zerolog.Logger {
	l := logger().With().Str("component", component).Logger()
	return l
}

// Derive attaches arbitrary fields to a child logger using the provided builder function.
func Derive(build func(*zerolog.Context)) zerolog.Logger {
	ctx := logger().With()
	if build != nil {
		build(&ctx)
	}
	return ctx.Logger()
}

func init() {
	Configure(Config{})
}
