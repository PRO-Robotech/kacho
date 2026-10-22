// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// Register крепит /me литералом (его читает проба консоли api-path-surface),
// а прочие читатели края — константой SessionIdentityPath. Разойдись они, и
// перечень обхода полосы входа назвал бы путь, которого маршрутизатор не
// ведёт: проба держит литерал и константу одним путём.
func TestSessionIdentityRouteIsTheDeclaredPath(t *testing.T) {
	mux := http.NewServeMux()
	middleware.NewSessionIdentityHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).Register(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, middleware.SessionIdentityPath, nil))
	if w.Code != http.StatusOK || w.Body.String() != `{"user":null}` {
		t.Fatalf("путь SessionIdentityPath не ведёт маршрут /me: %d %q", w.Code, w.Body.String())
	}
}
