# blue-guy

Mob programming tool that shares a live workspace between machines using a FUSE virtual filesystem.

The host serves real files over gRPC. Clients mount a virtual filesystem that proxies all reads and writes to the host. Changes auto-commit to a `mob/session-*` git branch.

```
  Host Machine                         Client Machine(s)
  ┌─────────────────────┐              ┌──────────────────────┐
  │  Real filesystem     │              │  FUSE mount          │
  │  /path/to/project    │              │  ~/mob/project-name  │
  │       │              │              │       │              │
  │  fsnotify watcher    │◄── gRPC ────►│  remotefs (proxy)    │
  │       │              │  (bidi       │                      │
  │  git auto-commit     │  streaming)  │                      │
  │  & push to mob/*     │              │                      │
  └─────────────────────┘              └──────────────────────┘
```

AI agents interact via standard file read/write. The sync layer is invisible to them.

## Quick start

```bash
# On the host machine — start sharing the current directory
blue-guy
# > Session: abc123 | Branch: mob/session-abc123
# > Listening on 0.0.0.0:7654
# > Join with: blue-guy --connect <YOUR_IP>

# On a client machine — join and get a FUSE mount
blue-guy --connect 192.168.1.42
# > Mounted workspace at ~/mob/192.168.1.42
# > Ready. All changes sync to host.
```

Files you edit on the client appear on the host. Files edited on the host appear on the client. Git auto-commits every 5 seconds of quiet.

## Build

```bash
# Host-only (no FUSE dependency)
make build

# Full build with FUSE client support
# macOS: brew install fuse-t
# Linux: apt install libfuse-dev fuse3
make build-fuse
```

## How it works

**Host mode** (default): starts a gRPC server that serves files from the current directory, watches for changes with fsnotify, and auto-commits to a `mob/session-<id>` branch.

**Client mode** (`--connect`): connects to a host via gRPC and mounts a FUSE filesystem. All filesystem operations (read, write, mkdir, rename, etc.) are proxied to the host.

**Git integration**: on startup, creates a `mob/session-<id>` branch. File changes trigger debounced auto-commits (5s quiet period). On shutdown (Ctrl+C), performs a final commit and restores the original branch.

**No locking**: last-writer-wins, same as NFS/SSHFS. Simple and works well for mob programming where participants communicate out-of-band.

## Project structure

```
cmd/blue-guy/          CLI entry point, host/client dispatch
internal/
  host/
    fileserver.go      gRPC FileService (Stat, ReadFile, WriteFile, ReadDir, ...)
    watcher.go         Recursive fsnotify + push-based change broadcasting
    host.go            Host orchestrator
  client/
    remotefs.go        FUSE filesystem proxying ops via gRPC
    client.go          Client orchestrator (connect + mount)
  gitops/
    gitops.go          Branch lifecycle, auto-commit, push
    debouncer.go       Debounced timer for commit batching
proto/blueguy.proto    gRPC service definition
```

## Requirements

- Go 1.24+
- FUSE (client mode only):
  - macOS: [FUSE-T](https://github.com/macos-fuse-t/fuse-t) (`brew install fuse-t`)
  - Linux: `libfuse-dev` + `fuse3`
- git (for auto-commit)
- protoc + protoc-gen-go + protoc-gen-go-grpc (only if regenerating proto)

## License

MIT
