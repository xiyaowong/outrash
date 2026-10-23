// plan.go turns scan results and the user's choices into an ordered list of
// removals. Nothing in this file deletes anything: a Plan is what the window
// shows before anyone acts.
package core

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Policy is what a selection keeps or gives up. Its zero value removes
// every chosen entry.
type Policy struct {
	// KeepNewest keeps the newest version of each versioned package.
	KeepNewest bool
	// StaleDays drops entries used more recently than this from the plan.
	// Zero means no age test.
	StaleDays int
	// Now is the clock the age test uses; zero means time.Now.
	Now time.Time
}

// Pick is one thing the user chose: a file, a package folder, or a single
// version inside one. Picks are what the tree's check boxes produce.
type Pick struct {
	Path    string
	Name    string
	Bytes   int64
	ModTime time.Time
	// Package groups versions of the same dependency, so KeepNewest can
	// keep one of them. Empty for entries that are not versioned.
	Package string
}

// Mode names why a removal is on the list, which the window shows as the
// step's label.
type Mode string

const (
	ModeAll   Mode = "全部"
	ModeOld   Mode = "旧版本"
	ModeStale Mode = "陈旧"
)

// Step is one removal: what, why, and what it costs to rebuild.
type Step struct {
	Path    string
	Name    string
	Bytes   int64
	ModTime time.Time
	Reason  string
	Mode    Mode
	Group   string
	Item    string
}

// Plan is the whole job, largest removal first.
type Plan struct {
	Steps []Step
	Bytes int64
	// Title says which action made the plan, for the sheet's header.
	Title string
}

// Select decides what the policy removes from the picks.
func Select(picks []Pick, pol Policy) *Plan {
	if pol.Now.IsZero() {
		pol.Now = time.Now()
	}
	var newest map[string]int
	if pol.KeepNewest {
		newest = make(map[string]int, len(picks))
		for i, p := range picks {
			if p.Package == "" {
				continue
			}
			if j, ok := newest[p.Package]; !ok || newer(p, picks[j]) {
				newest[p.Package] = i
			}
		}
	}
	var steps []Step
	for i, p := range picks {
		if pol.KeepNewest && p.Package != "" && newest[p.Package] == i {
			continue
		}
		fresh := pol.Fresh(p.ModTime)
		versioned := p.Package != ""
		switch {
		case fresh:
			continue
		case versioned && pol.KeepNewest && pol.StaleDays > 0:
			steps = append(steps, step(p, ModeOld, "旧版本，且很久未使用"))
		case versioned && pol.KeepNewest:
			steps = append(steps, step(p, ModeOld, "旧版本，下次构建时重新下载"))
		case pol.StaleDays > 0:
			steps = append(steps, step(p, ModeStale, fmt.Sprintf("超过 %d 天未使用，用时自动重建", pol.StaleDays)))
		default:
			steps = append(steps, step(p, ModeAll, "用时自动重新下载"))
		}
	}
	return (&Plan{Steps: steps, Title: describe(pol, len(picks))}).Total()
}

// newer reports whether a is a later version than b. It reads the version
// from the pick name, which a touch or restore order cannot distort, and
// falls back to ModTime only when neither name carries one.
func newer(a, b Pick) bool {
	av, aok := pickVersion(a)
	bv, bok := pickVersion(b)
	switch {
	case aok && bok:
		return compareVersion(av, bv) > 0
	case aok:
		return true
	case bok:
		return false
	}
	return a.ModTime.After(b.ModTime)
}

// version is up to four numeric segments plus an optional prerelease.
type version struct {
	nums [4]uint64
	pre  []string
}

// pickVersion takes the name's last field, or the part after the package
// prefix in its last path element.
func pickVersion(p Pick) (version, bool) {
	name := strings.TrimSpace(p.Name)
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return version{}, false
	}
	cand := fields[len(fields)-1]
	if v, ok := parseVersion(cand); ok {
		return v, true
	}
	if p.Package == "" {
		return version{}, false
	}
	for _, sep := range []string{".", "-", "_"} {
		tail, ok := strings.CutPrefix(cand, p.Package+sep)
		if !ok {
			continue
		}
		if v, ok := parseVersion(tail); ok {
			return v, true
		}
	}
	return version{}, false
}

