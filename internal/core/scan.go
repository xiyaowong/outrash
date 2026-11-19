// scan.go walks the cache directories a catalog names and measures what is
// inside them.
package core

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	// EnvSrc marks a root an environment variable or a tool's own config
	// pointed at. It is scanned and listed, but the recommendation leaves it
	// unchecked: a mis-set value would otherwise turn one click into a sweep
	// of whatever it names.
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
// environment variable or a tool's own config. Such a root is a claim to
// read, not a cache anything vouches for, which the window says where the
// status column does.
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
	pool := newMeasurePool(stop)
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
				r := scanItem(items[i].Group.ID, items[i].Item, o, pool)
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
	// Every listing has drained its own tasks by now, so the shared queue has
	// no producer left: the workers can end.
	pool.close()
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

func scanItem(groupID string, it Item, o Options, pool *measurePool) Result {
	stop := pool.stopCh()
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
			l = readVersioned(d, pool)
		} else {
			l = readEntries(d, it.Depth, pool)
		}
		if l.stopped {
			// Half a listing must never pass for a complete one; ErrStopped
			// is already outside the Unreadable count, so an aborted root is
			// not reported as a read failure either.
			r.Err = ErrStopped
			return r
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
	// stopped marks a listing abandoned because the scan was stopped. The
	// caller must not present what came back as a complete listing.
	stopped bool
}

// readEntries lists the removable entries of dir, depth levels of folder
// below it. A folder past the last level is a leaf: it is measured whole,
// and a check on it removes it whole. Closing stop abandons the pool at the
// next path, so a rescan does not leave the old workers grinding through
// multi-gigabyte trees that nothing will read.
func readEntries(dir string, depth int, pool *measurePool) listing {
	if depth > 1 {
		return readTree(dir, depth, pool)
	}
	return readShallow(dir, pool)
}

// readShallow lists one level. Its leaves go through the scan's pool, so a
// root holding thousands of children overlaps their walks instead of
// measuring one at a time.
func readShallow(dir string, pool *measurePool) listing {
	stop := pool.stopCh()
	if stopped(stop) {
		return listing{stopped: true}
	}
	children, err := os.ReadDir(dir)
	if err != nil {
		return listing{err: err}
	}
	var l listing
	var set taskSet
	// A leaf's bytes arrive when the set drains; the entry takes its place
	// now so the listing keeps the order the directory gave.
	type pending struct {
		i int
		s *treeStats
	}
	var pend []pending
	for _, ch := range children {
		if stopped(stop) {
			return listing{stopped: true}
		}
		p := filepath.Join(dir, ch.Name())
		s, ok := pool.begin(&set, p, ch)
		if !ok {
			l.bad++
			continue
		}
		pend = append(pend, pending{len(l.entries), s})
		l.entries = append(l.entries, Entry{Name: ch.Name(), Path: p, IsDir: ch.IsDir()})
	}
	set.Wait()
	if pool.isAborted() || stopped(stop) {
		return listing{stopped: true}
	}
	for _, pd := range pend {
		e := &l.entries[pd.i]
		size, mt, bad := pd.s.result()
		e.Size, e.ModTime = size, mt
		l.bad += bad
		l.size += size
		if mt.After(l.newest) {
			l.newest = mt
		}
	}
	sortEntries(l.entries)
	l.count = len(l.entries)
	return l
}

// levelDir is one directory of a deep listing: its children in the order the
// directory gave them, and the row each child's bytes land in. Only the task
// that lists the directory touches it until the fold at the end.
type levelDir struct {
	path    string
	entries []Entry
	// stats is one row per entry, filled by the pool for a leaf and left nil
	// for a folder the next level lists.
	stats  []*treeStats
	bad    int
	err    error
	parent *levelDir
	// at is this directory's row in its parent's entries.
	at     int
	size   int64
	newest time.Time
}

