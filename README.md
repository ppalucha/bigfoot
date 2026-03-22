# bigfoot

[![CI](https://github.com/ppalucha/bigfoot/actions/workflows/ci.yml/badge.svg)](https://github.com/ppalucha/bigfoot/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/ppalucha/bigfoot)](https://github.com/ppalucha/bigfoot/releases/latest)
[![Go version](https://img.shields.io/github/go-mod/go-version/ppalucha/bigfoot)](go.mod)
[![Go Report Card](https://goreportcard.com/badge/github.com/ppalucha/bigfoot)](https://goreportcard.com/report/github.com/ppalucha/bigfoot)
[![CodeQL](https://github.com/ppalucha/bigfoot/actions/workflows/github-code-scanning/codeql/badge.svg)](https://github.com/ppalucha/bigfoot/actions/workflows/github-code-scanning/codeql)
[![License](https://img.shields.io/github/license/ppalucha/bigfoot)](LICENSE)

Interactive disk usage analyzer for macOS and Linux. Finds what's eating your storage, fast.

> Vibecoded with [Claude Code](https://claude.com/claude-code).

```
  234.5 GB  /home/alice
──────────────────────────────────────────────────────────────────────────────
▶   187.2 GB ████████████████░░░░  79.8%  projects/
    ▼   142.0 GB ████████████░░░░░░░░  60.5%  work/
        ▶    98.3 GB ████████░░░░░░░░░░░░  41.9%  datasets/
        ▶    31.4 GB ███░░░░░░░░░░░░░░░░░░  13.4%  builds/
        ▶    12.3 GB █░░░░░░░░░░░░░░░░░░░░   5.2%  src/
    ▶    45.2 GB ████░░░░░░░░░░░░░░░░  19.3%  personal/
     26.8 GB ██░░░░░░░░░░░░░░░░░░  11.4%  Downloads/
     14.1 GB █░░░░░░░░░░░░░░░░░░░   6.0%  Virtual Machines/
      6.4 GB ░░░░░░░░░░░░░░░░░░░░   2.7%  Library/
──────────────────────────────────────────────────────────────────────────────
↑↓/jk move  →←/Enter expand/collapse  g/G top/end  q quit          4/9
```

## Features

- **Interactive TUI** — expand/collapse directory tree, navigate with keyboard
- **Smart caching** — warm scans finish in milliseconds by caching directory structure
- **Accurate sizes** — uses `stat.Blocks × 512` (actual allocated bytes, like `du`), not logical file size
- **macOS-aware** — stays on one filesystem by default, avoiding APFS firmlink double-counting
- **Parallel scanning** — bounded goroutine pool for fast cold scans
- **Safe with sudo** — read-only operations only; never writes to the scanned path

## Install

**Homebrew (macOS/Linux):**

```sh
brew tap ppalucha/tap
brew install bigfoot
```

**Download binary:**

Grab the latest release from the [releases page](https://github.com/ppalucha/bigfoot/releases), verify the checksum, and move to your `$PATH`:

```sh
tar xzf bigfoot_darwin_arm64.tar.gz
sha256sum -c checksums.txt
mv bigfoot /usr/local/bin/
```

**Verify build provenance** (requires [GitHub CLI](https://cli.github.com/)):
```sh
gh attestation verify bigfoot --owner ppalucha
```

**From source:**
```sh
go install github.com/ppalucha/bigfoot@latest
```

## Usage

```sh
bigfoot [flags] [path]
```

With no path, scans the current directory. Output is an interactive TUI when stdout is a terminal, or a plain tree otherwise (e.g. when piped).

### Flags

| Flag | Default | Description |
|---|---|---|
| `-depth N` | `3` | Tree depth for non-interactive output |
| `-top N` | `10` | Top N entries per level (non-interactive) |
| `-no-cache` | off | Ignore cache and rescan from scratch |
| `-cache-only` | off | Show last saved scan without rescanning |
| `-cross-device` | off | Follow mount points into other filesystems |
| `-verbose` | off | Print skipped paths and timing info |

### Examples

```sh
# Scan home directory
bigfoot ~

# Scan root (stays on one filesystem, safe on macOS)
sudo bigfoot /

# Quick look without rescanning
bigfoot --cache-only ~

# Pipe to less (plain tree output)
bigfoot --depth 5 ~ | less -R
```

## TUI key bindings

| Key | Action |
|---|---|
| `↑` / `k` | Move up |
| `↓` / `j` | Move down |
| `→` / `l` / `Enter` | Expand directory |
| `←` / `h` / `Enter` | Collapse directory |
| `g` | Jump to top |
| `G` | Jump to bottom |
| `Space` / `PgDn` | Page down |
| `PgUp` | Page up |
| `d` | Mark / unmark for deletion |
| `q` / `Ctrl+C` | Quit |

## Marking files for deletion

Press `d` to mark or unmark any file or directory. Marked items are highlighted in red with a `✗` prefix. The status bar shows the total count and size of marked items.

When you quit, if anything is marked, bigfoot writes a shell script to `~/.cache/bigfoot/remove.sh` and prints its path:

```sh
Marked paths saved to: /home/alice/.cache/bigfoot/remove.sh
Review and run:  sh /home/alice/.cache/bigfoot/remove.sh
```

The script uses `rm -rf` — **review it before running**. bigfoot itself never deletes anything.

## How caching works

bigfoot stores a structure cache at `~/.cache/bigfoot/cache.gob.gz`. On a warm scan:

1. For each directory, if the mtime matches the cached value, `ReadDir` is skipped entirely.
2. File sizes come from the cache; subdirectories are still recursed to catch changes deeper in the tree.
3. If you add or delete a file anywhere, its parent directory's mtime changes, invalidating exactly that subtree.

Cache is keyed per user. When running with `sudo`, the cache is stored in the original user's home directory (via `$SUDO_USER`), not root's.

## Running with sudo

bigfoot only calls read syscalls (`stat`, `readdir`) on the scanned path. It never writes to, deletes from, or executes anything in the scanned directory. The only write operation is updating its own cache in `~/.cache/bigfoot/`.

For Linux, you can grant just the capability to read protected directories instead of running as full root:

```sh
sudo setcap cap_dac_read_search+ep $(which bigfoot)
bigfoot /root  # works without sudo
```

## License

Apache 2.0 — see [LICENSE](LICENSE).
