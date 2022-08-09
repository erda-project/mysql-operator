//go:build windows

package log

import (
	"os"
	"syscall"
)

const (
	stdout = syscall.STD_OUTPUT_HANDLE
	stderr = syscall.STD_ERROR_HANDLE
)

var setStdHandle = syscall.NewLazyDLL("kernel32.dll").NewProc("SetStdHandle")

func dup2(f *os.File, fd int) error {
	r, _, lastErr := setStdHandle.Call(uintptr(fd), f.Fd())
	if r == 0 {
		return lastErr
	}
	return nil
}
