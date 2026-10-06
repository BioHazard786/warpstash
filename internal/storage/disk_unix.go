//go:build !windows

package storage

import (
	"syscall"
)

// DiskSpace returns the physical filesystem statistics for the storage directory.
func (s *DiskStorage) DiskSpace() (DiskSpaceInfo, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(s.baseDir, &stat); err != nil {
		return DiskSpaceInfo{}, err
	}

	bsize := uint64(stat.Bsize)
	return DiskSpaceInfo{
		TotalBytes:     stat.Blocks * bsize,
		FreeBytes:      stat.Bfree * bsize,
		AvailableBytes: stat.Bavail * bsize,
	}, nil
}
