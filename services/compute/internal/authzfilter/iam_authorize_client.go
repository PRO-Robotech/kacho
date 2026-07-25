// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

import (
	"context"

	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
)

// NewIAMAuthorizeClient оборачивает gRPC conn в AuthorizeClient.
// conn обычно указывает на kacho-iam internal-port (:9091) — там живёт
// AuthorizeService.
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
// SystemPrincipal() = user:bootstrap. Без wrap'а IAM authzguard'ы видели
// "system:bootstrap" и отбивали вызов как
// "authz_anonymous_mutation_denied" → compute list-filter возвращал 403
// для всех user'ов независимо от их FGA-tuple'ов.
func (g *grpcAuthorizeClient) BatchCheck(ctx context.Context, req *iamv1.BatchAuthorizeCheckRequest, opts ...grpc.CallOption) (*iamv1.BatchAuthorizeCheckResponse, error) {
	return g.cli.BatchCheck(auth.PropagateOutgoing(ctx), req, opts...)
}
