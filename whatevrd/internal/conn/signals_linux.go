//go:build linux

package conn

import (
	"bufio"
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/rs/zerolog"
	"golang.org/x/sys/unix"
)

const (
	rtfUp     = 0x0001
	rtfReject = 0x0200
	// changes come in bursts (link, address, route), one kick covers a burst
	settleNetwork = 500 * time.Millisecond
)

type linuxNetwork struct {
	changes chan struct{}
}

// WatchNetwork is the route table over netlink: Up is a default route that
// is not a reject, Changes fires on any link, address or route change. with
// no netlink it still answers Up and never fires.
func WatchNetwork(ctx context.Context, log zerolog.Logger) Network {
	n := &linuxNetwork{changes: make(chan struct{}, 1)}
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_ROUTE)
	if err != nil {
		log.Warn().Err(err).Msg("conn: no netlink, network changes go unseen")
		return n
	}
	sa := &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: unix.RTMGRP_LINK |
		unix.RTMGRP_IPV4_IFADDR | unix.RTMGRP_IPV6_IFADDR | unix.RTMGRP_IPV4_ROUTE | unix.RTMGRP_IPV6_ROUTE}
	if err := unix.Bind(fd, sa); err != nil {
		unix.Close(fd)
		log.Warn().Err(err).Msg("conn: netlink bind, network changes go unseen")
		return n
	}
	// a timeout so the reader sees ctx end; close does not wake a blocked recv
	tv := unix.NsecToTimeval(time.Second.Nanoseconds())
	_ = unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv)
	go n.read(ctx, fd, log)
	return n
}

func (n *linuxNetwork) read(ctx context.Context, fd int, log zerolog.Logger) {
	defer unix.Close(fd)
	buf := make([]byte, 1<<16)
	var settle <-chan time.Time
	got := make(chan struct{}, 1)
	go func() {
		for ctx.Err() == nil {
			_, _, err := unix.Recvfrom(fd, buf, 0)
			if err == unix.EAGAIN || err == unix.EINTR {
				continue
			}
			if err != nil && err != unix.ENOBUFS {
				log.Warn().Err(err).Msg("conn: netlink read stopped")
				return
			}
			// ENOBUFS is changes lost to a full buffer: still a change
			select {
			case got <- struct{}{}:
			default:
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-got:
			if settle == nil {
				settle = time.After(settleNetwork)
			}
		case <-settle:
			settle = nil
			select {
			case n.changes <- struct{}{}:
			default:
			}
		}
	}
}

func (n *linuxNetwork) Changes() <-chan struct{} { return n.changes }

func (n *linuxNetwork) Up() bool {
	return defaultRoute4("/proc/net/route") || defaultRoute6("/proc/net/ipv6_route")
}

func defaultRoute4(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		// unknown is up: better a failed attempt than none
		return true
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Scan()
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 8 || fields[0] == "lo" || fields[1] != "00000000" || fields[7] != "00000000" {
			continue
		}
		if flags, err := strconv.ParseUint(fields[3], 16, 32); err == nil && flags&rtfUp != 0 && flags&rtfReject == 0 {
			return true
		}
	}
	return false
}

func defaultRoute6(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 10 || fields[9] == "lo" || fields[1] != "00" || strings.Trim(fields[0], "0") != "" {
			continue
		}
		if flags, err := strconv.ParseUint(fields[8], 16, 32); err == nil && flags&rtfUp != 0 && flags&rtfReject == 0 {
			return true
		}
	}
	return false
}

// WatchSleep calls resumed each time logind says the machine woke up. with no
// system bus it returns at once, the clock jump check covers resume then.
func WatchSleep(ctx context.Context, log zerolog.Logger, resumed func()) {
	bus, err := dbus.ConnectSystemBus()
	if err != nil {
		log.Info().Err(err).Msg("conn: no system bus, resume seen only as a clock jump")
		return
	}
	defer bus.Close()
	if err := bus.AddMatchSignal(
		dbus.WithMatchObjectPath("/org/freedesktop/login1"),
		dbus.WithMatchInterface("org.freedesktop.login1.Manager"),
		dbus.WithMatchMember("PrepareForSleep"),
	); err != nil {
		log.Info().Err(err).Msg("conn: logind match, resume seen only as a clock jump")
		return
	}
	signals := make(chan *dbus.Signal, 8)
	bus.Signal(signals)
	for {
		select {
		case <-ctx.Done():
			return
		case sig, ok := <-signals:
			if !ok {
				return
			}
			if len(sig.Body) == 1 {
				if sleeping, ok := sig.Body[0].(bool); ok && !sleeping {
					log.Info().Msg("conn: resumed from sleep")
					resumed()
				}
			}
		}
	}
}
