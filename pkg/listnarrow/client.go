// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package listnarrow

import (
	"context"

	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
)

// NewAuthorizeClient оборачивает соединение с kaname в порт сужателя. conn
// указывает на kaname, где живёт `AuthorizeService` (`BatchCheck` — публичный
// read-RPC, зарегистрирован и на внутреннем листенере).
func NewAuthorizeClient(conn grpc.ClientConnInterface) AuthorizeClient {
	return &grpcAuthorizeClient{cli: iamv1.NewAuthorizeServiceClient(conn)}
}

type grpcAuthorizeClient struct {
	cli iamv1.AuthorizeServiceClient
}

// BatchCheck пробрасывает запрос в kaname.
//
// Исходящий контекст обёрнут auth.PropagateOutgoing, чтобы извлекатель личности на
// стороне kaname увидел РЕАЛЬНОГО вызывающего, а не запасное значение уровня
// начальной загрузки. Без обёртки внутренний страж kaname видит служебную
// личность и отбивает запрос: сужатель возвращал бы отказ каждому пользователю —
// страж пропускает вопрос субъекта О СЕБЕ, поэтому настоящий принципал обязателен.
func (g *grpcAuthorizeClient) BatchCheck(ctx context.Context, req *iamv1.BatchAuthorizeCheckRequest,
	opts ...grpc.CallOption) (*iamv1.BatchAuthorizeCheckResponse, error) {
	return g.cli.BatchCheck(auth.PropagateOutgoing(ctx), req, opts...)
}
