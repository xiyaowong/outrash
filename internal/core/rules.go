// rules.go reads the cache catalog: which tools keep rebuildable files,
// where they put them, and how their entries are shaped.
package core

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Unit says how the children of a root are counted.
const (
	// UnitEntries treats each child of the root as one removable entry.
	UnitEntries = "entries"
	// UnitVersioned treats grandchildren as entries, grouped by package:
	// root/<package>/<version>. Old versions are what a "keep latest"
	// pass removes.
	UnitVersioned = "versioned"
)

// Config is rules.json.
type Config struct {
	Version   int     `json:"version"`
	StaleDays int     `json:"staleDays"`
	Groups    []Group `json:"groups"`
}

// Group is a tool family shown as one card.
type Group struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Items []Item `json:"items"`
}

// Item is one cache a user can clean.
type Item struct {
	ID    string              `json:"id"`
	Name  string              `json:"name"`
	Note  string              `json:"note"`
	Env   []string            `json:"env"`
	Paths map[string][]string `json:"paths"`
	Probe *Probe              `json:"probe"`
	Unit  string              `json:"unit"`
	// Exclude names children of a root that live there but are not cache:
	// Poetry keeps project virtualenvs inside its cache directory, and a
	// venv is not rebuilt by the tool the way a downloaded package is.
	Exclude []string `json:"exclude"`
}

// Probe asks a tool where it keeps its cache, for the paths a user moved
// with the tool's own config rather than an environment variable.
type Probe struct {
	Cmd  string   `json:"cmd"`
	Args []string `json:"args"`
}

// Parse reads and checks a catalog.
func Parse(data []byte) (*Config, error) {
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("rules: %w", err)
	}
	if c.StaleDays <= 0 {
		c.StaleDays = 30
	}
	seen := map[string]bool{}
	for gi := range c.Groups {
		g := &c.Groups[gi]
		if g.ID == "" || g.Name == "" {
			return nil, fmt.Errorf("rules: group %d needs an id and a name", gi)
		}
		if seen[g.ID] {
			return nil, fmt.Errorf("rules: duplicate group %q", g.ID)
		}
		seen[g.ID] = true
		itemSeen := map[string]bool{}
		for ii := range g.Items {
			it := &g.Items[ii]
			if it.ID == "" || it.Name == "" {
				return nil, fmt.Errorf("rules: %s: item %d needs an id and a name", g.ID, ii)
			}
			if itemSeen[it.ID] {
				return nil, fmt.Errorf("rules: %s: duplicate item %q", g.ID, it.ID)
			}
			itemSeen[it.ID] = true
			switch it.Unit {
			case "":
				it.Unit = UnitEntries
			case UnitEntries, UnitVersioned:
			default:
				return nil, fmt.Errorf("rules: %s/%s: unknown unit %q", g.ID, it.ID, it.Unit)
			}
		}
	}
	return &c, nil
}

// Items returns every item with the group it belongs to.
func (c *Config) Items() []struct {
	Group Group
	Item  Item
} {
	var out []struct {
		Group Group
		Item  Item
	}
	for _, g := range c.Groups {
		for _, it := range g.Items {
			out = append(out, struct {
				Group Group
				Item  Item
			}{g, it})
		}
	}
	return out
}

// Options carries what path expansion needs, so that it can run without
// touching the process environment in tests.
type Options struct {
	GOOS   string
	Home   string
	Getenv func(string) string
	// Run asks a tool where it keeps its cache. It is only called for
	// items whose environment and default paths both came up empty.
	Run func(cmd string, args []string) (string, error)
}

// DefaultOptions reads the real environment.
func DefaultOptions() Options {
	home, _ := os.UserHomeDir()
	return Options{
		GOOS:   runtime.GOOS,
		Home:   home,
		Getenv: os.Getenv,
		Run:    runOnce,
	}
}

// runOnce runs a probe command, through the shell on Windows where tools
// are .cmd shims.
func runOnce(name string, args []string) (string, error) {
	cmd := exec.Command(name, args...)
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", append([]string{"/c", name}, args...)...)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range slices.Backward(lines) {
		if s := strings.TrimSpace(line); s != "" {
			return s, nil
		}
	}
	return "", fmt.Errorf("%s printed nothing", name)
}

