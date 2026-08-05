// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// internal_grpc_reflection_test.go — where operator schema-discovery lives now.
//
// Reflection moved off the advertised edge, so the tooling case it served has to
// be answered somewhere, or the move is a removal wearing a move's name. It is
// answered on the cluster-internal listener, which is the repository's own rule
// for admin surface (security.md §"Internal-vs-external").
//
// "Behind mTLS" is asserted as a property of the listener rather than trusted as
// a property of the network: the same file proves an allow-listed identity gets
// a descriptor stream, a verified-but-not-allow-listed identity is refused, and
// the insecure dev posture — the one where no interceptor runs — does not
// register reflection at all. A surface that authenticates nobody is not made
// internal by the port it sits on.
package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	reflectpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// nonAllowlistedSPIFFE — a real Kachō module identity that the internal
// listener's caller allow-list does not name. It carries a certificate this CA
// signed, so it clears the transport; it is the exact shape of caller the
// allow-list exists to stop.
const nonAllowlistedSPIFFE = "spiffe://kacho.cloud/ns/kacho-vpc/sa/kacho-vpc"

// requireTransportReady blocks until the connection has actually completed its
// TLS handshake, and fails the test if it never does.
//
// Why this is not ceremony. grpc.NewClient connects LAZILY, so without it the
// first RPC carries two unrelated failures in one return value: the transport
// never came up (wrong certificate, wrong server name, listener gone) and the
// server refused the call on its merits. Both surface as "err != nil", and the
// code that comes with them differs — a refused stream carries the interceptor's
// PermissionDenied, a broken handshake carries Unknown or Unavailable. A test
// asserting the former therefore reads a transport failure as "the listener
// answered with the wrong code", which is precisely the wrong conclusion: it
// points the next reader at authorisation when the connection is what broke.
//
// Observed, not theorised: this file's refusal case failed once in CI with
// Unknown where PermissionDenied was asserted, while 44 local runs of the same
// package — plain, single-CPU, and under external load — stayed green. Waiting
// here does not weaken the assertion below (still PermissionDenied and nothing
// else); it separates the two outcomes so that whichever one happens says so.
func requireTransportReady(t *testing.T, conn *grpc.ClientConn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn.Connect()
	for {
		s := conn.GetState()
		if s == connectivity.Ready {
			return
		}
		require.NotEqual(t, connectivity.Shutdown, s, "connection shut down before it was ready")
		require.True(t, conn.WaitForStateChange(ctx, s),
			"connection never reached READY (last state %s): the mTLS handshake did not complete, "+
				"so anything the test asserts about the listener's answer would be about the transport instead", s)
	}
}

// listServices drives one ServerReflectionInfo exchange and returns the service
// names the listener reports, or the gRPC status of the refusal.
func listServices(t *testing.T, conn *grpc.ClientConn) ([]string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := reflectpb.NewServerReflectionClient(conn).ServerReflectionInfo(ctx)
	if err != nil {
		return nil, err
	}
	if err := stream.Send(&reflectpb.ServerReflectionRequest{
		MessageRequest: &reflectpb.ServerReflectionRequest_ListServices{ListServices: ""},
	}); err != nil {
		return nil, err
	}
	resp, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, s := range resp.GetListServicesResponse().GetService() {
		names = append(names, s.GetName())
	}
	return names, nil
}

// internalMTLSFixture mints the CA, the listener's server leaf and one client
// leaf per requested identity, and starts the listener in the mTLS posture with
// only iamDrainerSPIFFE on the caller allow-list.
func internalMTLSFixture(t *testing.T) (addr, caFile string, drainerCert, drainerKey, otherCert, otherKey string) {
	t.Helper()
	ca := newTestCA(t, "kacho-internal-ca")
	srvCert, srvKey := ca.issueLeaf(t, leafOpts{
		commonName: internalListenerServerName,
		dnsNames:   []string{internalListenerServerName},
		isServer:   true,
	})
	drainerCert, drainerKey = ca.issueLeaf(t, leafOpts{
		commonName: "kacho-iam",
		uriSANs:    []string{iamDrainerSPIFFE},
	})
	otherCert, otherKey = ca.issueLeaf(t, leafOpts{
		commonName: "kacho-vpc",
		uriSANs:    []string{nonAllowlistedSPIFFE},
	})
	caFile = ca.caFile(t)

	sec := mtlsInternalSecurityFromFiles(t, srvCert, srvKey, caFile, iamDrainerSPIFFE)
	addr = startSecuredInternalListener(t, &fakeInvalidator{}, sec)
	return addr, caFile, drainerCert, drainerKey, otherCert, otherKey
}

