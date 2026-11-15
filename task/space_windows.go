//go:build windows

package task

import "golang.org/x/sys/windows"

// availableSpace returns the number of bytes free for the current user on
// the volume containing dir, using GetDiskFreeSpaceEx.
func availableSpace(dir string) (int64, error) {
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	if err := windows.GetDiskFreeSpaceEx(p, &freeBytesAvailable, &totalBytes, &totalFreeBytes); err != nil {
		return 0, err
	}
	return int64(freeBytesAvailable), nil
}
