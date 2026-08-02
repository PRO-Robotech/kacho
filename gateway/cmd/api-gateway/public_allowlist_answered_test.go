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
	registerExternalGRPCServices(srv, nil, opsproxy.New(nil))

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
	empty := []byte{}
	if err := st.SendMsg(&empty); err != nil {
		return status.Code(err)
	}
	if err := st.CloseSend(); err != nil {
		return status.Code(err)
	}
	var out []byte
	if err := st.RecvMsg(&out); err != nil {
		return status.Code(err)
	}
	return codes.OK
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
		"kacho.cloud.iam.v1.GhostService/Vanish",                   // service the edge does not serve
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
