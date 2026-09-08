// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

// iam_subject_blocked_test.go — край не заводит личность, которой запрещено
// аутентифицироваться.
//
// «Не найдена» на этом пути означает «первый вход, заведи зеркало»: край идёт
// в провизионирование. Поэтому владельцу личности нельзя отвечать отсутствием
// там, где строка есть и запрещена — иначе отказ выглядит приглашением. Здесь
// проверяется вторая половина той же пары: получив вердикт о состоянии, край
// НЕ провизионирует и не выдаёт принципала.

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
)

// TestLookupOrUpsertFromKratos_BlockedIdentityIsNotProvisioned — вердикт о
// состоянии не должен попадать в ветку «заводим зеркало».
func TestLookupOrUpsertFromKratos_BlockedIdentityIsNotProvisioned(t *testing.T) {
	stub := &fakeSubjectStub{lookupFn: func(int32, *iamv1.LookupSubjectRequest) (*iamv1.LookupSubjectResponse, error) {
		return nil, status.Error(codes.FailedPrecondition, "identity zit-blocked is blocked")
	}}
	user := &fakeUserStub{}
	c := newTestClient(stub, user)

	_, err := c.LookupOrUpsertFromKratos(context.Background(), "zit-blocked", "blocked@example.com", "Blocked")
	if err == nil {
		t.Fatal("blocked identity must not resolve to a principal")
	}
	if n := user.calls.Load(); n != 0 {
		t.Fatalf("UpsertFromIdentity called %d time(s); a blocked identity must never be provisioned", n)
	}
	if n := stub.calls.Load(); n != 1 {
		t.Fatalf("LookupSubject called %d time(s); the refusal must be terminal, not retried as a miss", n)
	}
}

// TestLookupOrUpsertFromKratos_AbsentIdentityStillProvisions — контрольный
// случай той же формы: настоящее отсутствие обязано по-прежнему заводить
// зеркало, иначе первый вход перестал бы работать вовсе.
func TestLookupOrUpsertFromKratos_AbsentIdentityStillProvisions(t *testing.T) {
	stub := &fakeSubjectStub{lookupFn: func(n int32, _ *iamv1.LookupSubjectRequest) (*iamv1.LookupSubjectResponse, error) {
		if n == 1 {
			return nil, status.Error(codes.NotFound, "subject not found by external_id=zit-new")
		}
		return userResp("usr_new", "new@example.com", "New"), nil
	}}
	user := &fakeUserStub{}
	c := newTestClient(stub, user)

	subj, err := c.LookupOrUpsertFromKratos(context.Background(), "zit-new", "new@example.com", "New")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if subj.ID != "usr_new" {
		t.Fatalf("got %+v, want usr_new", subj)
	}
	if n := user.calls.Load(); n != 1 {
		t.Fatalf("UpsertFromIdentity called %d time(s), want 1", n)
	}
}
