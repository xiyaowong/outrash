// clean.go removes the paths a plan lists, and nothing else.
//
// The guards here exist because a cache cleaner that is wrong once is worse
// than none: it only removes paths measured under a catalog root, refuses
// anything that looks like a root itself, and never follows a link.
package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Removal is what one removal did.
type Removal struct {
	Path  string
	Bytes int64
	Err   error
	// Gone marks a path that was already absent, which is not a failure:
	// the tool or the user cleared it between the scan and the run.
	Gone bool
}

// Outcome totals a run.
type Outcome struct {
	Runs    []Removal
	Freed   int64
	Failed  int
	Gone    int
	Elapsed time.Duration
	// Canceled marks a run stopped from outside. Runs holds only the steps
	// that were over; the step in flight at the stop is counted in Freed for
	// the files it did remove, but is not a finished step.
	Canceled bool
}

const (
	removalProgressBytes = 1 << 20
	removalProgressFiles = 256
	removalProgressTime  = 100 * time.Millisecond
	// removalWorkers bounds the goroutines unlinking inside one tree. Removal
	// is metadata-bound, so a few workers overlap the wait; four measured
	// roughly twice the throughput of one, and eight added little.
	removalWorkers = 4
)

// removalProgress batches per-file reports for one tree. Workers report
// concurrently, so the batch counters sit behind the mutex.
type removalProgress struct {
	mu     sync.Mutex
	report func(int64)
	last   int64
	at     time.Time
	files  int
}

func (p *removalProgress) due(done int64) bool {
	p.files++
	if p.at.IsZero() {
		return true
	}
	return p.files%removalProgressFiles == 0 || done-p.last >= removalProgressBytes
}

// file reports the counter's value once a batch boundary has passed. The load
// sits under the mutex: workers add between reports, and reading outside it
// could hand the UI a smaller number after a larger one.
func (p *removalProgress) file(removed *atomic.Int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	done := removed.Load()
	if p.due(done) {
		p.update(done, time.Now(), false)
	}
}

// flush reports the final count whatever the batching would have said.
func (p *removalProgress) flush(done int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.update(done, time.Now(), true)
}

func (p *removalProgress) update(done int64, now time.Time, force bool) {
	if !force && !p.at.IsZero() && done-p.last < removalProgressBytes && now.Sub(p.at) < removalProgressTime {
		return
	}
	p.report(done)
	p.last, p.at = done, now
}

// ErrUnsafe marks a path the cleaner refuses to touch. It is an immutable
// sentinel; the localized sentence is wrapped around it at creation time by
// unsafeErr, so errors.Is keeps its identity across language switches.
var ErrUnsafe = errors.New("unsafe")

// Remove removes every step in order. roots are the directories the scan
// measured; a step outside all of them is refused, so a catalog change
// between scan and clean cannot widen the blast radius.
func Remove(p *Plan, roots []string) Outcome { return RemoveWith(p, roots, nil) }

// RemoveWith is Remove, reporting how many bytes it has finished with as it
// goes. A step counts as done whether it was removed, was already gone, or
// failed: the estimate is about time spent, not success.
func RemoveWith(p *Plan, roots []string, progress func(done, total int64)) Outcome {
	return RemoveStepsContext(context.Background(), p, roots, progress, nil)
}

// RemoveSteps is RemoveWith, and tells onStep about each step the moment it
// is over, so a UI can strike its row off while the estimate keeps running.
func RemoveSteps(p *Plan, roots []string, progress func(done, total int64), onStep func(Removal)) Outcome {
	return RemoveStepsContext(context.Background(), p, roots, progress, onStep)
}

// RemoveStepsContext is RemoveSteps that stops once ctx is done. The stop is
// caught at the next file, so an interrupted tree leaves what it had not
// reached on disk, and the outcome marks itself Canceled.
func RemoveStepsContext(ctx context.Context, p *Plan, roots []string, progress func(done, total int64), onStep func(Removal)) Outcome {
	return removePlan(ctx, p, roots, progress, onStep)
}

func removePlan(ctx context.Context, p *Plan, roots []string, progress func(done, total int64), onStep func(Removal)) Outcome {
	if ctx == nil {
		ctx = context.Background()
	}
	start := time.Now()
	var out Outcome
	var done int64
	for _, s := range p.Steps {
		if ctx.Err() != nil {
			out.Canceled = true
			break
		}
		r := Removal{Path: s.Path, Bytes: s.Bytes}
		stopped := false
		switch err := check(s.Path, roots); {
		case errors.Is(err, fs.ErrNotExist):
			// Nothing to remove; the cache went away on its own.
			r.Gone = true
			out.Gone++
		case err != nil:
			r.Err = err
		default:
			// One step can hold most of the plan's bytes on its own, so it
			// goes file by file and the estimate follows the walk instead of
			// sitting still until the whole step is over.
			removed, err := removeTree(ctx, s.Path, func(n int64) {
				if progress != nil {
					progress(done+n, p.Bytes)
				}
			})
			// The files that did leave the disk still count, whether the tree
			// stopped on an error or on a cancel: the report's freed bytes must
			// match what the walk did.
			out.Freed += removed
			switch {
			case errors.Is(err, context.Canceled):
				stopped = true
			case err != nil:
				r.Err = err
			}
		}
		if stopped {
			// A half-removed step is not a finished one: leaving it out of Runs
			// keeps the report from claiming it, and keeps its row unstruck.
			out.Canceled = true
			break
		}
		if r.Err != nil {
			out.Failed++
		}
		out.Runs = append(out.Runs, r)
		if onStep != nil {
			onStep(r)
		}
		done += s.Bytes
		if progress != nil {
			progress(done, p.Bytes)
		}
	}
	out.Elapsed = time.Since(start)
	return out
}

