// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The admin hop carries a LIVE end-user bearer, on every introspection cache
// miss — before the revocation check moved onto the authN layer only
// administrative calls went that way. Moving the hop to TLS therefore needs a
// client that trusts the internal CA, and until this file existed there was no
// way to give it one: the HTTPClient field of the introspection cache's config
// was never filled at the composition root, so the hop always ran on a client
// that trusts the system roots only.
//
// These tests pin the capability at the level an operator experiences it:
//   - no trust anchor configured → the caller still gets a bounded client, and
//     one with no custom transport (today's behaviour, unchanged);
//   - trust anchor configured → a handshake against a server holding a leaf
//     from THAT CA succeeds, and one from a different CA still fails. Asserting
//     only the first half would pass equally for a client that verifies nothing;
//   - trust anchor unreadable / not a bundle → REFUSE TO START. Falling back to
//     the system roots is the worst outcome here: the operator believes the hop
//     is verified against the internal CA while it is not, and nothing shows it
//     until a certificate rotates.

func TestAdminHopClient_NoTrustAnchor_StaysOnThePlainBoundedClient(t *testing.T) {
	c, err := newAdminHopClient("", 1500*time.Millisecond)
	require.NoError(t, err, "no trust anchor configured is not an error — a stand may legitimately have a plaintext in-cluster admin hop")
	require.NotNil(t, c, "the client must never be nil: a nil client sends the hop through http.DefaultClient, which has no timeout at all")
	require.Equal(t, 1500*time.Millisecond, c.Timeout, "per-call budget must be applied")
	require.Nil(t, c.Transport, "without a trust anchor no custom transport may be installed")
}

func TestAdminHopClient_TrustAnchor_VerifiesThatCAAndRejectsAnother(t *testing.T) {
	caA := newTestCA(t, "internal-ca-A")
	caB := newTestCA(t, "internal-ca-B")

	c, err := newAdminHopClient(caA.caFile(t), time.Second)
	require.NoError(t, err)

	srvA := tlsServerSignedBy(t, caA)
	defer srvA.Close()
	srvB := tlsServerSignedBy(t, caB)
	defer srvB.Close()

	respA, err := c.Get(srvA.URL)
	require.NoError(t, err, "a server holding a leaf from the CONFIGURED CA must be accepted")
	_ = respA.Body.Close()

	_, err = c.Get(srvB.URL)
	require.Error(t, err, "a server holding a leaf from an UNCONFIGURED CA was accepted — then the client trusts everything and configuring a bundle proves nothing")

	var unknown x509.UnknownAuthorityError
	var verify *tls.CertificateVerificationError
	require.True(t, errors.As(err, &unknown) || errors.As(err, &verify),
		"expected a certificate-verification failure, got %v", err)
}

func TestAdminHopClient_UnreadableAnchor_RefusesToStart(t *testing.T) {
	_, err := newAdminHopClient(filepath.Join(t.TempDir(), "absent.crt"), time.Second)
	require.Error(t, err, "a trust anchor that cannot be read must refuse to start rather than fall back to the system roots")
	require.Contains(t, err.Error(), adminHopCAEnv,
		"the refusal must name the knob, so the stand can be fixed without reading the source")
}

func TestAdminHopClient_FileHoldingNoCertificate_RefusesToStart(t *testing.T) {
	junk := filepath.Join(t.TempDir(), "ca.crt")
	require.NoError(t, os.WriteFile(junk, []byte("this is not a PEM bundle\n"), 0o600))

	_, err := newAdminHopClient(junk, time.Second)
	require.Error(t, err, "a file holding no certificate must refuse to start: AppendCertsFromPEM reports failure by returning false, and the resulting empty pool trusts NOTHING while looking configured")
	require.Contains(t, err.Error(), adminHopCAEnv, "the refusal must name the knob")
}

// tlsServerSignedBy starts an HTTPS test server whose leaf is signed by ca.
func tlsServerSignedBy(t *testing.T, ca *testCA) *httptest.Server {
	t.Helper()
	certFile, keyFile := ca.issueLeaf(t, leafOpts{
		commonName:  "hydra-admin.kacho.svc.cluster.local",
		dnsNames:    []string{"hydra-admin.kacho.svc.cluster.local"},
		ipAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		isServer:    true,
	})
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)

	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	s.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	s.StartTLS()
	return s
}
