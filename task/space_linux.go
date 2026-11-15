//go:build linux

package task

import "golang.org/x/sys/unix"

// availableSpace returns the number of bytes free for an unprivileged user
// on the filesystem containing dir, using statfs.
func availableSpace(dir string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, err
	}
	// Bavail is the number of free blocks available to unprivileged users;
	// Bsize is the fundamental block size. This avoids counting the
	// reserved blocks that only root may use.
	return int64(st.Bavail) * int64(st.Bsize), nil
}
