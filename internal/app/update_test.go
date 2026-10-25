package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The about sheet carries the check button, and a build without updates —
// every development build — answers its click without a failure.
func TestUpdateCheckDisabledShowsQuietButton(t *testing.T) {
	a := testApp(t, false)
	a.post = func(f func()) { f() }
	tt := ui.NewTester(a.view, 1200, 780)
	if err := tt.Click("关于"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("检查更新") {
		t.Fatalf("no check button; texts %v", tt.Texts())
	}
	prevCheck := checkUpdate
	defer func() { checkUpdate = prevCheck }()
	checkUpdate = func() (*mygo.Update, error) { return nil, mygo.ErrUpdatesDisabled }
	if err := tt.Click("检查更新"); err != nil {
		t.Fatal(err)
	}
	if a.update.phase != updateIdle {
		t.Fatalf("phase = %v, want back at idle: %v", a.update.phase, a.update.err)
	}
	if !tt.HasText("检查更新") {
		t.Fatalf("the button should read 检查更新 again; texts %v", tt.Texts())
	}
}

// A found update turns the button into the download; the download fills the
// bar and repaints as bytes arrive; its end turns the button into the
// restart.
func TestUpdateFlowThroughTheSheet(t *testing.T) {
	a := testApp(t, false)
	repaints := 0
	a.post = func(f func()) { repaints++; f() }
	tt := ui.NewTester(a.view, 1200, 780)
	if err := tt.Click("关于"); err != nil {
		t.Fatal(err)
	}

	prevCheck := checkUpdate
	prevInstall := runInstallUpdate
	defer func() { checkUpdate, runInstallUpdate = prevCheck, prevInstall }()
	checkUpdate = func() (*mygo.Update, error) {
		return &mygo.Update{Version: "0.2.0"}, nil
	}
	var progress func(int64, int64)
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	runInstallUpdate = func(up *mygo.Update, fn func(int64, int64)) error {
		progress = fn
		close(started)
		<-release
		return nil
	}

	if err := tt.Click("检查更新"); err != nil {
		t.Fatal(err)
	}
	if a.update.phase != updateReady || a.update.version != "0.2.0" {
		t.Fatalf("phase = %v version %q, want ready 0.2.0", a.update.phase, a.update.version)
	}
	if !tt.HasText("发现新版本 0.2.0") {
		t.Fatalf("the sheet does not name the version; texts %v", tt.Texts())
	}

	if err := tt.Click("下载更新"); err != nil {
		t.Fatal(err)
	}
	<-started
	if a.update.phase != updateDownloading {
		t.Fatalf("phase = %v, want downloading", a.update.phase)
	}
	if progress == nil {
		t.Fatal("Install never received the progress callback")
	}
	// A cursor without a repaint fills only in memory: the bar would stand
	// still for the whole download.
	before := repaints
	progress(250, 1000)
	if repaints == before {
		t.Fatal("a progress step never repainted")
	}
	if doneBytes, total := a.update.progress(); doneBytes != 250 || total != 1000 {
		t.Fatalf("progress = %d/%d, want 250/1000", doneBytes, total)
	}
	// The hand-off lands through the post seam; hold it until the test lets
	// go, then wait for it.
	a.post = func(f func()) { f(); close(done) }
	close(release)
	<-done
	// The hand-off happened outside a frame; rebuild one to see it.
	tt = ui.NewTester(a.view, 1200, 780)
	if !tt.HasText("重启以更新") {
		t.Fatalf("no restart button; texts %v", tt.Texts())
	}
}

// Closing the sheet ends a finished check: reopening offers 检查更新 again,
// not the old answer. A download in flight survives the close.
func TestUpdateStateClearsWhenTheSheetCloses(t *testing.T) {
	a := testApp(t, false)
	a.post = func(f func()) { f() }
	tt := ui.NewTester(a.view, 1200, 780)
	if err := tt.Click("关于"); err != nil {
		t.Fatal(err)
	}

	prevCheck, prevInstall := checkUpdate, runInstallUpdate
	defer func() { checkUpdate, runInstallUpdate = prevCheck, prevInstall }()
	checkUpdate = func() (*mygo.Update, error) {
		return &mygo.Update{Version: "0.2.0"}, nil
	}
	started, release := make(chan struct{}), make(chan struct{})
	runInstallUpdate = func(*mygo.Update, func(int64, int64)) error {
		close(started)
		<-release
		return nil
	}

	if err := tt.Click("检查更新"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	if a.update != nil {
		t.Fatalf("a finished check survived the close: %v", a.update.phase)
	}
	if err := tt.Click("关于"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("检查更新") {
		t.Fatalf("the sheet does not start over; texts %v", tt.Texts())
	}

	// A download in flight keeps its state, because its goroutine still has
	// to land the result somewhere.
	if err := tt.Click("检查更新"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("下载更新"); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	if a.update == nil || a.update.phase != updateDownloading {
		t.Fatalf("phase = %v, want the download kept", a.update)
	}
	close(release)
}

// A failed check says why and offers the button again.
func TestUpdateCheckFailure(t *testing.T) {
	a := testApp(t, false)
	a.post = func(f func()) { f() }
	tt := ui.NewTester(a.view, 1200, 780)
	if err := tt.Click("关于"); err != nil {
		t.Fatal(err)
	}

	prev := checkUpdate
	defer func() { checkUpdate = prev }()
	checkUpdate = func() (*mygo.Update, error) {
		return nil, errors.New("no route to host")
	}

	if err := tt.Click("检查更新"); err != nil {
		t.Fatal(err)
	}
	if a.update.phase != updateFailed {
		t.Fatalf("phase = %v, want failed", a.update.phase)
	}
	if !tt.HasText("检查失败：no route to host") {
		t.Fatalf("the failure is unnamed; texts %v", tt.Texts())
	}
}

// A failed install is a failure too, with the same way back.
func TestUpdateInstallFailure(t *testing.T) {
	a := testApp(t, false)
	a.post = func(f func()) { f() }
	tt := ui.NewTester(a.view, 1200, 780)
	if err := tt.Click("关于"); err != nil {
		t.Fatal(err)
	}

	prevCheck, prevInstall := checkUpdate, runInstallUpdate
	defer func() { checkUpdate, runInstallUpdate = prevCheck, prevInstall }()
	checkUpdate = func() (*mygo.Update, error) {
		return &mygo.Update{Version: "0.2.0"}, nil
	}
	runInstallUpdate = func(*mygo.Update, func(int64, int64)) error {
		return errors.New("disk full")
	}

	if err := tt.Click("检查更新"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("下载更新"); err != nil {
		t.Fatal(err)
	}
	if a.update.phase != updateFailed || a.update.err != "disk full" {
		t.Fatalf("phase = %v err %q, want failed disk full", a.update.phase, a.update.err)
	}
}

// A screenshot of the sheet in each state the check has, for the PR.
func TestUpdateSheetStates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		phase updatePhase
	}{
		{"idle", updateIdle},
		{"downloading", updateDownloading},
		{"installed", updateInstalled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testApp(t, false)
			a.update = &updater{phase: tc.phase, version: "0.2.0"}
			a.update.setProgress(2, 5)
			tt := ui.NewTester(a.view, 1200, 780)
			if err := tt.Click("关于"); err != nil {
				t.Fatal(err)
			}
			write(t, "update-"+tc.name+".png", tt.Image())
		})
	}
}

// The failure note wraps: an error can be a whole URL chain, and cutting it
// to one line hides the part that says what went wrong.
func TestUpdateFailureNoteWraps(t *testing.T) {
	a := testApp(t, false)
	long := strings.Repeat("无法连接到更新服务器，", 12) + "请稍后再试。"
	a.update = &updater{phase: updateFailed, err: long}
	tt := ui.NewTester(a.view, 1200, 780)
	if err := tt.Click("关于"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(long) {
		t.Fatalf("the long error was cut; texts %v", tt.Texts())
	}
	write(t, "update-failed.png", tt.Image())
}
