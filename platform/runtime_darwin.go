package platform

/*
#include <unistd.h>
#include <stdlib.h>
static char *user_temp(void) {
 size_t size = confstr(_CS_DARWIN_USER_TEMP_DIR, NULL, 0);
 if (!size) return NULL;
 char *p = malloc(size);
 if (p && !confstr(_CS_DARWIN_USER_TEMP_DIR, p, size)) { free(p); return NULL; }
 return p;
}
*/
import "C"
import (
	"errors"
	"unsafe"
)

func nativeRuntimeDir() (string, error) {
	p := C.user_temp()
	if p == nil {
		return "", errors.New("cannot resolve macOS per-user temporary directory")
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p), nil
}
