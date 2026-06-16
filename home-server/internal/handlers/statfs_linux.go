package handlers

import "syscall"

// syscallStatfs wraps syscall.Statfs_t for disk stats.
type syscallStatfs = syscall.Statfs_t

// statfs wraps syscall.Statfs.
func statfs(path string, stat *syscallStatfs) error {
	return syscall.Statfs(path, stat)
}
