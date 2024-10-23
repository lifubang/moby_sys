// Copyright 2023 The Capability Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package capability_test

import (
	"log"
	"os"
	"os/exec"
	"runtime"
	"testing"

	. "github.com/moby/sys/capability"
)

// Based on the fact Go 1.18+ supports Linux >= 2.6.32, and
//   - CAP_MAC_ADMIN (33) was added in 2.6.25;
//   - CAP_SYSLOG (34) was added in 2.6.38;
//   - CAP_CHECKPOINT_RESTORE (40) was added in 5.9, and it is
//     the last added capability as of today (July 2024);
//
// LastCap return value should be between minLastCap and maxLastCap.
const (
	minLastCap = CAP_MAC_ADMIN
	maxLastCap = CAP_CHECKPOINT_RESTORE
)

func requirePCapSet(t testing.TB) {
	t.Helper()
	pid, err := NewPid2(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := pid.Load(); err != nil {
		t.Fatal(err)
	}
	if !pid.Get(EFFECTIVE, CAP_SETPCAP) {
		t.Skip("The test needs `CAP_SETPCAP`.")
	}
}

// testInChild runs fn as a separate process, and returns its output.
// This is useful for tests which manipulate capabilties, allowing to
// preserve those of the main test process.
//
// The fn is a function which must end with os.Exit. In case exit code
// is non-zero, t.Fatal is called.
func testInChild(t *testing.T, fn func()) []byte {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		fn()
	}

	// Re-exec the current test as a new process.
	args := []string{"-test.run=^" + t.Name() + "$"}
	if testing.Verbose() {
		args = append(args, "-test.v")
	}
	cmd := exec.Command("/proc/self/exe", args...)
	cmd.Env = append(cmd.Environ(), "GO_WANT_HELPER_PROCESS=1")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Helper()
		t.Fatalf("exec failed: %v\n\n%s\n", err, out)
	}

	return out
}

func TestLastCap(t *testing.T) {
	last, err := LastCap()
	switch runtime.GOOS {
	default:
		if err == nil {
			t.Fatal(runtime.GOOS, ": want error, got nil")
		}
		return
	case "linux":
	}
	if err != nil {
		t.Fatalf("LastCap: want nil, got error: %v", err)
	}
	// Sanity checks.
	if last < minLastCap {
		t.Errorf("LastCap: want >= %d (%s), got %d (%s)",
			last, last, minLastCap, minLastCap)
	}
	if last > maxLastCap {
		t.Errorf("LastCap: want <= %d (%s), got %d (%s). Package needs to be updated.",
			last, last, maxLastCap, maxLastCap)
	}
}

func TestListSupported(t *testing.T) {
	list, err := ListSupported()
	switch runtime.GOOS {
	default:
		if err == nil {
			t.Fatal(runtime.GOOS, ": want error, got nil")
		}
		return
	case "linux":
	}
	if err != nil {
		t.Fatalf("ListSupported: want nil, got error: %v", err)
	}
	t.Logf("got +%v (len %d)", list, len(list))
	minLen := int(minLastCap) + 1
	if len(list) < minLen {
		t.Errorf("ListSupported: too short (want %d, got %d): +%v", minLen, len(list), list)
	}
}

func TestNewPid2Load(t *testing.T) {
	c, err := NewPid2(0)
	switch runtime.GOOS {
	default:
		if err == nil {
			t.Fatal(runtime.GOOS, ": want error, got nil")
		}
		return
	case "linux":
	}
	if err != nil {
		t.Fatalf("NewPid2: want nil, got error: %v", err)
	}
	err = c.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Assuming that at least bounding set is not empty.
	bset := c.StringCap(BOUNDING)
	t.Logf("Bounding set: %s", bset)
	if len(bset) == 0 {
		t.Fatal("loaded bounding set: want non-empty, got empty")
	}
}

func TestApplyCaps(t *testing.T) {
	if runtime.GOOS != "linux" {
		return
	}
	requirePCapSet(t)

	out := testInChild(t, childAmbientCapSet)

	t.Logf("output from child:\n%s", out)
}

func childAmbientCapSet() {
	// We can't use t.Log etc. here, yet filename and line number is nice
	// to have. Set up and use the standard logger for this.
	log.SetFlags(log.Lshortfile)

	pid, err := NewPid2(0)
	if err != nil {
		log.Fatal(err)
	}

	list := []Cap{CAP_KILL, CAP_CHOWN, CAP_SYS_CHROOT}
	pid.Set(CAPS|AMBIENT, list...)
	if err = pid.Apply(CAPS | AMBIENT); err != nil {
		log.Fatal(err)
	}

	// Check if ambient caps were applied.
	if err = pid.Load(); err != nil {
		log.Fatal(err)
	}
	for _, cap := range list {
		want := true
		if got := pid.Get(AMBIENT, cap); want != got {
			log.Fatalf("Get(AMBIENT, %s): want %v, got %v", cap, want, got)
		}
	}

	// Unset a single ambient cap, to check `PR_CAP_AMBIENT_CLEAR_ALL` work.
	const unsetIdx = 1
	pid.Unset(AMBIENT, list[unsetIdx])
	if err = pid.Apply(AMBIENT); err != nil {
		log.Fatal(err)
	}

	if err = pid.Load(); err != nil {
		log.Fatal(err)
	}
	for i, cap := range list {
		want := i != unsetIdx
		if got := pid.Get(AMBIENT, cap); want != got {
			log.Fatalf("Get(AMBIENT, %s): want %v, got %v", cap, want, got)
		}
	}
	os.Exit(0)
}

func TestAmbientCapAPI(t *testing.T) {
	if runtime.GOOS != "linux" {
		return
	}
	requirePCapSet(t)

	out := testInChild(t, childAmbientCapSetByAPI)

	t.Logf("output from child:\n%s", out)
}

func childAmbientCapSetByAPI() {
	log.SetFlags(log.Lshortfile)

	pid, err := NewPid2(0)
	if err != nil {
		log.Fatal(err)
	}

	list := []Cap{CAP_KILL, CAP_CHOWN, CAP_SYS_CHROOT}
	pid.Set(CAPS|AMBIENT, list...)
	if err = pid.Apply(CAPS); err != nil {
		log.Fatal(err)
	}
	if err = AmbientRaise(list...); err != nil {
		log.Fatal(err)
	}

	// Check if ambient caps were raised.
	if err = pid.Load(); err != nil {
		log.Fatal(err)
	}
	for _, cap := range list {
		want := true
		if got := pid.Get(AMBIENT, cap); want != got {
			log.Fatalf("Get(AMBIENT, %s): want %v, got %v", cap, want, got)
		}
	}

	// Lower one ambient cap.
	const unsetIdx = 1
	if err = AmbientLower(list[unsetIdx]); err != nil {
		log.Fatal(err)
	}
	if err = pid.Load(); err != nil {
		log.Fatal(err)
	}
	for i, cap := range list {
		want := i != unsetIdx
		if got := pid.Get(AMBIENT, cap); want != got {
			log.Fatalf("Get(AMBIENT, %s): want %v, got %v", cap, want, got)
		}
	}

	// Lower all ambient caps
	if err = AmbientClearAll(); err != nil {
		log.Fatal(err)
	}
	if err = pid.Load(); err != nil {
		log.Fatal(err)
	}
	for _, cap := range list {
		if got := pid.Get(AMBIENT, cap); got {
			log.Fatalf("Get(AMBIENT, %s): want false, got %v", cap, got)
		}
	}
	os.Exit(0)
}
