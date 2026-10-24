//go:build !windows

package app

import "testing"

func TestDetectLangReadsLANGUAGE(t *testing.T) {
	for _, tc := range []struct {
		language, lcAll, want string
	}{
		// The common Chinese-desktop shape: an English locale with a
		// Chinese-first LANGUAGE list, which gettext ranks higher.
		{"zh_CN:en", "en_US.UTF-8", "zh"},
		{"en:zh_CN", "zh_CN.UTF-8", "en"},
		{"", "zh_CN.UTF-8", "zh"},
		{"C", "zh_CN.UTF-8", "zh"},
		{"", "", "en"},
	} {
		t.Setenv("LANGUAGE", tc.language)
		t.Setenv("LC_ALL", tc.lcAll)
		t.Setenv("LC_MESSAGES", "")
		t.Setenv("LANG", "")
		if got := detectLang(); got != tc.want {
			t.Errorf("LANGUAGE=%q LC_ALL=%q: detectLang = %q, want %q", tc.language, tc.lcAll, got, tc.want)
		}
	}
}
