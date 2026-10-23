// scan.go walks the cache directories a catalog names and measures what is
// inside them.
package core

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
		wg.Add(1)
		go func() {
			defer wg.Done()
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
		}()
	}
	for i := range items {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	cache.Elapsed = time.Since(cache.At)
	return cache
}

func scanItem(groupID string, it Item, o Options, stop <-chan struct{}) Result {
	dirs, source := it.Roots(o)
	r := Result{GroupID: groupID, ItemID: it.ID, Unit: it.Unit}
	for _, d := range dirs {
		select {
		case <-stop:
			r.Err = ErrStopped
			return r
		default:
		}
		info, err := os.Stat(d)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				r.Err = errors.Join(r.Err, fmt.Errorf("%s: %w", d, err))
			}
			continue
		}
		if !info.IsDir() {
			continue
		}
		var l listing
		if it.Unit == UnitVersioned {
			l = readVersioned(d)
		} else {
			l = readEntries(d)
		}
		root := Root{
			Path: d, Source: source,
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
	// bad counts paths that could not be measured; err means the root
	// itself could not be listed.
	bad int
	err error
}

// readEntries measures each child of dir as one entry.
func readEntries(dir string) listing {
	children, err := os.ReadDir(dir)
	if err != nil {
		return listing{err: err}
	}
	var l listing
	for _, ch := range children {
		p := filepath.Join(dir, ch.Name())
		sz, mt, bad, ok := measure(p, ch)
		l.bad += bad
		if !ok {
			l.bad++
			continue
		}
		l.entries = append(l.entries, Entry{Name: ch.Name(), Path: p, Size: sz, ModTime: mt, IsDir: ch.IsDir()})
		l.size += sz
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

func sortEntries(in []Entry) {
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].Size != in[j].Size {
			return in[i].Size > in[j].Size
		}
		return strings.ToLower(in[i].Name) < strings.ToLower(in[j].Name)
	})
}
