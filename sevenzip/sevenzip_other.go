//go:build !linux && !windows
// +build !linux,!windows

package sevenzip

func binaryName() string {
	return "7z"
}

func binaryData() []byte {
	return nil
}
