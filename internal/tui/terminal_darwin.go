//go:build darwin

package tui

import (
	"syscall"
	"unsafe"
)

const (
	tiocgeta = 0x40487413
	tiocseta = 0x80487414
)

type terminalState syscall.Termios

type windowSize struct{ Row, Col, Xpixel, Ypixel uint16 }

func terminalSize(fd uintptr) (int, int) {
	var size windowSize
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, 0x40087468, uintptr(unsafe.Pointer(&size)))
	if errno != 0 || size.Row == 0 || size.Col == 0 {
		return 30, 96
	}
	return int(size.Row), int(size.Col)
}

func isTerminal(fd uintptr) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, tiocgeta, uintptr(unsafe.Pointer(&t)))
	return errno == 0
}

func makeRaw(fd uintptr) (*terminalState, error) {
	var old syscall.Termios
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, tiocgeta, uintptr(unsafe.Pointer(&old))); errno != 0 {
		return nil, errno
	}
	raw := old
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Oflag &^= syscall.OPOST
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cflag &^= syscall.CSIZE | syscall.PARENB
	raw.Cflag |= syscall.CS8
	raw.Cc[syscall.VMIN], raw.Cc[syscall.VTIME] = 1, 0
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, tiocseta, uintptr(unsafe.Pointer(&raw))); errno != 0 {
		return nil, errno
	}
	state := terminalState(old)
	return &state, nil
}

func restore(fd uintptr, state *terminalState) {
	if state == nil {
		return
	}
	t := syscall.Termios(*state)
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, fd, tiocseta, uintptr(unsafe.Pointer(&t)))
}
