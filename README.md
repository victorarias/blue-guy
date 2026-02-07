<p align="center">
  <img src="banner.png" alt="blue-guy banner" width="700" />
</p>

<h1 align="center">blue-guy</h1>

<p align="center">
  <em>The dumber, simpler sibling of doctor-manhattan.</em><br/>
  Mob programming without the infrastructure headache.
</p>

---

You know that glowing blue guy from Watchmen? He could teleport, manipulate matter, exist in multiple places at once. **This is not that.** This is his little brother who just learned to share files over a network and got really excited about it.

`blue-guy` is a peer-to-peer mob programming tool. One machine hosts, others mount. No central server. No database. No Kubernetes. Just a FUSE virtual filesystem, some gRPC, and git doing its thing in the background.

Where doctor-manhattan (a friend's tool tackling the same problem) is building toward a proper central server with advanced features and evolved architecture, `blue-guy` takes the opposite bet: **what's the simplest thing that could possibly work?**

## How it looks

```
  Host Machine                         Client Machine(s)
  +---------------------+              +----------------------+
  |  Real filesystem     |              |  FUSE mount          |
  |  /path/to/project    |              |  ~/mob/project-name  |
  |       |              |              |       |              |
  |  fsnotify watcher    |<--- gRPC --->|  remotefs (proxy)    |
  |       |              |  (bidi       |                      |
  |  git auto-commit     |  streaming)  |                      |
  |  & push to mob/*     |              |                      |
  +---------------------+              +----------------------+
```

Host serves real files over gRPC. Clients mount a FUSE filesystem that proxies reads and writes. Changes auto-commit to a `mob/session-*` branch. AI agents, editors, terminals -- they all just see normal files. The sync layer is invisible.

## Quick start

```bash
# On the host -- share current directory
blue-guy
# > Session: abc123 | Branch: mob/session-abc123
# > Listening on 0.0.0.0:7654
# > Join with: blue-guy --connect <YOUR_IP>

# On a client -- join and get a live mount
blue-guy --connect 192.168.1.42
# > Mounted workspace at ~/mob/192.168.1.42
# > Ready. All changes sync to host.
```

Edit on the client, it shows up on the host. Edit on the host, it shows up on the client. Git quietly commits every 5 seconds of silence. That's it. That's the tool.

## Build

```bash
# Host-only (no FUSE dependency, just go build)
make build

# Full build with FUSE client support
# macOS: brew install fuse-t
# Linux: apt install libfuse-dev fuse3
make build-fuse
```

## The philosophy

| | doctor-manhattan | blue-guy |
|---|---|---|
| Architecture | Central server | Peer-to-peer |
| Locking | Smart conflict resolution | Last-writer-wins (lol) |
| Infrastructure | Proper | `go build` and vibes |
| Ambition | The future of agentic mobbing | Files go brrr |

Both are valid. Sometimes you need the sophisticated approach. Sometimes you just need files to show up on the other machine.

## How it works

**Host mode** (default) -- starts a gRPC server, watches files with fsnotify, auto-commits to `mob/session-<id>`. Hit Ctrl+C and it does a final commit, restores your branch. Clean.

**Client mode** (`--connect`) -- connects via gRPC, mounts FUSE at `~/mob/<host>`. Every open, read, write, mkdir, rename goes over the wire. Your editor doesn't know. Your terminal doesn't know. Nobody knows.

**Git** -- creates a mob branch on startup, debounced auto-commits (5s quiet), best-effort push. On shutdown, one last commit and back to your original branch.

**Concurrency model** -- there isn't one. Last write wins. Same as NFS, same as SSHFS. Talk to each other like humans (or agents, we don't judge).

## Project structure

```
cmd/blue-guy/          CLI entry point, host/client dispatch
internal/
  host/
    fileserver.go      gRPC FileService (Stat, ReadFile, WriteFile, ...)
    watcher.go         Recursive fsnotify + change broadcasting
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
- git
- protoc + protoc-gen-go + protoc-gen-go-grpc (only if regenerating proto)

## License

MIT
