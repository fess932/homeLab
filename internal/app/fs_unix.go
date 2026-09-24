//go:build !windows

package app

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}

func unlockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}

func diskSpace(path string) (total, free int64, err error) {
	var sfs unix.Statfs_t
	if err := unix.Statfs(path, &sfs); err != nil {
		return 0, 0, err
	}
	return int64(sfs.Blocks) * int64(sfs.Bsize), int64(sfs.Bavail) * int64(sfs.Bsize), nil
}
