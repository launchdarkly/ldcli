//go:build darwin || linux

package render

import (
	"os"
	"syscall"
	"unsafe"
)

// winsize mirrors the C struct filled by the TIOCGWINSZ ioctl.
type winsize struct {
	rows, cols, xpixel, ypixel uint16
}

// TerminalWidth returns stdout's column count via the TIOCGWINSZ ioctl, or 0 when
// stdout isn't a terminal (piped/redirected). Stdlib-only — no golang.org/x/term.
func TerminalWidth() int {
	ws := &winsize{}
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		os.Stdout.Fd(),
		uintptr(tiocgwinsz),
		uintptr(unsafe.Pointer(ws)),
	)
	if errno != 0 || ws.cols == 0 {
		return 0
	}
	return int(ws.cols)
}
