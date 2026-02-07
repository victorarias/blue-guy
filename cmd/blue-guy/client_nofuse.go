//go:build !cgo

package main

import (
	"context"
	"fmt"
	"os"
)

func runClient(_ context.Context, _ string) {
	fmt.Fprintln(os.Stderr, "Client mode requires CGO and FUSE.")
	fmt.Fprintln(os.Stderr, "On macOS: brew install fuse-t")
	fmt.Fprintln(os.Stderr, "Then build with: CGO_ENABLED=1 go build ./cmd/blue-guy")
	os.Exit(1)
}
