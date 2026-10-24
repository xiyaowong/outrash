// i18n.go holds the language switch. The Chinese text stays where it is
// spoken — in the call sites and in rules.json — and L picks the English
// line when the user (or the OS) asked for it. The catalog's English comes
// from the id-keyed maps below, so rules.json never grows a schema for it.
package core

import "strings"

// The display language: "zh" (the default) or "en".
var lang = "zh"

// SetLang picks the display language; anything unknown reads as "zh".
func SetLang(l string) {
	if l == "en" {
		lang = "en"
		ErrUnsafe = errUnsafeEn
	} else {
		lang = "zh"
		ErrUnsafe = errUnsafeZh
	}
}

// Lang reports the display language.
func Lang() string { return lang }

// L picks between the Chinese and the English line.
func L(zh, en string) string {
	if lang == "en" {
		return en
	}
	return zh
}

// GroupName is a group's display name.
func GroupName(g *Group) string {
	if lang == "en" {
		if s := groupEN[g.ID]; s != "" {
			return s
		}
	}
	return g.Name
}

// ItemName is an item's display name.
func ItemName(it *Item) string {
	if lang == "en" {
		if s := itemEN[it.ID]; s != "" {
			return s
		}
	}
	return it.Name
}

// ItemNote is an item's note on how its cache comes back. The notes are a
// small closed set, so the map is keyed by the Chinese line itself.
func ItemNote(it *Item) string {
	return L(it.Note, noteEN[it.Note])
}

var groupEN = map[string]string{
	"node":     "Node.js",
	"dotnet":   ".NET",
	"go":       "Go",
	"rust":     "Rust",
	"python":   "Python",
	"ai":       "AI models",
	"ruby":     "Ruby",
	"jvm":      "JVM",
	"browsers": "Browsers",
	"editors":  "Editors",
	"chat":     "Chat",
	"media":    "Media",
	"cn":       "Domestic apps",
	"other":    "Other",
}

var itemEN = map[string]string{
	"npm":            "npm cache",
	"pnpm":           "pnpm store",
	"yarn":           "Yarn cache",
	"bun":            "Bun cache",
	"deno":           "Deno cache",
	"playwright":     "Playwright browsers",
	"electron":       "Electron binaries",
	"cypress":        "Cypress binaries",
	"nuget-packages": "NuGet global packages",
	"nuget-http":     "NuGet HTTP cache",
	"go-build":       "Build cache",
	"go-mod":         "Module cache",
	"cargo-registry": "Cargo registry",
	"cargo-git":      "Cargo git checkouts",
	"pip":            "pip cache",
	"uv":             "uv cache",
	"poetry":         "Poetry cache",
	"conda":          "Conda package cache",
	"huggingface":    "Hugging Face models",
	"gems":           "RubyGems cache",
	"gradle":         "Gradle caches",
	"maven":          "Maven local repository",
	"coursier":       "Coursier cache",
	"ivy":            "Ivy cache",
	"edge":           "Microsoft Edge cache",
	"chrome":         "Google Chrome cache",
	"firefox":        "Firefox cache",
	"brave":          "Brave cache",
	"vivaldi":        "Vivaldi cache",
	"opera":          "Opera cache",
	"vscode":         "VS Code cache",
	"jetbrains":      "JetBrains IDE indexes",
	"slack":          "Slack cache",
	"discord":        "Discord cache",
	"teams":          "Teams cache",
	"spotify":        "Spotify cache",
	"qqnt":           "QQ temp files",
	"baidunetdisk":   "Baidu Netdisk cache",
	"wps":            "WPS cache",
	"netease-music":  "NetEase Cloud Music cache",
	"pub":            "Flutter / Dart",
	"composer":       "Composer cache",
	"swiftpm":        "Swift Package cache",
	"vcpkg":          "vcpkg binaries",
	"conan":          "Conan package cache",
	"hex":            "Hex package cache",
	"cabal":          "Cabal caches",
	"xcode":          "Xcode derived data",
}

var noteEN = map[string]string{
	"下次安装依赖时自动重新下载":                  "Re-downloaded on the next install",
	"下次运行时自动重新下载":                    "Re-downloaded on the next run",
	"下次安装时自动重新下载":                    "Re-downloaded on the next install",
	"还原项目时自动重新下载":                    "Re-downloaded when a project restores",
	"go build 时自动重新编译":               "Recompiled by go build",
	"构建时自动重新下载":                      "Re-fetched on the next build",
	"构建时自动重新编译":                      "Recompiled on the next build",
	"构建时自动重新下载或重新编译":                 "Re-fetched or recompiled on the next build",
	"下次加载模型时自动重新下载":                  "Re-downloaded on the next model load",
	"下次打开网页时自动重新下载":                  "Re-downloaded as you browse",
	"重新打开时自动重建":                      "Rebuilt on the next launch",
	"打开项目时自动重建":                      "Rebuilt when a project reopens",
	"重新打开时自动重新下载":                    "Re-downloaded on the next launch",
	"使用时自动重建":                         "Rebuilt on use",
	"下次播放时自动重新下载":                    "Re-downloaded on the next play",
	"可用 npx playwright install 重新下载": "Re-download with npx playwright install",
	"可用 npx cypress install 重新下载":    "Re-download with npx cypress install",
}

// joinList joins names the way the current language reads.
func joinList(names []string) string {
	return strings.Join(names, L("、", ", "))
}
