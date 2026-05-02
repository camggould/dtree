package cli

import (
	"os"
	"syscall"
	"unsafe"
)

// isTTY reports whether stdout is an interactive terminal.
func isTTY() bool {
	var termios syscall.Termios
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		os.Stdout.Fd(),
		syscall.TCGETS,
		uintptr(unsafe.Pointer(&termios)),
	)
	return errno == 0
}
