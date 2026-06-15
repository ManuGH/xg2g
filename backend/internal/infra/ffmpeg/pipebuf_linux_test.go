//go:build linux

package ffmpeg

import "syscall"

// fcntlFSetPipeSz is the Linux F_SETPIPE_SZ command (not exported by the syscall package).
const fcntlFSetPipeSz = 1031

// growPipeBuffer enlarges the kernel buffer of the pipe behind fd. A large buffer lets the
// writer get far ahead of the reader, which is the condition the stderr-tail truncation race
// needs (the prod scanner is normally the pace-setter under backpressure and stays caught up,
// so on platforms without a resizable pipe the loss is not reproducible).
func growPipeBuffer(fd uintptr, size int) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, fd, fcntlFSetPipeSz, uintptr(size)); errno != 0 {
		return errno
	}
	return nil
}
