package main

/*
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// glibc only; weak so another libc still links and skips them
extern int mallopt(int, int) __attribute__((weak));
extern int malloc_info(int, FILE *) __attribute__((weak));

// glibc opens a malloc arena per thread that allocates at once, up to eight
// per core, and each keeps its freed pages: 70 MB of a 200 MB daemon. go
// starts its threads before main and an arena is never closed, so the cap
// has to be in before go is. M_ARENA_MAX is -8.
__attribute__((constructor)) static void capArenas(void) {
	if (mallopt) mallopt(-8, 2);
}

static int arenas(void) {
	if (!malloc_info) return -1;
	char *buf;
	size_t n;
	FILE *f = open_memstream(&buf, &n);
	if (!f) return -1;
	malloc_info(0, f);
	fclose(f);
	int c = 0;
	for (char *p = buf; (p = strstr(p, "<heap nr=")); p++) c++;
	free(buf);
	return c;
}

static void churn(int n) {
	for (int i = 0; i < n; i++) free(malloc(1000 + i % 5000));
}
*/
import "C"

import (
	"os"
	"runtime/debug"
)

// mallocArenas is how many malloc arenas glibc has open, -1 off glibc.
func mallocArenas() int { return int(C.arenas()) }

// mallocChurn allocates and frees n blocks on the calling thread, for the test.
func mallocChurn(n int) { C.churn(C.int(n)) }

// goMemoryLimit is where the go heap starts collecting harder than every
// doubling, so a burst of garbage (a history chunk decoded) does not sit in
// memory until the next cycle. GOMEMLIMIT still overrides it.
const goMemoryLimit = 64 << 20

func limitMemory() {
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(goMemoryLimit)
	}
}
