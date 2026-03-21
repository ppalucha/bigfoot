package scanner

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type Entry struct {
	Path     string
	Size     int64
	IsDir    bool
	Children []*Entry
}

const maxRecursionDepth = 100
const maxGoroutines = 2048

type inodeKey struct {
	dev uint64
	ino uint64
}

type Walker struct {
	cache          *Cache
	IgnoreDirs     map[string]bool
	sem            chan struct{}
	goroutineCount atomic.Int64
	OnSkipped      func(path string, err error)
	SkippedCount   atomic.Int64
	DirsScanned    atomic.Int64
	CrossDevice    bool      // if true, follow mount points into other filesystems
	rootDev        uint64    // device ID of the root path being scanned
	visited        sync.Map  // tracks visited (dev, ino) pairs to avoid double-counting firmlinks
}

func NewWalker(cache *Cache) *Walker {
	return &Walker{
		cache: cache,
		IgnoreDirs: map[string]bool{
			".git":         true,
			".Trash":       true,
			"node_modules": true,
		},
		sem: make(chan struct{}, 64),
	}
}

func (w *Walker) Walk(root string) (*Entry, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		w.rootDev = uint64(stat.Dev)
	}
	return w.walkDir(root, 0)
}

func (w *Walker) walkDir(path string, depth int) (*Entry, error) {
	if depth > maxRecursionDepth {
		err := fmt.Errorf("max recursion depth exceeded (possible directory loop)")
		w.SkippedCount.Add(1)
		if w.OnSkipped != nil {
			w.OnSkipped(path, err)
		}
		return &Entry{Path: path, Size: 0, IsDir: true}, nil
	}

	info, err := os.Lstat(path)
	if err != nil {
		w.SkippedCount.Add(1)
		if w.OnSkipped != nil {
			w.OnSkipped(path, err)
		}
		return nil, err
	}

	if !info.IsDir() {
		return &Entry{Path: path, Size: diskUsage(info), IsDir: false}, nil
	}

	// Skip directories on a different filesystem unless --cross-device is set.
	if !w.CrossDevice && w.rootDev != 0 {
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			if uint64(stat.Dev) != w.rootDev {
				return &Entry{Path: path, Size: 0, IsDir: true}, nil
			}
		}
	}

	// Deduplicate by inode. On macOS APFS, all volumes in a container share the
	// same Dev, so the device check above cannot detect firmlinks. /Users and
	// /System/Volumes/Data/Users expose the same inode via two paths and would
	// both be counted. Track visited (dev, ino) pairs and skip any directory
	// we've already entered.
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		key := inodeKey{dev: uint64(stat.Dev), ino: uint64(stat.Ino)}
		if _, seen := w.visited.LoadOrStore(key, struct{}{}); seen {
			return &Entry{Path: path, Size: 0, IsDir: true}, nil
		}
	}

	// Structure cache hit: skip ReadDir entirely.
	// File sizes come from cache; subdirs are recursed to catch deep changes.
	if sc, ok := w.cache.GetStruct(path, info.ModTime()); ok {
		w.DirsScanned.Add(1)
		return w.buildFromStruct(path, sc, depth)
	}

	// Cache miss: read directory and cache its structure for next run.
	w.sem <- struct{}{}
	entries, err := os.ReadDir(path)
	<-w.sem
	if err != nil {
		w.SkippedCount.Add(1)
		if w.OnSkipped != nil {
			w.OnSkipped(path, err)
		}
		return &Entry{Path: path, Size: 0, IsDir: true}, nil
	}

	w.DirsScanned.Add(1)
	return w.buildFromDirEntries(path, entries, info.ModTime(), depth)
}

// buildFromStruct builds an Entry using cached child info, skipping ReadDir.
func (w *Walker) buildFromStruct(path string, sc []ChildInfo, depth int) (*Entry, error) {
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		children []*Entry
	)

	for _, c := range sc {
		if !c.IsDir {
			mu.Lock()
			children = append(children, &Entry{
				Path:  filepath.Join(path, c.Name),
				Size:  c.Size,
				IsDir: false,
			})
			mu.Unlock()
			continue
		}
		w.spawnOrInline(&wg, &mu, &children, filepath.Join(path, c.Name), depth+1)
	}

	wg.Wait()
	return makeEntry(path, children), nil
}

// buildFromDirEntries builds an Entry from fresh ReadDir results and caches the structure.
func (w *Walker) buildFromDirEntries(path string, entries []fs.DirEntry, modTime time.Time, depth int) (*Entry, error) {
	var (
		mu          sync.Mutex
		wg          sync.WaitGroup
		children    []*Entry
		structCache []ChildInfo
	)

	for _, e := range entries {
		typ := e.Type()
		if typ&fs.ModeSymlink != 0 {
			continue
		}
		name := e.Name()
		if w.IgnoreDirs[name] {
			continue
		}

		if !typ.IsDir() {
			info, err := e.Info()
			if err != nil {
				continue
			}
			size := diskUsage(info)
			mu.Lock()
			children = append(children, &Entry{
				Path:  filepath.Join(path, name),
				Size:  size,
				IsDir: false,
			})
			mu.Unlock()
			structCache = append(structCache, ChildInfo{Name: name, IsDir: false, Size: size})
			continue
		}

		structCache = append(structCache, ChildInfo{Name: name, IsDir: true})
		w.spawnOrInline(&wg, &mu, &children, filepath.Join(path, name), depth+1)
	}

	wg.Wait()

	w.cache.SetStruct(path, modTime, structCache)

	return makeEntry(path, children), nil
}

// spawnOrInline recurses into a subdir, using a goroutine if under the limit.
func (w *Walker) spawnOrInline(wg *sync.WaitGroup, mu *sync.Mutex, children *[]*Entry, path string, depth int) {
	if w.goroutineCount.Add(1) <= maxGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer w.goroutineCount.Add(-1)
			child, err := w.walkDir(path, depth)
			if err == nil && child != nil {
				mu.Lock()
				*children = append(*children, child)
				mu.Unlock()
			}
		}()
	} else {
		w.goroutineCount.Add(-1)
		child, err := w.walkDir(path, depth)
		if err == nil && child != nil {
			mu.Lock()
			*children = append(*children, child)
			mu.Unlock()
		}
	}
}

// diskUsage returns actual allocated disk space for a file, like du.
// Uses stat.Blocks * 512 (POSIX: Blocks is always in 512-byte units).
// Falls back to stat.Size for filesystems that don't report blocks.
func diskUsage(info os.FileInfo) int64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Blocks > 0 {
		return stat.Blocks * 512
	}
	return info.Size()
}

func makeEntry(path string, children []*Entry) *Entry {
	var total int64
	for _, c := range children {
		total += c.Size
	}
	sort.Slice(children, func(i, j int) bool {
		return children[i].Size > children[j].Size
	})
	return &Entry{Path: path, Size: total, IsDir: true, Children: children}
}
