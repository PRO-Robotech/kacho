// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package proxy_test

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
)

// proxy/server.go states that the NotFound answer on this listener is
// load-bearing: it makes an Internal* method indistinguishable from a method
// that does not exist, so the external listener is not an existence-oracle for
// the admin surface.
//
// That statement was not true for every Internal* method. The decision "there is
// no such route" is taken by the UnknownServiceHandler, and the authorization
// interceptor runs BEFORE it — so an Internal* method whose permission the
// caller lacks answered PermissionDenied, NAMING THE PERMISSION, while its
// seventy neighbours answered an indistinguishable "unknown method". The shape
// of the answer told an outside caller which internal methods exist and what
// their permissions are called.
//
// These tests pin the property at the level it is observed: the two answers must
// be THE SAME BYTES. They are written against a chain in the shape of the real
// one — refusal, then an authorization interceptor that would deny — because the
// ordering IS the subject.

const (
	internalMethod    = "/kacho.cloud.storage.v1.InternalVolumeService/ListAttachments"
	nonexistentMethod = "/kacho.cloud.storage.v1.NoSuchService/NoSuchMethod"
	publicMethod      = "/kacho.cloud.vpc.v1.NetworkService/Get"
)

// denyingAuthz stands in for the gateway's authorization interceptor: it answers
// PermissionDenied and names the permission, exactly as the catalog does. It is
// what the refusal must run AHEAD of.
func denyingAuthz() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, h grpc.StreamHandler) error {
		method, _ := grpc.MethodFromServerStream(ss)
		if method == internalMethod {
			return status.Error(codes.PermissionDenied, "permission denied: storage.volumes.attach")
		}
		return h(srv, ss)
	}
}

// externalServer builds a server shaped like the advertised external listener:
// the transparent proxy over the real Resolver, with the given stream
// interceptor chain in front of it.
func externalServer(t *testing.T, interceptors ...grpc.StreamServerInterceptor) *grpc.ClientConn {
	t.Helper()
	backendLis := newFakeBackend(t)
	backendConn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return backendLis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("backend conn: %v", err)
	}
	t.Cleanup(func() { _ = backendConn.Close() })

	backends := proxy.Backends{"vpc": backendConn, "storage": backendConn}
	srv := proxy.NewServer(proxy.Resolver(backends),
		grpc.ChainStreamInterceptor(interceptors...))

	lis := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	cc, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("client conn: %v", err)
	}
	t.Cleanup(func() { _ = cc.Close() })
	return cc
}

// answer invokes method and returns the server's status as the wire carries it.
func answer(t *testing.T, cc *grpc.ClientConn, method string) *status.Status {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := &rawMsg{data: []byte{}}
	resp := &rawMsg{}
	err := cc.Invoke(ctx, method, req, resp)
	return status.Convert(err)
}

// withoutTheCallersOwnInput removes from the answer the one thing the caller
// already knew — the method it named itself. What remains is everything the
// listener CONTRIBUTED, and it is that remainder which must be identical for an
// Internal* method and for a method that does not exist. Comparing the raw
// messages would be meaningless: both echo the method, so they can never be
// equal, and an assertion written that way could only ever fail.
func withoutTheCallersOwnInput(st *status.Status, method string) string {
	return strings.ReplaceAll(st.Message(), method, "<method the caller named>")
}

