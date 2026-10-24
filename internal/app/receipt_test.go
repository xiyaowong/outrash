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
