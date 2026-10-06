//go:build windows

package storage

import (
	"syscall"
	"unsafe"
)

var (
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceExW = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// DiskSpace returns the physical filesystem statistics for Windows.
func (s *DiskStorage) DiskSpace() (DiskSpaceInfo, error) {
	ptr, err := syscall.UTF16PtrFromString(s.baseDir)
	if err != nil {
		return DiskSpaceInfo{}, err
	}

	var freeBytesAvailable, totalNumberOfBytes, totalNumberOfFreeBytes uint64
	r1, _, err := getDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(ptr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalNumberOfBytes)),
		uintptr(unsafe.Pointer(&totalNumberOfFreeBytes)),
	)
	if r1 == 0 {
		return DiskSpaceInfo{}, err
	}

	return DiskSpaceInfo{
		TotalBytes:     totalNumberOfBytes,
		FreeBytes:      totalNumberOfFreeBytes,
		AvailableBytes: freeBytesAvailable,
	}, nil
}
