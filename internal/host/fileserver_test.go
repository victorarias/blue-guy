package host_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/victorarias/blue-guy/internal/host"
	pb "github.com/victorarias/blue-guy/internal/proto/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func setupServer(t *testing.T) (*host.FileServer, string) {
	t.Helper()
	dir := t.TempDir()
	s := host.NewFileServer(dir, nil)
	return s, dir
}

func assertGRPCCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %v error, got nil", want)
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}
	if st.Code() != want {
		t.Errorf("expected code %v, got %v: %v", want, st.Code(), err)
	}
}

func TestStat(t *testing.T) {
	s, dir := setupServer(t)
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("world"), 0644)

	resp, err := s.Stat(context.Background(), &pb.StatRequest{Path: "hello.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Info.Name != "hello.txt" {
		t.Errorf("got name %q, want hello.txt", resp.Info.Name)
	}
	if resp.Info.Size != 5 {
		t.Errorf("got size %d, want 5", resp.Info.Size)
	}
	if resp.Info.IsDir {
		t.Error("expected file, got directory")
	}
}

func TestStat_NotFound(t *testing.T) {
	s, _ := setupServer(t)

	_, err := s.Stat(context.Background(), &pb.StatRequest{Path: "nope.txt"})
	assertGRPCCode(t, err, codes.NotFound)
}

func TestReadDir(t *testing.T) {
	s, dir := setupServer(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)

	resp, err := s.ReadDir(context.Background(), &pb.ReadDirRequest{Path: "/"})
	if err != nil {
		t.Fatal(err)
	}

	names := make(map[string]bool)
	for _, e := range resp.Entries {
		names[e.Name] = true
	}
	if !names["a.txt"] {
		t.Error("missing a.txt in listing")
	}
	if !names["subdir"] {
		t.Error("missing subdir in listing")
	}
}

func TestReadWriteFile(t *testing.T) {
	s, dir := setupServer(t)
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("initial"), 0644)

	// Read
	resp, err := s.ReadFile(context.Background(), &pb.ReadFileRequest{Path: "test.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Data) != "initial" {
		t.Errorf("got %q, want %q", resp.Data, "initial")
	}

	// Write (overwrite)
	_, err = s.WriteFile(context.Background(), &pb.WriteFileRequest{
		Path:     "test.txt",
		Data:     []byte("updated"),
		Truncate: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify
	data, _ := os.ReadFile(filepath.Join(dir, "test.txt"))
	if string(data) != "updated" {
		t.Errorf("got %q, want %q", data, "updated")
	}
}

func TestReadFile_EmptyFile(t *testing.T) {
	s, dir := setupServer(t)
	os.WriteFile(filepath.Join(dir, "empty.txt"), nil, 0644)

	resp, err := s.ReadFile(context.Background(), &pb.ReadFileRequest{Path: "empty.txt"})
	if err != nil {
		t.Fatalf("reading empty file should succeed, got: %v", err)
	}
	if len(resp.Data) != 0 {
		t.Errorf("expected empty data, got %d bytes", len(resp.Data))
	}
}

func TestCreateAndRemove(t *testing.T) {
	s, dir := setupServer(t)

	_, err := s.Create(context.Background(), &pb.CreateRequest{Path: "new.txt", Mode: 0644})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err != nil {
		t.Errorf("file should exist: %v", err)
	}

	_, err = s.Remove(context.Background(), &pb.RemoveRequest{Path: "new.txt"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Error("file should be deleted")
	}
}

func TestRemove_NonEmptyDir(t *testing.T) {
	s, dir := setupServer(t)
	os.MkdirAll(filepath.Join(dir, "mydir", "sub"), 0755)
	os.WriteFile(filepath.Join(dir, "mydir", "sub", "file.txt"), []byte("x"), 0644)

	// Remove should fail on non-empty directory (no longer uses RemoveAll)
	_, err := s.Remove(context.Background(), &pb.RemoveRequest{Path: "mydir"})
	if err == nil {
		t.Fatal("expected error removing non-empty directory")
	}

	// The directory and its contents should still exist
	if _, err := os.Stat(filepath.Join(dir, "mydir", "sub", "file.txt")); err != nil {
		t.Error("directory contents should still exist")
	}
}

func TestMkdirAndRename(t *testing.T) {
	s, dir := setupServer(t)

	_, err := s.Mkdir(context.Background(), &pb.MkdirRequest{Path: "mydir", Mode: 0755})
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.Rename(context.Background(), &pb.RenameRequest{
		OldPath: "mydir",
		NewPath: "renamed",
	})
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(dir, "renamed"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestPathTraversal_DotDot(t *testing.T) {
	s, _ := setupServer(t)

	// filepath.Clean("/" + "../../etc/passwd") -> "/etc/passwd"
	// filepath.Join(root, "/etc/passwd") -> root + "/etc/passwd" (safely under root)
	// So we get NotFound, not InvalidArgument — the traversal was neutralized
	_, err := s.Stat(context.Background(), &pb.StatRequest{Path: "../../etc/passwd"})
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
	assertGRPCCode(t, err, codes.NotFound)
}

func TestPathTraversal_SiblingPrefix(t *testing.T) {
	// Root is /tmp/TestXXX/app
	// Sibling /tmp/TestXXX/app-secrets exists with a real file
	parent := t.TempDir()
	root := filepath.Join(parent, "app")
	sibling := filepath.Join(parent, "app-secrets")
	os.Mkdir(root, 0755)
	os.Mkdir(sibling, 0755)
	os.WriteFile(filepath.Join(sibling, "key.pem"), []byte("secret"), 0644)

	s := host.NewFileServer(root, nil)

	// "../app-secrets/key.pem" cleans to "/app-secrets/key.pem"
	// joins as root + "/app-secrets/key.pem" (safely under root, NOT the sibling)
	// So we get NotFound — the sibling's real file is not accessible
	_, err := s.Stat(context.Background(), &pb.StatRequest{Path: "../app-secrets/key.pem"})
	if err == nil {
		t.Fatal("expected error — sibling directory should not be accessible")
	}
	assertGRPCCode(t, err, codes.NotFound)
}
