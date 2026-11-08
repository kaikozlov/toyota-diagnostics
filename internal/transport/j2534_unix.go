//go:build (darwin || linux) && cgo

package transport

/*
#cgo linux LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>
static int32_t jcall(void *fn,int op,uintptr_t *a) {
 switch(op) {
 case 0: return ((int32_t(*)(void*,uint32_t*))fn)((void*)a[0],(uint32_t*)a[1]);
 case 1: case 3: return ((int32_t(*)(uint32_t))fn)((uint32_t)a[0]);
 case 2: return ((int32_t(*)(uint32_t,uint32_t,uint32_t,uint32_t,uint32_t*))fn)(a[0],a[1],a[2],a[3],(uint32_t*)a[4]);
 case 4: case 5: return ((int32_t(*)(uint32_t,void*,uint32_t*,uint32_t))fn)(a[0],(void*)a[1],(uint32_t*)a[2],a[3]);
 case 6: return ((int32_t(*)(uint32_t,uint32_t,void*,void*,void*,uint32_t*))fn)(a[0],a[1],(void*)a[2],(void*)a[3],(void*)a[4],(uint32_t*)a[5]);
 case 7: return ((int32_t(*)(uint32_t,uint32_t))fn)(a[0],a[1]);
 case 8: return ((int32_t(*)(uint32_t,char*,char*,char*))fn)(a[0],(char*)a[1],(char*)a[2],(char*)a[3]);
 case 9: return ((int32_t(*)(char*))fn)((char*)a[0]);
 }
 return -1;
}
*/
import "C"
import (
	"fmt"
	"runtime"
	"unsafe"
)

type nativeJ2534 struct {
	handle    unsafe.Pointer
	functions [10]unsafe.Pointer
}

func loadJ2534(path string) (jLibrary, error) {
	name := C.CString(path)
	defer C.free(unsafe.Pointer(name))
	handle := C.dlopen(name, C.RTLD_NOW|C.RTLD_LOCAL)
	if handle == nil {
		return nil, fmt.Errorf("J2534 library load %s: %s", path, C.GoString(C.dlerror()))
	}
	lib := &nativeJ2534{handle: handle}
	for i, export := range jExports {
		symbol := C.CString(export)
		lib.functions[i] = C.dlsym(handle, symbol)
		C.free(unsafe.Pointer(symbol))
		if lib.functions[i] == nil {
			lib.Close()
			return nil, fmt.Errorf("J2534 library missing required export %s", export)
		}
	}
	return lib, nil
}
func (lib *nativeJ2534) Call(op int, args ...any) int32 {
	var values [6]C.uintptr_t
	for i, arg := range args {
		values[i] = C.uintptr_t(jArgument(arg))
	}
	result := int32(C.jcall(lib.functions[op], C.int(op), &values[0]))
	runtime.KeepAlive(args)
	return result
}
func (lib *nativeJ2534) Close() error {
	if lib.handle == nil {
		return nil
	}
	rc := C.dlclose(lib.handle)
	lib.handle = nil
	if rc != 0 {
		return fmt.Errorf("J2534 library close: %s", C.GoString(C.dlerror()))
	}
	return nil
}
