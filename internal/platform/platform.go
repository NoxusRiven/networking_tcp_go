package platform

import (
	"fmt"
	"path/filepath"
	"runtime"
)

func Executable(name string) string {
	path := filepath.Join("..", "..", "bin", name)

	if runtime.GOOS == "windows" {
		path += ".exe"
	}

	return path
}

type Color uint8

const (
	DEFAULT_COLOR = iota

	GRAY
	DARK_GRAY
	WHITE

	GREEN
	YELLOW
	RED
	Cyan
)

const (
	colorResetLinux  = "\033[0m"
	colorRedLinux    = "\033[31m"
	colorYellowLinux = "\033[33m"
	colorGreenLinux  = "\033[32m"

	colorCyanLinux = "\033[36m"

	colorBrightBlack      = "\033[90m"
	colorWhiteLinux       = "\033[37m"
	colorBrightWhiteLinux = "\033[97m"
)

func colorTerminalLinux(color Color, msg string) string {
	var colorString string

	switch color {
	case WHITE:
		colorString = colorBrightWhiteLinux
	case GRAY:
		colorString = colorWhiteLinux
	case DARK_GRAY:
		colorString = colorBrightBlack
	case Cyan:
		colorString = colorCyanLinux
	case GREEN:
		colorString = colorGreenLinux
	case YELLOW:
		colorString = colorYellowLinux
	case RED:
		colorString = colorRedLinux
	default:
		return msg
	}
	return colorString + msg + colorResetLinux
}

func colorTerminalWindows(color Color, msg string) string {
	fmt.Println("[colorTerminalWindows] Windows terminal coloring isn't supported yet...")
	switch color {
	case GREEN:
		fallthrough
	case YELLOW:
		fallthrough
	case RED:
		fallthrough
	default:
		return msg
	}
}

func TerminalColoring(color Color, msg string) string {
	switch runtime.GOOS {
	case "linux":
		return colorTerminalLinux(color, msg)
	case "windows":
		return colorTerminalWindows(color, msg)
	default:
		panic("[ERROR][Terminal Coloring] - System not supported for function")
	}
}
