package host

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/victorarias/blue-guy/internal/gitops"
	pb "github.com/victorarias/blue-guy/internal/proto/gen"
	"google.golang.org/grpc"
)

type Host struct {
	root       string
	port       int
	sessionID  string
	grpcServer *grpc.Server
	fileServer *FileServer
	watcher    *Watcher
	git        *gitops.GitOps
	log        zerolog.Logger
}

func New(root string, port int, sessionID string) (*Host, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("root %q is not a directory", absRoot)
	}

	log := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
		With().Timestamp().Str("role", "host").Logger()

	return &Host{
		root:      absRoot,
		port:      port,
		sessionID: sessionID,
		log:       log,
	}, nil
}

func (h *Host) Start(ctx context.Context) error {
	// Start file watcher
	watcher, err := NewWatcher(h.root, h.log)
	if err != nil {
		return fmt.Errorf("start watcher: %w", err)
	}
	h.watcher = watcher
	go h.watcher.Run()

	// Start git integration
	g, err := gitops.New(h.root, h.sessionID, h.log)
	if err != nil {
		h.log.Warn().Err(err).Msg("Git integration disabled (not a git repo?)")
	} else {
		h.git = g
		if err := h.git.Start(ctx); err != nil {
			h.log.Warn().Err(err).Msg("Failed to start git integration")
			h.git = nil
		}
	}

	// Wire watcher events to git auto-commit
	if h.git != nil {
		changeCh := h.watcher.Subscribe()
		go func() {
			for range changeCh {
				h.git.NotifyChange()
			}
		}()
	}

	h.fileServer = NewFileServer(h.root, h.watcher)
	h.grpcServer = grpc.NewServer()
	pb.RegisterFileServiceServer(h.grpcServer, h.fileServer)

	addr := fmt.Sprintf("0.0.0.0:%d", h.port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	dirName := filepath.Base(h.root)
	h.log.Info().
		Str("path", h.root).
		Str("session", h.sessionID).
		Str("branch", "mob/session-"+h.sessionID).
		Msgf("Starting mob session on %s", h.root)

	fmt.Printf("Session: %s | Branch: mob/session-%s\n", h.sessionID, h.sessionID)
	fmt.Printf("Listening on %s\n", addr)
	fmt.Printf("Join with: blue-guy --connect <YOUR_IP>:%d\n", h.port)
	fmt.Printf("Workspace: %s\n", dirName)

	// Shut down when context is cancelled
	go func() {
		<-ctx.Done()
		h.log.Info().Msg("Shutting down")
		if h.git != nil {
			h.git.Stop()
		}
		h.watcher.Close()
		h.grpcServer.GracefulStop()
	}()

	return h.grpcServer.Serve(lis)
}

func (h *Host) Root() string      { return h.root }
func (h *Host) SessionID() string { return h.sessionID }
