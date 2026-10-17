#!/usr/bin/env python3
"""Offline C ABI smoke test. No production configuration, credentials or network requests."""
import base64
import ctypes as C
import json
from pathlib import Path
import sys

class Buffer(C.Structure):
    _fields_ = [('ptr', C.c_void_p), ('len', C.c_size_t)]

HostCall = C.CFUNCTYPE(C.c_int, C.c_void_p, C.c_char_p, C.POINTER(C.c_uint8), C.c_size_t, C.POINTER(Buffer))
Free = C.CFUNCTYPE(None, C.c_void_p, C.c_size_t)
PluginCall = C.CFUNCTYPE(C.c_int, C.c_char_p, C.POINTER(C.c_uint8), C.c_size_t, C.POINTER(Buffer))
Shutdown = C.CFUNCTYPE(None)

class Host(C.Structure):
    _fields_ = [('abi_version', C.c_uint32), ('ctx', C.c_void_p), ('call', HostCall), ('free', Free)]

class Plugin(C.Structure):
    _fields_ = [('abi_version', C.c_uint32), ('call', PluginCall), ('free', Free), ('shutdown', Shutdown)]

host_calls = []

@HostCall
def reject_host_call(ctx, method, request, size, response):
    host_calls.append(method.decode())
    response.contents.ptr = None
    response.contents.len = 0
    return 1

@Free
def no_host_buffer(ptr, size):
    pass

def check(condition, message):
    if not condition:
        raise RuntimeError(message)

def main():
    if len(sys.argv) != 2:
        raise SystemExit('Usage: python3 scripts/check-abi.py dist/fanzha-vVERSION-linux-amd64.so')
    library = C.CDLL(str(Path(sys.argv[1]).resolve(strict=True)))
    host = Host(1, None, reject_host_call, no_host_buffer)
    plugin = Plugin()
    library.cliproxy_plugin_init.argtypes = [C.POINTER(Host), C.POINTER(Plugin)]
    library.cliproxy_plugin_init.restype = C.c_int
    check(library.cliproxy_plugin_init(C.byref(host), C.byref(plugin)) == 0, 'ABI initialization failed')
    check(plugin.abi_version == 1, 'Wrong ABI version')

    def rpc(method, payload, error_status=None):
        raw = json.dumps(payload).encode()
        request = (C.c_uint8 * len(raw)).from_buffer_copy(raw)
        response = Buffer()
        rc = plugin.call(method.encode(), request, len(raw), C.byref(response))
        check(bool(response.ptr), 'Empty RPC response')
        try:
            env = json.loads(C.string_at(response.ptr, response.len))
        finally:
            plugin.free(response.ptr, response.len)
        if error_status is not None:
            check(rc != 0 and not env.get('ok'), 'Expected an RPC error')
            check(env.get('error', {}).get('http_status') == error_status, 'Wrong RPC error status')
            return env
        check(rc == 0 and env.get('ok'), 'RPC failed: ' + method)
        return env['result']

    registration = rpc('plugin.register', {})
    check(registration['schema_version'] == 6, 'Wrong RPC schema')
    meta = registration['metadata']
    check(meta['GitHubRepository'] == 'https://github.com/qqlikeyuanshen/cpa-fanzha-ai', 'Wrong repository metadata')
    check(registration['capabilities']['executor_model_scope'] == 'oauth', 'Wrong credential scope')
    models = rpc('model.static', {})['Models']
    check({m['ID'] for m in models} == {'fanzha-ai', 'fanzha-ai-deep', 'fanzha-ai-en', 'fanzha-ai-en-deep'}, 'Wrong models')
    credential = json.dumps({'type': 'fanzha', 'access_token': 'mock-access', 'refresh_token': 'mock-refresh'}).encode()
    parsed = rpc('auth.parse', {'FileName': 'fanzha-mock.json', 'RawJSON': base64.b64encode(credential).decode()})
    check(parsed['Handled'] and parsed['Auth']['ID'] == 'fanzha-mock.json', 'Credential identity changed')
    for asset, content_type in [('login', 'text/html'), ('quota', 'text/html'), ('logo.png', 'image/png')]:
        result = rpc('management.handle', {'Method': 'GET', 'Path': '/v0/resource/plugins/fanzha/' + asset})
        check(result['StatusCode'] == 200, 'Static asset unavailable: ' + asset)
        check(result['Headers']['Content-Type'][0].startswith(content_type), 'Wrong content type')
        check(bool(base64.b64decode(result['Body'])), 'Empty static asset')
    rpc('executor.count_tokens', {}, error_status=501)
    rpc('quota.reset', {}, error_status=501)
    check(not host_calls, 'Offline test unexpectedly requested host data')
    plugin.shutdown()
    print('PASS: C ABI 1, RPC schema 6, metadata, models, mock auth, static assets, error mapping and shutdown')
    print('No real credentials or upstream requests used.')

if __name__ == '__main__':
    main()
