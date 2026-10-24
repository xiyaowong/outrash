// clean.go removes the paths a plan lists, and nothing else.
//
// The guards here exist because a cache cleaner that is wrong once is worse
// than none: it only removes paths measured under a catalog root, refuses
// anything that looks like a root itself, and never follows a link.
package core

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
}

// ErrUnsafe marks a path the cleaner refuses to touch. SetLang re-points it
// per language; identity holds within a session, which is what errors.Is
// needs.
var (
	errUnsafeZh = errors.New("拒绝删除：不在扫描范围内，或路径过于靠近根目录")
	errUnsafeEn = errors.New("refusing to delete: outside the scanned roots, or too close to a drive root")
	ErrUnsafe   = errUnsafeZh
)

// Remove removes every step in order. roots are the directories the scan
// measured; a step outside all of them is refused, so a catalog change
// between scan and clean cannot widen the blast radius.
func Remove(p *Plan, roots []string) Outcome { return RemoveWith(p, roots, nil) }

// RemoveWith is Remove, reporting how many bytes it has finished with as it
// goes. A step counts as done whether it was removed, was already gone, or
// failed: the estimate is about time spent, not success.
func RemoveWith(p *Plan, roots []string, progress func(done, total int64)) Outcome {
	return removePlan(p, roots, progress, nil)
}

// RemoveSteps is RemoveWith, and tells onStep about each step the moment it
// is over, so a UI can strike its row off while the estimate keeps running.
func RemoveSteps(p *Plan, roots []string, progress func(done, total int64), onStep func(Removal)) Outcome {
	return removePlan(p, roots, progress, onStep)
}

func removePlan(p *Plan, roots []string, progress func(done, total int64), onStep func(Removal)) Outcome {
	start := time.Now()
	var out Outcome
	var done int64
	for _, s := range p.Steps {
		r := Removal{Path: s.Path, Bytes: s.Bytes}
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
			if err := removeTree(s.Path, func(n int64) {
				if progress != nil {
					progress(done+n, p.Bytes)
				}
			}); err != nil {
				r.Err = err
			} else {
				out.Freed += s.Bytes
			}
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
// far. It removes what os.RemoveAll would, with the same rule the cleaner is
// built on: a link is unlinked, never entered.
func removeTree(root string, report func(int64)) error {
	if report == nil {
		report = func(int64) {}
	}
	info, err := os.Lstat(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // the cache went away between the scan and the run
		}
		return err
	}
	if !info.IsDir() {
		// A plan step can be one file, as the extension caches are.
		if err := removePath(root); err != nil {
			return err
		}
		report(info.Size())
		return nil
	}
	var removed int64
	if err := removeBelow(root, &removed, report); err != nil {
		return err
	}
	return removePath(root)
}

// removeBelow empties dir bottom-up. DirEntry.IsDir is false for links, so a
// junction or symlink inside the cache is unlinked where it sits rather than
// walked through: the scan refuses a root behind a link, and the walk must
// not reopen that door from inside the tree.
func removeBelow(dir string, removed *int64, report func(int64)) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // the cache went away under the walk
		}
		return err
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		// Files only: the scan measures file bytes, and on Linux a directory
		// entry carries its own size that the estimate must not count.
		if !e.IsDir() {
			if info, err := e.Info(); err == nil {
				*removed += info.Size()
			}
		}
		if e.IsDir() {
			if err := removeBelow(p, removed, report); err != nil {
				return err
			}
		}
		if err := removePath(p); err != nil {
			return err
		}
		if !e.IsDir() {
			report(*removed)
		}
	}
	return nil
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
		return ErrUnsafe
	}
	p = filepath.Clean(p)
	if filepath.Dir(p) == p {
		return ErrUnsafe
	}
	// An entry always sits at least two levels under a drive: never the
	// drive itself or a folder directly under it.
	rest := strings.Trim(strings.TrimPrefix(p, filepath.VolumeName(p)), `/\`)
	if len(strings.Split(rest, string(filepath.Separator))) < 2 {
		return ErrUnsafe
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
	return ErrUnsafe
}

// noLinks fails if the root itself or any component of p below it is a
// link. A root swapped for a junction after the scan moves everything under
// it, and a link inside the root moves one step; both would let RemoveAll
// act outside the directory the scan measured. Only junctions and symlinks
// matter here: a plain folder at the entry is exactly what a plan step is.
func noLinks(root, p string) error {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return ErrUnsafe
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
		return fmt.Errorf(L("%w：%s 是链接", "%w: %s is a link"), ErrUnsafe, p)
	}
	return nil
}
