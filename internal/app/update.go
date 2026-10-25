// update.go is the manual update check: the one button in the about sheet
// that ever touches the network, and only while the user holds it there.
// Nothing checks on its own, and nothing is written down — the state lives
// in this struct and dies with the window.
package app

import (
	"context"
	"errors"
	"sync"

	"github.com/egoist/mygo"
)

// updatePhase is where a manual check stands. The button's label, colour and
// the progress bar beside it all follow it.
type updatePhase int

const (
	// updateIdle has never run, or found nothing newer: the button reads
	// 检查更新 again.
	updateIdle updatePhase = iota
	// updateChecking is the manifest request in flight.
	updateChecking
	// updateNone came back: the running build is the newest.
	updateNone
	// updateReady holds an update, downloading not started.
	updateReady
	// updateDownloading runs Install, whose progress fills the bar.
	updateDownloading
	// updateInstalled has replaced the app on disk; the button restarts
	// into it.
	updateInstalled
	// updateFailed says why in updateErr.
	updateFailed
)

// updater is the state of one check. A pointer on App, replaced wholesale by
// the goroutine that runs the check through applyUpdate; every read happens
// on the UI thread, so no lock is needed beyond the progress pair.
type updater struct {
	phase updatePhase
	// version is what was found, once Check returned an update.
	version string
	// up is the update Check returned; Install runs it.
	up *mygo.Update
	// done and total are the download's cursor, written by Install's
	// progress callback on its goroutine and read by the frame.
	done, total int64
	mu          sync.Mutex
	err         string
}

func (u *updater) setProgress(done, total int64) {
	u.mu.Lock()
	u.done, u.total = done, total
	u.mu.Unlock()
}

func (u *updater) progress() (done, total int64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.done, u.total
}

// checkUpdate and runInstallUpdate stand in for mygo.Updater.Check and
// Update.Install, which talk to the network and the install directory.
// Tests point them elsewhere.
var (
	checkUpdate      = func() (*mygo.Update, error) { return mygo.Updater.Check(context.Background()) }
	runInstallUpdate = func(up *mygo.Update, progress func(int64, int64)) error {
		return up.Install(context.Background(), progress)
	}
)

// checkForUpdate and installUpdate need the network; the goroutine that
// runs them goes through the same applyUpdate seam every other async
// update uses, which tests replace with a synchronous call.

// checkForUpdate starts the only network call the app ever makes, from the
// click on 检查更新. It returns immediately; the goroutine lands each
// outcome on the UI thread.
func (a *App) checkForUpdate() {
	if a.update.phase == updateChecking || a.update.phase == updateDownloading {
		return
	}
	a.update = &updater{phase: updateChecking}
	a.repaint()
	go func() {
		up, err := checkUpdate()
		a.applyUpdate(func() {
			switch {
			case err != nil && errors.Is(err, mygo.ErrUpdatesDisabled):
				// A build without updates has nothing to ask the network;
				// that is a fact about the build, not a failure.
				a.update = &updater{phase: updateIdle}
			case err != nil:
				a.update = &updater{phase: updateFailed, err: err.Error()}
			case up == nil:
				a.update = &updater{phase: updateNone}
			default:
				a.update = &updater{phase: updateReady, version: up.Version, up: up}
			}
			a.repaint()
		})
	}()
}

// installUpdate downloads and applies the found update, reporting bytes to
// the progress bar. When it ends, the app on disk is the new version.
func (a *App) installUpdate() {
	up := a.update.up
	if up == nil || a.update.phase != updateReady {
		return
	}
	a.update.phase = updateDownloading
	a.repaint()
	go func() {
		err := runInstallUpdate(up, a.update.setProgress)
		a.applyUpdate(func() {
			if err != nil {
				a.update = &updater{phase: updateFailed, err: err.Error()}
			} else {
				a.update = &updater{phase: updateInstalled, version: a.update.version}
			}
			a.repaint()
		})
	}()
}

// relaunchIntoUpdate quits and starts the new version, the user's second
// click after the download.
func (a *App) relaunchIntoUpdate() {
	mygo.App.Relaunch()
}
