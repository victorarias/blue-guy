package gitops

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const commitDelay = 5 * time.Second

type GitOps struct {
	root       string
	sessionID  string
	branch     string
	origBranch string
	debouncer  *Debouncer
	commitMu   sync.Mutex // serializes commitAndPush calls
	log        zerolog.Logger
}

func New(root string, sessionID string, log zerolog.Logger) (*GitOps, error) {
	l := log.With().Str("component", "gitops").Logger()

	// Check if git is available and this is a repo
	if _, err := runGit(root, "rev-parse", "--git-dir"); err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}

	return &GitOps{
		root:      root,
		sessionID: sessionID,
		branch:    "mob/session-" + sessionID,
		log:       l,
	}, nil
}

// Start creates the mob branch and begins auto-commit lifecycle.
func (g *GitOps) Start(ctx context.Context) error {
	// Record current branch to restore later
	out, err := runGit(g.root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}
	g.origBranch = strings.TrimSpace(out)

	// Create and switch to mob branch
	if _, err := runGit(g.root, "checkout", "-b", g.branch); err != nil {
		return fmt.Errorf("create mob branch %s: %w", g.branch, err)
	}

	g.log.Info().
		Str("branch", g.branch).
		Str("from", g.origBranch).
		Msg("Created mob branch")

	// Set up debounced auto-commit
	g.debouncer = NewDebouncer(commitDelay, func() {
		if err := g.commitAndPush(); err != nil {
			g.log.Warn().Err(err).Msg("Auto-commit failed")
		}
	})

	return nil
}

// NotifyChange should be called when files change. It triggers a debounced commit.
func (g *GitOps) NotifyChange() {
	if g.debouncer != nil {
		g.debouncer.Trigger()
	}
}

// Stop performs final commit, push, and restores the original branch.
func (g *GitOps) Stop() {
	if g.debouncer != nil {
		g.debouncer.Stop()
	}

	// Final commit
	if err := g.commitAndPush(); err != nil {
		g.log.Warn().Err(err).Msg("Final commit failed")
	}

	// Restore original branch
	if g.origBranch != "" {
		if _, err := runGit(g.root, "checkout", g.origBranch); err != nil {
			g.log.Error().Err(err).Str("branch", g.origBranch).Msg("Failed to restore original branch")
		} else {
			g.log.Info().Str("branch", g.origBranch).Msg("Restored original branch")
		}
	}
}

func (g *GitOps) commitAndPush() error {
	g.commitMu.Lock()
	defer g.commitMu.Unlock()

	// Stage all changes
	if _, err := runGit(g.root, "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	// Check if there are staged changes
	if _, err := runGit(g.root, "diff", "--cached", "--quiet"); err == nil {
		// No changes to commit
		return nil
	}

	// Commit
	ts := time.Now().Format("15:04:05")
	msg := fmt.Sprintf("mob: auto-save at %s", ts)
	if _, err := runGit(g.root, "commit", "-m", msg); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	g.log.Info().Str("msg", msg).Msg("Auto-committed")

	// Push (best effort — don't fail if remote is unavailable)
	if _, err := runGit(g.root, "push", "-u", "origin", g.branch); err != nil {
		g.log.Warn().Err(err).Msg("Push failed (no remote?)")
	}

	return nil
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
