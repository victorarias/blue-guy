//go:build cgo

package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/victorarias/blue-guy/internal/client"
)

func runClient(ctx context.Context, connect string) {
	addr := connect
	if !strings.Contains(addr, ":") {
		addr += ":7654"
	}
	c := client.New(addr)
	if err := c.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
