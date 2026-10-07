package conn

/*
#cgo CFLAGS: -fblocks
#cgo LDFLAGS: -framework Network -framework IOKit -framework CoreFoundation
#include <Network/Network.h>
#include <IOKit/pwr_mgt/IOPMLib.h>
#include <IOKit/IOMessage.h>
#include <dispatch/dispatch.h>
#include <stdint.h>
#include <stdlib.h>

// Native callbacks carry integer cgo handles, never pointers into the Go heap.
extern void nativeGoNetworkEvent(uintptr_t handle, int up);
extern void nativeGoWakeEvent(uintptr_t handle);
typedef struct {
 nw_path_monitor_t monitor;
 dispatch_queue_t queue;
 uintptr_t handle;
} network_watch;
static network_watch *network_start(uintptr_t handle) {
 network_watch *w = calloc(1, sizeof(*w));
 if (!w) return NULL;
 w->handle = handle;
 w->monitor = nw_path_monitor_create();
 w->queue = dispatch_queue_create("in.codelif.whatevr.network", DISPATCH_QUEUE_SERIAL);
 if (!w->monitor || !w->queue) {
  if (w->monitor) nw_release(w->monitor);
  if (w->queue) dispatch_release(w->queue);
  free(w); return NULL;
 }
 nw_path_monitor_set_queue(w->monitor, w->queue);
 nw_path_monitor_set_update_handler(w->monitor, ^(nw_path_t p) {
  nativeGoNetworkEvent(w->handle, nw_path_get_status(p) == nw_path_status_satisfied);
 });
 nw_path_monitor_start(w->monitor);
 return w;
}
static void network_stop(network_watch *w) {
 dispatch_semaphore_t done = dispatch_semaphore_create(0);
 nw_path_monitor_set_cancel_handler(w->monitor, ^{ dispatch_semaphore_signal(done); });
 nw_path_monitor_cancel(w->monitor);
 dispatch_semaphore_wait(done, DISPATCH_TIME_FOREVER);
 dispatch_sync(w->queue, ^{});
 nw_release(w->monitor);
 dispatch_release(w->queue);
 dispatch_release(done);
 free(w);
}
typedef struct {
 io_connect_t root;
 io_object_t notifier;
 IONotificationPortRef port;
 dispatch_queue_t queue;
 uintptr_t handle;
} sleep_watch;
static void power_changed(void *ref, io_service_t service, natural_t message, void *arg) {
 sleep_watch *w = ref;
 if (message == kIOMessageCanSystemSleep || message == kIOMessageSystemWillSleep)
  IOAllowPowerChange(w->root, (long)arg);
 else if (message == kIOMessageSystemHasPoweredOn) nativeGoWakeEvent(w->handle);
}
static sleep_watch *sleep_start(uintptr_t handle) {
 sleep_watch *w = calloc(1, sizeof(*w));
 if (!w) return NULL;
 w->handle = handle;
 w->root = IORegisterForSystemPower(w, &w->port, power_changed, &w->notifier);
 if (!w->root) { free(w); return NULL; }
 w->queue = dispatch_queue_create("in.codelif.whatevr.power", DISPATCH_QUEUE_SERIAL);
 IONotificationPortSetDispatchQueue(w->port, w->queue);
 return w;
}
static void sleep_stop(sleep_watch *w) {
 IODeregisterForSystemPower(&w->notifier);
 IONotificationPortDestroy(w->port);
 dispatch_sync(w->queue, ^{});
 IOServiceClose(w->root);
 dispatch_release(w->queue);
 free(w);
}
*/
import "C"

import (
	"context"
	"runtime/cgo"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

type darwinNetwork struct {
	up      atomic.Bool
	changes chan struct{}
	events  chan struct{}
	done    chan struct{}
}

func (n *darwinNetwork) Up() bool                 { return n.up.Load() }
func (n *darwinNetwork) Changes() <-chan struct{} { return n.changes }
func WatchNetwork(ctx context.Context, log zerolog.Logger) Network {
	n := &darwinNetwork{changes: make(chan struct{}, 1), events: make(chan struct{}, 1), done: make(chan struct{})}
	n.up.Store(true)
	handle := cgo.NewHandle(n)
	w := C.network_start(C.uintptr_t(handle))
	if w == nil {
		handle.Delete()
		close(n.done)
		log.Warn().Msg("conn: macOS path monitor unavailable")
		return n
	}
	go func() {
		defer func() { C.network_stop(w); handle.Delete(); close(n.done) }()
		var settle <-chan time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-n.events:
				if settle == nil {
					settle = time.After(500 * time.Millisecond)
				}
			case <-settle:
				settle = nil
				select {
				case n.changes <- struct{}{}:
				default:
				}
			}
		}
	}()
	return n
}
func WatchSleep(ctx context.Context, log zerolog.Logger, resumed func()) {
	events := make(chan struct{}, 1)
	handle := cgo.NewHandle(events)
	w := C.sleep_start(C.uintptr_t(handle))
	if w == nil {
		handle.Delete()
		log.Warn().Msg("conn: macOS wake monitor unavailable; using clock jumps")
		return
	}
	defer func() { C.sleep_stop(w); handle.Delete() }()
	for {
		select {
		case <-ctx.Done():
			return
		case <-events:
			log.Info().Msg("conn: resumed from sleep")
			resumed()
		}
	}
}
