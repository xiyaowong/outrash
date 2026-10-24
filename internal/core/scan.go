// scan.go walks the cache directories a catalog names and measures what is
// inside them.
package core

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry is one removable thing at the top of a root: a package folder, a
// build-cache shard, a downloaded archive.
type Entry struct {
	Name    string
	Path    string
	Size    int64
	ModTime time.Time
	// Package groups the versions of one dependency, so a "keep the newest"
	// selection can keep one of them.
	Package string
	// Versions is set for a versioned unit, oldest first.
	Versions []Version
	// Children lists the levels below an entry of a deep catalog, each
	// already measured with the bytes and mtime of its whole subtree. A
	// check on the entry acts on the leaves the children end in.
	Children []Entry
	// IsDir marks a folder, which an entry listing can show as such.
	IsDir bool
}

// Version is one version folder of a versioned package.
type Version struct {
	Name    string
	Path    string
	Size    int64
	ModTime time.Time
}

// Root is one directory an item may live in after scanning.
type Root struct {
	Path   string
	Source string
	// EnvSrc marks a root an environment variable pointed at. It is scanned
	// and listed, but the recommendation leaves it unchecked: a mis-set
	// variable would otherwise turn one click into a sweep of whatever it
	// names.
	EnvSrc bool
	Size   int64
	Count  int
	// Entries is what the root holds, largest first.
	Entries []Entry
	// Skipped counts paths below the root that could not be read.
	Skipped int
	Err     error
}

// Result is what scanning one item found.
type Result struct {
	GroupID string
	ItemID  string
	Unit    string
	Roots   []Root
	Size    int64
	Count   int
	Err     error
}

// Unreadable counts the roots that could not be listed and the paths whose
// bytes could not be read. Anything above zero means the totals on screen
// are a lower bound, and the window says so.
func (r Result) Unreadable() (roots, paths int) {
	if r.Err != nil && !errors.Is(r.Err, ErrStopped) {
		roots++
	}
	for _, root := range r.Roots {
		if root.Err != nil {
			roots++
		}
		paths += root.Skipped
	}
	return roots, paths
}

// Failed reports whether this item was scanned incompletely.
func (r Result) Failed() bool {
	roots, paths := r.Unreadable()
	return roots > 0 || paths > 0
}

// EnvSourced reports whether any root of this result was named by an
// environment variable. Such a root is a claim to read, not a cache
// anything vouches for, which the window says where the status column does.
func (r Result) EnvSourced() bool {
	for _, root := range r.Roots {
		if root.EnvSrc {
			return true
		}
	}
	return false
}

// Cache is everything one scan found, keyed by group and item.
type Cache struct {
	At      time.Time
	Elapsed time.Duration
	Results map[string]Result // groupID + "/" + itemID
}

// Get returns the result of an item.
func (c *Cache) Get(groupID, itemID string) (Result, bool) {
	r, ok := c.Results[groupID+"/"+itemID]
	return r, ok
}

// Progress reports how far a scan got, for a view that fills in as it
// goes.
type Progress func(r Result)

// Run scans every item of cfg, up to workers at a time, calling onResult
// as each finishes. It returns when every item is done or stop is closed.
func Run(cfg *Config, o Options, workers int, onResult Progress, stop <-chan struct{}) *Cache {
	if workers < 1 {
		workers = 4
	}
	items := cfg.Items()
	cache := &Cache{At: time.Now(), Results: make(map[string]Result, len(items))}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		jobs = make(chan int)
	)
	for range workers {
		wg.Go(func() {
			for i := range jobs {
				select {
				case <-stop:
					return
				default:
				}
				r := scanItem(items[i].Group.ID, items[i].Item, o, stop)
				mu.Lock()
				cache.Results[r.GroupID+"/"+r.ItemID] = r
				mu.Unlock()
				if onResult != nil {
					onResult(r)
				}
			}
		})
	}
