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

// testHopCAEnv — имя ручки в текстах отказа, поданное общей реализации в пробе.
// Пробы судят ОДНУ реализацию, которой пользуются все хопы края
// (`newPinnedHopClient`); имя ручки у неё параметр.
const testHopCAEnv = "KACHO_TEST_HOP_CA_FILE"

// These tests pin the capability at the level an operator experiences it:
//   - no trust anchor configured → the caller still gets a bounded client, and
//     one with no custom transport;
//   - trust anchor configured → a handshake against a server holding a leaf
//     from THAT CA succeeds, and one from a different CA still fails. Asserting
//     only the first half would pass equally for a client that verifies nothing;
//   - trust anchor unreadable / not a bundle → REFUSE TO START. Falling back to
//     the system roots is the worst outcome here: the operator believes the hop
//     is verified against the internal CA while it is not, and nothing shows it
//     until a certificate rotates.

func TestPinnedHopClient_NoTrustAnchor_StaysOnThePlainBoundedClient(t *testing.T) {
	c, err := newPinnedHopClient(testHopCAEnv, "", 1500*time.Millisecond)
	require.NoError(t, err, "no trust anchor configured is not an error — a stand may legitimately have a plaintext in-cluster hop")
	require.NotNil(t, c, "the client must never be nil: a nil client sends the hop through http.DefaultClient, which has no timeout at all")
	require.Equal(t, 1500*time.Millisecond, c.Timeout, "per-call budget must be applied")
	require.Nil(t, c.Transport, "without a trust anchor no custom transport may be installed")
}

func TestPinnedHopClient_TrustAnchor_VerifiesThatCAAndRejectsAnother(t *testing.T) {
	caA := newTestCA(t, "internal-ca-A")
	caB := newTestCA(t, "internal-ca-B")

	c, err := newPinnedHopClient(testHopCAEnv, caA.caFile(t), time.Second)
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

func TestPinnedHopClient_UnreadableAnchor_RefusesToStart(t *testing.T) {
	_, err := newPinnedHopClient(testHopCAEnv, filepath.Join(t.TempDir(), "absent.crt"), time.Second)
	require.Error(t, err, "a trust anchor that cannot be read must refuse to start rather than fall back to the system roots")
	require.Contains(t, err.Error(), testHopCAEnv,
		"the refusal must name the knob, so the stand can be fixed without reading the source")
}

func TestPinnedHopClient_FileHoldingNoCertificate_RefusesToStart(t *testing.T) {
	junk := filepath.Join(t.TempDir(), "ca.crt")
	require.NoError(t, os.WriteFile(junk, []byte("this is not a PEM bundle\n"), 0o600))

	_, err := newPinnedHopClient(testHopCAEnv, junk, time.Second)
	require.Error(t, err, "a file holding no certificate must refuse to start: AppendCertsFromPEM reports failure by returning false, and the resulting empty pool trusts NOTHING while looking configured")
	require.Contains(t, err.Error(), testHopCAEnv, "the refusal must name the knob")
}

// tlsServerSignedBy starts an HTTPS test server whose leaf is signed by ca.
func tlsServerSignedBy(t *testing.T, ca *testCA) *httptest.Server {
	t.Helper()
	certFile, keyFile := ca.issueLeaf(t, leafOpts{
		commonName:  "authority.kacho.svc",
		dnsNames:    []string{"authority.kacho.svc"},
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
