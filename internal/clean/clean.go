// Package clean removes the paths a plan lists, and nothing else.
//
// The guards here exist because a cache cleaner that is wrong once is worse
// than none: it only removes paths measured under a catalog root, refuses
// anything that looks like a root itself, and never follows a link.
package clean

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"outrash/internal/plan"
)

// Result is what one removal did.
type Result struct {
	Path  string
	Bytes int64
	Err   error
}

// Outcome totals a run.
type Outcome struct {
	Runs    []Result
	Freed   int64
	Failed  int
	Elapsed time.Duration
}

// ErrUnsafe marks a path the cleaner refuses to touch.
var ErrUnsafe = errors.New("拒绝删除：不在扫描范围内，或路径过于靠近根目录")

// Run removes every step in order. roots are the directories the scan
// measured; a step outside all of them is refused, so a catalog change
// between scan and clean cannot widen the blast radius.
func Run(p *plan.Plan, roots []string) Outcome {
	start := time.Now()
	var out Outcome
	for _, s := range p.Steps {
		r := Result{Path: s.Path, Bytes: s.Bytes}
		if err := check(s.Path, roots); err != nil {
			r.Err = err
		} else if err := os.RemoveAll(s.Path); err != nil {
			r.Err = err
		} else {
			out.Freed += s.Bytes
		}
		if r.Err != nil {
			out.Failed++
		}
		out.Runs = append(out.Runs, r)
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
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w：%s 是链接", ErrUnsafe, p)
		}
		return nil
	}
	return ErrUnsafe
}
