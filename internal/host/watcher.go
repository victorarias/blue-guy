package host

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
	pb "github.com/victorarias/blue-guy/internal/proto/gen"
)

// Watcher monitors filesystem changes and broadcasts them to subscribers.
type Watcher struct {
	root    string
	watcher *fsnotify.Watcher
	log     zerolog.Logger

	mu          sync.RWMutex
	subscribers map[chan *pb.FileChangeEvent]struct{}
}

func NewWatcher(root string, log zerolog.Logger) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		root:        root,
		watcher:     fw,
		log:         log.With().Str("component", "watcher").Logger(),
		subscribers: make(map[chan *pb.FileChangeEvent]struct{}),
	}

	// Add the root directory. fsnotify watches directories non-recursively,
	// so we walk and add each subdirectory.
	if err := w.addRecursive(root); err != nil {
		fw.Close()
		return nil, err
	}

	return w, nil
}

func (w *Watcher) addRecursive(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}
		if info.IsDir() {
			// Skip hidden directories and .git
			name := info.Name()
			if name != "." && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return w.watcher.Add(path)
		}
		return nil
	})
}

// Run starts the event loop. Blocks until the watcher is closed.
func (w *Watcher) Run() {
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.log.Warn().Err(err).Msg("fsnotify error")
		}
	}
}

func (w *Watcher) handleEvent(event fsnotify.Event) {
	rel, err := filepath.Rel(w.root, event.Name)
	if err != nil {
		return
	}
	rel = "/" + rel

	var changeType pb.ChangeType
	switch {
	case event.Op&fsnotify.Create != 0:
		changeType = pb.ChangeType_CHANGE_TYPE_CREATED
		// If a new non-hidden directory was created, watch it too
		name := filepath.Base(event.Name)
		if !strings.HasPrefix(name, ".") {
			w.watcher.Add(event.Name) // no-op if it's a file
		}
	case event.Op&fsnotify.Write != 0:
		changeType = pb.ChangeType_CHANGE_TYPE_MODIFIED
	case event.Op&fsnotify.Remove != 0:
		changeType = pb.ChangeType_CHANGE_TYPE_DELETED
	case event.Op&fsnotify.Rename != 0:
		changeType = pb.ChangeType_CHANGE_TYPE_RENAMED
	default:
		return
	}

	change := &pb.FileChangeEvent{
		Path: rel,
		Type: changeType,
	}

	w.broadcast(change)
}

func (w *Watcher) broadcast(event *pb.FileChangeEvent) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	for ch := range w.subscribers {
		select {
		case ch <- event:
		default:
			// Drop if subscriber is slow — they'll catch up via Stat
			w.log.Debug().Str("path", event.Path).Msg("dropped change event for slow subscriber")
		}
	}
}

// Subscribe returns a channel that receives change events.
// Call Unsubscribe to stop receiving and clean up.
func (w *Watcher) Subscribe() chan *pb.FileChangeEvent {
	ch := make(chan *pb.FileChangeEvent, 64)
	w.mu.Lock()
	w.subscribers[ch] = struct{}{}
	w.mu.Unlock()
	return ch
}

func (w *Watcher) Unsubscribe(ch chan *pb.FileChangeEvent) {
	w.mu.Lock()
	delete(w.subscribers, ch)
	w.mu.Unlock()
	close(ch)
}

func (w *Watcher) Close() error {
	err := w.watcher.Close()
	// Close all subscriber channels so blocked readers unblock
	w.mu.Lock()
	for ch := range w.subscribers {
		delete(w.subscribers, ch)
		close(ch)
	}
	w.mu.Unlock()
	return err
}
