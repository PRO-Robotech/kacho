// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

// proxy_authn_test.go — форвардер обязан предъявляться хранилищу слоёв ЗА СВОЙ
// счёт, а не за счёт вызывающего.
//
// Две вещи должны держаться одновременно, и они легко ломают друг друга:
// предъявитель ВЫЗЫВАЮЩЕГО в хранилище не уезжает (его права уже энфорснуты
// выше, а осевший в чужих логах Bearer расширяет harvest-поверхность), и при
// этом запрос не приходит анонимным — иначе хранилище с включённой
// аутентификацией отвергнет весь трафик push/pull.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDataplane_Forward_PresentsOwnCredentials — форвардер ставит СВОИ учётные
// данные и снимает предъявителя вызывающего.
func TestDataplane_Forward_PresentsOwnCredentials(t *testing.T) {
	var sawUser, sawPass string
	var sawAuthHeader string
	zot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuthHeader = r.Header.Get("Authorization")
		u, p, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="zot"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		sawUser, sawPass = u, p
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer zot.Close()

	fw, err := NewZotForwarder(zot.URL, nil, WithZotBasicAuth("kacho-registry", "layer-store-credential"))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/v2/reg-A/app/manifests/v1", nil)
	req.Header.Set("Authorization", "Bearer caller-identity-jwt")
	rec := httptest.NewRecorder()
	status := fw.Forward(rec, req)

	require.Equal(t, http.StatusOK, status, "the store refused the forwarded request")
	require.Equal(t, "kacho-registry", sawUser)
	require.Equal(t, "layer-store-credential", sawPass)
	require.NotContains(t, sawAuthHeader, "caller-identity-jwt",
		"the caller's bearer must never reach the layer store")
	body, _ := io.ReadAll(rec.Result().Body)
	require.Equal(t, "ok", string(body))
}

// TestDataplane_ForwardCapture_PresentsOwnCredentials — буферизующий путь
// (blob-finalize) использует тот же прокси, поэтому обязан нести те же учётные
// данные; забыть одну из двух функций — типовой промах.
func TestDataplane_ForwardCapture_PresentsOwnCredentials(t *testing.T) {
	zot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "kacho-registry" || p != "layer-store-credential" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer zot.Close()

	fw, err := NewZotForwarder(zot.URL, nil, WithZotBasicAuth("kacho-registry", "layer-store-credential"))
	require.NoError(t, err)

	got := fw.ForwardCapture(httptest.NewRequest(http.MethodPut,
		"/v2/reg-A/app/blobs/uploads/u1?digest=sha256:abc", nil))
	require.Equal(t, http.StatusCreated, got.Status)
}
