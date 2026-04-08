package logger

import (
	"os"
	"strings"
)

type BaseLoggerOpt func(*BaseLogger)

func FormatField(f Format) BaseLoggerOpt {
	return func(l *BaseLogger) {
		l.Format = f
	}
}

func PrefixField(prefix string) BaseLoggerOpt {
	return func(l *BaseLogger) {
		l.Prefix = strings.ToUpper(prefix)
	}
}

func IdField(id string) BaseLoggerOpt {
	return func(l *BaseLogger) {
		l.ID = id
	}
}

func NewLoggerOpt(opts ...BaseLoggerOpt) *BaseLogger {
	l := &BaseLogger{
		Format: BASE_FORMAT,
	}

	if len(opts) > 0 {
		for _, opt := range opts {
			opt(l)
		}
	}

	return l
}

type LoggerConfig struct {
	out      *os.File
	err      *os.File
	baseOpts []BaseLoggerOpt
}

type LogInitOption func(*LoggerConfig)

func WithConsole(out, err *os.File) LogInitOption {
	return func(c *LoggerConfig) {
		c.out = out
		c.err = err
	}
}

func WithBaseOptions(opts ...BaseLoggerOpt) LogInitOption {
	return func(c *LoggerConfig) {
		c.baseOpts = opts
	}
}

type Loggers map[string]Logger

func InitLoggers(opts ...LogInitOption) Loggers {
	logCnfg := &LoggerConfig{}

	for _, opt := range opts {
		opt(logCnfg)
	}

	loggers := make(Loggers)

	base := NewLoggerOpt(logCnfg.baseOpts...)

	if logCnfg.out != nil && logCnfg.err != nil {
		console := NewConsoleLogger(logCnfg.out, logCnfg.err)
		console.Base = base

		loggers["console"] = console
	}

	strLog := NewStringLogger()
	strLog.Base = base

	loggers["string"] = strLog

	return loggers
}
