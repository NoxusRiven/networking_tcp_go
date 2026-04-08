package main

import (
	"fmt"
	"networking/tcp/internal/logger"
	"os"
)

var Log logger.Loggers = logger.InitLoggers(
	logger.CustomLoggerKeys("s", "c", "d", "e", "f", "g"),
	logger.WithConsole(os.Stdout, os.Stderr),
	logger.WithBaseOptions(
		logger.PrefixField("test"),
		logger.FormatField(logger.BASE_PREFIX),
	),
)

func main() {
	str := logger.GetString(Log["s"], func() {
		Log["s"].Debug("test message")
	})

	fmt.Print(str)

	Log["c"].Error("Error has occured!")
}
