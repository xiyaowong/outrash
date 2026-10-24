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

// ErrUnsafe marks a path the cleaner refuses to touch.
var ErrUnsafe = errors.New("拒绝删除：不在扫描范围内，或路径过于靠近根目录")

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
			if err := os.RemoveAll(s.Path); err != nil {
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
		return fmt.Errorf("%w：%s 是链接", ErrUnsafe, p)
	}
	return nil
}