// removeTree deletes the tree at root bottom-up, reporting the bytes gone so
// far and returning how much actually left the disk, even on failure: a step
// that stops halfway has still freed the bytes it got through.
//
// Directories cannot be deleted as a unit: every platform unlinks their
// entries one by one, and rmdir only succeeds once they are empty. What can
// overlap is the unlinking itself, so one tree's directories go through a
// small pool of workers. A directory, and the tree root, is removed only after
// its last descendant reports back.
func removeTree(ctx context.Context, root string, report func(int64)) (int64, error) {
	if report == nil {
		report = func(int64) {}
	}
	if ctx.Err() != nil {
		return 0, context.Canceled
	}
	info, err := os.Lstat(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil // the cache went away between the scan and the run
		}
		return 0, err
	}
	if !info.IsDir() {
		// A plan step can be one file, as the extension caches are.
		if err := removePath(root); err != nil {
			return 0, err
		}
		report(info.Size())
		return info.Size(), nil
	}
	w := newTreeWalk(ctx, report)
	err = w.remove(root)
	w.progress.flush(w.removed.Load())
	if w.canceled.Load() {
		// A stop that lands after the last unlink changed nothing: the tree is
		// gone, so the step finished. Only a root still on disk was cut short.
		if _, statErr := os.Lstat(root); statErr == nil {
			return w.removed.Load(), context.Canceled
		}
	}
	return w.removed.Load(), err
}

// dirNode is one directory in a running removal. children counts the
// directory's unfinished subdirectories plus one for its own listing, so the
// counter reaches zero exactly once: after the directory has been read and
// every descendant is gone.
type dirNode struct {
	path     string
	parent   *dirNode
	children atomic.Int32
	failed   atomic.Bool
}

// treeWalk removes one tree with a bounded pool. The pool starts on the first
// subdirectory, so a tree of plain files pays nothing for it.
type treeWalk struct {
	queue   chan *dirNode
	tasks   sync.WaitGroup // queued nodes plus the root being processed
	workers sync.WaitGroup
	once    sync.Once
	started atomic.Bool

	removed  atomic.Int64
	progress removalProgress
	// canceled is the stop signal read on the hot path. A channel select or
	// ctx.Err per file would cost more than the unlink it guards; the context
	// is watched once and the flag carries the answer.
	canceled   atomic.Bool
	stopCancel func() bool

	errMu sync.Mutex
	err   error
}

func newTreeWalk(ctx context.Context, report func(int64)) *treeWalk {
	w := &treeWalk{queue: make(chan *dirNode, removalWorkers*8)}
	w.progress.report = report
	w.stopCancel = context.AfterFunc(ctx, func() { w.canceled.Store(true) })
	return w
}

func (w *treeWalk) start() {
	w.once.Do(func() {
		w.started.Store(true)
		for range removalWorkers {
			w.workers.Add(1)
			go func() {
				defer w.workers.Done()
				for n := range w.queue {
					w.visit(n)
					w.tasks.Done()
				}
			}()
		}
	})
}

func (w *treeWalk) submit(n *dirNode) {
	w.start()
	w.tasks.Add(1)
	select {
	case w.queue <- n:
	default:
		// Every worker is busy and the queue is full; doing the node here
		// keeps the walk moving instead of blocking a worker on its own queue.
		w.visit(n)
		w.tasks.Done()
	}
}

// remove walks the root and waits for every worker, so no report outlives it.
func (w *treeWalk) remove(root string) error {
	n := &dirNode{path: root}
	n.children.Add(1)
	w.tasks.Add(1)
	w.visit(n)
	w.tasks.Done()
	w.tasks.Wait()
	if w.started.Load() {
		close(w.queue)
		w.workers.Wait()
	}
	if w.stopCancel != nil {
		w.stopCancel()
	}
	return w.err
}

