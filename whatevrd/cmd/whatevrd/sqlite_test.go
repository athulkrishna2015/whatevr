package main

import (
	"os"
	"os/exec"
	"testing"
)

// in a process of its own: once any database has opened, sqlite refuses
// configuration, and other tests here open them
func TestMemstatusOffTakes(t *testing.T) {
	if os.Getenv("WHATEVR_MEMSTATUS_CHILD") != "" {
		if rc := memstatusOff(); rc != 0 {
			t.Fatalf("sqlite3_config returned %d", rc)
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMemstatusOffTakes$", "-test.count=1")
	cmd.Env = append(os.Environ(), "WHATEVR_MEMSTATUS_CHILD=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
