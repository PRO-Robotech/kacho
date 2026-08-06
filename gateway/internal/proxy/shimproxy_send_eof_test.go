// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package proxy_test

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
)

// The subject of this file: which of the two halves of a proxied bidi stream is
// allowed to decide the code the caller is told.
//
// grpc-go states the rule on ClientStream.SendMsg: "If the error was generated
// by the client, the status is returned directly; otherwise, io.EOF is returned
// and the status of the stream may be discovered using RecvMsg." So io.EOF out
// of SendMsg carries no code at all — it says only "this stream is already
// over". status.Code(io.EOF) is Unknown, because io.EOF has no GRPCStatus().
//
// The transparent proxy copies in two goroutines and returns whichever reports
// first. When a backend refuses a call before reading its request, BOTH halves
// observe the same finished stream: the send half as io.EOF, the receive half as
// the backend's real status. If the send half reports first, the caller is told
// Unknown for a refusal the backend spelled out — the gateway invents a code no
// backend ever produced, and it does so only when the ordering goes one way,
// which is why it reads as an occasional flake rather than as a constant bug.
//
// The stub below makes that ordering happen on purpose instead of waiting for a
// loaded CI machine to produce it. It is not more lenient than the product: it
// reproduces exactly the contract quoted above and nothing else.

// refusedByBackend is a ClientStream shaped like one the backend has already
// finished: SendMsg reports the bare io.EOF grpc-go specifies, and the status
// that ended the stream is available only from RecvMsg.
//
// RecvMsg blocks until SendMsg has been observed. That is the whole point: it
// pins the ordering in which the send half reports first, which is the ordering
// under test. Without it the test would sample a race and pass for the wrong
// reason roughly half the time.
type refusedByBackend struct {
	ctx      context.Context
	st       error
	sendSeen chan struct{}
	once     sync.Once
	// sendErr is what SendMsg returns. io.EOF models a server-side termination;
	// anything else models an error the client itself generated.
	sendErr error
}

func (s *refusedByBackend) Header() (metadata.MD, error) { return metadata.MD{}, nil }
func (s *refusedByBackend) Trailer() metadata.MD         { return metadata.MD{} }
func (s *refusedByBackend) CloseSend() error             { return nil }
func (s *refusedByBackend) Context() context.Context     { return s.ctx }

func (s *refusedByBackend) SendMsg(any) error {
	s.once.Do(func() { close(s.sendSeen) })
	return s.sendErr
}

func (s *refusedByBackend) RecvMsg(any) error {
	<-s.sendSeen
	return s.st
}

// backendThatRefuses is a ClientConnInterface handing out exactly one such stream.
type backendThatRefuses struct {
	st      error
	sendErr error
}

func (b *backendThatRefuses) Invoke(context.Context, string, any, any, ...grpc.CallOption) error {
	return b.st
}

func (b *backendThatRefuses) NewStream(
	ctx context.Context, _ *grpc.StreamDesc, _ string, _ ...grpc.CallOption,
) (grpc.ClientStream, error) {
	return &refusedByBackend{
		ctx:      ctx,
		st:       b.st,
		sendSeen: make(chan struct{}),
		sendErr:  b.sendErr,
	}, nil
}

// edgeOver serves the transparent proxy in front of the given backend and
// returns a connection to it.
func edgeOver(t *testing.T, backend grpc.ClientConnInterface) *grpc.ClientConn {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(grpc.UnknownServiceHandler(proxy.Handler(
		func(fullMethod string) (string, grpc.ClientConnInterface, bool) {
			return fullMethod, backend, true
		},
	)))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial the edge: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func callThroughEdge(t *testing.T, cc *grpc.ClientConn, method string) *status.Status {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return status.Convert(cc.Invoke(ctx, method, &rawMsg{data: []byte{}}, &rawMsg{}))
}

// TestEdge_BackendRefusalKeepsItsCode_WhenTheSendHalfSeesTheFinishedStreamFirst
// — the send half must not be the one that decides.
//
// Pre-fix this returns Unknown: the send half reports io.EOF, the proxy returns
// it as the handler error, and grpc-go converts a non-status handler error to
// Unknown. The caller is then told the backend answered with a code the backend
// never sent, and the next reader goes looking at the backend for a defect that
// is in the copy loop.
func TestEdge_BackendRefusalKeepsItsCode_WhenTheSendHalfSeesTheFinishedStreamFirst(t *testing.T) {
	cc := edgeOver(t, &backendThatRefuses{
		st:      status.Error(codes.PermissionDenied, "permission denied: vpc.networks.get"),
		sendErr: io.EOF,
	})

	got := callThroughEdge(t, cc, echoMethod)

	if got.Code() != codes.PermissionDenied {
		t.Fatalf("the edge replaced the backend's code with one the backend never sent: got %s (%q), want %s. "+
			"io.EOF out of SendMsg means only «this stream is already over» — the status that ended it is "+
			"delivered by RecvMsg, and status.Code(io.EOF) is Unknown, so reporting the send half's error "+
			"fabricates a code",
			got.Code(), got.Message(), codes.PermissionDenied)
	}
	if got.Message() != "permission denied: vpc.networks.get" {
		t.Fatalf("the backend's message was lost on the way out: %q", got.Message())
	}
}

// Same ordering, a different backend code — so the fix cannot be a hard-coded
// PermissionDenied and must actually read the status off the receive half.
func TestEdge_BackendNotFoundKeepsItsCode_WhenTheSendHalfSeesTheFinishedStreamFirst(t *testing.T) {
	cc := edgeOver(t, &backendThatRefuses{
		st:      status.Error(codes.NotFound, "Network enp_x not found"),
		sendErr: io.EOF,
	})

	got := callThroughEdge(t, cc, echoMethod)

	if got.Code() != codes.NotFound {
		t.Fatalf("got %s (%q), want %s — the receive half is the only authority on the backend's status",
			got.Code(), got.Message(), codes.NotFound)
	}
}

// The legal twin, in the other direction: an error SendMsg generated ITSELF is
// a real status and must keep travelling. grpc-go returns such errors directly
// rather than as io.EOF, so swallowing every send error would trade one silent
// wrong code for another — the caller would be told the backend refused when in
// fact the edge could not put the message on the wire at all.
func TestEdge_ClientGeneratedSendErrorIsStillReported(t *testing.T) {
	cc := edgeOver(t, &backendThatRefuses{
		// The receive half would say NotFound if it were ever consulted; it must
		// not be, because the send half failed on its own account first.
		st:      status.Error(codes.NotFound, "must not be reported"),
		sendErr: status.Error(codes.ResourceExhausted, "trying to send message larger than max"),
	})

	got := callThroughEdge(t, cc, echoMethod)

	if got.Code() != codes.ResourceExhausted {
		t.Fatalf("got %s (%q), want %s — an error SendMsg generated itself carries a status and is not io.EOF, "+
			"so it must be reported as-is; discarding it would hide a failure to send behind whatever the "+
			"backend happened to say",
			got.Code(), got.Message(), codes.ResourceExhausted)
	}
}
