package main

import (
	"os"
	"syscall"
	"testing"
)

// TestStopSignalsIncludeWhatSystemdSends is the bug this guards.
//
// The watch loop trapped os.Interrupt alone. SIGINT is what Ctrl-C sends, and
// it is not what a service manager sends: `systemctl stop` and `systemctl
// restart` send SIGTERM. Under systemd the daemon therefore died by Go's
// default disposition -- a review in flight killed outright instead of
// cancelled, nothing in the journal saying it stopped, and on a restart a
// pull request possibly left with half its comments posted and no record
// that the run did not finish.
//
// Worth a test because the symptom appears only on the box, only on a stop,
// and looks like a crash rather than a missing signal.
func TestStopSignalsIncludeWhatSystemdSends(t *testing.T) {
	want := map[os.Signal]string{
		os.Interrupt:    "Ctrl-C, how an operator stops it by hand",
		syscall.SIGTERM: "systemctl stop and restart",
	}
	for sig, why := range want {
		found := false
		for _, got := range stopSignals {
			if got == sig {
				found = true
			}
		}
		if !found {
			t.Errorf("stopSignals must include %v (%s)", sig, why)
		}
	}
}
