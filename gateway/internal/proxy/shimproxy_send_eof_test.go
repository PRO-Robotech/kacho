// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package proxy_test

import (
	"context"
	"errors"
	"fmt"
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
// allowed to decide the answer the caller is given.
//
// grpc-go states the rule on ClientStream.SendMsg: "If the error was generated
// by the client, the status is returned directly; otherwise, io.EOF is returned
// and the status of the stream may be discovered using RecvMsg." So io.EOF out
// of SendMsg carries no code at all — it says only "this stream is already
// over". status.Code(io.EOF) is Unknown, because io.EOF has no GRPCStatus().
// TestGrpcGoStillHandsTheSendHalfABareEOF below asserts that premise against the
// grpc-go actually in this module, instead of trusting the quote.
//
// WHICH CASE IS THE LOCK, AND WHY THE OTHERS CANNOT BE. The handler copies in
// two goroutines and returns the FIRST NON-NIL report. So a case is decided by
// construction only when at most one half can report non-nil in the state under
// test; otherwise the scheduler picks the answer and the case samples a race
// instead of pinning a rule. Sorted by that criterion:
//
//   - TestEdge_FinishedBackendStreamIsNotReportedAsAnError arranges exactly one
//     candidate: the receive half ends the stream cleanly (nil), so the send
//     half's io.EOF is the only non-nil report in existence. With the fix
//     reverted it fails in EVERY ordering. This is the case injection is proved
//     against, and it states the harshest form of the defect — a call the
//     backend COMPLETED was reported to the caller as an error.
//     TestEdge_AWrappedEOFFromTheSendHalfIsStillNotAnAnswer is the same
//     arrangement pointed at the PREDICATE rather than at the branch: it is the
//     only producer in the tree for an io.EOF that is wrapped, and therefore the
//     only thing standing between errors.Is and a "simplification" back to ==.
//
//   - The two "keeps its code" cases pair nil with the backend's status once the
//     fix is in, so their GREEN verdict is order-independent too and they cannot
//     flake in CI. Their RED verdict is not: with the fix reverted the two
//     candidates are io.EOF and the backend's status, and either may arrive
//     first. Measured, not estimated — 600 runs at GOMAXPROCS=2 under twelve
//     busy cores, shimproxy.go reverted: they passed for the WRONG reason 9 and
//     7 times out of 600, and WHICH of the two came off worse changed between
//     runs, which is the signature of a coin rather than of a property. The lock
//     above failed 600 out of 600 in the same runs. These two are kept because
//     they assert what the lock cannot — that the backend's own code AND message
//     come out unaltered — and they are deliberately not the injection proof.
//
//   - TestEdge_ClientGeneratedSendErrorIsStillReported is decided by
//     construction in both directions: its backend keeps the stream open, so the
//     receive half has nothing to report while the call is alive and the send
//     half's status is the only candidate.
//
// LENIENCY RUNS ONE WAY ONLY, AND ONE STUB IS DELIBERATELY OUTSIDE THE QUOTE.
// Every stub but one reproduces an arrangement the quoted contract permits and
// nothing else. The exception is the wrapped-EOF stub: the contract says io.EOF,
// meaning the sentinel, so a WRAPPED one is not something the contract describes
// and not something this tree produces (checked: exactly one construction of a
// wrapped io.EOF exists here, and it is that stub). It is there on purpose and
// in the safe direction — it demands the handler tolerate a WIDER class than can
// occur, so it can only ever produce a false red if the predicate is narrowed.
// What is forbidden here is a stub LOOSER than the product, one that accepts
// what the product would refuse; there is none.

// ---------------------------------------------------------------------------
// The premise, asserted rather than quoted.
// ---------------------------------------------------------------------------

// TestGrpcGoStillHandsTheSendHalfABareEOF pins the fact the fix in shimproxy.go
// is built on: that a send onto a stream the server has already finished comes
// back as io.EOF carrying no status.
//
// Without this case the premise would live only in a godoc quote, and a grpc-go
// upgrade that started returning a status-bearing error there would reopen the
// defect with the whole suite green — the stubs below would keep modelling a
// contract the dependency no longer has. The case therefore uses the real
// client and the real server, never a stub.
//
// The ordering is pinned the way this repository already pins it in
// public_allowlist_answered_test.go: settle the call with RecvMsg first. From
// that point the stream is FINISHED, so every later SendMsg on it meets the
// finished state deterministically.
func TestGrpcGoStillHandsTheSendHalfABareEOF(t *testing.T) {
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer() // nothing registered: every method is refused, trailers-only
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial the premise server: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// ClientStreams: true is load-bearing. grpc-go converts a failed write to
	// io.EOF only for client-streaming descriptors; for a unary descriptor the
	// same failure returns nil and the status is left entirely to RecvMsg. The
	// gateway's shim marks every proxied call client-streaming, which is why the
	// io.EOF branch exists there at all.
	desc := &grpc.StreamDesc{StreamName: "premise", ClientStreams: true, ServerStreams: true}
	cs, err := conn.NewStream(ctx, desc, "/kacho.cloud.vpc.v1.GhostService/Vanish")
	if err != nil {
		t.Fatalf("open a stream to an unserved method: %v", err)
	}

	// Premise of the premise: the server really did refuse, so the stream really
	// is finished before the send below.
	if got := status.Code(cs.RecvMsg(&rawMsg{})); got != codes.Unimplemented {
		t.Fatalf("premise not set up: an unserved method must be refused Unimplemented, got %s", got)
	}

	sendErr := cs.SendMsg(&rawMsg{data: []byte{}})
	if sendErr == nil {
		t.Fatalf("grpc-go accepted a send onto a finished stream: the shim's io.EOF branch now has no producer, " +
			"and the rule it encodes must be re-derived from the current contract before it is trusted")
	}
	if !errors.Is(sendErr, io.EOF) {
		t.Fatalf("grpc-go no longer reports io.EOF from the send half of a finished stream: got %#v. "+
			"shimproxy.go stands down on exactly this error to let the receive half name the status; if the "+
			"dependency changed the signal, that branch stops firing and backend codes start arriving as Unknown "+
			"again", sendErr)
	}
	if got := status.Code(sendErr); got != codes.Unknown {
		t.Fatalf("io.EOF from the send half now carries a status (%s): the fix assumes it carries none and that "+
			"the receive half is the only authority. Re-derive the rule before relying on it", got)
	}
	// And the other half of the same premise: the status is still there to be read.
	if got := status.Code(cs.RecvMsg(&rawMsg{})); got != codes.Unimplemented {
		t.Fatalf("the receive half no longer holds the status after the send half saw io.EOF: got %s, want %s. "+
			"Standing down on the send half only works while the receive half still answers", got, codes.Unimplemented)
	}
}

// ---------------------------------------------------------------------------
// Backend stubs. Each models one arrangement the contract above permits.
// ---------------------------------------------------------------------------

// finishedBackendStream is a ClientStream the backend has already finished:
// SendMsg reports the bare io.EOF grpc-go specifies, and the status — or the
// clean end — that finished it is available only from RecvMsg.
//
// recvPlan is what successive RecvMsg calls return, in order; the last entry
// repeats. Only the proxy's receive goroutine calls RecvMsg and only its send
// goroutine calls SendMsg, so the two are never in the same field concurrently;
// sendReturned is the one crossing point and it is a channel.
//
// sendReturned, when non-nil, holds RecvMsg until SendMsg has RETURNED — not
// until it was entered, which would release the receive half while the send half
// still had its whole report ahead of it and would arrange nothing at all. What
// is left after the barrier is the handful of instructions between SendMsg
// returning and the send goroutine's channel write, and no stub can close that:
// the two halves report into a channel this test cannot observe. The barrier is
// therefore an ARRANGEMENT of the likely ordering, not a proof of it — the cases
// that need proof are built so that only one half can report at all.
//
// The barrier gives up on ctx. Without that it is a deadlock waiting for a bad
// day: the proxy's send goroutine has paths that return WITHOUT ever calling
// SendMsg (the caller half-closes, or its stream errors), and on any of them the
// barrier is never lifted, the receive goroutine parks forever, the handler
// parks on its second report, and srv.Stop in cleanup parks on the handler. The
// package would then die on the test binary's global timeout — one legible red
// traded for "did not execute" across every case in it. Giving up on ctx turns
// that back into a normal failed call: the caller's deadline cancels the RPC,
// which cancels this ctx.
type finishedBackendStream struct {
	ctx      context.Context
	recvPlan []error
	recvAt   int

	sendReturned chan struct{}
	sendOnce     sync.Once

	// sendErr is what SendMsg returns. io.EOF models a server-side termination;
	// anything else models an error the client itself generated.
	sendErr error
}

func (s *finishedBackendStream) Header() (metadata.MD, error) { return metadata.MD{}, nil }
func (s *finishedBackendStream) Trailer() metadata.MD         { return metadata.MD{} }
func (s *finishedBackendStream) CloseSend() error             { return nil }
func (s *finishedBackendStream) Context() context.Context     { return s.ctx }

func (s *finishedBackendStream) SendMsg(any) error {
	if s.sendReturned != nil {
		defer s.sendOnce.Do(func() { close(s.sendReturned) })
	}
	return s.sendErr
}

func (s *finishedBackendStream) RecvMsg(any) error {
	if s.sendReturned != nil {
		select {
		case <-s.sendReturned:
		case <-s.ctx.Done():
			return status.FromContextError(s.ctx.Err()).Err()
		}
	}
	err := s.recvPlan[s.recvAt]
	if s.recvAt < len(s.recvPlan)-1 {
		s.recvAt++
	}
	return err
}

// aliveBackendStream keeps the call open: the backend is healthy and has nothing
// to say yet, which is precisely the situation in which a send can fail on this
// side alone. Its receive half therefore never competes to report while the call
// is alive — that is what makes the twin below decided by construction.
type aliveBackendStream struct {
	ctx     context.Context
	sendErr error
}

func (s *aliveBackendStream) Header() (metadata.MD, error) { return metadata.MD{}, nil }
func (s *aliveBackendStream) Trailer() metadata.MD         { return metadata.MD{} }
func (s *aliveBackendStream) CloseSend() error             { return nil }
func (s *aliveBackendStream) Context() context.Context     { return s.ctx }
func (s *aliveBackendStream) SendMsg(any) error            { return s.sendErr }

func (s *aliveBackendStream) RecvMsg(any) error {
	<-s.ctx.Done()
	return status.FromContextError(s.ctx.Err()).Err()
}

// backendConn is a ClientConnInterface handing out one stream built by newStream.
type backendConn struct {
	newStream func(ctx context.Context) grpc.ClientStream
}

func (b *backendConn) Invoke(context.Context, string, any, any, ...grpc.CallOption) error {
	return status.Error(codes.Unimplemented, "the proxy only ever opens streams")
}

func (b *backendConn) NewStream(
	ctx context.Context, _ *grpc.StreamDesc, _ string, _ ...grpc.CallOption,
) (grpc.ClientStream, error) {
	return b.newStream(ctx), nil
}

// finishedBackend refuses (or completes) before reading the request: the send
// half meets io.EOF, the receive half carries recvPlan. No barrier — the halves
// run as they fall out, which is all the cases using it need, because in those
// cases only one half can report non-nil.
func finishedBackend(sendErr error, recvPlan ...error) *backendConn {
	return &backendConn{newStream: func(ctx context.Context) grpc.ClientStream {
		return &finishedBackendStream{ctx: ctx, recvPlan: recvPlan, sendErr: sendErr}
	}}
}

// finishedBackendSendHalfFirst is the same backend with the barrier described on
// finishedBackendStream: the receive half is held until the send half has been
// through SendMsg. Used by the cases whose names claim that ordering.
func finishedBackendSendHalfFirst(sendErr error, recvPlan ...error) *backendConn {
	return &backendConn{newStream: func(ctx context.Context) grpc.ClientStream {
		return &finishedBackendStream{
			ctx:          ctx,
			recvPlan:     recvPlan,
			sendErr:      sendErr,
			sendReturned: make(chan struct{}),
		}
	}}
}

// aliveBackend stays open; only the send half has anything to report.
func aliveBackend(sendErr error) *backendConn {
	return &backendConn{newStream: func(ctx context.Context) grpc.ClientStream {
		return &aliveBackendStream{ctx: ctx, sendErr: sendErr}
	}}
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

func callThroughEdge(t *testing.T, cc *grpc.ClientConn, timeout time.Duration) *status.Status {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return status.Convert(cc.Invoke(ctx, echoMethod, &rawMsg{data: []byte{}}, &rawMsg{}))
}

// edgeCallBudget is ONE budget for every case here, and it is generous on
// purpose. On the green path the handler answers over an in-memory listener —
// slowest observed was 0.02s, measured at GOMAXPROCS=2 under twelve busy cores,
// which is the shape of the runner these cases have to survive. Nothing on that
// path can approach ten seconds.
//
// A tighter budget was tried for the twin, whose broken state is reached BY the
// budget expiring, and was dropped: it buys nothing (the budget is only ever
// reached in the broken state, where waiting ten seconds instead of two costs
// nothing at all) and it puts a clock back into a verdict, which is the one
// thing this file exists to take out of them. A budget short enough to matter
// is a budget a stalled runner can trip on the green path, and a false red HERE
// reads as "the fix swallows errors" and invites someone to weaken it.
const edgeCallBudget = 10 * time.Second

// ---------------------------------------------------------------------------
// THE LOCK. Decided by construction in both states.
// ---------------------------------------------------------------------------

// TestEdge_FinishedBackendStreamIsNotReportedAsAnError — a call the backend
// COMPLETED must not be turned into an error because the send half arrived at
// the finish line second.
//
// The arrangement: the backend answers and ends the stream cleanly while the
// proxy is still pushing the request at it — an everyday race for a fast
// backend, and the shim marks every proxied call client-streaming, so the send
// half learns about it as a bare io.EOF. The receive half then reports nil,
// which makes it the ONLY case here where a single non-nil report can exist.
// Consequences, both of them checkable rather than hoped for:
//
//   - with the fix in place both halves report nil, and the caller gets OK plus
//     the backend's response, in every ordering;
//   - with the fix reverted the send half's io.EOF is the only non-nil report,
//     so "first non-nil wins" returns it no matter which half got there first,
//     and grpc-go converts a non-status handler error to Unknown. The caller is
//     told the call failed with a code no backend ever produced, for a call that
//     in fact succeeded.
//
// That is why this case, and not the two below, is what a revert is proved
// against: it has no ordering left to be lucky about.
func TestEdge_FinishedBackendStreamIsNotReportedAsAnError(t *testing.T) {
	cc := edgeOver(t, finishedBackend(
		io.EOF,       // the send half meets the already-finished stream
		nil, io.EOF)) // the receive half: one response frame, then a clean end

	got := callThroughEdge(t, cc, edgeCallBudget)

	if got.Code() != codes.OK {
		t.Fatalf("the edge failed a call the backend completed: got %s (%q), want OK. io.EOF out of SendMsg means "+
			"only «this stream is already over»; it carries no status (status.Code(io.EOF) is Unknown), so "+
			"reporting it invents a failure the backend never had",
			got.Code(), got.Message())
	}
}

// TestEdge_AWrappedEOFFromTheSendHalfIsStillNotAnAnswer — the same lock, aimed
// at how the question is asked rather than at what is done with the answer.
//
// The loop asks "is this io.EOF?" with errors.Is, in all three places, and this
// case is the only thing that makes that choice testable. An error that WRAPS
// io.EOF is the same signal — the stream is over — and identity comparison
// misses it and reports it as the RPC's error: the original defect wearing a
// prefix, and silent, because every other case here passes a bare sentinel and
// stays green either way.
//
// The input is synthetic and this case does not pretend otherwise: grpc-go
// returns the bare sentinel (pinned above), and the tree declares no stream
// client interceptor anywhere — measured, not assumed. So no producer of a
// wrapped EOF exists today, and without this case errors.Is would be an
// untestable preference that the next reader "simplifies" back to == with the
// whole suite green. Synthetic in the safe direction, too: it demands the
// handler tolerate a WIDER class than currently occurs, so it can only ever
// produce a false red if the predicate is narrowed — never a false green.
//
// Decided by construction for the same reason as the lock above: the receive
// half ends cleanly, so the wrapped EOF is the only non-nil report there can be.
func TestEdge_AWrappedEOFFromTheSendHalfIsStillNotAnAnswer(t *testing.T) {
	cc := edgeOver(t, finishedBackend(
		fmt.Errorf("stream interceptor: %w", io.EOF),
		nil, io.EOF))

	got := callThroughEdge(t, cc, edgeCallBudget)

	if got.Code() != codes.OK {
		t.Fatalf("a wrapped io.EOF was reported as the call's outcome: got %s (%q), want OK. Wrapping does not "+
			"turn «this stream is already over» into a status; asking with == instead of errors.Is misses the "+
			"signal and answers the caller with a code no backend produced",
			got.Code(), got.Message())
	}
}

// ---------------------------------------------------------------------------
// The backend's own code and message survive. Green verdict is decided by
// construction; the red verdict is not — see the file header.
// ---------------------------------------------------------------------------

// TestEdge_BackendRefusalKeepsItsCode_WhenTheSendHalfSeesTheFinishedStreamFirst
// — the send half must not be the one that decides.
//
// With the fix in place the two candidate reports are nil and the backend's
// status, so the answer is the status whichever half arrives first. What this
// adds to the lock above is that the code and the message come out UNALTERED:
// the fix has to read the status off the receive half, and cannot be a constant.
func TestEdge_BackendRefusalKeepsItsCode_WhenTheSendHalfSeesTheFinishedStreamFirst(t *testing.T) {
	cc := edgeOver(t, finishedBackendSendHalfFirst(
		io.EOF,
		status.Error(codes.PermissionDenied, "permission denied: vpc.networks.get")))

	got := callThroughEdge(t, cc, edgeCallBudget)

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

// Same arrangement, a different backend code — so the fix cannot be a hard-coded
// PermissionDenied and must actually read the status off the receive half.
func TestEdge_BackendNotFoundKeepsItsCode_WhenTheSendHalfSeesTheFinishedStreamFirst(t *testing.T) {
	cc := edgeOver(t, finishedBackendSendHalfFirst(
		io.EOF,
		status.Error(codes.NotFound, "Network enp_x not found")))

	got := callThroughEdge(t, cc, edgeCallBudget)

	if got.Code() != codes.NotFound {
		t.Fatalf("got %s (%q), want %s — the receive half is the only authority on the backend's status",
			got.Code(), got.Message(), codes.NotFound)
	}
}

// ---------------------------------------------------------------------------
// THE LEGAL TWIN. Also decided by construction, in the opposite direction.
// ---------------------------------------------------------------------------

// TestEdge_ClientGeneratedSendErrorIsStillReported — an error SendMsg generated
// ITSELF is a real status and must keep travelling. grpc-go returns such errors
// directly rather than as io.EOF, so swallowing every send error would trade one
// silent wrong code for another: the caller would be told whatever the backend
// happened to say, when in fact the edge could not put the message on the wire.
//
// Decided by construction, like the lock and unlike the two cases above: this
// backend keeps the call OPEN, which is the honest model of a send that failed
// on this side alone — the peer is healthy and has nothing to report. So the
// receive half is not a competing writer, the send half's status is the only
// candidate, and the verdict cannot be chosen by the scheduler.
//
// A fix that over-reached and swallowed every send error would leave the handler
// waiting on a receive half with nothing to say; the call then runs out
// edgeCallBudget and this case fails naming the property, rather than passing.
// That is the only way this budget is ever reached — see the constant.
func TestEdge_ClientGeneratedSendErrorIsStillReported(t *testing.T) {
	cc := edgeOver(t, aliveBackend(
		status.Error(codes.ResourceExhausted, "trying to send message larger than max")))

	got := callThroughEdge(t, cc, edgeCallBudget)

	if got.Code() != codes.ResourceExhausted {
		t.Fatalf("got %s (%q), want %s — an error SendMsg generated itself carries a status and is not io.EOF, "+
			"so it must be reported as-is; discarding it would hide a failure to send behind whatever the "+
			"backend happened to say, or behind no answer at all until the call times out",
			got.Code(), got.Message(), codes.ResourceExhausted)
	}
}