// Roots returns the directories this item may keep files in, existing or
// not, in the order they should be tried. fromEnv marks the roots an
// environment variable named: the tool's own config says where to look, but
// nothing vouches for what the value actually is, so callers treat those
// roots as a claim to display, not as a verified cache.
func (it Item) Roots(o Options) (dirs []string, source string, fromEnv bool) {
	for _, name := range it.Env {
		if v := strings.TrimSpace(o.Getenv(name)); v != "" {
			if d, ok := clean(v, o); ok {
				dirs = append(dirs, d)
			}
			if len(dirs) > 0 {
				return dirs, L("环境变量 ", "Environment variable ") + name, true
			}
		}
	}
	for _, tpl := range it.Paths[o.GOOS] {
		// A template may name a per-profile folder with a wildcard, because
		// browsers and chat apps keep one cache per profile under a name the
		// user chose. Only paths that exist come back, so a pattern that
		// matches nothing adds nothing.
		if strings.ContainsAny(tpl, "*?[") {
			dirs = append(dirs, globPath(tpl, o)...)
			continue
		}
		d, ok := expand(tpl, o)
		if !ok {
			continue
		}
		dirs = append(dirs, d)
	}
	dirs = unique(dirs)
	if anyExists(dirs) {
		return dirs, L("默认位置", "Default location"), false
	}
	if it.Probe == nil || o.Run == nil {
		return dirs, L("默认位置", "Default location"), false
	}
	if out, err := o.Run(it.Probe.Cmd, it.Probe.Args); err == nil {
		if d, ok := clean(out, o); ok && exists(d) {
			return append(dirs, d), it.Probe.Cmd + " " + strings.Join(it.Probe.Args, " "), false
		}
	}
	return dirs, L("默认位置", "Default location"), false
}

// expand resolves ~ and %VAR% in a path template. It reports false when a
// variable is unset, which means the template does not apply here.
func expand(tpl string, o Options) (string, bool) {
	s := tpl
	if rest, ok := strings.CutPrefix(s, "~"); ok {
		if o.Home == "" {
			return "", false
		}
		s = filepath.Join(o.Home, rest)
	}
	s, ok := expandVars(s, o)
	if !ok {
		return "", false
	}
	return clean(s, o)
}

// expandVars substitutes %VAR% references, reporting false when one is
// unset — the template does not apply here.
func expandVars(s string, o Options) (string, bool) {
	var b strings.Builder
	for {
		i := strings.Index(s, "%")
		if i < 0 {
			b.WriteString(s)
			break
		}
		j := strings.Index(s[i+1:], "%")
		if j < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		name := s[i+1 : i+1+j]
		v := o.Getenv(name)
		if v == "" {
			return "", false
		}
		b.WriteString(v)
		s = s[i+j+2:]
	}
	return b.String(), true
}

func clean(p string, o Options) (string, bool) {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, `"'`)
	if p == "" {
		return "", false
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if o.Home == "" {
			return "", false
		}
		p = filepath.Join(o.Home, p[2:])
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	return filepath.Clean(abs), true
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func anyExists(dirs []string) bool {
	return slices.ContainsFunc(dirs, exists)
}

func unique(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// globPath expands a template whose wildcards the catalog wrote. The
// template is walked segment by segment, and only the segments that carry
// metacharacters before expansion are matched as patterns: a home directory
// or variable whose value contains * or [ is data, not a pattern.
// filepath.Glob cannot tell the two apart — and on Windows it cannot escape
// metacharacters at all — so the catalog's wildcards are matched by hand,
// and only paths that exist come back.
func globPath(tpl string, o Options) []string {
	tpl = strings.ReplaceAll(tpl, `\`, "/")
	var cur []string
	if strings.HasPrefix(tpl, "/") {
		cur = []string{"/"}
	}
	for seg := range strings.SplitSeq(tpl, "/") {
		if seg == "" {
			continue
		}
		if seg == "~" && len(cur) == 0 {
			if o.Home == "" {
				return nil
			}
			cur = []string{o.Home}
			continue
		}
		pattern := strings.ContainsAny(seg, "*?[")
		seg, ok := expandVars(seg, o)
		if !ok {
			return nil
		}
		if len(cur) == 0 {
			if pattern {
				return nil
			}
			cur = []string{seg}
			continue
		}
		if pattern {
			var next []string
			for _, d := range cur {
				children, err := os.ReadDir(d)
				if err != nil {
					continue
				}
				for _, ch := range children {
					if ok, err := filepath.Match(seg, ch.Name()); err != nil || !ok {
						continue
					}
					next = append(next, joinSeg(d, ch.Name()))
				}
			}
			cur = next
			if len(cur) == 0 {
				return nil
			}
			continue
		}
		for i, d := range cur {
			cur[i] = joinSeg(d, seg)
		}
	}
	out := cur[:0]
	for _, d := range cur {
		if abs, err := filepath.Abs(d); err == nil {
			d = abs
		}
		if exists(d) {
			out = append(out, d)
		}
	}
	return out
}

// joinSeg appends one path segment; a bare drive letter has no separator
// for filepath.Join to keep, so it is joined by hand.
func joinSeg(d, seg string) string {
	if strings.HasSuffix(d, ":") {
		return d + string(filepath.Separator) + seg
	}
	return filepath.Join(d, seg)
}
