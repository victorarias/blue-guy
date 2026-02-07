//go:build cgo

package client

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/winfsp/cgofuse/fuse"
	pb "github.com/victorarias/blue-guy/internal/proto/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RemoteFS is a FUSE filesystem that proxies all operations to a remote host via gRPC.
type RemoteFS struct {
	fuse.FileSystemBase
	client  pb.FileServiceClient
	log     zerolog.Logger
	timeout time.Duration

	// File handle tracking
	mu      sync.Mutex
	nextFH  uint64
	handles map[uint64]string // fh -> path
}

func NewRemoteFS(client pb.FileServiceClient, log zerolog.Logger) *RemoteFS {
	return &RemoteFS{
		client:  client,
		log:     log,
		timeout: 10 * time.Second,
		nextFH:  1,
		handles: make(map[uint64]string),
	}
}

func (fs *RemoteFS) ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), fs.timeout)
}

func (fs *RemoteFS) allocFH(path string) uint64 {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fh := fs.nextFH
	fs.nextFH++
	fs.handles[fh] = path
	return fh
}

func (fs *RemoteFS) freeFH(fh uint64) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	delete(fs.handles, fh)
}

func (fs *RemoteFS) pathForFH(fh uint64) string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.handles[fh]
}

func (fs *RemoteFS) Getattr(path string, stat *fuse.Stat_t, fh uint64) int {
	ctx, cancel := fs.ctx()
	defer cancel()

	resp, err := fs.client.Stat(ctx, &pb.StatRequest{Path: path})
	if err != nil {
		return fs.errToFuse(err, "Getattr", path)
	}

	fillStat(stat, resp.Info)
	return 0
}

func (fs *RemoteFS) Readdir(path string, fill func(name string, stat *fuse.Stat_t, ofst int64) bool, ofst int64, fh uint64) int {
	ctx, cancel := fs.ctx()
	defer cancel()

	resp, err := fs.client.ReadDir(ctx, &pb.ReadDirRequest{Path: path})
	if err != nil {
		return fs.errToFuse(err, "Readdir", path)
	}

	// Always include . and ..
	fill(".", nil, 0)
	fill("..", nil, 0)

	for _, entry := range resp.Entries {
		var st fuse.Stat_t
		fillStat(&st, entry)
		if !fill(entry.Name, &st, 0) {
			break
		}
	}
	return 0
}

func (fs *RemoteFS) Open(path string, flags int) (int, uint64) {
	// Verify the file exists via Stat
	ctx, cancel := fs.ctx()
	defer cancel()

	_, err := fs.client.Stat(ctx, &pb.StatRequest{Path: path})
	if err != nil {
		return fs.errToFuse(err, "Open", path), ^uint64(0)
	}

	fh := fs.allocFH(path)
	return 0, fh
}

func (fs *RemoteFS) Release(path string, fh uint64) int {
	fs.freeFH(fh)
	return 0
}

func (fs *RemoteFS) Opendir(path string) (int, uint64) {
	ctx, cancel := fs.ctx()
	defer cancel()

	_, err := fs.client.Stat(ctx, &pb.StatRequest{Path: path})
	if err != nil {
		return fs.errToFuse(err, "Opendir", path), ^uint64(0)
	}

	fh := fs.allocFH(path)
	return 0, fh
}

func (fs *RemoteFS) Releasedir(path string, fh uint64) int {
	fs.freeFH(fh)
	return 0
}

func (fs *RemoteFS) Read(path string, buff []byte, ofst int64, fh uint64) int {
	ctx, cancel := fs.ctx()
	defer cancel()

	resp, err := fs.client.ReadFile(ctx, &pb.ReadFileRequest{
		Path:   path,
		Offset: ofst,
		Length: int64(len(buff)),
	})
	if err != nil {
		return fs.errToFuse(err, "Read", path)
	}

	n := copy(buff, resp.Data)
	return n
}

func (fs *RemoteFS) Write(path string, buff []byte, ofst int64, fh uint64) int {
	ctx, cancel := fs.ctx()
	defer cancel()

	_, err := fs.client.WriteFile(ctx, &pb.WriteFileRequest{
		Path:   path,
		Data:   buff,
		Offset: ofst,
	})
	if err != nil {
		return fs.errToFuse(err, "Write", path)
	}

	return len(buff)
}

func (fs *RemoteFS) Create(path string, flags int, mode uint32) (int, uint64) {
	ctx, cancel := fs.ctx()
	defer cancel()

	_, err := fs.client.Create(ctx, &pb.CreateRequest{
		Path: path,
		Mode: mode,
	})
	if err != nil {
		return fs.errToFuse(err, "Create", path), ^uint64(0)
	}

	fh := fs.allocFH(path)
	return 0, fh
}

