// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package clients — adapter: wraps InternalIAMServiceClient to satisfy
// watcher.Poller. Clean Architecture: adapter is the only place that talks gRPC.
package clients

import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/gateway/internal/watcher"
	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz"
)

// SubjectChangePoller wraps the generated gRPC client to satisfy watcher.Poller.
type SubjectChangePoller struct {
	client iamv1.InternalIAMServiceClient
}

// NewSubjectChangePoller wires the adapter onto an existing gRPC connection to
// kacho-iam:9091 (backends["iamInternal"]). No new connection is opened.
func NewSubjectChangePoller(cc grpc.ClientConnInterface) *SubjectChangePoller {
	return &SubjectChangePoller{client: iamv1.NewInternalIAMServiceClient(cc)}
}

// PollSubjectChanges calls InternalIAMService.PollSubjectChanges with cursor
// since and the limit its CALLER named, returning the changes and the head cursor.
//
// Предел не объявляется здесь: по нему вызывающий решает, могла ли порция быть
// усечена, и объявить его в двух местах значило бы дать этому решению второе,
// расходящееся основание.
//
// # Почему имя субъекта едет дальше, а не остаётся здесь
//
// Прежде адаптер оставлял от строки ОДИН номер и выбрасывал имя субъекта. Для
// сброса кэша этого хватало: он и так сбрасывался целиком. Для закрытия
// открытого потока (kacho#1022) — нет: закрыть можно только НАЗВАННОГО, и
// реплика, до которой не дозвонился толчок iam, других имён не получает ниоткуда.
// То есть отзыв на такой реплике не имел бы действия вовсе.
func (p *SubjectChangePoller) PollSubjectChanges(
	ctx context.Context, since int64, limit int32,
) ([]watcher.SubjectChange, int64, error) {
	resp, err := p.client.PollSubjectChanges(ctx, &iamv1.PollSubjectChangesRequest{
		SinceId: since,
		Limit:   limit,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("poll subject changes: %w", err)
	}
	changes := make([]watcher.SubjectChange, 0, len(resp.GetChanges()))
	for _, c := range resp.GetChanges() {
		// Субъект собирается ТЕМ ЖЕ кодеком, которым его называет всякий, кто
		// спрашивает право (`authz.TenantSubject`), — а не конкатенацией здесь.
		// Ключ, под которым учтён открытый поток, обязан совпасть с именем
		// отзыва ПО ПОСТРОЕНИЮ: две похожие сборки строки разошлись бы молча, и
		// разошлись бы именно там, где расхождение не видно — обе непусты, обе
		// выглядят субъектом, а закрыть по второй нельзя ничего.
		//
		// Строка, чьего типа мы не знаем (записана до того, как производители
		// стали его проставлять), едет БЕЗ имени: она двигает курсор и никого не
		// закрывает. Выводить тип из написания идентификатора запрещено — этот
		// приём уже давал совпадение с тем, чего продукт не производит.
		subject, _ := authz.TenantSubject(c.GetSubjectType(), c.GetSubjectId())
		changes = append(changes, watcher.SubjectChange{ID: c.GetId(), Subject: subject})
	}
	return changes, resp.GetHeadId(), nil
}
