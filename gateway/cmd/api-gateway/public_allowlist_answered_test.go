// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// public_allowlist_answered_test.go — the second half of the bypass-list gate:
// an entry must name an RPC the edge actually ANSWERS, not merely one that
// exists in the contract.
//
// WHY A SECOND GATE. middleware/authz_public_allowlist_resolves_test.go resolves
// every entry against protoregistry — it proves the name is real. It says so
// itself, and deliberately treats a served-but-unimplemented method as
// legitimate: "exists in the contract is the property, not returns OK". That is
// the right predicate for the question it asks and the wrong one for this
// question. A method the edge answers Unimplemented cannot be reached through
// the bypass by anybody, so the exemption it carries has no subject — and an
// exemption with nothing left to exempt is a finding, not tidy bookkeeping.
//
// SCOPE, STATED SO IT CANNOT DRIFT. The probe can only speak for methods the
// edge serves ITSELF. An entry naming a proxied backend RPC is out of the
// probe's reach, and the census below says how many were skipped for that
// reason — "0 findings" has to stay distinguishable from "0 examined".
package main

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/opsproxy"
)

// rawCodec sends and receives message bodies as opaque bytes so the probe can
// invoke any method without linking its request type. It reports the name
// "proto" so the wire content-subtype is unchanged and the SERVER still decodes
// with its own proto codec — an empty body is a valid empty message for every
// request type, which is exactly what a reachability probe wants to send.
type rawCodec struct{}

func (rawCodec) Marshal(v any) ([]byte, error) {
	b, ok := v.(*[]byte)
	if !ok {
		return nil, fmt.Errorf("rawCodec: cannot marshal %T", v)
	}
	return *b, nil
}

func (rawCodec) Unmarshal(data []byte, v any) error {
	b, ok := v.(*[]byte)
	if !ok {
		return fmt.Errorf("rawCodec: cannot unmarshal into %T", v)
	}
	*b = append((*b)[:0], data...)
	return nil
}

func (rawCodec) Name() string { return "proto" }

// edgeProbe serves the edge's native gRPC surface over an in-memory listener and
// answers, for any FQN, the status code the edge returns.
type edgeProbe struct {
	conn     *grpc.ClientConn
	services map[string]grpc.ServiceInfo
}

// newEdgeProbe builds the surface with a plain grpc.NewServer — no
// UnknownServiceHandler. That omission is the point: with the transparent
// handler installed, an unserved method would be dialed at a backend instead of
// refused, and Unimplemented would stop meaning "nothing here answers this".
func newEdgeProbe(t *testing.T) *edgeProbe {
	t.Helper()

	srv := grpc.NewServer()
	// Оба регистратора — один и тот же сервер: предмет здесь — что край отвечает
	// сам, а не то, что из этого покрыто потолком темпа (admission_wiring_test.go).
	registerExternalGRPCServices(srv, srv, nil, opsproxy.New(nil))

	lis := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
	)
	require.NoError(t, err, "dial the in-memory edge")
	t.Cleanup(func() { _ = conn.Close() })

	return &edgeProbe{conn: conn, services: srv.GetServiceInfo()}
}

// serves reports whether the edge registers the service half of this FQN itself.
func (p *edgeProbe) serves(fqn string) bool {
	slash := strings.IndexByte(fqn, '/')
	if slash <= 0 {
		return false
	}
	_, ok := p.services[fqn[:slash]]
	return ok
}

// code invokes the FQN with an empty body and returns the gRPC status code.
//
// Every method is driven as a stream: gRPC's unary path is a stream underneath,
// so one code path covers Check (unary) and Watch (server-streaming) without the
// probe having to know which is which — and knowing which is which is precisely
// what a probe about reachability must not depend on.
func (p *edgeProbe) code(fqn string) codes.Code {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	desc := &grpc.StreamDesc{StreamName: "probe", ClientStreams: true, ServerStreams: true}
	st, err := p.conn.NewStream(ctx, desc, "/"+fqn, grpc.ForceCodec(rawCodec{}))
	if err != nil {
		return status.Code(err)
	}
	return statusOf(st)
}

// statusOf drives one already-created stream to completion and reports the
// RPC's status code.
//
// The verdict is taken from RecvMsg ONLY. SendMsg does not deliver a status: it
// queues bytes, and it reports io.EOF once the call is over — which for an
// unserved method (trailers-only Unimplemented) can already be true before the
// probe gets its empty body out. Reading the code off the send would then answer
// Unknown, and WHICH of the two happens first is decided by machine load, not by
// the edge. The same holds for CloseSend. Both errors are therefore left for
// RecvMsg to report authoritatively — a genuine transport failure surfaces there
// too, so nothing is swallowed. See TestEdgeProbe_ReadsStatusOffAFinishedStream.
func statusOf(st grpc.ClientStream) codes.Code {
	empty := []byte{}
	_ = st.SendMsg(&empty)
	_ = st.CloseSend()
	var out []byte
	if err := st.RecvMsg(&out); err != nil {
		return status.Code(err)
	}
	return codes.OK
}

