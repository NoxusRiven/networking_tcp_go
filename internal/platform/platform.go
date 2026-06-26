package platform

import (
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