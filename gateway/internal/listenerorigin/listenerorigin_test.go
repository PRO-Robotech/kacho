// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listenerorigin_test

import (
	"context"
	"net"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

// TestIsExternal_DefaultExternal — a bare context (no marker) is EXTERNAL origin
// (fail-closed). This is the inverted default: any listener that does not
// explicitly mark its connections internal is treated as the untrusted edge.
func TestIsExternal_DefaultExternal(t *testing.T) {
	if !listenerorigin.IsExternal(context.Background()) {
		t.Fatal("bare context must be EXTERNAL origin (IsExternal=true, fail-closed)")
	}
	if !listenerorigin.IsExternal(nil) { //nolint:staticcheck // intentionally exercises nil-context handling
		t.Fatal("nil context must be EXTERNAL origin (IsExternal=true, fail-closed)")
	}
}

// TestWithInternal_Marks — WithInternal flips the marker to internal.
func TestWithInternal_Marks(t *testing.T) {
	ctx := listenerorigin.WithInternal(context.Background())
	if listenerorigin.IsExternal(ctx) {
		t.Fatal("WithInternal context must report IsExternal=false")
	}
}

// fakeConn is a minimal net.Conn for ConnContext tests. It simulates a
// connection accepted on the plaintext/ingress-facing listener (NOT wrapped by
// InternalListener), which must stay external.
type fakeConn struct{ net.Conn }

// tlsLikeConn mimics crypto/tls.Conn's NetConn() unwrap so ConnContext
// can see through TLS/cmux to the wrapped internal listener conn.
type tlsLikeConn struct {
	net.Conn
	inner net.Conn
}

func (c tlsLikeConn) NetConn() net.Conn { return c.inner }

// TestConnContext_TagsInternalListenerConn — a conn accepted via
// InternalListener is tagged internal, even through a TLS/cmux-like wrapper.
func TestConnContext_TagsInternalListenerConn(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	in := listenerorigin.InternalListener(ln)

	done := make(chan net.Conn, 1)
	go func() {
		c, aerr := in.Accept()
		if aerr != nil {
			done <- nil
			return
		}
		done <- c
	}()
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	srvConn := <-done
	if srvConn == nil {
		t.Fatal("accept failed")
	}
	defer srvConn.Close()

	// Direct internal conn → tagged internal (IsExternal=false).
	ctx := listenerorigin.ConnContext(context.Background(), srvConn)
	if listenerorigin.IsExternal(ctx) {
		t.Fatal("conn from InternalListener must be tagged internal (IsExternal=false)")
	}

	// Through a TLS/cmux-like wrapper (NetConn unwraps to the internal conn) → tagged.
	wrapped := tlsLikeConn{inner: srvConn}
	ctx2 := listenerorigin.ConnContext(context.Background(), wrapped)
	if listenerorigin.IsExternal(ctx2) {
		t.Fatal("wrapped internal conn must be tagged internal (NetConn unwrap)")
	}
}

// TestConnContext_DoesNotTagPlainConn — a conn NOT from the internal
// listener (the plaintext/ingress-facing listener) stays EXTERNAL origin. This
// is the core fail-closed guarantee: the ingress-facing listener is external.
func TestConnContext_DoesNotTagPlainConn(t *testing.T) {
	ctx := listenerorigin.ConnContext(context.Background(), fakeConn{})
	if !listenerorigin.IsExternal(ctx) {
		t.Fatal("non-internal conn (ingress-facing listener) must stay EXTERNAL (IsExternal=true)")
	}
}

// stubListener hands out one conn per Accept; ConnContext reads only the type
// chain of the conn, so a stub conn is enough.
type stubListener struct {
	net.Listener
	conn net.Conn
}

func (l stubListener) Accept() (net.Conn, error) { return l.conn, nil }

func acceptVia(t *testing.T, ln net.Listener) net.Conn {
	t.Helper()
	c, err := ln.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	return c
}

// TestOnExternalListener_DefaultIsNotExternal — the external-listener mark has
// the opposite default of IsExternal: a bare or nil context is NOT on an
// external listener. Records that answer on external listeners only refuse such
// a request.
func TestOnExternalListener_DefaultIsNotExternal(t *testing.T) {
	if listenerorigin.OnExternalListener(context.Background()) {
		t.Fatal("bare context must NOT be on an external listener (OnExternalListener=false)")
	}
	if listenerorigin.OnExternalListener(nil) { //nolint:staticcheck // intentionally exercises nil-context handling
		t.Fatal("nil context must NOT be on an external listener (OnExternalListener=false)")
	}
	if listenerorigin.OnExternalListener(listenerorigin.ConnContext(context.Background(), fakeConn{})) {
		t.Fatal("conn from an unwrapped listener must NOT be on an external listener")
	}
	if listenerorigin.OnExternalListener(listenerorigin.WithInternal(context.Background())) {
		t.Fatal("internal-marked context must NOT be on an external listener")
	}
}

// TestConnContext_TagsExternalListenerConn — a conn accepted via
// ExternalListener is on an external listener, directly and through a
// TLS-like wrapper; it stays external for IsExternal too.
func TestConnContext_TagsExternalListenerConn(t *testing.T) {
	conn := acceptVia(t, listenerorigin.ExternalListener(stubListener{conn: fakeConn{}}))
	for name, c := range map[string]net.Conn{"direct": conn, "tls-like wrap": tlsLikeConn{inner: conn}} {
		ctx := listenerorigin.ConnContext(context.Background(), c)
		if !listenerorigin.OnExternalListener(ctx) {
			t.Errorf("%s: conn from ExternalListener must be on an external listener", name)
		}
		if !listenerorigin.IsExternal(ctx) {
			t.Errorf("%s: conn from ExternalListener must stay external for IsExternal", name)
		}
	}
}

// TestConnContext_BothWrappersMarkNeither — a conn wrapped by BOTH wrappers is
// a wiring error. Neither mark is set, so each reader gets its own refusal:
// IsExternal=true (Internal* 404) and OnExternalListener=false (external-only
// records refused).
func TestConnContext_BothWrappersMarkNeither(t *testing.T) {
	inner := listenerorigin.ExternalListener(stubListener{conn: fakeConn{}})
	for name, ln := range map[string]net.Listener{
		"internal over external": listenerorigin.InternalListener(inner),
		"external over internal": listenerorigin.ExternalListener(listenerorigin.InternalListener(stubListener{conn: fakeConn{}})),
	} {
		ctx := listenerorigin.ConnContext(context.Background(), acceptVia(t, ln))
		if !listenerorigin.IsExternal(ctx) {
			t.Errorf("%s: IsExternal must stay true (Internal* refused)", name)
		}
		if listenerorigin.OnExternalListener(ctx) {
			t.Errorf("%s: OnExternalListener must stay false (external-only records refused)", name)
		}
	}
}
