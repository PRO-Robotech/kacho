// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package kanameclient

// Перевод ответа kaname `ResolveSend` в решение порта grant.Peer: три решения
// контракта — свои значения; нуль и значение вне перечня (расхождение версий
// контракта) — grant.DecisionUnset, который путь права судит отказом, а не
// решением о письме. Вопрос несёт пространство, шаблон и момент постановки в
// полной точности.

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/services/notify/internal/grant"
)

type stub struct {
	resp *iamv1.ResolveSendResponse
	err  error
	got  *iamv1.ResolveSendRequest
}

func (s *stub) ResolveSend(_ context.Context, in *iamv1.ResolveSendRequest, _ ...grpc.CallOption) (*iamv1.ResolveSendResponse, error) {
	s.got = in
	return s.resp, s.err
}

func TestResolveSendMapsEveryDecision(t *testing.T) {
	at := time.Date(2026, 10, 6, 12, 0, 0, 123456789, time.UTC)
	for _, c := range []struct {
		in   iamv1.SendDecision
		want grant.Decision
	}{
		{iamv1.SendDecision_ALLOW, grant.DecisionAllow},
		{iamv1.SendDecision_NOT_YET_GRANTED, grant.DecisionNotYetGranted},
		{iamv1.SendDecision_REVOKED, grant.DecisionRevoked},
		{iamv1.SendDecision_SEND_DECISION_UNSPECIFIED, grant.DecisionUnset},
		{iamv1.SendDecision(42), grant.DecisionUnset},
	} {
		s := &stub{resp: &iamv1.ResolveSendResponse{Decision: c.in}}
		got, err := newWith(s).ResolveSend(context.Background(), grant.Query{Namespace: "notify-probe", Template: "probe-hello", EnqueuedAt: at})
		if err != nil || got != c.want {
			t.Fatalf("%v → (%v, %v), ожидалось %v", c.in, got, err, c.want)
		}
		if s.got.GetNamespace() != "notify-probe" || s.got.GetTemplate() != "probe-hello" || !s.got.GetEnqueuedAt().AsTime().Equal(at) {
			t.Fatalf("вопрос %v не несёт пространство, шаблон и момент полной точности", s.got)
		}
	}
}

func TestResolveSendPassesTheCallStatus(t *testing.T) {
	want := status.Error(codes.Unavailable, "notification grant service temporarily unavailable")
	_, err := newWith(&stub{err: want}).ResolveSend(context.Background(), grant.Query{Namespace: "n", Template: "t", EnqueuedAt: time.Now()})
	if !errors.Is(err, want) || status.Code(err) != codes.Unavailable {
		t.Fatalf("статус вызова не дошёл до пути права как есть: %v", err)
	}
}
