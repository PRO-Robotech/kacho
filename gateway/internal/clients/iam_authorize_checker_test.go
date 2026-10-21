// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients_test

import (
	"context"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/clients"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// recordingAuthorizeClient captures the AuthorizeCheckInput the adapter forwards
// to the inner gRPC client, so we can assert field pass-through.
type recordingAuthorizeClient struct {
	got clients.AuthorizeCheckInput
}

func (r *recordingAuthorizeClient) Check(_ context.Context, in clients.AuthorizeCheckInput) (clients.AuthorizeCheckResult, error) {
	r.got = in
	return clients.AuthorizeCheckResult{Allowed: true}, nil
}

func (r *recordingAuthorizeClient) Close() error { return nil }

// TestAuthzChecker_ForwardsRequiredRelation закрепляет дефект: переходник
// middleware.AuthzCheckInput → clients.AuthorizeCheckInput терял
// RequiredRelation. Без него служба прав выводит отношение из глагола, и RPC
// только для администратора (required_relation=system_admin) с глаголом
// `list`/`get` получает `viewer` и проходит по системной выдаче
// `cluster.viewer=system_class:authenticated` — дыра повышения привилегий, — а
// глаголы вне CRUD (issue/grant/…) отказывают закрыто с "does not resolve to a
// known relation".
func TestAuthzChecker_ForwardsRequiredRelation(t *testing.T) {
	rec := &recordingAuthorizeClient{}
	checker := clients.NewAuthzChecker(rec)

	_, err := checker.Check(context.Background(), middleware.AuthzCheckInput{
		Subject:          "user:usr_abc",
		Action:           "vpc.address_pools.list",
		RequiredRelation: "system_admin",
		ResourceType:     "cluster",
		ResourceID:       "cluster_root",
		TraceID:          "trace-1",
	})
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}

	if rec.got.RequiredRelation != "system_admin" {
		t.Errorf("RequiredRelation not forwarded: got %q, want %q", rec.got.RequiredRelation, "system_admin")
	}
	// Guard the rest of the fields stay wired too (regression net for the adapter).
	if rec.got.Subject != "user:usr_abc" || rec.got.Action != "vpc.address_pools.list" ||
		rec.got.ResourceType != "cluster" || rec.got.ResourceID != "cluster_root" ||
		rec.got.TraceID != "trace-1" {
		t.Errorf("adapter dropped a field: %+v", rec.got)
	}
}