func TestExternalListener_InternalMethodAnswersExactlyAsAMethodThatDoesNotExist(t *testing.T) {
	cc := externalServer(t,
		proxy.StreamRefuseInternalRoute(),
		denyingAuthz(),
	)

	internal := answer(t, cc, internalMethod)
	absent := answer(t, cc, nonexistentMethod)

	gotInternal := withoutTheCallersOwnInput(internal, internalMethod)
	gotAbsent := withoutTheCallersOwnInput(absent, nonexistentMethod)

	if internal.Code() != absent.Code() || gotInternal != gotAbsent {
		t.Fatalf("the external listener distinguishes an Internal* method from one that does not exist — "+
			"the shape of the answer is an existence-oracle for the admin surface.\n"+
			"  Internal*   : %s / %q\n"+
			"  nonexistent : %s / %q",
			internal.Code(), gotInternal, absent.Code(), gotAbsent)
	}
	if internal.Code() != codes.NotFound {
		t.Fatalf("expected the route refusal contract (NotFound), got %s / %q", internal.Code(), internal.Message())
	}
	// The message must not carry anything else either — a permission name, a
	// service description, a hint. Anything beyond the echo is the leak.
	if gotInternal != "unknown method: <method the caller named>" {
		t.Fatalf("the answer says more than «no such method»: %q", gotInternal)
	}
	if len(internal.Details()) != len(absent.Details()) {
		t.Fatalf("answers carry different error details (%d vs %d) — still distinguishable",
			len(internal.Details()), len(absent.Details()))
	}
}

// The injection, kept as a test rather than as a note: with the previous
// ordering the two answers differ, and the difference is exactly the leak.
func TestExternalListener_WithoutTheRefusal_TheAnswersDiffer(t *testing.T) {
	cc := externalServer(t, denyingAuthz())

	internal := answer(t, cc, internalMethod)
	absent := answer(t, cc, nonexistentMethod)

	if internal.Code() == absent.Code() && internal.Message() == absent.Message() {
		t.Fatal("without the route refusal the two answers were identical — then this test proves nothing " +
			"about the refusal, and the sibling test above would stay green if the refusal were deleted")
	}
	if internal.Code() != codes.PermissionDenied {
		t.Fatalf("expected the pre-fix behaviour (authorization answers first), got %s", internal.Code())
	}
}

// Control in the other direction: refusing MORE than Internal* would trade one
// blindness for another — the public surface must keep routing.
func TestExternalListener_PublicMethodStillRoutes(t *testing.T) {
	cc := externalServer(t, proxy.StreamRefuseInternalRoute(), denyingAuthz())

	st := answer(t, cc, publicMethod)
	if st.Code() == codes.NotFound {
		t.Fatalf("a public allowlisted method was refused a route: %s / %q", st.Code(), st.Message())
	}
	if st.Code() != codes.OK {
		t.Fatalf("public method should have reached the fake backend and echoed, got %s / %q", st.Code(), st.Message())
	}
}

// Control in the other direction, part two: the refusal belongs to the EXTERNAL
// listener only. A server built without it — the shape of the cluster-internal
// listener — must still serve Internal* methods, otherwise "absent outside"
// becomes indistinguishable from "absent everywhere".
func TestInternalListenerShape_StillServesInternalMethods(t *testing.T) {
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, ss grpc.ServerStream) error {
		return status.Error(codes.FailedPrecondition, "reached the internal handler")
	}))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	cc, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("client conn: %v", err)
	}
	t.Cleanup(func() { _ = cc.Close() })

	st := answer(t, cc, internalMethod)
	if st.Code() != codes.FailedPrecondition {
		t.Fatalf("an Internal* method must still be served on a listener that does not carry the refusal, got %s / %q",
			st.Code(), st.Message())
	}
}

func TestRefuseInternalRoute_MatchesOnlyInternalServices(t *testing.T) {
	cases := map[string]bool{
		"/kacho.cloud.storage.v1.InternalVolumeService/ListAttachments":      true,
		"/kacho.cloud.vpc.v1.InternalNetworkInterfaceService/ListByInstance": true,
		"/kacho.cloud.vpc.v1.NetworkService/Get":                             false,
		"/kacho.cloud.operation.OperationService/Get":                        false,
		"/grpc.health.v1.Health/Check":                                       false,
		"/grpc.reflection.v1.ServerReflection/ServerReflectionInfo":          false,
	}
	for method, want := range cases {
		if got := proxy.IsInternalRoute(method); got != want {
			t.Errorf("IsInternalRoute(%q) = %v, want %v", method, got, want)
		}
	}
}
