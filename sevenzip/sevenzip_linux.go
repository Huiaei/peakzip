//go:build linux
// +build linux

package sevenzip

import _ "embed"

//go:embed embedded/7z_linux
var embeddedBinary []byte

func binaryName() string {
	return "7z"
}

func binaryData() []byte {
	return embeddedBinary
}
