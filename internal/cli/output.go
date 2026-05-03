package cli

import "os"

// isTTY reports whether stdout is an interactive terminal.
//
// Uses Stat() + ModeCharDevice instead of an ioctl so it works portably on
// Linux, macOS, and Windows without per-OS build tags. A char device with
// no other mode bits set is the standard fingerprint of a terminal; pipes,
// files, and sockets all fail this check.
func isTTY() bool {
	st, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}
