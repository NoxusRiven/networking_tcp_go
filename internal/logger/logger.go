package logger

import (
	"fmt"
	"time"
)

/**
*Loggers are created with a single function that let's you customize your loggers, wich types of loggers to add and how format thier messages. Defined in "logger/config.go"

*To access loggers you need to create instance of "Loggers" and use key of logger you inted to use
'loggers["console"]' ect. You can define your own keys with provided function in "logger/config.go"

*Use logger messages like how would you use formated strings (Sprintf)

*/

type Format string

const (
	BASE_FORMAT         Format = "[%s]: %s"
	BASE_PREFIX         Format = "[%s][%s]: %s"
	BASE_DATE           Format = "[ %s ][%s]: %s"
	BASE_PREFIX_ID      Format = "[%s][%s - %s]: %s"
	BASE_PREFIX_DATE    Format = "[%s][%s][%s]: %s"
	BASE_PREFIX_ID_DATE Format = "[%s][%s][%s - %s]: %s"
)

const ()

type Logger interface {
	Debug(fstring string, args ...any)
	Info(fstring string, args ...any)
	Error(fstring string, args ...any)
}

type BaseLogger struct {
	Prefix string
	ID     string
	Format Format
}

func (b *BaseLogger) formatMessage(level string, msg string) string {
	var formated string
	switch b.Format {
	case BASE_FORMAT:
		formated = fmt.Sprintf(string(BASE_FORMAT), level, msg)

	case BASE_PREFIX:
		formated = fmt.Sprintf(string(BASE_PREFIX), level, b.Prefix, msg)

	case BASE_DATE:
		formated = fmt.Sprintf(string(BASE_DATE), time.Now().Format("2006-01-02 15:04:05"), level, msg)

	case BASE_PREFIX_ID:
		formated = fmt.Sprintf(string(BASE_PREFIX_ID), level, b.Prefix, b.ID, msg)

	case BASE_PREFIX_DATE:
		formated = fmt.Sprintf(string(BASE_PREFIX_DATE), time.Now().Format("2006-01-02 15:04:05"), level, b.Prefix, msg)

	case BASE_PREFIX_ID_DATE:
		formated = fmt.Sprintf(string(BASE_PREFIX_ID_DATE), time.Now().Format("2006-01-02 15:04:05"), level, b.Prefix, b.ID, msg)
	}

	return formated
}
