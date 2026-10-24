//go:build windows

// detectLang reads the user's Windows display language: the primary language
// of the UI language id decides, so zh-CN and zh-TW alike read as "zh".
package app

import "syscall"

var userLang = syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")

func detectLang() string {
	id, _, _ := userLang.Call()
	if uint16(id)&0xFF == 0x04 {
		return "zh"
	}
	return "en"
}
