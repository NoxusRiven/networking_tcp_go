package main

import (
	"fmt"
	"networking/tcp/internal/logger"
)

var Log logger.Loggers = logger.InitLoggers(
	logger.WithBaseOptions(
		logger.PrefixField("test"),
		logger.FormatField(logger.BASE_PREFIX),
	),
)

func main() {
	str := logger.GetString(Log["string"], func() {
		Log["string"].Debug("test message")
	})

	fmt.Print(str)
}
