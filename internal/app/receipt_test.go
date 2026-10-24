package app

import (
	"testing"

	"outrash/internal/core"
)

// A receipt settles only when the scan finish started for it comes home. A
// rescan that was already on its way when a newer clean replaced the report
// wears a different number, and its data must never be filed under the new
// clean.
func TestReceiptWaitsForItsOwnScan(t *testing.T) {
	a := testApp(t, false)
	a.now = fixedTime(0)
	a.seq = 5
	rc := &receipt{planned: map[string]bool{}, failed: map[string]bool{}, after: -1}
	a.report = &report{receipt: rc}
	cache := &core.Cache{Results: map[string]core.Result{}}

	a.closeReceipt(cache)
	if rc.measured {
		t.Fatal("a scan wearing a foreign number settled the receipt")
	}

	rc.scanSeq = 5
	a.closeReceipt(cache)
	if !rc.measured {
		t.Fatal("the scan the receipt was started for did not settle it")
	}
}

// The delta keys survive a language toggle mid-clean: the footer stays
// clickable while the removal runs, and the rescan settles the receipt
// whenever the user got there, so the snapshot can only be matched by the
// catalog's own IDs — a display name would answer to whichever language the
// frame speaks at settle time and miss every family whose name differs.
func TestReceiptSurvivesLanguageToggle(t *testing.T) {
	a := testApp(t, false)
	a.now = fixedTime(0)
	rc := a.openReceipt(&core.Plan{})
	rc.scanSeq = a.seq
	defer core.SetLang("zh")
	core.SetLang("en")
	a.report = &report{receipt: rc}
	cache := &core.Cache{Results: map[string]core.Result{}}
	a.closeReceipt(cache)
	if !rc.measured || rc.after != rc.before {
		t.Fatalf("after a language toggle the receipt lost the families: measured %v, after %d, before %d",
			rc.measured, rc.after, rc.before)
	}
}