// readTree lists a deep root one level at a time, breadth first. A level goes
// through the pool as one wave of directory reads, so the walkers stay busy
// across the whole level: a module cache has a few hundred directories at its
// second level and thousands at its third, and listing them a folder at a
// time would leave most walkers idle between folders. The levels fold back
// together once every byte is measured.
func readTree(root string, depth int, pool *measurePool) listing {
	stop := pool.stopCh()
	if stopped(stop) {
		return listing{stopped: true}
	}
	var leaves taskSet
	var levels [][]*levelDir
	cur := []*levelDir{{path: root}}
	for lvl := depth; lvl >= 1; lvl-- {
		levels = append(levels, cur)
		var wave taskSet
		for _, n := range cur {
			pool.submit(&wave, func() {
				if stopped(stop) {
					return
				}
				children, err := os.ReadDir(n.path)
				if err != nil {
					n.err = err
					return
				}
				for _, ch := range children {
					if stopped(stop) {
						return
					}
					p := filepath.Join(n.path, ch.Name())
					if lvl > 1 && ch.IsDir() {
						n.entries = append(n.entries, Entry{Name: ch.Name(), Path: p, IsDir: true})
						n.stats = append(n.stats, nil)
						continue
					}
					s, ok := pool.begin(&leaves, p, ch)
					if !ok {
						n.bad++
						continue
					}
					n.entries = append(n.entries, Entry{Name: ch.Name(), Path: p, IsDir: ch.IsDir()})
					n.stats = append(n.stats, s)
				}
			})
		}
		wave.Wait()
		if pool.isAborted() || stopped(stop) {
			return listing{stopped: true}
		}
		if lvl == 1 {
			break
		}
		var next []*levelDir
		for _, n := range cur {
			if n.err != nil || len(n.entries) == 0 {
				// The folder could not be listed, or holds nothing at the
				// levels below: measure it whole, as a leaf.
				if n.parent == nil {
					if n.err != nil {
						return listing{err: n.err}
					}
					return listing{}
				}
				pe := &n.parent.entries[n.at]
				n.parent.stats[n.at] = pool.beginTree(&leaves, pe.Path)
				continue
			}
			for i := range n.entries {
				if n.stats[i] != nil {
					continue
				}
				next = append(next, &levelDir{path: n.entries[i].Path, parent: n, at: i})
			}
		}
		cur = next
	}
	leaves.Wait()
	if pool.isAborted() || stopped(stop) {
		return listing{stopped: true}
	}
	// Fold the levels back up, deepest first: a folder's row is complete once
	// everything below it is.
	for i := len(levels) - 1; i >= 0; i-- {
		for _, n := range levels[i] {
			var size int64
			var newest time.Time
			for j := range n.entries {
				e := &n.entries[j]
				if s := n.stats[j]; s != nil {
					sz, mt, bad := s.result()
					e.Size, e.ModTime = sz, mt
					n.bad += bad
				}
				size += e.Size
				if e.ModTime.After(newest) {
					newest = e.ModTime
				}
			}
			sortEntries(n.entries)
			n.size, n.newest = size, newest
			if p := n.parent; p != nil {
				pe := &p.entries[n.at]
				pe.Size, pe.ModTime, pe.Children = size, newest, n.entries
				p.bad += n.bad
			}
		}
	}
	top := levels[0][0]
	return listing{
		entries: top.entries, size: top.size, count: len(top.entries),
		newest: top.newest, bad: top.bad,
	}
}

