package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ppalucha/bigfoot/scanner"
)

// createTestTree builds a directory tree with known relative sizes:
//
//	root/
//	  a/           (leaf: file1=1KB, file2=2KB → total ~3KB)
//	  b/           (non-leaf)
//	    c/         (leaf: file3=512B → total ~512B)
//	    file4      (4KB)
//	  .git/        (ignored: ignored=10KB)
func createTestTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	mkdir := func(parts ...string) {
		t.Helper()
		p := filepath.Join(append([]string{dir}, parts...)...)
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(size int, parts ...string) {
		t.Helper()
		p := filepath.Join(append([]string{dir}, parts...)...)
		if err := os.WriteFile(p, make([]byte, size), 0644); err != nil {
			t.Fatal(err)
		}
	}

	mkdir("a")
	write(1024, "a", "file1")
	write(2048, "a", "file2")

	mkdir("b", "c")
	write(512, "b", "c", "file3")
	write(65536, "b", "file4") // 64KB — clearly larger than a/ regardless of block size

	mkdir(".git")
	write(10240, ".git", "ignored")

	return dir
}

func TestWalkStructure(t *testing.T) {
	dir := createTestTree(t)

	entry, err := scanner.NewWalker(scanner.NewFreshCache()).Walk(dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if entry.Path != dir {
		t.Errorf("root path = %q, want %q", entry.Path, dir)
	}
	if !entry.IsDir {
		t.Error("root should be a directory")
	}
	// .git ignored → only a/ and b/
	if len(entry.Children) != 2 {
		t.Errorf("root has %d children %v, want 2 (a, b)", len(entry.Children), childNames(entry))
	}
}

func TestWalkSizesSumToParent(t *testing.T) {
	dir := createTestTree(t)

	var check func(e *scanner.Entry)
	check = func(e *scanner.Entry) {
		if !e.IsDir {
			return
		}
		var sum int64
		for _, c := range e.Children {
			sum += c.Size
		}
		if e.Size != sum {
			t.Errorf("%s: size %d != sum of children %d", e.Path, e.Size, sum)
		}
		for _, c := range e.Children {
			check(c)
		}
	}

	entry, err := scanner.NewWalker(scanner.NewFreshCache()).Walk(dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	check(entry)
}

func TestWalkSortedBySize(t *testing.T) {
	dir := createTestTree(t)

	var check func(e *scanner.Entry)
	check = func(e *scanner.Entry) {
		for i := 1; i < len(e.Children); i++ {
			if e.Children[i].Size > e.Children[i-1].Size {
				t.Errorf("%s: children not sorted by size desc at index %d", e.Path, i)
			}
		}
		for _, c := range e.Children {
			check(c)
		}
	}

	entry, err := scanner.NewWalker(scanner.NewFreshCache()).Walk(dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	check(entry)
}

func TestWalkIgnoreDirs(t *testing.T) {
	dir := createTestTree(t)

	entry, err := scanner.NewWalker(scanner.NewFreshCache()).Walk(dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if findChild(entry, ".git") != nil {
		t.Error(".git should be ignored")
	}
}

func TestWalkRelativeSizes(t *testing.T) {
	dir := createTestTree(t)

	entry, err := scanner.NewWalker(scanner.NewFreshCache()).Walk(dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	// b/ has file4=4KB + c/file3=512B, a/ has file1=1KB + file2=2KB
	// b should be larger than a
	a := findChild(entry, "a")
	b := findChild(entry, "b")
	if a == nil || b == nil {
		t.Fatalf("missing child: a=%v b=%v", a, b)
	}
	if b.Size <= a.Size {
		t.Errorf("b (%d) should be larger than a (%d)", b.Size, a.Size)
	}
}

func TestCacheHitReturnsSameSize(t *testing.T) {
	dir := createTestTree(t)
	cache := scanner.NewFreshCache()

	entry1, err := scanner.NewWalker(cache).Walk(dir)
	if err != nil {
		t.Fatalf("first Walk: %v", err)
	}

	entry2, err := scanner.NewWalker(cache).Walk(dir)
	if err != nil {
		t.Fatalf("second Walk: %v", err)
	}

	if entry1.Size != entry2.Size {
		t.Errorf("size mismatch: first=%d second=%d", entry1.Size, entry2.Size)
	}
	if len(entry1.Children) != len(entry2.Children) {
		t.Errorf("child count mismatch: first=%d second=%d", len(entry1.Children), len(entry2.Children))
	}
}

func TestCacheInvalidatedOnChange(t *testing.T) {
	dir := createTestTree(t)
	cache := scanner.NewFreshCache()

	entry1, err := scanner.NewWalker(cache).Walk(dir)
	if err != nil {
		t.Fatalf("first Walk: %v", err)
	}

	// Add a new file to a/ — should invalidate its cache entry
	if err := os.WriteFile(filepath.Join(dir, "a", "new"), make([]byte, 8192), 0644); err != nil {
		t.Fatal(err)
	}

	entry2, err := scanner.NewWalker(cache).Walk(dir)
	if err != nil {
		t.Fatalf("second Walk: %v", err)
	}

	if entry2.Size <= entry1.Size {
		t.Errorf("size should increase after adding file: before=%d after=%d", entry1.Size, entry2.Size)
	}
}

func TestWalkDeepTree(t *testing.T) {
	dir := t.TempDir()

	// 20 levels deep with a file at the bottom
	path := dir
	for i := 0; i < 20; i++ {
		path = filepath.Join(path, "sub")
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(path, "file"), make([]byte, 1024), 0644); err != nil {
		t.Fatal(err)
	}

	entry, err := scanner.NewWalker(scanner.NewFreshCache()).Walk(dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if entry.Size == 0 {
		t.Error("expected non-zero size for deep tree")
	}
}

func TestWalkPermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read all dirs")
	}
	dir := t.TempDir()

	restricted := filepath.Join(dir, "restricted")
	if err := os.Mkdir(restricted, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(restricted, 0755) })

	w := scanner.NewWalker(scanner.NewFreshCache())
	_, err := w.Walk(dir)
	if err != nil {
		t.Fatalf("Walk should not fail on permission error: %v", err)
	}
	if w.SkippedCount.Load() == 0 {
		t.Error("expected SkippedCount > 0 for restricted dir")
	}
}

// helpers

func childNames(e *scanner.Entry) []string {
	names := make([]string, len(e.Children))
	for i, c := range e.Children {
		names[i] = filepath.Base(c.Path)
	}
	return names
}

func findChild(e *scanner.Entry, name string) *scanner.Entry {
	for _, c := range e.Children {
		if filepath.Base(c.Path) == name {
			return c
		}
	}
	return nil
}
