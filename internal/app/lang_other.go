//go:build !windows

// detectLang reads the Unix locale. Outside Windows the app is a secondary
// citizen, so the usual environment variables decide and anything that is
// not Chinese reads as English.
package app

import (
	"os"
	"strings"
)

func detectLang() string {
	// gettext's LANGUAGE outranks every LC_* variable for messages, and
	// Chinese desktops routinely ship LANG=en with LANGUAGE=zh_CN:en. "C"
	// and "POSIX" switch it off, so they fall through to the LC_* chain.
	if v := strings.TrimSpace(os.Getenv("LANGUAGE")); v != "" && v != "C" && v != "POSIX" {
		first := strings.TrimSpace(strings.SplitN(v, ":", 2)[0])
		if strings.HasPrefix(strings.ToLower(first), "zh") {
			return "zh"
		}
		return "en"
	}
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := strings.TrimSpace(os.Getenv(k))
		if v == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(v), "zh") {
			return "zh"
		}
		return "en"
	}
	return "en"
}
