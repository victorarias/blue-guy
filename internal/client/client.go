//go:build cgo

package client

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/winfsp/cgofuse/fuse"
	pb "github.com/victorarias/blue-guy/internal/proto/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	addr      string
	mountPath string
	conn      *grpc.ClientConn
	fsHost    *fuse.FileSystemHost
	log       zerolog.Logger
}

func New(addr string) *Client {
	log := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
		With().Timestamp().Str("role", "client").Logger()

	return &Client{
		addr: addr,
		log:  log,
	}
}

func (c *Client) Start(ctx context.Context) error {
	c.log.Info().Str("addr", c.addr).Msg("Connecting to host")

	conn, err := grpc.NewClient(c.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("connect to %s: %w", c.addr, err)
	}
	c.conn = conn

	fc := pb.NewFileServiceClient(conn)

	// Probe the connection by listing the root
	resp, err := fc.ReadDir(ctx, &pb.ReadDirRequest{Path: "/"})
	if err != nil {
		conn.Close()
		return fmt.Errorf("probe host: %w", err)
	}

	// Determine mount path: ~/mob/<workspace-name>
	// We infer the workspace name from the directory listing (or use addr as fallback)
	workspaceName := inferWorkspaceName(c.addr)
	c.mountPath = filepath.Join(os.Getenv("HOME"), "mob", workspaceName)

	if err := os.MkdirAll(c.mountPath, 0755); err != nil {
		conn.Close()
		return fmt.Errorf("create mount point %s: %w", c.mountPath, err)
	}

	c.log.Info().
		Str("mount", c.mountPath).
		Int("files", len(resp.Entries)).
		Msg("Connected to host workspace")

	fmt.Printf("Mounted workspace at %s\n", c.mountPath)
	fmt.Printf("Ready. All changes sync to host.\n")

	remoteFS := NewRemoteFS(fc, c.log)
	c.fsHost = fuse.NewFileSystemHost(remoteFS)

	// Unmount on context cancellation
	go func() {
		<-ctx.Done()
		c.log.Info().Msg("Unmounting FUSE filesystem")
		c.fsHost.Unmount()
	}()

	// Mount blocks until unmounted
	ok := c.fsHost.Mount(c.mountPath, mountOptions())
	if !ok {
		conn.Close()
		return fmt.Errorf("FUSE mount failed — is FUSE-T installed? (brew install fuse-t)")
	}

	conn.Close()
	return nil
}

func (c *Client) MountPath() string {
	return c.mountPath
}

// inferWorkspaceName extracts a mount name from the host address.
func inferWorkspaceName(addr string) string {
	// For now, just use the host portion
	for i := 0; i < len(addr); i++ {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}
