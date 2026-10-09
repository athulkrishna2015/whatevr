//go:build cgo

package main

/*
extern int sqlite3_config(int, ...);
// sqlite3_config is variadic, cgo cannot call it as is
static int memstatusOff(void) { return sqlite3_config(9, 0); }
*/
import "C"

// memstatusOff stops sqlite counting memory, which takes one global lock on
// every allocation of every connection. nothing reads the counts. it has to
// run before the first database opens.
func memstatusOff() int { return int(C.memstatusOff()) }
