package logger

import (
	"fmt"
	"os"
)

type ConsoleLogger struct {
	Base *BaseLogger
	Out  *os.File
	Err  *os.File
}

func NewConsoleLogger(out *os.File, err *os.File) *ConsoleLogger {
	return &ConsoleLogger{
		Out:  out,
		Err:  err,
		Base: &BaseLogger{},
	}
}

func (c *ConsoleLogger) InitConsoleLogger(out *os.File, err *os.File) {
	c.Out = out
	c.Err = err
}

func (c *ConsoleLogger) Debug(fstring string, args ...any) {
	fmt.Fprintln(c.Out, c.Base.formatMessage("DEBUG", fmt.Sprintf(fstring, args...)))
}

func (c *ConsoleLogger) Error(fstring string, args ...any) {
	fmt.Fprintln(c.Err, c.Base.formatMessage("ERROR", fmt.Sprintf(fstring, args...)))
}

func (c *ConsoleLogger) Info(fstring string, args ...any) {
	fmt.Fprintln(c.Err, c.Base.formatMessage("INFO", fmt.Sprintf(fstring, args...)))
}
