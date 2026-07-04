package definitions

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// CachedSource wraps LoadRoot with atomic snapshot swaps and lazy reload.
// Every call to Load checks whether the definitions directory tree has changed
// (via a lightweight mtime fingerprint). When unchanged, it returns the
// in-memory snapshot instantly. When changed, LoadRoot is called and the cache
// is swapped only on a successful, valid load — keeping the previous snapshot
// on failure.
//
// It is safe for concurrent use and implements the
//
//	Load(context.Context) (Snapshot, error)
//
// signature expected by looprunner.DefinitionsSource and app.DefinitionsProvider.
type CachedSource struct {
	root      string
	mu        sync.Mutex
	current   Snapshot
	lastStamp string
}

// NewCachedSource loads the initial snapshot eagerly and fails if the
// definition root is invalid.
func NewCachedSource(root string) (*CachedSource, error) {
	initial, err := LoadRoot(root)
	if err != nil {
		return nil, err
	}
	cs := &CachedSource{root: root, current: initial}
	cs.lastStamp, _ = cs.stamp() // best-effort; empty on walk error forces reload next call
	return cs, nil
}

// Load returns the cached snapshot. It checks whether the definitions tree has
// changed on disk and reloads atomically only when needed. On a reload failure
// the previous valid snapshot is retained and the error is logged.
// Context cancellation is honored: if ctx is done before a reload, the current
// cached snapshot is returned.
func (cs *CachedSource) Load(ctx context.Context) (Snapshot, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if ctx.Err() != nil {
		return cs.current, nil
	}
	stamp, err := cs.stamp()
	if err != nil {
		cs.lastStamp = ""
		log.Printf("definitions: stamp failed, will reload on next call: %v", err)
		return cs.current, nil
	}
	if stamp == cs.lastStamp {
		return cs.current, nil
	}
	snap, err := LoadRoot(cs.root)
	if err != nil {
		log.Printf("definitions: invalid snapshot, keeping previous: %v", err)
		return cs.current, nil
	}
	cs.current = snap
	cs.lastStamp = stamp
	return cs.current, nil
}

// stamp builds a lightweight fingerprint of the definitions directory tree.
// We walk the tree collecting each file's relative path and mtime, then join
// them into a deterministic string.
func (cs *CachedSource) stamp() (string, error) {
	var parts []string
	err := filepath.Walk(cs.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(cs.root, path)
		if relErr != nil {
			rel = path
		}
		parts = append(parts, rel+":"+info.ModTime().UTC().Format(time.RFC3339Nano))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(parts)
	// ponytail: no hash library — join with sentinel for deterministic compare
	return strings.Join(parts, "\x00"), nil
}
