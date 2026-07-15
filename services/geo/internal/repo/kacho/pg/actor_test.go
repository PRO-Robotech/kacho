// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// TestActorFromCtx_present — установленный principal форматируется как
// "<type>:<id>" в audit-payload.
func TestActorFromCtx_present(t *testing.T) {
	ctx := operations.WithPrincipal(context.Background(), operations.Principal{Type: "user", ID: "usr-1"})
	if got := actorFromCtx(ctx); got != "user:usr-1" {
		t.Fatalf("actorFromCtx = %q, want %q", got, "user:usr-1")
	}
}

// TestActorFromCtx_noAuth_systemStub — пустой ctx → PrincipalFromContext отдаёт
// SystemPrincipal (system:bootstrap), а не пустую атрибуцию.
func TestActorFromCtx_noAuth_systemStub(t *testing.T) {
	if got := actorFromCtx(context.Background()); got != "system:bootstrap" {
		t.Fatalf("actorFromCtx(empty) = %q, want %q", got, "system:bootstrap")
	}
}

// TestActorFromCtx_emptyPrincipal_sentinel — явно установленный principal с
// пустым ID (misconfig / потерянная атрибуция) НЕ пишется в audit пустой строкой:
// возвращается наблюдаемый sentinel, чтобы утрата атрибуции была видна в самой
// audit-строке geo_outbox, а не молча blank (CWE-778).
func TestActorFromCtx_emptyPrincipal_sentinel(t *testing.T) {
	ctx := operations.WithPrincipal(context.Background(), operations.Principal{})
	if got := actorFromCtx(ctx); got != actorUnknown {
		t.Fatalf("actorFromCtx(emptyPrincipal) = %q, want sentinel %q", got, actorUnknown)
	}
}
