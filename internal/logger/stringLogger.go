package logger

import (
	"fmt"
	"strings"
)

type StringLogger struct {
	Base   *BaseLogger
	Buffer strings.Builder
}

func NewStringLogger() *StringLogger {
	return &StringLogger{
		Base: &BaseLogger{},
	}
}

func (s *StringLogger) Debug(fstring string, args ...any) {
	s.Buffer.WriteString(s.Base.formatMessage("DEBUG", fmt.Sprintf(fstring, args...)) + "\n")
}

func (s *StringLogger) Error(fstring string, args ...any) {
	s.Buffer.WriteString(s.Base.formatMessage("ERROR", fmt.Sprintf(fstring, args...)) + "\n")
}

func (s *StringLogger) Info(fstring string, args ...any) {
	s.Buffer.WriteString(s.Base.formatMessage("INFO", fmt.Sprintf(fstring, args...)) + "\n")
}

func (s *StringLogger) String() string {
	str := s.Buffer.String()
	s.Buffer.Reset()
	return str
}

func GetString(log Logger, f func()) string {
	f()
	if str, ok := log.(*StringLogger); ok {
		return str.String()
	}
	return "logger.GetString(): Error while coverting Logger to StringLogger"
}

// ! depracted
// ! change its usage in other files
func StrToError(log Logger, f func()) error {
	str := GetString(log, f)

	return fmt.Errorf("%s", str)
}

func StrToErrorNew(log Logger, fstr string) error {
	str := GetString(log, func() { log.Error(fstr) })

	return fmt.Errorf("%s", str)
}
