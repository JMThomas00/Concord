//go:build !wasip1

package client

// Outside WebAssembly there's no Concord to talk to: Run returns at once
// and calls fail with ErrUnavailable, unless a test sets a Transport
// (see clienttest).

type noTransport struct{}

func hostTransport() Transport { return noTransport{} }

func (noTransport) NextEvent() ([]byte, bool)   { return nil, false }
func (noTransport) Call([]byte) ([]byte, int32) { return nil, -6 }
