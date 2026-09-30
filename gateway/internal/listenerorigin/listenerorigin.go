// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package listenerorigin carries the listener-origin of an inbound request
// (external edge vs cluster-internal admin listener) through the request
// context, so downstream HTTP handlers / middleware can enforce
// Internal-vs-external isolation: Internal* methods must never be reachable from
// the external edge.
//
// # Fail-closed origin model (inverted 2026-07-05, sec-hardening-r3)
//
// The DEFAULT origin is EXTERNAL. A connection is treated as cluster-internal
// ONLY when it arrives on a listener explicitly wrapped with InternalListener.
// This inverts the earlier opt-in-external model, which marked ONLY the TLS
// listener as external and left every other listener (including the plaintext
// cmux listener the shipped ingress actually targets) unmarked → treated as
// trusted-internal → Internal* REST served to real external callers.
//
// Why the inversion matters: the production ingress terminates external TLS and
// forwards plaintext gRPC/HTTP to the pod's plaintext cmux listener (Service
// port `cmux` → :8080), NOT the pod's own TLS listener. Under the old model
// IsExternal(ctx) was false for that ingress-facing listener, so the REST
// dispatcher's Internal-path 404 gate never fired and Internal* REST
// (/vpc/v1/addressPools, `:internal` infra-sensitive projections,
// InternalRegistry/Cluster/Operations admin) was reachable from the edge.
//
// Under the fail-closed model EVERY listener is external unless it is the
// dedicated cluster-internal admin REST listener
// (KACHO_API_GATEWAY_INTERNAL_REST_ADDR, wrapped with InternalListener and never
// targeted by the ingress). So:
//
//   - plaintext cmux listener (ingress-facing) → ExternalListener → external → Internal* 404
//   - external TLS listener                    → ExternalListener → external → Internal* 404
//   - dedicated internal admin REST listener   → InternalListener → internal → served
//   - any listener without a wrapper           → unmarked → external → Internal* 404
//
// # How the marker propagates
//
// api-gateway serves the SAME *http.Server (and the SAME REST dispatcher) on
// every HTTP listener. The shared http.Server.ConnContext hook (ConnContext)
// inspects the accepted net.Conn: a connection whose listener was wrapped by
// InternalListener is tagged internal; any other connection is left without the
// internal tag (external — the fail-closed default). The REST dispatcher then
// 404s Internal* paths whenever IsExternal(ctx) is true, which is every listener
// except the marked internal admin one.
//
// # The second reader: records that answer on external listeners only
//
// Some records must answer ONLY on the external listeners (the authorization
// ceremony coordinates). For them the restricted direction is the opposite one:
// a missing marker must refuse, not serve. IsExternal cannot say that — its
// default is external. Such records read OnExternalListener, whose default is
// FALSE: it is true only when the connection was accepted on a listener wrapped
// by ExternalListener. The composition root wraps both external HTTP listeners
// (plaintext cmux and TLS). A listener that lost its wrapper serves neither
// Internal* paths nor external-only records.
//
// A connection wrapped by BOTH wrappers is a wiring error; ConnContext sets no
// marker for it, so each reader falls back to its own refusal.
package listenerorigin

import (
	"context"
	"net"
)

// originKey is the unexported context key carrying the listener-origin marker.
// A dedicated empty-struct type avoids collisions with other packages' context
// keys (Go idiom).
type originKey struct{}

// markInternal is the sentinel stored under originKey for connections accepted
// on the dedicated cluster-internal admin listener. Absence of the key (the
// default) means EXTERNAL origin — fail-closed: any request whose origin was NOT
// explicitly marked internal is treated as external (the untrusted edge), so a
// missing marker never accidentally exposes an Internal* path. The INTERNAL side
// is the one that must opt in to the marker, and the internal-listener wiring is
// the single place that does.
type markInternal struct{}

// IsExternal reports whether the request must be treated as arriving from the
// external edge. It returns true UNLESS the connection was accepted on a
// listener wrapped by InternalListener (fail-closed default). A nil context is
// treated as external.
func IsExternal(ctx context.Context) bool {
	if ctx == nil {
		return true
	}
	_, ok := ctx.Value(originKey{}).(markInternal)
	return !ok
}

// WithInternal returns a copy of ctx tagged as cluster-internal-listener origin.
// Exposed for tests; production marks origin via the listener wrapper +
// ConnContext.
func WithInternal(ctx context.Context) context.Context {
	return context.WithValue(ctx, originKey{}, markInternal{})
}

// internalConn wraps a net.Conn accepted on the cluster-internal admin listener
// so that ConnContext can recognise it and tag the request context.
type internalConn struct {
	net.Conn
}

// InternalListener wraps lis so every connection it accepts is tagged as
// cluster-internal origin (recognisable by ConnContext). Wrap ONLY the
// dedicated cluster-internal admin REST listener; the plaintext cmux listener
// and the external TLS listener carry ExternalListener instead.
func InternalListener(lis net.Listener) net.Listener {
	return &internalListener{Listener: lis}
}

type internalListener struct {
	net.Listener
}

func (l *internalListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &internalConn{Conn: c}, nil
}

// markExternal is the sentinel stored under originKey for connections accepted
// on a listener wrapped by ExternalListener. It is the only thing that makes
// OnExternalListener true.
type markExternal struct{}

// OnExternalListener reports whether the connection was accepted on a listener
// wrapped by ExternalListener. Its default is the OPPOSITE of IsExternal: a nil
// context, an unmarked connection and an internal-marked connection all report
// false. Records that answer on external listeners only refuse unless it is
// true.
func OnExternalListener(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	_, ok := ctx.Value(originKey{}).(markExternal)
	return ok
}

// externalConn wraps a net.Conn accepted on an external listener so that
// ConnContext can recognise it and tag the request context.
type externalConn struct {
	net.Conn
}

// ExternalListener wraps lis so every connection it accepts is tagged as
// accepted on an external listener (recognisable by ConnContext). Wrap the
// external HTTP listeners the ingress and external TLS clients reach; never the
// dedicated cluster-internal admin REST listener.
func ExternalListener(lis net.Listener) net.Listener {
	return &externalListener{Listener: lis}
}

type externalListener struct {
	net.Listener
}

func (l *externalListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &externalConn{Conn: c}, nil
}

// ConnContext is an http.Server.ConnContext hook. A connection accepted on a
// listener wrapped by InternalListener is tagged internal; one accepted on a
// listener wrapped by ExternalListener is tagged external; any other connection
// passes through unmarked. A connection carrying both wrappers passes through
// unmarked too: IsExternal then reports true and OnExternalListener false.
//
// The same http.Server serves every HTTP listener; ConnContext is invoked per
// connection with the concrete net.Conn, so this is the one place that can tell
// the listeners apart without splitting into multiple http.Server instances.
func ConnContext(ctx context.Context, c net.Conn) context.Context {
	internal, external := connOrigin(c)
	switch {
	case internal && external:
		return ctx
	case internal:
		return WithInternal(ctx)
	case external:
		return context.WithValue(ctx, originKey{}, markExternal{})
	}
	return ctx
}

// connOrigin unwraps the conn chain (crypto/tls and similar expose the
// underlying conn via NetConn) and reports which wrappers it passes.
func connOrigin(c net.Conn) (internal, external bool) {
	type wrappedConn interface {
		NetConn() net.Conn
	}
	for c != nil {
		switch w := c.(type) {
		case *internalConn:
			internal = true
			c = w.Conn
		case *externalConn:
			external = true
			c = w.Conn
		case wrappedConn:
			c = w.NetConn()
		default:
			return internal, external
		}
	}
	return internal, external
}
