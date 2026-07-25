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
// kacho-iam internal-порт (:9091) — там живёт AuthorizeService.
func NewIAMAuthorizeClient(conn grpc.ClientConnInterface) AuthorizeClient {
	return &grpcAuthorizeClient{cli: iamv1.NewAuthorizeServiceClient(conn)}
}

type grpcAuthorizeClient struct {
	cli iamv1.AuthorizeServiceClient
}

// BatchCheck пробрасывает request в kacho-iam AuthorizeService.
//
// outgoing ctx обёрнут `auth.PropagateOutgoing`, чтобы iam-side
// `grpcsrv.UnaryPrincipalExtract` увидел реального caller'а, а не
// SystemPrincipal() = user:bootstrap. Без wrap'а iam-authzguard'ы видят
// "system:bootstrap" и отбивают вызов, из-за чего list-filter вернул бы 403 всем
// пользователям независимо от их FGA-tuple'ов (тот же дефект уже ловили в compute).
func (g *grpcAuthorizeClient) BatchCheck(ctx context.Context, req *iamv1.BatchAuthorizeCheckRequest, opts ...grpc.CallOption) (*iamv1.BatchAuthorizeCheckResponse, error) {
	return g.cli.BatchCheck(auth.PropagateOutgoing(ctx), req, opts...)
}
