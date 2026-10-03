//go:build linux

package conn

import (
	"os"
	"path/filepath"
	"testing"
)

func TestADefaultRouteIsUpAndARejectIsNot(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	head := "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n"
	if !defaultRoute4(write("v4up", head+"wlan0\t00000000\t0132A8C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n")) {
		t.Error("a default route via wlan0 is not up")
	}
	if defaultRoute4(write("v4lan", head+"wlan0\t0032A8C0\t00000000\t0001\t0\t0\t600\t00FFFFFF\t0\t0\t0\n")) {
		t.Error("a subnet route alone is up")
	}
	if defaultRoute6(write("v6reject", "00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 ffffffff 00000001 00000000 00200200       lo\n")) {
		t.Error("the unreachable default on lo is up")
	}
	if !defaultRoute6(write("v6up", "00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000400 00000001 00000000 00000003 wlan0\n")) {
		t.Error("a v6 default via wlan0 is not up")
	}
	if !defaultRoute4(filepath.Join(dir, "missing")) {
		t.Error("no route table should read as up")
	}
}
