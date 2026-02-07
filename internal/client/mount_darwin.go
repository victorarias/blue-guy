//go:build cgo

package client

func mountOptions() []string {
	return []string{"-o", "volname=blue-guy"}
}
