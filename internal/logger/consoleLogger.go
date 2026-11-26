package logger

import (
	"fmt"
	"io"
	"networking/tcp/internal/platform"
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

func (c *ConsoleLogger) coloredTerminalMsg(w io.Writer, level string, color platform.Color, fstring string, args ...any) {
	plainStr := c.Base.formatMessage(level, fmt.Sprintf(fstring, args...))
	fmt.Fprintln(w, platform.TerminalColoring(color, plainStr))
}

// for testing specific cases so it will stand out
func (c *ConsoleLogger) Test(fstring string, args ...any) {
	c.coloredTerminalMsg(c.Out, "Test", platform.Cyan, fstring, args...)
}

func (c *ConsoleLogger) Debug(fstring string, args ...any) {
	c.coloredTerminalMsg(c.Out, "DEBUG", platform.WHITE, fstring, args...)
}

func (c *ConsoleLogger) Info(fstring string, args ...any) {
	c.coloredTerminalMsg(c.Out, "INFO", platform.DARK_GRAY, fstring, args...)
}

func (c *ConsoleLogger) Warning(fstring string, args ...any) {
	c.coloredTerminalMsg(c.Out, "WARNING", platform.YELLOW, fstring, args...)
}

func (c *ConsoleLogger) Error(fstring string, args ...any) {
	c.coloredTerminalMsg(c.Out, "ERROR", platform.RED, fstring, args...)
}