// visit reads one directory and unlinks its files. Subdirectories go to the
// pool; DirEntry.IsDir is false for links, so a junction or symlink inside the
// cache is unlinked where it sits rather than walked through: the scan refuses
// a root behind a link, and the walk must not reopen that door from inside.
func (w *treeWalk) visit(n *dirNode) {
	if w.canceled.Load() {
		// The stop still has to unwind the counters, or the root never
		// settles; it just does so without touching the disk.
		w.finish(n)
		return
	}
	entries, err := readDir(n.path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			w.fail(err)
			n.failed.Store(true)
		}
		w.finish(n)
		return
	}
	for _, e := range entries {
		if w.canceled.Load() {
			// A directory that stays readable through the stop is left whole,
			// its unvisited children included: they are on disk either way.
			n.failed.Store(true)
			break
		}
		p := filepath.Join(n.path, e.Name())
		if e.IsDir() {
			child := &dirNode{path: p, parent: n}
			child.children.Add(1)
			n.children.Add(1)
			w.submit(child)
			continue
		}
		w.removeFile(n, p, e)
	}
	w.finish(n)
}

// removeFile unlinks one file and counts the bytes the scan measured for it.
// The size joins the total only after the removal succeeds, so a file that
// refuses to go is not reported as freed. Files only: on Linux a directory
// entry carries a size of its own that the estimate must not count.
func (w *treeWalk) removeFile(n *dirNode, p string, e fs.DirEntry) {
	if w.canceled.Load() {
		n.failed.Store(true)
		return
	}
	var size int64
	if info, err := e.Info(); err == nil {
		size = info.Size()
	}
	if err := removePath(p); err != nil {
		w.fail(err)
		// A directory that still holds this file cannot be removed either.
		n.failed.Store(true)
		return
	}
	w.removed.Add(size)
	w.progress.file(&w.removed)
}

// finish lowers n's outstanding count and, once it reaches zero, removes n
// and answers to its parent. Only the last descendant reaches zero, so each
// directory is removed exactly once, after everything below it. A canceled
// walk only unwinds: removing a directory would claim a step it did not finish.
func (w *treeWalk) finish(n *dirNode) {
	if n.children.Add(-1) != 0 {
		return
	}
	if !n.failed.Load() && !w.canceled.Load() {
		if err := removePath(n.path); err != nil {
			w.fail(err)
			n.failed.Store(true)
		}
	}
	if p := n.parent; p != nil {
		if n.failed.Load() {
			p.failed.Store(true)
		}
		w.finish(p)
	}
}

func (w *treeWalk) fail(err error) {
	w.errMu.Lock()
	if w.err == nil {
		w.err = err
	}
	w.errMu.Unlock()
}

func readDir(dir string) ([]fs.DirEntry, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	entries, err := f.ReadDir(-1)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return entries, err
}

// removePath is one unlink, with the retry RemoveAll does: caches mark files
// read-only, and on Windows the bit clears with a chmod.
func removePath(p string) error {
	err := os.Remove(p)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if info, ierr := os.Lstat(p); ierr == nil {
		if err := os.Chmod(p, info.Mode()|0o222); err == nil {
			if err := os.Remove(p); err == nil || errors.Is(err, fs.ErrNotExist) {
				return nil
			}
		}
	}
	return err
}

func check(p string, roots []string) error {
	if !filepath.IsAbs(p) {
		return unsafeErr()
	}
	p = filepath.Clean(p)
	if filepath.Dir(p) == p {
		return unsafeErr()
	}
	// An entry always sits at least two levels under a drive: never the
	// drive itself or a folder directly under it.
	rest := strings.Trim(strings.TrimPrefix(p, filepath.VolumeName(p)), `/\`)
	if len(strings.Split(rest, string(filepath.Separator))) < 2 {
		return unsafeErr()
	}
	for _, root := range roots {
		rel, err := filepath.Rel(filepath.Clean(root), p)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		// The root's own link is caught at scan time; anything left between
		// it and the entry is caught here, for every component: RemoveAll
		// walks through a junction inside the root and acts on its target.
		if err := noLinks(filepath.Clean(root), p); err != nil {
			return err
		}
		return nil
	}
	return unsafeErr()
}

// noLinks fails if the root itself or any component of p below it is a
// link. A root swapped for a junction after the scan moves everything under
// it, and a link inside the root moves one step; both would let RemoveAll
// act outside the directory the scan measured. Only junctions and symlinks
// matter here: a plain folder at the entry is exactly what a plan step is.
func noLinks(root, p string) error {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return unsafeErr()
	}
	dir := root
	if err := linkCheck(dir); err != nil {
		return err
	}
	for name := range strings.SplitSeq(rel, string(filepath.Separator)) {
		dir = filepath.Join(dir, name)
		if err := linkCheck(dir); err != nil {
			return err
		}
	}
	return nil
}

func linkCheck(p string) error {
	info, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if isLink(info) {
		// unsafeErr already wraps the sentinel; the link context joins the
		// sentence in the language it was built in.
		return fmt.Errorf("%w：%s 是链接", unsafeErr(), p)
	}
	return nil
}
