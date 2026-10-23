// scan.go walks the cache directories a catalog names and measures what is
// inside them.
package core

import (
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
		if err != nil || !info.IsDir() {
			continue
		}
		root := Root{Path: d, Source: source}
		if it.Unit == UnitVersioned {
			root.Entries, root.Size, root.Count = readVersioned(d)
		} else {
			root.Entries, root.Size, root.Count = readEntries(d)
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

// readEntries measures each child of dir as one entry.
func readEntries(dir string) ([]Entry, int64, int) {
	children, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, 0
	}
	var (
		out   []Entry
		total int64
	)
	for _, ch := range children {
		p := filepath.Join(dir, ch.Name())
		sz, mt := measure(p, ch)
		if sz < 0 {
			continue
		}
		out = append(out, Entry{Name: ch.Name(), Path: p, Size: sz, ModTime: mt, IsDir: ch.IsDir()})
		total += sz
	}
	sortEntries(out)
	return out, total, len(out)
}

// readVersioned measures root/<package>/<version> folders, plus any stray
// files at the root as entries of their own.
func readVersioned(dir string) ([]Entry, int64, int) {
	children, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, 0
	}
	var (
		out   []Entry
		total int64
		count int
	)
	for _, ch := range children {
		p := filepath.Join(dir, ch.Name())
		if !ch.IsDir() {
			sz, mt := measure(p, ch)
			if sz < 0 {
				continue
			}
			out = append(out, Entry{Name: ch.Name(), Path: p, Size: sz, ModTime: mt})
			total += sz
			count++
			continue
		}
		versions, err := os.ReadDir(p)
		if err != nil || len(versions) == 0 {
			sz, mt := measure(p, ch)
			if sz < 0 {
				continue
			}
			out = append(out, Entry{Name: ch.Name(), Path: p, Size: sz, ModTime: mt, IsDir: true})
			total += sz
			count++
			continue
		}
		e := Entry{Name: ch.Name(), Path: p, Package: ch.Name(), ModTime: time.Time{}, IsDir: true}
		for _, v := range versions {
			vp := filepath.Join(p, v.Name())
			sz, mt := measure(vp, v)
			if sz < 0 {
				continue
			}
			e.Versions = append(e.Versions, Version{Name: v.Name(), Path: vp, Size: sz, ModTime: mt})
			e.Size += sz
			if mt.After(e.ModTime) {
				e.ModTime = mt
			}
			count++
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
		out = append(out, e)
		total += e.Size
	}
	sortEntries(out)
	return out, total, count
}

// measure returns the bytes and newest modification time below p. A
// symlink counts as its link size and is never followed, so a junction
// that points back up cannot loop the walk.
func measure(p string, ch fs.DirEntry) (int64, time.Time) {
	if ch.Type()&fs.ModeSymlink != 0 {
		if info, err := os.Lstat(p); err == nil {
			return info.Size(), info.ModTime()
		}
		return -1, time.Time{}
	}
	info, err := ch.Info()
	if err != nil {
		return -1, time.Time{}
	}
	if !ch.IsDir() {
		return info.Size(), info.ModTime()
	}
	var (
		size   int64
		newest time.Time
	)
	err = filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable folder still counts as an entry
		}
		if d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
			if d.Type()&fs.ModeSymlink != 0 {
				if li, err := d.Info(); err == nil {
					size += li.Size()
				}
			}
			return nil
		}
		if i, err := d.Info(); err == nil {
			size += i.Size()
			if i.ModTime().After(newest) {
				newest = i.ModTime()
			}
		}
		return nil
	})
	if newest.IsZero() {
		newest = info.ModTime()
	}
	return size, newest
}

func sortEntries(in []Entry) {
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].Size != in[j].Size {
			return in[i].Size > in[j].Size
		}
		return strings.ToLower(in[i].Name) < strings.ToLower(in[j].Name)
	})
}
