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