func parseVersion(s string) (version, bool) {
	s = strings.TrimSpace(s)
	if len(s) > 1 && (s[0] == 'v' || s[0] == 'V') && isDigit(s[1]) {
		s = s[1:]
	}
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	var pre []string
	if i := strings.IndexByte(s, '-'); i >= 0 {
		if i == len(s)-1 {
			return version{}, false
		}
		s, pre = s[:i], strings.Split(s[i+1:], ".")
	}
	parts := strings.Split(s, ".")
	if len(parts) > 4 {
		return version{}, false
	}
	var v version
	for i, part := range parts {
		n, ok := number(part)
		if !ok {
			return version{}, false
		}
		v.nums[i] = n
	}
	v.pre = pre
	return v, true
}

func compareVersion(a, b version) int {
	for i := range a.nums {
		if c := cmp.Compare(a.nums[i], b.nums[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	return comparePre(a.pre, b.pre)
}

func comparePre(a, b []string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		an, aok := number(a[i])
		bn, bok := number(b[i])
		switch {
		case aok && bok:
			if c := cmp.Compare(an, bn); c != 0 {
				return c
			}
		case aok:
			return -1
		case bok:
			return 1
		default:
			if c := strings.Compare(a[i], b[i]); c != 0 {
				return c
			}
		}
	}
	return cmp.Compare(len(a), len(b))
}

func number(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	for i := range len(s) {
		if !isDigit(s[i]) {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	return n, err == nil
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func step(p Pick, m Mode, reason string) Step {
	return Step{Path: p.Path, Name: p.Name, Bytes: p.Bytes, ModTime: p.ModTime, Reason: reason, Mode: m}
}

func describe(pol Policy, chosen int) string {
	switch {
	case pol.KeepNewest && pol.StaleDays > 0:
		return "保留最新版，并清理很久未使用的"
	case pol.KeepNewest:
		return "每个依赖只保留最新版本"
	case pol.StaleDays > 0:
		return fmt.Sprintf("清理 %d 天以上未使用的内容", pol.StaleDays)
	case chosen == 1:
		return "清理这一项"
	}
	return "清理已勾选的内容"
}

// Add merges another plan into p, labelling its steps.
func (p *Plan) Add(group, item string, other *Plan) {
	for _, s := range other.Steps {
		s.Group, s.Item = group, item
		p.Steps = append(p.Steps, s)
	}
	p.Total()
}

// Total recomputes Bytes and orders the steps.
func (p *Plan) Total() *Plan {
	p.Bytes = 0
	for _, s := range p.Steps {
		p.Bytes += s.Bytes
	}
	slices.SortStableFunc(p.Steps, func(a, b Step) int { return cmp.Compare(b.Bytes, a.Bytes) })
	return p
}

// Groups lists the distinct group names in a plan, in plan order, with the
// steps of each.
func (p *Plan) Groups() []struct {
	Name  string
	Steps []Step
	Bytes int64
} {
	var out []struct {
		Name  string
		Steps []Step
		Bytes int64
	}
	at := map[string]int{}
	for _, s := range p.Steps {
		i, ok := at[s.Group]
		if !ok {
			i = len(out)
			at[s.Group] = i
			out = append(out, struct {
				Name  string
				Steps []Step
				Bytes int64
			}{Name: s.Group})
		}
		out[i].Steps = append(out[i].Steps, s)
		out[i].Bytes += s.Bytes
	}
	return out
}

// Empty reports whether the plan has nothing to do.
func (p *Plan) Empty() bool { return len(p.Steps) == 0 }

// Fresh reports whether an entry was used recently enough to keep.
func (p Policy) Fresh(t time.Time) bool {
	if p.StaleDays <= 0 {
		return false
	}
	now := p.Now
	if now.IsZero() {
		now = time.Now()
	}
	return now.Sub(t) < time.Duration(p.StaleDays)*24*time.Hour
}

// Bytes formats a byte count the way people read disk sizes.
func Bytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTP"[exp])
}
