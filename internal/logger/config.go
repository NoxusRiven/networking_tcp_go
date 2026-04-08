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

const (
	LogCount = 2
)

type LoggerConfig struct {
	keys []string

	out *os.File
	err *os.File

	baseOpts []BaseLoggerOpt
}

type LogInitOption func(*LoggerConfig)

// 1 string, 2 console, only first 2 labels will be concidered
func CustomLoggerKeys(keys ...string) LogInitOption {
	return func(c *LoggerConfig) {
		c.keys = keys
	}
}

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
	logCnfg := &LoggerConfig{
		keys: []string{"string"},
	}

	for _, opt := range opts {
		opt(logCnfg)
	}

	loggers := make(Loggers)

	base := NewLoggerOpt(logCnfg.baseOpts...)

	if logCnfg.out != nil && logCnfg.err != nil {
		console := NewConsoleLogger(logCnfg.out, logCnfg.err)
		console.Base = base

		if len(logCnfg.keys) < 2 {
			loggers["console"] = console
		} else {
			loggers[logCnfg.keys[1]] = console
		}

	}

	strLog := NewStringLogger()
	strLog.Base = base

	loggers[logCnfg.keys[0]] = strLog

	return loggers
}
