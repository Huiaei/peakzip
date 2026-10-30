//go:build windows
// +build windows

package sevenzip

import _ "embed"

//go:embed embedded/7z.exe
var embeddedBinary []byte

func binaryName() string {
	return "7z.exe"
}

func binaryData() []byte {
	return embeddedBinary
}
