package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"
	"github.com/victorarias/blue-guy/internal/host"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "Print version and exit")
	connect := flag.String("connect", "", "Host address to connect to (client mode)")
	port := flag.Int("port", 7654, "Port to listen on (host mode)")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if *connect != "" {
		runClient(ctx, *connect)
		return
	}

	// Host mode
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	sessionID := uuid.New().String()[:8]
	h, err := host.New(cwd, *port, sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := h.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