dispatch:
	for i := range items {
		select {
		case jobs <- i:
		case <-stop:
			// Every worker has left; sending now would block forever.
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()
	cache.Elapsed = time.Since(cache.At)
	return cache
}

// isLink reports a reparse point: a symlink, or the junction the guard has
// to treat the same way. Windows reports a junction as irregular rather than
// as ModeSymlink, and EvalSymlinks does not resolve it either.
func isLink(info fs.FileInfo) bool {
	m := info.Mode()
	return m&fs.ModeSymlink != 0 || m&fs.ModeIrregular != 0
}

// covers reports whether dir is p itself or an ancestor of it. Both sides go
// through longPath first: Windows keeps 8.3 aliases of real paths, and a
// short-spelled root would lexically miss the home it really holds.
func covers(dir, p string) bool {
	dir, p = longPath(dir), longPath(p)
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

func scanItem(groupID string, it Item, o Options, stop <-chan struct{}) Result {
	dirs, source, fromEnv := it.Roots(o)
	r := Result{GroupID: groupID, ItemID: it.ID, Unit: it.Unit}
	for _, d := range dirs {
		select {
		case <-stop:
			r.Err = ErrStopped
			return r
		default:
		}
		// Lstat, not Stat: a root that is itself a link measures the target's
		// children while every entry claims to sit under this root, and no
		// guard at removal can tell. Such a root is reported, not offered.
		info, err := os.Lstat(d)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				r.Err = errors.Join(r.Err, fmt.Errorf("%s: %w", d, err))
			}
			continue
		}
		if isLink(info) {
			r.Err = errors.Join(r.Err, fmt.Errorf(L("%s: 缓存根是链接，不扫描", "%s: cache root is a link; skipped"), d))
			continue
		}
		// A root that covers the home directory itself is one mis-set
		// environment variable away from sweeping everything the user owns.
		// No catalog root may contain the home.
		if o.Home != "" && covers(d, o.Home) {
			r.Err = errors.Join(r.Err, fmt.Errorf(L("%s: 缓存根包含用户主目录，不扫描", "%s: cache root contains the home directory; skipped"), d))
			continue
		}
		if !info.IsDir() {
			continue
		}
		var l listing
		if it.Unit == UnitVersioned {
			l = readVersioned(d)
		} else {
			l = readEntries(d, it.Depth)
		}
		if len(it.Exclude) > 0 {
			l.entries, _ = excludeTree(l.entries, it.Exclude)
			l.size, l.count = 0, 0
			countEntries(l.entries, &l.size, &l.count)
		}
		root := Root{
			Path: d, Source: source, EnvSrc: fromEnv,
			Entries: l.entries, Size: l.size, Count: l.count,
			Skipped: l.bad, Err: l.err,
		}
		r.Roots = append(r.Roots, root)
		r.Size += root.Size
		r.Count += root.Count
	}
	sort.SliceStable(r.Roots, func(i, j int) bool { return r.Roots[i].Size > r.Roots[j].Size })
	return r
}

// ErrStopped marks a scan cut short by a closing window.
var ErrStopped = fs.ErrClosed

// listing is what one root holds, plus what could not be read of it.
type listing struct {
	entries []Entry
	size    int64
	count   int
	// newest is the latest write in the listing, folded up from the levels
	// below it.
	newest time.Time
	// bad counts paths that could not be measured; err means the root
	// itself could not be listed.
	bad int
	err error
}

// readEntries lists the removable entries of dir, depth levels of folder
// below it. A folder past the last level is a leaf: it is measured whole,
// and a check on it removes it whole.
func readEntries(dir string, depth int) listing {
	children, err := os.ReadDir(dir)
	if err != nil {
		return listing{err: err}
	}
	var l listing
	for _, ch := range children {
		p := filepath.Join(dir, ch.Name())
		if depth > 1 && ch.IsDir() {
			sub := readEntries(p, depth-1)
			l.bad += sub.bad
			if sub.err != nil || len(sub.entries) == 0 {
				// The folder could not be listed, or holds nothing at the
				// levels below: measure it whole, as a leaf.
				sz, mt, bad, ok := measure(p, ch)
				l.bad += bad
				if !ok {
					l.bad++
					continue
				}
				l.entries = append(l.entries, Entry{Name: ch.Name(), Path: p, Size: sz, ModTime: mt, IsDir: true})
				l.size += sz
				if mt.After(l.newest) {
					l.newest = mt
				}
				continue
			}
			l.entries = append(l.entries, Entry{
				Name: ch.Name(), Path: p, Size: sub.size, ModTime: sub.newest,
				IsDir: true, Children: sub.entries,
			})
			l.size += sub.size
			if sub.newest.After(l.newest) {
				l.newest = sub.newest
			}
			continue
		}
		sz, mt, bad, ok := measure(p, ch)
		l.bad += bad
		if !ok {
			l.bad++
			continue
		}
		l.entries = append(l.entries, Entry{Name: ch.Name(), Path: p, Size: sz, ModTime: mt, IsDir: ch.IsDir()})
		l.size += sz
		if mt.After(l.newest) {
			l.newest = mt
		}
	}
	sortEntries(l.entries)
	l.count = len(l.entries)
	return l
}

