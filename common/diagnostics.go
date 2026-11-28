package common

import (
	"fmt"
	"log"
)

// LogOption configures optional diagnostics for a loader or player.
type LogOption func(*diagnostics)

type diagnostics struct {
	verbosity int
	logger    *log.Logger
}

// WithVerbosity controls diagnostic output: zero (the default) is silent,
// and a positive value enables loader details and playback warnings.
func WithVerbosity(level int) LogOption {
	return func(config *diagnostics) { config.verbosity = level }
}

// WithLogger selects the logger used when diagnostics are enabled.
// A nil logger selects the standard logger.
func WithLogger(logger *log.Logger) LogOption {
	return func(config *diagnostics) { config.logger = logger }
}

// Diagnostics is a logger whose zero value discards diagnostic messages.
type Diagnostics struct {
	logger *log.Logger
}

// NewDiagnostics resolves logging settings without changing the global logger.
func NewDiagnostics(options ...LogOption) Diagnostics {
	var config diagnostics
	for _, option := range options {
		option(&config)
	}
	if config.verbosity <= 0 {
		return Diagnostics{}
	}
	if config.logger == nil {
		config.logger = log.Default()
	}
	return Diagnostics{logger: config.logger}
}

func (diagnostics Diagnostics) Enabled() bool { return diagnostics.logger != nil }

func (diagnostics Diagnostics) Printf(format string, args ...any) {
	if diagnostics.Enabled() {
		_ = diagnostics.logger.Output(2, fmt.Sprintf(format, args...))
	}
}