func (fs *RemoteFS) Mkdir(path string, mode uint32) int {
	ctx, cancel := fs.ctx()
	defer cancel()

	_, err := fs.client.Mkdir(ctx, &pb.MkdirRequest{
		Path: path,
		Mode: mode,
	})
	if err != nil {
		return fs.errToFuse(err, "Mkdir", path)
	}
	return 0
}

func (fs *RemoteFS) Unlink(path string) int {
	ctx, cancel := fs.ctx()
	defer cancel()

	_, err := fs.client.Remove(ctx, &pb.RemoveRequest{Path: path})
	if err != nil {
		return fs.errToFuse(err, "Unlink", path)
	}
	return 0
}

func (fs *RemoteFS) Rmdir(path string) int {
	ctx, cancel := fs.ctx()
	defer cancel()

	_, err := fs.client.Remove(ctx, &pb.RemoveRequest{Path: path})
	if err != nil {
		return fs.errToFuse(err, "Rmdir", path)
	}
	return 0
}

func (fs *RemoteFS) Rename(oldpath string, newpath string) int {
	ctx, cancel := fs.ctx()
	defer cancel()

	_, err := fs.client.Rename(ctx, &pb.RenameRequest{
		OldPath: oldpath,
		NewPath: newpath,
	})
	if err != nil {
		return fs.errToFuse(err, "Rename", oldpath)
	}
	return 0
}

func (fs *RemoteFS) Truncate(path string, size int64, fh uint64) int {
	ctx, cancel := fs.ctx()
	defer cancel()

	_, err := fs.client.Truncate(ctx, &pb.TruncateRequest{
		Path: path,
		Size: size,
	})
	if err != nil {
		return fs.errToFuse(err, "Truncate", path)
	}
	return 0
}

func (fs *RemoteFS) Chmod(path string, mode uint32) int {
	ctx, cancel := fs.ctx()
	defer cancel()

	_, err := fs.client.Chmod(ctx, &pb.ChmodRequest{
		Path: path,
		Mode: mode,
	})
	if err != nil {
		return fs.errToFuse(err, "Chmod", path)
	}
	return 0
}

func (fs *RemoteFS) Statfs(path string, stat *fuse.Statfs_t) int {
	// Return reasonable defaults for a remote filesystem
	stat.Bsize = 4096
	stat.Frsize = 4096
	stat.Blocks = 1 << 30 // ~4TB
	stat.Bfree = 1 << 29
	stat.Bavail = 1 << 29
	stat.Files = 1 << 20
	stat.Ffree = 1 << 19
	stat.Favail = 1 << 19
	stat.Namemax = 255
	return 0
}

// fillStat populates a fuse.Stat_t from a protobuf FileInfo.
func fillStat(stat *fuse.Stat_t, info *pb.FileInfo) {
	stat.Mode = info.Mode
	stat.Size = info.Size
	stat.Mtim = fuse.Timespec{Sec: info.ModTimeUnix}
	stat.Atim = stat.Mtim
	stat.Ctim = stat.Mtim
	stat.Birthtim = stat.Mtim
	stat.Nlink = 1
	if info.IsDir {
		stat.Nlink = 2
	}
	stat.Uid = uint32(os.Getuid())
	stat.Gid = uint32(os.Getgid())
	stat.Blksize = 4096
	if info.Size > 0 {
		stat.Blocks = (info.Size + 511) / 512
	}
}

func (fs *RemoteFS) errToFuse(err error, op, path string) int {
	if err == nil {
		return 0
	}

	st, ok := status.FromError(err)
	if !ok {
		fs.log.Warn().Err(err).Str("op", op).Str("path", path).Msg("non-gRPC error")
		return -fuse.EIO
	}

	switch st.Code() {
	case codes.NotFound:
		return -fuse.ENOENT
	case codes.PermissionDenied:
		return -fuse.EACCES
	case codes.AlreadyExists:
		return -fuse.EEXIST
	case codes.InvalidArgument:
		return -fuse.EINVAL
	case codes.DeadlineExceeded, codes.Unavailable:
		fs.log.Warn().Err(err).Str("op", op).Str("path", path).Msg("connection issue")
		return -fuse.EIO
	default:
		fs.log.Warn().Err(err).Str("op", op).Str("path", path).Msg("gRPC error")
		return -fuse.EIO
	}
}
