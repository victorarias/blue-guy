package host

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	pb "github.com/victorarias/blue-guy/internal/proto/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maxReadSize = 1 << 20 // 1MB

// FileServer implements the gRPC FileService by serving files from a real directory.
type FileServer struct {
	pb.UnimplementedFileServiceServer
	root    string
	watcher *Watcher
}

func NewFileServer(root string, watcher *Watcher) *FileServer {
	return &FileServer{root: root, watcher: watcher}
}

// resolve turns a workspace-relative path into an absolute path,
// rejecting any traversal outside the root.
func (s *FileServer) resolve(rel string) (string, error) {
	cleaned := filepath.Clean("/" + rel)
	abs := filepath.Join(s.root, cleaned)
	// Allow exact root match (for "/") or require separator after root
	// to prevent prefix attacks (e.g. root="/tmp/app" matching "/tmp/app-secrets")
	if abs != s.root && !strings.HasPrefix(abs, s.root+string(filepath.Separator)) {
		return "", status.Error(codes.InvalidArgument, "path escapes workspace root")
	}
	return abs, nil
}

func fileInfoToProto(info fs.FileInfo) *pb.FileInfo {
	return &pb.FileInfo{
		Name:        info.Name(),
		Size:        info.Size(),
		Mode:        goModeToUnix(info.Mode()),
		ModTimeUnix: info.ModTime().Unix(),
		IsDir:       info.IsDir(),
	}
}

// goModeToUnix converts Go's os.FileMode to Unix mode bits (for FUSE compatibility).
// Go uses its own bit layout for type bits; Unix puts them at bits 12-15.
func goModeToUnix(m os.FileMode) uint32 {
	mode := uint32(m.Perm()) // lower 9 bits (rwxrwxrwx)

	if m&os.ModeSetuid != 0 {
		mode |= 0o4000
	}
	if m&os.ModeSetgid != 0 {
		mode |= 0o2000
	}
	if m&os.ModeSticky != 0 {
		mode |= 0o1000
	}

	switch {
	case m.IsDir():
		mode |= 0o040000 // S_IFDIR
	case m&os.ModeSymlink != 0:
		mode |= 0o120000 // S_IFLNK
	case m&os.ModeNamedPipe != 0:
		mode |= 0o010000 // S_IFIFO
	case m&os.ModeSocket != 0:
		mode |= 0o140000 // S_IFSOCK
	case m&os.ModeDevice != 0 && m&os.ModeCharDevice != 0:
		mode |= 0o020000 // S_IFCHR
	case m&os.ModeDevice != 0:
		mode |= 0o060000 // S_IFBLK
	default:
		mode |= 0o100000 // S_IFREG
	}

	return mode
}

func (s *FileServer) Stat(_ context.Context, req *pb.StatRequest) (*pb.StatResponse, error) {
	abs, err := s.resolve(req.Path)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return nil, osErrToStatus(err)
	}
	return &pb.StatResponse{Info: fileInfoToProto(info)}, nil
}

func (s *FileServer) ReadFile(_ context.Context, req *pb.ReadFileRequest) (*pb.ReadFileResponse, error) {
	abs, err := s.resolve(req.Path)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(abs)
	if err != nil {
		return nil, osErrToStatus(err)
	}
	defer f.Close()

	length := req.Length
	if length <= 0 || length > maxReadSize {
		length = maxReadSize
	}

	buf := make([]byte, length)
	if req.Offset > 0 {
		if _, err := f.Seek(req.Offset, 0); err != nil {
			return nil, status.Errorf(codes.Internal, "seek: %v", err)
		}
	}

	n, err := f.Read(buf)
	if err != nil && err != io.EOF && n == 0 {
		return nil, osErrToStatus(err)
	}
	return &pb.ReadFileResponse{Data: buf[:n]}, nil
}