// readVersioned measures root/<package>/<version> folders, plus any stray
// files at the root as entries of their own.
func readVersioned(dir string) listing {
	children, err := os.ReadDir(dir)
	if err != nil {
		return listing{err: err}
	}
	var l listing
	for _, ch := range children {
		p := filepath.Join(dir, ch.Name())
		if !ch.IsDir() {
			sz, mt, bad, ok := measure(p, ch)
			l.bad += bad
			if !ok {
				l.bad++
				continue
			}
			l.entries = append(l.entries, Entry{Name: ch.Name(), Path: p, Size: sz, ModTime: mt})
			l.size += sz
			l.count++
			continue
		}
		versions, verr := os.ReadDir(p)
		if verr != nil || len(versions) == 0 {
			sz, mt, bad, ok := measure(p, ch)
			l.bad += bad
			if !ok {
				l.bad++
				continue
			}
			if verr != nil {
				l.bad++ // the package listed as one entry, its versions unseen
			}
			l.entries = append(l.entries, Entry{Name: ch.Name(), Path: p, Size: sz, ModTime: mt, IsDir: true})
			l.size += sz
			l.count++
			continue
		}
		e := Entry{Name: ch.Name(), Path: p, Package: ch.Name(), ModTime: time.Time{}, IsDir: true}
		for _, v := range versions {
			vp := filepath.Join(p, v.Name())
			sz, mt, bad, ok := measure(vp, v)
			l.bad += bad
			if !ok {
				l.bad++
				continue
			}
			e.Versions = append(e.Versions, Version{Name: v.Name(), Path: vp, Size: sz, ModTime: mt})
			e.Size += sz
			if mt.After(e.ModTime) {
				e.ModTime = mt
			}
			l.count++
		}
		sort.SliceStable(e.Versions, func(i, j int) bool {
			if !e.Versions[i].ModTime.Equal(e.Versions[j].ModTime) {
				return e.Versions[i].ModTime.Before(e.Versions[j].ModTime)
			}
			return e.Versions[i].Name < e.Versions[j].Name
		})
		if len(e.Versions) == 0 {
			continue
		}
		l.entries = append(l.entries, e)
		l.size += e.Size
	}
	sortEntries(l.entries)
	return l
}

// measure returns the bytes and newest modification time below p, the number
// of paths it could not read, and whether p itself could be measured. A
// symlink counts as its link size and is never followed, so a junction that
// points back up cannot loop the walk.
func measure(p string, ch fs.DirEntry) (size int64, mod time.Time, bad int, ok bool) {
	if ch.Type()&fs.ModeSymlink != 0 {
		info, err := os.Lstat(p)
		if err != nil {
			return 0, time.Time{}, 0, false
		}
		return info.Size(), info.ModTime(), 0, true
	}
	info, err := ch.Info()
	if err != nil {
		return 0, time.Time{}, 0, false
	}
	if !ch.IsDir() {
		return info.Size(), info.ModTime(), 0, true
	}
	var newest time.Time
	err = filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			bad++
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			if li, err := d.Info(); err == nil {
				size += li.Size()
			} else {
				bad++
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if i, err := d.Info(); err == nil {
			size += i.Size()
			if i.ModTime().After(newest) {
				newest = i.ModTime()
			}
		} else {
			bad++
		}
		return nil
	})
	if newest.IsZero() {
		newest = info.ModTime()
	}
	return size, newest, bad, true
}

// countEntries re-derives a listing's totals from its tree: the bytes of
// the top level, each of which already names its whole subtree, and the
// leaves — a version counts one, a childless entry counts one.
func countEntries(entries []Entry, size *int64, count *int) {
	for _, e := range entries {
		*size += e.Size
		if len(e.Versions) > 0 {
			*count += len(e.Versions)
		} else if len(e.Children) > 0 {
			countLeaves(e.Children, count)
		} else {
			*count++
		}
	}
}

func countLeaves(entries []Entry, count *int) {
	for _, e := range entries {
		if len(e.Versions) > 0 {
			*count += len(e.Versions)
		} else if len(e.Children) > 0 {
			countLeaves(e.Children, count)
		} else {
			*count++
		}
	}
}

// excludeTree strips named children from every level and folds their bytes
// out of the parents above, so an excluded folder is unreachable however
// deep the catalog lists and no total still names its bytes. A folder whose
// children all went is dropped with them: keeping it whole would delete the
// excluded content, which is exactly what the exclusion is there to prevent.
func excludeTree(entries []Entry, exclude []string) ([]Entry, int64) {
	out := entries[:0]
	var cut int64
	for _, e := range entries {
		if e.Children != nil {
			var gone int64
			e.Children, gone = excludeTree(e.Children, exclude)
			e.Size -= gone
			cut += gone
		}
		if slices.Contains(exclude, e.Name) {
			cut += e.Size
			continue
		}
		if e.Children != nil && len(e.Children) == 0 {
			// The folder stands empty above its exclusion; nothing rebuildable
			// is left inside it to offer.
			cut += e.Size
			continue
		}
		out = append(out, e)
	}
	return out, cut
}

func sortEntries(in []Entry) {
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].Size != in[j].Size {
			return in[i].Size > in[j].Size
		}
		return strings.ToLower(in[i].Name) < strings.ToLower(in[j].Name)
	})
}