// readVersioned measures root/<package>/<version> folders, plus any stray
// files at the root as entries of their own. Each package is listed and its
// versions measured through the same pool the leaf walks use, so a registry
// of thousands of packages overlaps its directory reads too.
func readVersioned(dir string, pool *measurePool) listing {
	stop := pool.stopCh()
	if stopped(stop) {
		return listing{stopped: true}
	}
	children, err := os.ReadDir(dir)
	if err != nil {
		return listing{err: err}
	}
	var l listing
	var set taskSet
	// vrow is one version folder of a package; prow is one child of the root,
	// filled in by the pool and read back once it drains. A pointer, not a
	// slice element: the slice grows while the workers hold their rows.
	type vrow struct {
		name, path string
		s          *treeStats
	}
	type prow struct {
		name, path string
		dir        bool
		whole      *treeStats // the package as one entry, its versions absent or unseen
		unseen     bool       // its versions could not be listed
		bad        int        // children that could not be measured
		versions   []vrow
	}
	var rows []*prow
	for _, ch := range children {
		if stopped(stop) {
			return listing{stopped: true}
		}
		p := filepath.Join(dir, ch.Name())
		if !ch.IsDir() {
			s, ok := pool.begin(&set, p, ch)
			if !ok {
				l.bad++
				continue
			}
			rows = append(rows, &prow{name: ch.Name(), path: p, whole: s})
			continue
		}
		row := &prow{name: ch.Name(), path: p, dir: true}
		rows = append(rows, row)
		pool.submit(&set, func() {
			if pool.isAborted() || stopped(stop) {
				pool.abort()
				return
			}
			versions, verr := os.ReadDir(p)
			if verr != nil || len(versions) == 0 {
				// The package could not be listed, or holds no version
				// folders: measure it whole, as one entry. Measured here, not
				// resubmitted: a task must not feed the pool it runs on.
				if s, ok := measureOne(p, ch, stop); ok {
					row.whole = s
				} else {
					row.bad++
				}
				row.unseen = verr != nil
				return
			}
			for _, v := range versions {
				if stopped(stop) {
					pool.abort()
					return
				}
				vp := filepath.Join(p, v.Name())
				if s, ok := measureOne(vp, v, stop); ok {
					row.versions = append(row.versions, vrow{name: v.Name(), path: vp, s: s})
				} else {
					row.bad++
				}
			}
		})
	}
	set.Wait()
	if pool.isAborted() || stopped(stop) {
		return listing{stopped: true}
	}
	for _, row := range rows {
		l.bad += row.bad
		if row.whole != nil {
			sz, mt, bad := row.whole.result()
			l.bad += bad
			if row.unseen {
				l.bad++ // the package listed as one entry, its versions unseen
			}
			l.entries = append(l.entries, Entry{Name: row.name, Path: row.path, Size: sz, ModTime: mt, IsDir: row.dir})
			l.size += sz
			l.count++
			continue
		}
		e := Entry{Name: row.name, Path: row.path, Package: row.name, ModTime: time.Time{}, IsDir: true}
		for _, v := range row.versions {
			sz, mt, bad := v.s.result()
			l.bad += bad
			e.Versions = append(e.Versions, Version{Name: v.name, Path: v.path, Size: sz, ModTime: mt})
			e.Size += sz
			if mt.After(e.ModTime) {
				e.ModTime = mt
			}
			l.count++
		}
		if len(e.Versions) == 0 {
			continue
		}
		// The contract is oldest first, so a "keep the newest" pass knows
		// which row to leave standing.
		sort.SliceStable(e.Versions, func(i, j int) bool {
			if !e.Versions[i].ModTime.Equal(e.Versions[j].ModTime) {
				return e.Versions[i].ModTime.Before(e.Versions[j].ModTime)
			}
			return e.Versions[i].Name < e.Versions[j].Name
		})
		l.entries = append(l.entries, e)
		l.size += e.Size
	}
	sortEntries(l.entries)
	return l
}

// stopped reports a closed stop channel, the one check the walk callbacks
// also make. A nil channel never closes.
func stopped(stop <-chan struct{}) bool {
	select {
	case <-stop:
		return true
	default:
		return false
	}
}

// treeStats is one subtree's measurement, filled by the pool that walked it.
// Only that pool's goroutines touch a row, and never after wait returns.
type treeStats struct {
	mu      sync.Mutex
	size    int64
	newest  time.Time
	bad     int
	aborted bool
}

