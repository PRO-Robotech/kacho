// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

import (
	"context"

	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
)

// NewIAMAuthorizeClient оборачивает gRPC conn в AuthorizeClient. conn указывает на
// kacho-iam, где живёт AuthorizeService (BatchCheck — публичный read-RPC,
// зарегистрирован и на internal listener). См. cmd composition root.
func NewIAMAuthorizeClient(conn grpc.ClientConnInterface) AuthorizeClient {
	return &grpcAuthorizeClient{cli: iamv1.NewAuthorizeServiceClient(conn)}
}

type grpcAuthorizeClient struct {
	cli iamv1.AuthorizeServiceClient
}

// BatchCheck пробрасывает request в kacho-iam AuthorizeService.
//
// outgoing ctx обёрнут auth.PropagateOutgoing, чтобы iam-side
// grpcsrv.UnaryPrincipalExtract увидел РЕАЛЬНОГО caller'а (а не SystemPrincipal =
// user:bootstrap). Без wrap'а iam authzguard видит "system:bootstrap" и отбивает
// запрос → nlb list-filter возвращал бы 403/Unavailable для всех user'ов
// (inner-гейт пропускает self-query — caller спрашивает про СЕБЯ, поэтому реальный
// principal обязателен).
func (g *grpcAuthorizeClient) BatchCheck(ctx context.Context, req *iamv1.BatchAuthorizeCheckRequest, opts ...grpc.CallOption) (*iamv1.BatchAuthorizeCheckResponse, error) {
	return g.cli.BatchCheck(auth.PropagateOutgoing(ctx), req, opts...)
}
