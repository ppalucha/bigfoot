package scanner

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/gob"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"time"
)

// ChildInfo is the cached representation of a single directory entry.
type ChildInfo struct {
	Name  string
	IsDir bool
	Size  int64 // for files: last known size; for dirs: 0 (recomputed by recursion)
}

type structEntry struct {
	ModTime  time.Time
	Children []ChildInfo
}

// Cache stores per-directory structure (child list + file sizes) keyed by path.
// On a warm cache hit the walker skips ReadDir entirely, only recursing into
// subdir children to check their own cache entries.
type Cache struct {
	mu      sync.RWMutex
	structs map[string]structEntry
	file    string
	dirty   bool
}

func NewCache() *Cache {
	c := &Cache{
		structs: make(map[string]structEntry),
		file:    cacheFilePath(),
	}
	c.load()
	return c
}

func NewFreshCache() *Cache {
	return &Cache{
		structs: make(map[string]structEntry),
		file:    cacheFilePath(),
	}
}

func cacheFilePath() string {
	home, _ := os.UserHomeDir()
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		if u, err := user.Lookup(sudoUser); err == nil {
			home = u.HomeDir
		}
	}
	return filepath.Join(home, ".cache", "bigfoot", "cache.gob.gz")
}

// GetStruct returns the cached child list for path if the mtime still matches.
func (c *Cache) GetStruct(path string, modTime time.Time) ([]ChildInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.structs[path]
	if !ok || !e.ModTime.Equal(modTime) {
		return nil, false
	}
	return e.Children, true
}

// SetStruct stores the child list for path with the given mtime.
func (c *Cache) SetStruct(path string, modTime time.Time, children []ChildInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.structs[path] = structEntry{ModTime: modTime, Children: children}
	c.dirty = true
}

func (c *Cache) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.dirty {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.file), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(c.file), "bigfoot-*.tmp")
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	if err := gob.NewEncoder(gz).Encode(c.structs); err != nil {
		gz.Close()
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := gz.Close(); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	f.Close()
	return os.Rename(f.Name(), c.file)
}

func (c *Cache) load() {
	f, err := os.Open(c.file)
	if err != nil {
		return
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return
	}
	defer gz.Close()
	gob.NewDecoder(gz).Decode(&c.structs)
}

// pruneEntry returns a shallow copy of entry with children pruned to maxDepth
// levels and maxChildren entries per level, so the snapshot stays compact.
func pruneEntry(e *Entry, depth, maxDepth, maxChildren int) *Entry {
	out := &Entry{Path: e.Path, Size: e.Size, IsDir: e.IsDir}
	if !e.IsDir || depth >= maxDepth {
		return out
	}
	children := e.Children
	if len(children) > maxChildren {
		children = children[:maxChildren]
	}
	out.Children = make([]*Entry, len(children))
	for i, c := range children {
		out.Children[i] = pruneEntry(c, depth+1, maxDepth, maxChildren)
	}
	return out
}

// SaveFullScan writes the complete Entry tree for root to a gzipped snapshot file.
func (c *Cache) SaveFullScan(root string, entry *Entry) error {
	dir := filepath.Join(filepath.Dir(c.file), "scans")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	file := filepath.Join(dir, scanFileName(root))
	f, err := os.CreateTemp(dir, "bigfoot-scan-*.tmp")
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	if err := gob.NewEncoder(gz).Encode(pruneEntry(entry, 0, 8, 50)); err != nil {
		gz.Close()
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := gz.Close(); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	f.Close()
	return os.Rename(f.Name(), file)
}

// LoadFullScan loads a previously saved full scan for root, if it exists.
func (c *Cache) LoadFullScan(root string) (*Entry, bool) {
	file := filepath.Join(filepath.Dir(c.file), "scans", scanFileName(root))
	f, err := os.Open(file)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, false
	}
	defer gz.Close()
	var entry Entry
	if err := gob.NewDecoder(gz).Decode(&entry); err != nil {
		return nil, false
	}
	return &entry, true
}

func scanFileName(root string) string {
	h := sha256.Sum256([]byte(root))
	return fmt.Sprintf("%x.gob.gz", h[:8])
}
