package main

/*
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// glibc allocator tuning is unavailable on other libcs.
#ifdef __GLIBC__
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

#else
static int arenas(void) { return -1; }
#endif

static void churn(int n) {
	for (int i = 0; i < n; i++) free(malloc(1000 + i % 5000));
}
*/
import "C"

// mallocArenas reports -1 when the native allocator is not glibc.
func mallocArenas() int { return int(C.arenas()) }
func mallocChurn(n int) { C.churn(C.int(n)) }
