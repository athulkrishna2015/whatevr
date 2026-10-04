package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/codelif/whatevr/platform"
	"whatevrd/internal/app"
)

const serviceLabel = platform.ID + ".daemon"

func xmlText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func servicePlist(binary, logDir, path string) []byte {
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string></array>
<key>RunAtLoad</key><true/>
<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
<key>ThrottleInterval</key><integer>5</integer>
<key>LimitLoadToSessionType</key><string>Aqua</string>
<key>EnvironmentVariables</key><dict><key>PATH</key><string>%s</string></dict>
<key>StandardOutPath</key><string>%s</string>
<key>StandardErrorPath</key><string>%s</string>
</dict></plist>
`, serviceLabel, xmlText(binary), xmlText(path), xmlText(filepath.Join(logDir, "service.log")), xmlText(filepath.Join(logDir, "service.log"))))
}
func runService(args []string, out, stderr io.Writer) int {
	if len(args) != 1 || (args[0] != "enable" && args[0] != "disable" && args[0] != "status") {
		fmt.Fprintln(stderr, "usage: whatevrd service enable|disable|status")
		return 2
	}
	h, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	target := domain + "/" + serviceLabel
	plist := filepath.Join(h, "Library", "LaunchAgents", serviceLabel+".plist")
	run := func(argv ...string) ([]byte, error) { return exec.Command("/bin/launchctl", argv...).CombinedOutput() }
	_, statusErr := run("print", target)
	switch args[0] {
	case "status":
		if statusErr != nil {
			fmt.Fprintln(out, "disabled")
			return 0
		}
		data, err := run("print", target)
		if err != nil {
			fmt.Fprintln(stderr, string(data))
			return 1
		}
		_, _ = out.Write(data)
		return 0
	case "disable":
		if statusErr == nil {
			if b, e := run("bootout", target); e != nil {
				fmt.Fprintln(stderr, string(b))
				return 1
			}
		}
		if err := os.Remove(plist); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(out, "login service disabled")
		return 0
	}
	if statusErr == nil {
		fmt.Fprintln(out, "login service already enabled")
		return 0
	}
	p, err := platform.Resolve()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// An explicit shell override cannot be silently lost by launchd.
	for _, name := range []string{platform.SocketEnv, "XDG_RUNTIME_DIR", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_CONFIG_HOME"} {
		if os.Getenv(name) != "" {
			fmt.Fprintf(stderr, "unset %s before enabling the native login service\n", name)
			return 1
		}
	}
	if conn, err := net.DialTimeout("unix", p.SocketPath, 200*time.Millisecond); err == nil {
		_ = conn.Close()
		fmt.Fprintln(stderr, "stop the manually running daemon before enabling the login service")
		return 1
	}
	lock := filepath.Join(filepath.Dir(p.SocketPath), "whatevrd.lock")
	if _, err := os.Stat(lock); err == nil {
		l, err := app.AcquireProcessLock(lock)
		if err != nil {
			fmt.Fprintln(stderr, "stop the manually running daemon before enabling the login service:", err)
			return 1
		}
		_ = l.Close()
	}
	binary, err := os.Executable()
	if err == nil {
		binary, err = filepath.EvalSymlinks(binary)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	prefix := filepath.Dir(filepath.Dir(binary))
	brew := "/usr/local/bin"
	if _, err := os.Stat("/opt/homebrew/bin/brew"); err == nil {
		brew = "/opt/homebrew/bin"
	}
	searchPath := filepath.Join(prefix, "bin") + ":" + brew + ":/usr/bin:/bin:/usr/sbin:/sbin"
	for _, dir := range []string{filepath.Dir(plist), p.LogDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	raw := servicePlist(binary, p.LogDir, searchPath)
	temp, err := os.CreateTemp(filepath.Dir(plist), ".whatevr-agent-*")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(raw); err == nil {
		err = temp.Close()
	} else {
		_ = temp.Close()
	}
	if err == nil {
		err = os.Rename(temp.Name(), plist)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if b, err := run("enable", target); err != nil {
		fmt.Fprintln(stderr, string(b))
		return 1
	}
	if b, err := run("bootstrap", domain, plist); err != nil {
		fmt.Fprintln(stderr, string(b))
		return 1
	}
	fmt.Fprintln(out, "login service enabled")
	return 0
}
