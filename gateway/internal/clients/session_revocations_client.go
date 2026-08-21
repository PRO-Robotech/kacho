// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package clients — adapter: wraps the generated
// `InternalSessionRevocationsServiceClient` so the handler/logout package can
// depend on a narrow port-interface (`handler.SessionRevocationsClient`)
// instead of the full proto stub. Clean Architecture: adapter is the only
// place that talks gRPC.
package clients

import (
	"context"

	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
)

// SessionRevocationsAdapter wraps the generated gRPC client to satisfy
// handler.SessionRevocationsClient. The Operation result is discarded —
// callers care only about success/failure of the synchronous DB write.
type SessionRevocationsAdapter struct {
	client iamv1.InternalSessionRevocationsServiceClient
}

// NewSessionRevocationsAdapter wires the adapter onto an existing gRPC
// connection to kacho-iam:9091.
func NewSessionRevocationsAdapter(cc grpc.ClientConnInterface) *SessionRevocationsAdapter {
	return &SessionRevocationsAdapter{
		client: iamv1.NewInternalSessionRevocationsServiceClient(cc),
	}
}

// Revoke invokes InternalSessionRevocationsService.Revoke and discards the
// Operation envelope. Returns the underlying gRPC error unchanged — the
// handler caller is responsible for mapping it to a user-visible warning.
func (a *SessionRevocationsAdapter) Revoke(ctx context.Context, in *iamv1.RevokeRequest) error {
	_, err := a.client.Revoke(ctx, in)
	return err
}

// IsSessionRevoked asks kacho-iam whether the credential with this identifier
// has been revoked in OUR record — the one written by sign-out and by the
// administrative force-logout.
//
// ЗАЧЕМ ОТДЕЛЬНЫЙ ВОПРОС, ЕСЛИ КРАЙ УЖЕ СПРАШИВАЕТ ПРОВАЙДЕРА. Провайдер знает
// о своих отзывах и об истечении срока; о записи, которую делаем МЫ, он не знает
// и знать не может. До появления этого вызывающего наш отзыв не участвовал в
// решении на пути запроса вовсе: запись писалась и читалась только
// административными путями (#797).
//
// Ошибка возвращается БЕЗ ПОДМЕНЫ: «спросить не удалось» и «отозван» — разные
// исходы, и вызывающий (middleware.LocalThenProviderRevocation) обязан их
// различать, иначе недоступность соседа читалась бы как отзыв, а отзыв — как
// недоступность.
func (a *SessionRevocationsAdapter) IsSessionRevoked(ctx context.Context, jti string) (bool, error) {
	resp, err := a.client.IsRevoked(ctx, &iamv1.IsRevokedRequest{TokenJti: jti})
	if err != nil {
		return false, err
	}
	return resp.GetRevoked(), nil
}
