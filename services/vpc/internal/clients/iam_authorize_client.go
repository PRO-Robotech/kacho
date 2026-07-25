// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

import (
	"context"

	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/authzfilter"
)

// NewIAMAuthorizeClient оборачивает gRPC conn в authzfilter.AuthorizeClient.
// conn указывает на listener kacho-iam AuthorizeService (публичная проекция authz-слоя).
// Используется per-page фильтром видимости List.
func NewIAMAuthorizeClient(conn grpc.ClientConnInterface) authzfilter.AuthorizeClient {
	return &grpcAuthorizeClient{cli: iamv1.NewAuthorizeServiceClient(conn)}
}

type grpcAuthorizeClient struct {
	cli iamv1.AuthorizeServiceClient
}

// BatchCheck пробрасывает request в kacho-iam AuthorizeService.
//
// outgoing ctx обернут `auth.PropagateOutgoing`, чтобы principal-extract на стороне
// iam увидел реального caller'а, а не SystemPrincipal. Без wrap'а IAM-authzguard'ы
// видят "system:bootstrap"; inner-гейт `authorizeCaller` пропускает self-query
// (caller спрашивает про СЕБЯ), поэтому реальный principal обязателен — иначе
// фильтр вернул бы 403/Unavailable для всех subject'ов.
func (g *grpcAuthorizeClient) BatchCheck(ctx context.Context, req *iamv1.BatchAuthorizeCheckRequest, opts ...grpc.CallOption) (*iamv1.BatchAuthorizeCheckResponse, error) {
	return g.cli.BatchCheck(auth.PropagateOutgoing(ctx), req, opts...)
}
