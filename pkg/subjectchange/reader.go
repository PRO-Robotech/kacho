// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subjectchange

import (
	"context"
	"fmt"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"google.golang.org/grpc"
)

// Reader — адаптер над порождённым клиентом, исполняющий [Poller].
//
// Единственное место пакета, говорящее по gRPC: остальное — курсор и решение
// «гасить или нет», и оно не должно знать транспорта.
type Reader struct {
	client iamv1.InternalIAMServiceClient
}

// NewReader навешивает адаптер на УЖЕ ОТКРЫТОЕ соединение к внутреннему
// слушателю владельца прав. Своего соединения не открывает: адрес владельца
// потребитель объявляет один раз, и второе объявление того же адреса разошлось
// бы с первым молча.
func NewReader(cc grpc.ClientConnInterface) *Reader {
	return &Reader{client: iamv1.NewInternalIAMServiceClient(cc)}
}

// PollSubjectChanges читает журнал владельца с позиции since, отдавая
// идентификаторы строк и голову журнала.
func (p *Reader) PollSubjectChanges(ctx context.Context, since int64) ([]int64, int64, error) {
	resp, err := p.client.PollSubjectChanges(ctx, &iamv1.PollSubjectChangesRequest{
		SinceId: since,
		Limit:   1000,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("poll subject changes: %w", err)
	}
	ids := make([]int64, 0, len(resp.GetChanges()))
	for _, c := range resp.GetChanges() {
		ids = append(ids, c.GetId())
	}
	return ids, resp.GetHeadId(), nil
}