// fold merges a walked directory's outcome into its subtree row.
func (s *treeStats) fold(size int64, newest time.Time, bad int) {
	s.mu.Lock()
	s.size += size
	if newest.After(s.newest) {
		s.newest = newest
	}
	s.bad += bad
	s.mu.Unlock()
}

// abort marks the subtree's row as cut short by a closing stop channel.
func (s *treeStats) abort() {
	s.mu.Lock()
	s.aborted = true
	s.mu.Unlock()
}

func (s *treeStats) result() (size int64, newest time.Time, bad int) {
	return s.size, s.newest, s.bad
}

// measurePool is one scan's shared set of walkers. Every listing the scan
// reads hands its leaf walks to the same queue, so a heavy cache draws on the
// whole pool instead of the four walkers a listing of its own would start,
// and a light listing never sits idle next to it. A listing waits only for
// its own tasks, which is why the pool outlives it: the scan opens the pool
// before the first item and closes it after the last.
//
// A nil pool is a listing measured on its own, tasks run where they are
// submitted: the shape a test reads without a scan around it.
type measurePool struct {
	stop <-chan struct{}
	// queue is deep enough that a listing hands over a swathe of leaves
	// without blocking, and shallow enough that memory stays bounded by the
	// walkers' own progress.
	queue chan func()
	// done ends the walkers once the scan has handed over every task. The
	// queue is not closed: a task can hand over another while the pool winds
	// down, and a send on a closed channel would panic where this cannot.
	done    chan struct{}
	once    sync.Once
	workers sync.WaitGroup
	started atomic.Bool
	aborted atomic.Bool
	// size is how many walkers this pool starts.
	size int
}

// taskSet is one listing's share of the pool: the tasks it queued and has not
// seen finish. Waiting on it, rather than on the pool, is what keeps two
// listings from waiting for each other's walks.
type taskSet struct{ sync.WaitGroup }

// measurePoolMax caps the walkers one scan runs. The work is metadata-bound,
// so the walkers overlap the drive's queue more than they use cores; past a
// machine's core count the gain flattens, and a deeper queue only adds seeks.
const measurePoolMax = 16

// measureWorkers sizes one scan's pool: one walker per core, floored at four
// (a handful measured several times faster than one on a wide cache) and
// capped so a many-core machine does not turn a directory walk into a seek
// storm.
func measureWorkers() int { return max(4, min(runtime.NumCPU(), measurePoolMax)) }

func newMeasurePool(stop <-chan struct{}) *measurePool {
	size := measureWorkers()
	return &measurePool{
		stop: stop, size: size,
		queue: make(chan func(), size*8),
		done:  make(chan struct{}),
	}
}

// stopCh is the scan's stop channel; a nil pool has none.
func (p *measurePool) stopCh() <-chan struct{} {
	if p == nil {
		return nil
	}
	return p.stop
}

func (p *measurePool) isAborted() bool { return p != nil && p.aborted.Load() }

func (p *measurePool) abort() {
	if p != nil {
		p.aborted.Store(true)
	}
}

func (p *measurePool) start() {
	p.once.Do(func() {
		p.started.Store(true)
		for range p.size {
			p.workers.Go(func() {
				for {
					select {
					case fn := <-p.queue:
						fn()
					case <-p.done:
						// Tasks still queued are ones a listing handed over
						// before it returned, so a walker leaves only when
						// there is none left to run.
						if len(p.queue) == 0 {
							return
						}
					}
				}
			})
		}
	})
}

// submit queues one task, running it here when the pool is saturated: a task
// run inline keeps the producer moving instead of blocking it on a queue the
// walkers may not reach for a while. The task answers for its own set, so the
// listing that queued it knows when its share is done.
func (p *measurePool) submit(set *taskSet, fn func()) {
	if p == nil {
		fn()
		return
	}
	p.start()
	set.Add(1)
	select {
	case p.queue <- func() {
		fn()
		set.Done()
	}:
	default:
		fn()
		set.Done()
	}
}

