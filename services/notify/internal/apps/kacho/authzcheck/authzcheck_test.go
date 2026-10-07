// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzcheck

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/corelib/authz"
	"github.com/PRO-Robotech/corelib/operations"
)

func asUser(id string) context.Context {
	return operations.WithPrincipal(context.Background(), operations.Principal{Type: "user", ID: id})
}

func reasons(err error) []string {
	st, _ := status.FromError(err)
	var out []string
	for _, d := range st.Details() {
		if ei, ok := d.(*errdetails.ErrorInfo); ok {
			out = append(out, ei.GetReason())
		}
	}
	return out
}

// TestRequireScope_OneQuestionThreeOutcomes — один вопрос о субъекте и
// области; «да» — проход, «нет» — PERMISSION_DENIED, отказ вызова —
// UNAVAILABLE с PEER_UNAVAILABLE (вердикт из ошибки не выводится).
func TestRequireScope_OneQuestionThreeOutcomes(t *testing.T) {
	t.Parallel()
	var asked []string
	answer := func(ok bool, err error) authz.CheckClient {
		return authz.CheckClientFunc(func(_ context.Context, s, r, o string) (bool, error) {
			asked = append(asked, s+" "+r+" "+o)
			return ok, err
		})
	}
	scope := Scope{Type: "project", ID: "prj-1"}
	if err := RequireScope(asUser("usr-c"), answer(true, nil), "v_get", scope); err != nil {
		t.Fatalf("«да»: %v", err)
	}
	if len(asked) != 1 || asked[0] != "user:usr-c v_get project:prj-1" {
		t.Fatalf("вопросы %v", asked)
	}
	if err := RequireScope(asUser("usr-c"), answer(false, nil), "v_get", scope); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("«нет»: %v", err)
	}
	err := RequireScope(asUser("usr-c"), answer(true, errors.New("unreachable")), "v_get", scope)
	if status.Code(err) != codes.Unavailable || len(reasons(err)) != 1 || reasons(err)[0] != "PEER_UNAVAILABLE" {
		t.Fatalf("отказ вызова: %v %v", err, reasons(err))
	}
}

// TestRequireScope_UnnamedCallerAsksNothing — без пересланного принципала
// вопрос не задаётся вовсе.
func TestRequireScope_UnnamedCallerAsksNothing(t *testing.T) {
	t.Parallel()
	called := false
	c := authz.CheckClientFunc(func(context.Context, string, string, string) (bool, error) {
		called = true
		return true, nil
	})
	err := RequireScope(context.Background(), c, "v_get", Scope{Type: "account", ID: "acc-1"})
	if status.Code(err) != codes.Unauthenticated || called {
		t.Fatalf("отказ %v, вопрос задан=%v", err, called)
	}
}
