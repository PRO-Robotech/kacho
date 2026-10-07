// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package send — глагол пробы-источника `InternalNotifyProbeService/Send`
// (NTF-1, З29, решение Д75): поставить письмо шаблона `probe-hello` на адрес и
// ответить идентификатором поставленной строки ленты.
//
// # Ответ синхронный, а не Operation
//
// Постановка — одна транзакция (`feed.PutID` через порождённый SendProbeHello и
// строка журнала подписки в ней же); ответ отдаётся после коммита и равен
// закоммиченному, асинхронной работы после ответа нет. Исход доставки решает
// служба отправки, и виден он у приёмника и в её метриках.
//
// # Порядок
//
//  1. флаг доставки (`Source.DeliveryConfigured`, NTF1-N06): при выключенном —
//     единый отказ «доставка не настроена» до чтения адреса;
//  2. форма адреса: пустой — `address: required`, не нормализуемый — отказ с
//     именем поля и правилом, без значения;
//  3. транзакция помощника журнала, постановка, id своей строки, коммит.
//
// Пункты 1 и 2 к базе не обращаются.
//
// # Как узнаётся id своей строки
//
// Его отвечает постановка: порождённый SendProbeHello зовёт `feed.PutID`, и
// тот возвращает id строки, вставленной своим оператором. Лента не читается,
// имени её таблицы глагол не знает (NTF1-B19), уровень изоляции — умолчание
// пула. Конкурирующая постановка в той же ленте ответа не трогает: id выдан
// этой постановке, а не найден среди строк. Держат пробы
// TestSendIssuesOnlyTheFoundationStatements (операторы Send — ровно операторы
// постановки фундамента) и TestSendReturnsItsOwnIDUnderACompetingWrite.
//
// Постановка, не записавшая строки (флаг выключен у шаблона класса notice),
// id не несёт; у probe-hello флаг проверен в пункте 1, и такой исход —
// внутренняя ошибка, а не пустой id в ответе.
package send

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/corelib/journaltx"
	"github.com/PRO-Robotech/corelib/notify/address"
	"github.com/PRO-Robotech/corelib/notify/feed"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"
	notify "github.com/PRO-Robotech/kacho/services/notify"
)

// target — значение атрибута target письма: путь `/` — origin установки.
const target = "/"

// Server — служба InternalNotifyProbeService.
type Server struct {
	notifyv1.UnimplementedInternalNotifyProbeServiceServer
	db     journaltx.TxStarter
	source *feed.Source
	log    *slog.Logger
}

// New — служба над базой пробы db и источником ленты source; log — журнал
// корня (внутренняя ошибка уходит в журнал, наружу — только "internal error").
func New(db journaltx.TxStarter, source *feed.Source, log *slog.Logger) *Server {
	return &Server{db: db, source: source, log: log}
}

// Send ставит письмо probe-hello на адрес.
func (s *Server) Send(ctx context.Context, req *notifyv1.SendRequest) (*notifyv1.SendResponse, error) {
	if err := s.source.DeliveryConfigured(); err != nil {
		return nil, feed.DeliveryNotConfiguredStatus().Err()
	}
	addr := req.GetAddress()
	if addr == "" {
		return nil, status.Error(codes.InvalidArgument, "address: required")
	}
	if _, err := address.Normalize(addr); err != nil {
		// Текст фундамента уже начинается именем поля (`address: …`) и значения
		// адреса не несёт — отдаётся как есть, второго префикса нет.
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	ctx = s.source.Bind(ctx)
	id, err := s.put(ctx, addr)
	switch {
	case err == nil:
		return &notifyv1.SendResponse{NotificationId: id}, nil
	case errors.Is(err, feed.ErrDeliveryNotConfigured):
		return nil, feed.DeliveryNotConfiguredStatus().Err()
	case errors.Is(err, feed.ErrRecipientInvalid):
		return nil, status.Error(codes.InvalidArgument, "address: recipient is invalid")
	}
	s.log.ErrorContext(ctx, "notify-probe Send: постановка не удалась", "err", err)
	return nil, status.Error(codes.Internal, "internal error")
}

// put — транзакция постановки: строка ленты и строка журнала; id своей
// строки отвечает постановка.
func (s *Server) put(ctx context.Context, addr string) (string, error) {
	tx, err := journaltx.Begin(ctx, s.db, journaltx.NewOptions(s.source.Enabled()))
	if err != nil {
		return "", fmt.Errorf("начало транзакции постановки: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	queued, err := notify.SendProbeHello(ctx, tx, notify.ProbeHelloAttrs{To: addr, Target: target})
	if err != nil {
		return "", fmt.Errorf("постановка probe-hello: %w", err)
	}
	id, ok := queued.ID()
	if !ok {
		return "", errors.New("постановка probe-hello не записала строки ленты при включённом флаге")
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("коммит постановки: %w", err)
	}
	return id, nil
}
