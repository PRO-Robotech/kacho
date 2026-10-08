// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package kanameclient — клиент kaname `InternalNotificationGrantService/
// ResolveSend` на стороне notify: реализация порта grant.Peer (ребро
// notify → kaname, §9 замысла NTF-1). Решений здесь нет: перевод ответа в
// решение порта и статуса вызова — как есть. Классифицирует ответ путь права
// (grant, peeranswer); срок вызова ставит он же (Policy.CallTimeout).
package kanameclient

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/services/notify/internal/grant"
)

// resolver — часть клиента kaname, которой пользуется notify.
type resolver interface {
	ResolveSend(ctx context.Context, in *iamv1.ResolveSendRequest, opts ...grpc.CallOption) (*iamv1.ResolveSendResponse, error)
}

// Client — порт grant.Peer над соединением с внутренним слушателем kaname.
type Client struct {
	c resolver
}

var _ grant.Peer = (*Client)(nil)

// New — клиент над соединением cc (mTLS с точным SAN kaname — у вызывающего).
func New(cc grpc.ClientConnInterface) *Client {
	return newWith(iamv1.NewInternalNotificationGrantServiceClient(cc))
}

func newWith(c resolver) *Client { return &Client{c: c} }

// ResolveSend — один вызов. Момент постановки уходит в полной точности (не
// усекается). Ошибка — статус gRPC вызова как есть; решение вне трёх значений
// контракта — grant.DecisionUnset.
func (c *Client) ResolveSend(ctx context.Context, q grant.Query) (grant.Decision, error) {
	resp, err := c.c.ResolveSend(ctx, &iamv1.ResolveSendRequest{
		Namespace:  q.Namespace,
		Template:   q.Template,
		EnqueuedAt: timestamppb.New(q.EnqueuedAt),
	})
	if err != nil {
		return grant.DecisionUnset, err
	}
	switch resp.GetDecision() {
	case iamv1.SendDecision_ALLOW:
		return grant.DecisionAllow, nil
	case iamv1.SendDecision_NOT_YET_GRANTED:
		return grant.DecisionNotYetGranted, nil
	case iamv1.SendDecision_REVOKED:
		return grant.DecisionRevoked, nil
	case iamv1.SendDecision_SEND_DECISION_UNSPECIFIED:
		return grant.DecisionUnset, nil
	}
	return grant.DecisionUnset, nil
}
