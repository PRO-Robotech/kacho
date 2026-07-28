// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// Which interceptor a PROXIED RPC reaches is a property of grpc-go, not of the
// RPC's own shape — and it decides which arm of the authz middleware runs.
//
// The gateway serves domain traffic through `grpc.UnknownServiceHandler`: it
// registers no domain service of its own, and forwards whatever arrives to the
// owning backend. grpc-go dispatches an unregistered service through
// `processStreamingRPC`, so the call reaches the STREAM interceptor with
// `IsServerStream`/`IsClientStream` set — even when the RPC is an ordinary unary
// one and the client sent exactly one message. The unary interceptor is never
// consulted for that traffic.
//
// The consequence for authz is concrete and not theoretical: the middleware's
// stream arm is gated before any client message is read, so `ProtoReq` is nil and
// `dr.Stream` is true. Every catalog row whose scope is a concrete per-resource id
// therefore lands in the unmaterialised-request branch of phaseResource — the
// branch that fails closed rather than collapse the scope to a type-wide wildcard.
// That branch is not a guard held in reserve for a future streaming RPC; on this
// listener it is the branch ordinary unary RPCs take.
//
// This test pins the dispatch fact so the comment describing it cannot drift back
// into "streams only". The REST surface is unaffected and is covered elsewhere:
// grpc-gateway dials the backends directly and never re-enters this server.
func TestAuthz_ProxiedUnaryRPC_ArrivesOnTheStreamLane(t *testing.T) {
	var unaryRan, streamRan bool
	var streamMethod string
	var streamInfo *grpc.StreamServerInfo

	srv := grpc.NewServer(
		// The gateway's forwarding shape: no domain service registered, every
		// unknown one handled generically.
		grpc.UnknownServiceHandler(func(srv any, ss grpc.ServerStream) error { return nil }),
		grpc.ChainUnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
			unaryRan = true
			return h(ctx, req)
		}),
		grpc.ChainStreamInterceptor(func(s any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
			streamRan, streamMethod, streamInfo = true, info.FullMethod, info
			return h(s, ss)
		}),
	)
	lis := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	cc, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = cc.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// An ordinary UNARY RPC of a domain the gateway forwards.
	var out any
	_ = cc.Invoke(ctx, "/kacho.cloud.vpc.v1.NetworkService/Get", &emptyProxyFrame{}, &out)

	require.True(t, streamRan, "a proxied unary RPC must reach the stream interceptor")
	assert.False(t, unaryRan,
		"the unary interceptor is not consulted for forwarded traffic — an authz arm written only there would not run")
	assert.Equal(t, "/kacho.cloud.vpc.v1.NetworkService/Get", streamMethod,
		"the stream lane still carries the real method name, so the catalog row that matches is the concrete-scope one")
	require.NotNil(t, streamInfo)
	assert.True(t, streamInfo.IsServerStream && streamInfo.IsClientStream,
		"grpc-go marks the forwarded call as bidirectional regardless of the RPC's declared shape")
}

// The behaviour that follows from this lane — deny rather than collapse a
// concrete scope to a type-wide wildcard — is already pinned by
// TestAuthz_Stream_ConcreteScope_FailClosed in authz_stream_scope_test.go.

// emptyProxyFrame — a payload stand-in. The forwarded lane never decodes it; the
// test only needs something the codec accepts.
type emptyProxyFrame struct{}

func (*emptyProxyFrame) Reset()         {}
func (*emptyProxyFrame) String() string { return "" }
func (*emptyProxyFrame) ProtoMessage()  {}
