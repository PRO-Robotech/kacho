// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

import (
	"context"
	stderrors "errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/cache"
)

// fakeSubjectStub is a deterministic subjectLookupStub. lookupFn is invoked per
// call so a test can vary the response across calls.
type fakeSubjectStub struct {
	calls    atomic.Int32
	lookupFn func(n int32, in *iamv1.LookupSubjectRequest) (*iamv1.LookupSubjectResponse, error)
}

func (f *fakeSubjectStub) LookupSubject(_ context.Context, in *iamv1.LookupSubjectRequest, _ ...grpc.CallOption) (*iamv1.LookupSubjectResponse, error) {
	n := f.calls.Add(1)
	return f.lookupFn(n, in)
}

func (f *fakeSubjectStub) Check(context.Context, *iamv1.CheckRequest, ...grpc.CallOption) (*iamv1.CheckResponse, error) {
	return &iamv1.CheckResponse{}, nil
}

// newTestClient builds an IAMSubjectClient wired to the given fake.
func newTestClient(stub subjectLookupStub) *IAMSubjectClient {
	return &IAMSubjectClient{
		stub:        stub,
		cache:       cache.NewSubjectCache(1000, 30*time.Second, nil),
		logger:      slog.Default(),
		callTimeout: 5 * time.Second,
	}
}

func userResp(id, email, display string) *iamv1.LookupSubjectResponse {
	return &iamv1.LookupSubjectResponse{
		Subject: &iamv1.LookupSubjectResponse_User{
			User: &iamv1.User{Id: id, Email: email, DisplayName: display},
		},
	}
}

// TestLookupByExternalID_UserOneof — a User oneof maps to a user Subject with a
// display-name falling back to email.
func TestLookupByExternalID_UserOneof(t *testing.T) {
	c := newTestClient(&fakeSubjectStub{lookupFn: func(int32, *iamv1.LookupSubjectRequest) (*iamv1.LookupSubjectResponse, error) {
		return userResp("usr_1", "a@b.co", ""), nil
	}})

	subj, err := c.LookupByExternalID(context.Background(), "ext-1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if subj.Type != "user" || subj.ID != "usr_1" || subj.DisplayName != "a@b.co" {
		t.Fatalf("got %+v, want user/usr_1/a@b.co (display falls back to email)", subj)
	}
}

// TestLookupByExternalID_ServiceAccountOneof — a ServiceAccount oneof maps to a
// service_account Subject.
func TestLookupByExternalID_ServiceAccountOneof(t *testing.T) {
	c := newTestClient(&fakeSubjectStub{lookupFn: func(int32, *iamv1.LookupSubjectRequest) (*iamv1.LookupSubjectResponse, error) {
		return &iamv1.LookupSubjectResponse{Subject: &iamv1.LookupSubjectResponse_ServiceAccount{
			ServiceAccount: &iamv1.ServiceAccount{Id: "sva_1", Name: "ci-bot"},
		}}, nil
	}})

	subj, err := c.LookupByExternalID(context.Background(), "ext-2")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if subj.Type != "service_account" || subj.ID != "sva_1" || subj.DisplayName != "ci-bot" {
		t.Fatalf("got %+v, want service_account/sva_1/ci-bot", subj)
	}
}

// TestLookupByExternalID_UnexpectedOneof — an empty oneof is a hard error (no
// silent anonymous downgrade).
func TestLookupByExternalID_UnexpectedOneof(t *testing.T) {
	c := newTestClient(&fakeSubjectStub{lookupFn: func(int32, *iamv1.LookupSubjectRequest) (*iamv1.LookupSubjectResponse, error) {
		return &iamv1.LookupSubjectResponse{}, nil // nil Subject oneof
	}})

	_, err := c.LookupByExternalID(context.Background(), "ext-3")
	if err == nil {
		t.Fatal("empty subject oneof must error")
	}
}

// TestLookupByExternalID_NotFound_WrapsSentinel — a NotFound from iam must be a
// wrapped errSubjectNotFound so errors.Is matches (the reword-safe classifier).
// This FAILS before the %w fix.
func TestLookupByExternalID_NotFound_WrapsSentinel(t *testing.T) {
	c := newTestClient(&fakeSubjectStub{lookupFn: func(int32, *iamv1.LookupSubjectRequest) (*iamv1.LookupSubjectResponse, error) {
		return nil, status.Error(codes.NotFound, "no such subject")
	}})

	_, err := c.LookupByExternalID(context.Background(), "ext-missing")
	if err == nil {
		t.Fatal("expected NotFound error")
	}
	if !stderrors.Is(err, errSubjectNotFound) {
		t.Fatalf("NotFound error must wrap errSubjectNotFound (errors.Is), got %v", err)
	}
}

// deadlineCapturingStub records the ctx deadline observed by LookupSubject and
// Check so a test can assert both sibling RPCs derive their per-call deadline
// from the same configured timeout (architecture.md per-call-deadline invariant:
// "все sibling-методы клиента обязаны применять один и тот же configured-timeout").
type deadlineCapturingStub struct {
	lookupBudget time.Duration
	checkBudget  time.Duration
}

func (s *deadlineCapturingStub) LookupSubject(ctx context.Context, _ *iamv1.LookupSubjectRequest, _ ...grpc.CallOption) (*iamv1.LookupSubjectResponse, error) {
	if dl, ok := ctx.Deadline(); ok {
		s.lookupBudget = time.Until(dl)
	}
	return userResp("usr_1", "a@b.co", ""), nil
}

func (s *deadlineCapturingStub) Check(ctx context.Context, _ *iamv1.CheckRequest, _ ...grpc.CallOption) (*iamv1.CheckResponse, error) {
	if dl, ok := ctx.Deadline(); ok {
		s.checkBudget = time.Until(dl)
	}
	return &iamv1.CheckResponse{Allowed: true}, nil
}

// TestSiblingMethods_ShareSameCallTimeout — LookupByExternalID and IsSystemAdmin
// are sibling methods of the same client and MUST apply the same per-call
// deadline. Before the fix IsSystemAdmin hardcodes 3s while LookupByExternalID
// hardcodes 5s → the budgets diverge and this FAILS.
func TestSiblingMethods_ShareSameCallTimeout(t *testing.T) {
	stub := &deadlineCapturingStub{}
	c := newTestClient(stub)

	if _, err := c.LookupByExternalID(context.Background(), "ext-1"); err != nil {
		t.Fatalf("LookupByExternalID: unexpected err: %v", err)
	}
	if _, err := c.IsSystemAdmin(context.Background(), "user:usr_1"); err != nil {
		t.Fatalf("IsSystemAdmin: unexpected err: %v", err)
	}

	if stub.lookupBudget == 0 || stub.checkBudget == 0 {
		t.Fatalf("both RPCs must carry a per-call deadline, got lookup=%v check=%v",
			stub.lookupBudget, stub.checkBudget)
	}
	// Same configured source ⇒ budgets differ only by the microseconds elapsed
	// between the two WithTimeout calls, far below any hardcoded-divergence gap.
	if diff := stub.lookupBudget - stub.checkBudget; diff > 500*time.Millisecond || diff < -500*time.Millisecond {
		t.Fatalf("sibling methods must share one configured timeout; budgets diverge: lookup=%v check=%v (diff=%v)",
			stub.lookupBudget, stub.checkBudget, diff)
	}
}