func (s *FileServer) WriteFile(_ context.Context, req *pb.WriteFileRequest) (*pb.WriteFileResponse, error) {
	abs, err := s.resolve(req.Path)
	if err != nil {
		return nil, err
	}

	flags := os.O_WRONLY
	if req.Truncate {
		flags |= os.O_TRUNC
	}

	f, err := os.OpenFile(abs, flags, 0)
	if err != nil {
		return nil, osErrToStatus(err)
	}
	defer f.Close()

	if req.Offset > 0 {
		if _, err := f.Seek(req.Offset, 0); err != nil {
			return nil, status.Errorf(codes.Internal, "seek: %v", err)
		}
	}

	if _, err := f.Write(req.Data); err != nil {
		return nil, status.Errorf(codes.Internal, "write: %v", err)
	}

	return &pb.WriteFileResponse{}, nil
}

func (s *FileServer) ReadDir(_ context.Context, req *pb.ReadDirRequest) (*pb.ReadDirResponse, error) {
	abs, err := s.resolve(req.Path)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, osErrToStatus(err)
	}

	var infos []*pb.FileInfo
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		infos = append(infos, fileInfoToProto(info))
	}

	return &pb.ReadDirResponse{Entries: infos}, nil
}

func (s *FileServer) Create(_ context.Context, req *pb.CreateRequest) (*pb.CreateResponse, error) {
	abs, err := s.resolve(req.Path)
	if err != nil {
		return nil, err
	}

	mode := os.FileMode(req.Mode)
	if mode == 0 {
		mode = 0644
	}

	f, err := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return nil, osErrToStatus(err)
	}
	f.Close()
	return &pb.CreateResponse{}, nil
}

func (s *FileServer) Mkdir(_ context.Context, req *pb.MkdirRequest) (*pb.MkdirResponse, error) {
	abs, err := s.resolve(req.Path)
	if err != nil {
		return nil, err
	}

	mode := os.FileMode(req.Mode)
	if mode == 0 {
		mode = 0755
	}

	if err := os.Mkdir(abs, mode); err != nil {
		return nil, osErrToStatus(err)
	}
	return &pb.MkdirResponse{}, nil
}

func (s *FileServer) Remove(_ context.Context, req *pb.RemoveRequest) (*pb.RemoveResponse, error) {
	abs, err := s.resolve(req.Path)
	if err != nil {
		return nil, err
	}

	if err := os.Remove(abs); err != nil {
		return nil, osErrToStatus(err)
	}
	return &pb.RemoveResponse{}, nil
}

func (s *FileServer) Rename(_ context.Context, req *pb.RenameRequest) (*pb.RenameResponse, error) {
	oldAbs, err := s.resolve(req.OldPath)
	if err != nil {
		return nil, err
	}
	newAbs, err := s.resolve(req.NewPath)
	if err != nil {
		return nil, err
	}

	if err := os.Rename(oldAbs, newAbs); err != nil {
		return nil, osErrToStatus(err)
	}
	return &pb.RenameResponse{}, nil
}

func (s *FileServer) Chmod(_ context.Context, req *pb.ChmodRequest) (*pb.ChmodResponse, error) {
	abs, err := s.resolve(req.Path)
	if err != nil {
		return nil, err
	}

	if err := os.Chmod(abs, os.FileMode(req.Mode)); err != nil {
		return nil, osErrToStatus(err)
	}
	return &pb.ChmodResponse{}, nil
}

func (s *FileServer) Truncate(_ context.Context, req *pb.TruncateRequest) (*pb.TruncateResponse, error) {
	abs, err := s.resolve(req.Path)
	if err != nil {
		return nil, err
	}

	if err := os.Truncate(abs, req.Size); err != nil {
		return nil, osErrToStatus(err)
	}
	return &pb.TruncateResponse{}, nil
}

func (s *FileServer) WatchChanges(_ *pb.WatchChangesRequest, stream pb.FileService_WatchChangesServer) error {
	if s.watcher == nil {
		return status.Error(codes.Unavailable, "file watcher not running")
	}

	ch := s.watcher.Subscribe()
	defer s.watcher.Unsubscribe(ch)

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(event); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

func osErrToStatus(err error) error {
	if os.IsNotExist(err) {
		return status.Errorf(codes.NotFound, "%v", err)
	}
	if os.IsPermission(err) {
		return status.Errorf(codes.PermissionDenied, "%v", err)
	}
	if os.IsExist(err) {
		return status.Errorf(codes.AlreadyExists, "%v", err)
	}
	return status.Errorf(codes.Internal, "%v", err)
}
