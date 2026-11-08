//go:build windows

package transport

import (
	"fmt"
	"runtime"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

type nativeJ2534 struct {
	dll       *syscall.DLL
	functions [10]*syscall.Proc
}

func loadJ2534(path string) (jLibrary, error) {
	dll, err := syscall.LoadDLL(path)
	if err != nil {
		return nil, fmt.Errorf("J2534 library load %s (process and DLL architectures must match): %w", path, err)
	}
	lib := &nativeJ2534{dll: dll}
	for i, name := range jExports {
		proc, err := dll.FindProc(name)
		if err != nil {
			dll.Release()
			return nil, fmt.Errorf("J2534 library missing required export %s: %w", name, err)
		}
		lib.functions[i] = proc
	}
	return lib, nil
}
func (lib *nativeJ2534) Call(op int, args ...any) int32 {
	values := make([]uintptr, len(args))
	for i, arg := range args {
		values[i] = jArgument(arg)
	}
	result, _, _ := lib.functions[op].Call(values...)
	runtime.KeepAlive(args)
	return int32(result)
}
func (lib *nativeJ2534) Close() error {
	if lib.dll == nil {
		return nil
	}
	err := lib.dll.Release()
	lib.dll = nil
	return err
}
func jSystemProviders() []J2534Provider {
	var rows []J2534Provider
	for _, view := range []uint32{0, registry.WOW64_32KEY} {
		root, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\PassThruSupport.04.04`, registry.READ|view)
		if err != nil {
			continue
		}
		names, _ := root.ReadSubKeyNames(-1)
		for _, name := range names {
			key, err := registry.OpenKey(root, name, registry.READ|view)
			if err != nil {
				continue
			}
			library, _, err := key.GetStringValue("FunctionLibrary")
			label, _, _ := key.GetStringValue("Name")
			key.Close()
			if err != nil || library == "" {
				continue
			}
			if label == "" {
				label = name
			}
			rows = append(rows, J2534Provider{label, library, "windows-registry"})
		}
		root.Close()
	}
	return rows
}
