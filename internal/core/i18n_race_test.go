package core

import "testing"

// The window flips the language while scan and clean workers are reading it
// from their goroutines; run the two against each other so the race
// detector has something to look at.
func TestLangSwitchRaceFree(t *testing.T) {
	defer SetLang("zh")
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 500 {
			SetLang("en")
			SetLang("zh")
		}
	}()
	g := &Group{ID: "node", Name: "Node.js"}
	for range 500 {
		L("缓存", "Cache")
		Lang()
		GroupName(g)
		_ = unsafeErr()
	}
	<-done
}