// TestEdgeProbe_ReadsStatusOffAFinishedStream pins the one thing the gate below
// cannot survive being wrong about: WHERE the probe reads the RPC's status from.
//
// The status of a gRPC call is carried by RecvMsg and by nothing else. SendMsg
// only queues bytes; when the server has already finished the call — which is
// what a trailers-only Unimplemented is — SendMsg reports io.EOF, a plain error
// carrying no status, so status.Code() of it reads Unknown. Whether the probe
// hits that ordering is decided by the machine: on an idle laptop the client
// almost always gets its bytes out first, on a loaded runner the server's
// refusal often lands first. A probe that took its verdict from SendMsg would
// therefore answer Unknown to a live edge for no reason of the edge's, and the
// gate's own discriminator premise ("an unserved method must read Unimplemented")
// would fire — reading, to whoever finds it, as a broken bypass list.
//
// This case makes that ordering the ONLY ordering: the stream is driven to
// completion first, so the status is already settled before statusOf touches it.
func TestEdgeProbe_ReadsStatusOffAFinishedStream(t *testing.T) {
	probe := newEdgeProbe(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const unserved = "/kaname.cloud.iam.v1.GhostService/Vanish"
	desc := &grpc.StreamDesc{StreamName: "probe", ClientStreams: true, ServerStreams: true}
	st, err := probe.conn.NewStream(ctx, desc, unserved, grpc.ForceCodec(rawCodec{}))
	require.NoError(t, err, "open a stream to an unserved method")

	// Premise: settle the call before reading it. RecvMsg blocks until the
	// server's refusal has arrived, so from here on the stream is FINISHED and
	// every subsequent SendMsg on it yields io.EOF — deterministically, the same
	// state the loaded runner reaches by chance.
	var sink []byte
	require.Equal(t, codes.Unimplemented, status.Code(st.RecvMsg(&sink)),
		"premise: the edge must refuse %q with Unimplemented — otherwise this case is not set up on a finished stream", unserved)

	require.Equal(t, codes.Unimplemented, statusOf(st),
		"statusOf read the verdict off a send rather than off the RPC status: on a stream the server has already "+
			"finished, SendMsg reports io.EOF (Unknown) and the real status is still there to be read. Taking the "+
			"code from the send makes the probe's answer depend on machine speed, not on what the edge serves.")
}

// TestPublicAllowlist_EdgeAnswersEveryEntryItServes — THE GATE.
func TestPublicAllowlist_EdgeAnswersEveryEntryItServes(t *testing.T) {
	probe := newEdgeProbe(t)
	entries := middleware.DefaultPublicAllowlist()

	// ── Premise 1: there is something to check. ────────────────────────────
	require.NotEmpty(t, entries,
		"the bypass list is empty: this gate would then examine nothing and still print ok")

	// ── Premise 2: the probe discriminates, in BOTH directions. ────────────
	// Without the positive half, a probe that answered Unimplemented to
	// everything (wrong codec, dead listener, bad method path) would redden
	// honestly-live entries. Without the negative half, a probe that answered OK
	// to everything would pass a list of pure fiction.
	if got := probe.code("grpc.health.v1.Health/Check"); got == codes.Unimplemented {
		t.Fatalf("PROBE IS BROKEN: the edge's own health check reads as %s — no verdict below is trustworthy", got)
	}
	for _, unanswerable := range []string{
		"grpc.health.v1.Health/Evaporate",                          // registered service, no such method
		"kaname.cloud.iam.v1.GhostService/Vanish",                  // service the edge does not serve
		"grpc.reflection.v1.ServerReflection/ServerReflectionInfo", // moved to the internal listener
	} {
		if got := probe.code(unanswerable); got != codes.Unimplemented {
			t.Fatalf("PROBE IS BROKEN: %q must read as %s, got %s — the probe cannot tell answered from unanswered",
				unanswerable, codes.Unimplemented, got)
		}
	}

	// ── The property, over the entries the probe can speak for. ────────────
	var examined, skipped int
	var unanswered []string
	for _, fqn := range entries {
		if !probe.serves(fqn) {
			skipped++
			continue
		}
		examined++
		if got := probe.code(fqn); got == codes.Unimplemented {
			unanswered = append(unanswered, fmt.Sprintf("%s — the edge answers %s", fqn, got))
		}
	}

	census := fmt.Sprintf("census: %d entr(y/ies) on the bypass list; %d invoked against the edge's own surface; "+
		"%d skipped as not natively served (out of this probe's reach — resolution for those is covered by "+
		"middleware/authz_public_allowlist_resolves_test.go)", len(entries), examined, skipped)
	t.Log(census)

	// ── Premise 3: the scope was not empty. ───────────────────────────────
	// If every entry were skipped, the loop above would find nothing and the
	// test would pass having invoked no RPC at all.
	require.NotZero(t, examined,
		"GATE CANNOT RUN: no entry names a natively-served method, so nothing was invoked. %s", census)

	require.Empty(t, unanswered,
		"%d bypass entr(y/ies) name a method the edge answers Unimplemented. Membership of this list waives "+
			"authentication AND authorization at decide() step 1 — for an RPC no caller can reach, so the "+
			"exemption has no subject and only teaches the next author that pre-placing one is normal. If the "+
			"method is meant to be served, implement it and the entry becomes real; otherwise remove the entry "+
			"and add it back with the implementation.\n  %s\n%s",
		len(unanswered), strings.Join(unanswered, "\n  "), census)
}
