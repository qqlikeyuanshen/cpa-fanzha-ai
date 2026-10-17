package main

/*
#include <stdint.h>
#include <stdlib.h>
typedef struct { void* ptr; size_t len; } cliproxy_buffer;
typedef int (*host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*free_fn)(void*, size_t);
typedef struct { uint32_t abi_version; void* host_ctx; host_call_fn call; free_fn free_buffer; } cliproxy_host_api;
typedef int (*plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*shutdown_fn)(void);
typedef struct { uint32_t abi_version; plugin_call_fn call; free_fn free_buffer; shutdown_fn shutdown; } cliproxy_plugin_api;
extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);
static const cliproxy_host_api* stored_host;
static void set_host(const cliproxy_host_api* h) { stored_host=h; }
static int host_call(const char* m,const uint8_t* p,size_t n,cliproxy_buffer* b) {
  if (!stored_host || !stored_host->call) return 1;
  return stored_host->call(stored_host->host_ctx,m,p,n,b);
}
static void host_free(void* p,size_t n) {
  if (stored_host && stored_host->free_buffer && p) stored_host->free_buffer(p,n);
}
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"unsafe"
)

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if host == nil || plugin == nil || host.abi_version != 1 {
		return 1
	}
	C.set_host(host)
	hostRPC = callHostC
	plugin.abi_version = 1
	plugin.call = C.plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, n C.size_t, response *C.cliproxy_buffer) (code C.int) {
	if response == nil {
		return 1
	}
	response.ptr = nil
	response.len = 0
	defer func() {
		if recover() != nil {
			setResponse(response, failure("插件内部异常", 500))
			code = 1
		}
	}()
	if method == nil || uint64(n) > 32<<20 {
		setResponse(response, failure("无效 RPC 请求", 400))
		return 1
	}
	var raw []byte
	if request != nil && n > 0 {
		raw = C.GoBytes(unsafe.Pointer(request), C.int(n))
	}
	out, err := dispatch(C.GoString(method), raw)
	if err != nil {
		setResponse(response, failure(err.Error(), statusOf(err)))
		return 1
	}
	setResponse(response, out)
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(p unsafe.Pointer, n C.size_t) {
	if p != nil {
		C.free(p)
	}
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() { shutdown() }

func setResponse(b *C.cliproxy_buffer, raw []byte) {
	if len(raw) > 0 {
		b.ptr = C.CBytes(raw)
		b.len = C.size_t(len(raw))
	}
}
func callHostC(method string, value any) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	m := C.CString(method)
	defer C.free(unsafe.Pointer(m))
	p := C.CBytes(raw)
	defer C.free(p)
	var b C.cliproxy_buffer
	rc := C.host_call(m, (*C.uint8_t)(p), C.size_t(len(raw)), &b)
	if b.ptr != nil {
		defer C.host_free(b.ptr, b.len)
	}
	if b.ptr == nil || uint64(b.len) > 32<<20 {
		return nil, fmt.Errorf("主机回调不可用")
	}
	var env envelope
	if err = json.Unmarshal(C.GoBytes(b.ptr, C.int(b.len)), &env); err != nil {
		return nil, fmt.Errorf("无效主机响应")
	}
	if !env.OK {
		if env.Error != nil {
			return nil, &statusError{env.Error.HTTPStatus, env.Error.Message}
		}
		return nil, fmt.Errorf("主机回调失败")
	}
	if rc != 0 {
		return nil, fmt.Errorf("主机回调失败")
	}
	return env.Result, nil
}