// TestInternalGRPCListener_MTLS_ServesReflectionToAllowlistedCaller — the move's
// destination. An operator whose identity the listener authorises gets the
// schema stream that used to be answerable unauthenticated at the edge.
func TestInternalGRPCListener_MTLS_ServesReflectionToAllowlistedCaller(t *testing.T) {
	addr, caFile, cert, key, _, _ := internalMTLSFixture(t)

	conn, err := grpc.NewClient(addr, mtlsClientDialCreds(t, caFile, cert, key, internalListenerServerName))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	requireTransportReady(t, conn)

	svcs, err := listServices(t, conn)
	require.NoError(t, err,
		"reflection must be served on the cluster-internal listener: the edge no longer answers it, "+
			"so refusing here would leave operator schema-discovery with nowhere to go")
	require.Contains(t, svcs, "kacho.cloud.apigateway.v1.InternalAuthzCacheService",
		"reflection must enumerate the listener's own admin service — otherwise it is registered but useless")
}

// TestInternalGRPCListener_MTLS_RefusesReflectionToNonAllowlistedCaller — the
// half that makes "behind mTLS" mean something.
//
// Reflection is a STREAM, and a stream is authorised by the stream interceptor,
// not the unary one. Registering it on a listener whose stream chain was never
// exercised would move an unauthenticated surface rather than close it, so the
// refusal is asserted directly instead of inferred from the unary tests.
func TestInternalGRPCListener_MTLS_RefusesReflectionToNonAllowlistedCaller(t *testing.T) {
	addr, caFile, _, _, cert, key := internalMTLSFixture(t)

	conn, err := grpc.NewClient(addr, mtlsClientDialCreds(t, caFile, cert, key, internalListenerServerName))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	requireTransportReady(t, conn)

	_, err = listServices(t, conn)
	require.Error(t, err, "a verified but non-allow-listed identity must not obtain the schema stream")
	require.Equal(t, codes.PermissionDenied, status.Code(err),
		"the caller authenticated (its cert chains to the internal CA) but is not authorised: "+
			"PermissionDenied, not Unauthenticated and never OK")
}

// TestInternalGRPCListener_InsecureDev_DoesNotServeReflection — the posture
// pairing.
//
// The insecure dev/local listener mounts NO interceptors (serverOptions returns
// nil when mTLS is off), so anything registered there answers anyone who can
// reach the port. Reflection is therefore tied to the posture that authorises
// callers, not to a separate switch someone can flip on independently of it.
func TestInternalGRPCListener_InsecureDev_DoesNotServeReflection(t *testing.T) {
	cfg, err := config.Load() // all mTLS envs unset ⇒ insecure posture
	require.NoError(t, err)
	sec, err := buildInternalListenerSecurity(cfg)
	require.NoError(t, err)
	require.False(t, sec.mtlsEnabled, "premise: this case is about the posture with no interceptors")

	addr := startSecuredInternalListener(t, &fakeInvalidator{}, sec)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	requireTransportReady(t, conn)

	_, err = listServices(t, conn)
	require.Error(t, err, "the insecure listener must not answer reflection: nothing there authorises a caller")
	require.Equal(t, codes.Unimplemented, status.Code(err),
		"reflection must not be REGISTERED in the insecure posture — refusing it at the transport is not "+
			"available there, so the only way not to serve it is not to register it")
}