// close ends the walkers. It runs once the scan has handed over every task:
// each walker leaves when the queue has nothing left, and the ones already
// running finish what they hold.
func (p *measurePool) close() {
	if p == nil || !p.started.Load() {
		return
	}
	close(p.done)
	p.workers.Wait()
}

// begin schedules the walk of one leaf, returning the row its bytes land in.
// The caller reads the row only after its set has drained.
func (p *measurePool) begin(set *taskSet, path string, ch fs.DirEntry) (*treeStats, bool) {
	if ch.Type()&fs.ModeSymlink != 0 {
		// A symlink counts as its link size and is never followed, so a
		// junction that points back up cannot loop the walk.
		info, err := os.Lstat(path)
		if err != nil {
			return nil, false
		}
		s := &treeStats{size: info.Size(), newest: info.ModTime()}
		return s, true
	}
	info, err := ch.Info()
	if err != nil {
		return nil, false
	}
	if !ch.IsDir() {
		return &treeStats{size: info.Size(), newest: info.ModTime()}, true
	}
	return p.beginTree(set, path), true
}

// beginTree is begin for a directory a listing has already named: the deep
// listing learns at the next level that the folder could not be read, and
// measures it whole from the path it kept.
func (p *measurePool) beginTree(set *taskSet, path string) *treeStats {
	s := &treeStats{}
	stop := p.stopCh()
	p.submit(set, func() {
		if aborted := measureTreeInto(s, path, stop); aborted {
			p.abort()
		}
	})
	return s
}

// measureOne returns the stats of one entry measured at once: a file, a
// link, or a small tree. It runs on the caller's goroutine, which is why a
// pool task may use it and begin may not — a task that feeds the pool it
// runs on races the drain that closes the queue.
func measureOne(path string, ch fs.DirEntry, stop <-chan struct{}) (*treeStats, bool) {
	if ch.Type()&fs.ModeSymlink != 0 {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, false
		}
		return &treeStats{size: info.Size(), newest: info.ModTime()}, true
	}
	info, err := ch.Info()
	if err != nil {
		return nil, false
	}
	if !ch.IsDir() {
		return &treeStats{size: info.Size(), newest: info.ModTime()}, true
	}
	s := &treeStats{}
	if measureTreeInto(s, path, stop) {
		return nil, false
	}
	if s.newest.IsZero() {
		s.newest = info.ModTime()
	}
	return s, true
}

// measureTreeInto measures the tree at root into s and reports whether the
// walk was abandoned partway. Its own goroutine runs it once, so its local
// counters need no locking; s.fold is the only cross-goroutine touch.
func measureTreeInto(s *treeStats, root string, stop <-chan struct{}) bool {
	var (
		size   int64
		newest time.Time
		bad    int
	)
	dirs := []string{root}
	for len(dirs) > 0 {
		if stopped(stop) {
			s.fold(size, newest, bad)
			return true
		}
		dir := dirs[len(dirs)-1]
		dirs = dirs[:len(dirs)-1]
		f, err := os.Open(dir)
		if err != nil {
			bad++
			continue
		}
		for {
			children, err := f.ReadDir(128)
			for _, child := range children {
				if stopped(stop) {
					_ = f.Close()
					s.fold(size, newest, bad)
					return true
				}
				if child.Type()&fs.ModeSymlink != 0 {
					if li, err := child.Info(); err == nil {
						size += li.Size()
					} else {
						bad++
					}
					continue
				}
				if child.IsDir() {
					dirs = append(dirs, filepath.Join(dir, child.Name()))
					continue
				}
				if i, err := child.Info(); err == nil {
					size += i.Size()
					if i.ModTime().After(newest) {
						newest = i.ModTime()
					}
				} else {
					bad++
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					bad++
				}
				break
			}
		}
		_ = f.Close()
	}
	s.fold(size, newest, bad)
	return false
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
