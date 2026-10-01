//go:build wasip1

package client

import "unsafe"

// The host interface, module "concord". Every message is JSON.
//
//	next_event(buf, cap) -> n   blocks for the next event and copies it into
//	                            buf. n > cap: call again with n bytes of room.
//	                            n < 0: stop (return from main).
//	call(req, len) -> n         a host call; n < 0 is an error code, n >= 0 the
//	                            length of its result.
//	result(buf, cap) -> n       copies the last call's result into buf.

//go:wasmimport concord next_event
func hostNextEvent(buf unsafe.Pointer, size uint32) int32

//go:wasmimport concord call
func hostCall(req unsafe.Pointer, size uint32) int32

//go:wasmimport concord result
func hostResult(buf unsafe.Pointer, size uint32) int32

type wasmTransport struct{ buf []byte }

func hostTransport() Transport { return &wasmTransport{buf: make([]byte, 4096)} }

func (t *wasmTransport) NextEvent() ([]byte, bool) {
	for {
		n := hostNextEvent(unsafe.Pointer(&t.buf[0]), uint32(len(t.buf)))
		if n < 0 {
			return nil, false
		}
		if int(n) > len(t.buf) {
			t.buf = make([]byte, n)
			continue
		}
		out := make([]byte, n)
		copy(out, t.buf[:n])
		return out, true
	}
}

func (t *wasmTransport) Call(req []byte) ([]byte, int32) {
	if len(req) == 0 {
		return nil, -1
	}
	n := hostCall(unsafe.Pointer(&req[0]), uint32(len(req)))
	if n <= 0 {
		return nil, n
	}
	out := make([]byte, n)
	hostResult(unsafe.Pointer(&out[0]), uint32(n))
	return out, 0
}
