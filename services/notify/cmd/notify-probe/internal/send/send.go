// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package send — глагол пробы-источника `InternalNotifyProbeService/Send`
// (NTF-1, З29, решение Д75): поставить письмо шаблона `probe-hello` на адрес и
// ответить идентификатором поставленной строки ленты.
//
// # Ответ синхронный, а не Operation
//
// Постановка — одна транзакция (`feed.Put` через порождённый SendProbeHello и
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
// `feed.Put` id не возвращает: он выдаётся внутри постановки. Строка читается
// в той же транзакции, и транзакция открывается уровнем REPEATABLE READ: её
// снимок берётся первым оператором, до выдачи транзакции номера, поэтому чужая
// строка, видимая в снимке, закоммичена раньше — её `xmin` предшествует номеру
// этой транзакции, и `age(xmin) > 0`. Строка, вставленная этой транзакцией (в
// точке сохранения постановки — номером подтранзакции, старше номера
// транзакции), даёт `age(xmin) <= 0`. На уровне READ COMMITTED каждый оператор
// берёт новый снимок, и параллельная постановка с номером старше закоммитилась
// бы в него — ответ отдал бы чужой id; гонку держит проба
// TestParallelSendsGetTheirOwnIDs. Строк своей транзакции не ровно одна —
// внутренняя ошибка, а не «первая попавшаяся».
package send

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/corelib/journaltx"
	"github.com/PRO-Robotech/corelib/notify/address"
	"github.com/PRO-Robotech/corelib/notify/feed"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"
	notify "github.com/PRO-Robotech/kacho/services/notify"
	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/journal"
)

// target — значение атрибута target письма: путь `/` — origin установки.
const target = "/"

// ownRowQuery — id строк ленты, вставленных этой транзакцией (см. шапку).
var ownRowQuery = `SELECT id FROM ` + journal.FeedOutbox + ` WHERE age(xmin) <= 0`

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
	return &Server{db: repeatableRead{db}, source: source, log: log}
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
		return nil, status.Error(codes.InvalidArgument, "address: "+err.Error())
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

// put — транзакция постановки: строка ленты, строка журнала, id своей строки.
func (s *Server) put(ctx context.Context, addr string) (string, error) {
	tx, err := journaltx.Begin(ctx, s.db, journaltx.NewOptions(s.source.Enabled()))
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := notify.SendProbeHello(ctx, tx, notify.ProbeHelloAttrs{To: addr, Target: target}); err != nil {
		return "", err
	}
	rows, err := tx.Query(ctx, ownRowQuery)
	if err != nil {
		return "", fmt.Errorf("id поставленной строки: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", fmt.Errorf("id поставленной строки: %w", err)
	}
	if len(ids) != 1 {
		return "", fmt.Errorf("строк ленты этой транзакции %d, а постановка одна", len(ids))
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("коммит постановки: %w", err)
	}
	return ids[0], nil
}

// repeatableRead открывает транзакции уровнем REPEATABLE READ (см. шапку).
type repeatableRead struct{ db journaltx.TxStarter }

func (r repeatableRead) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	opts.IsoLevel = pgx.RepeatableRead
	return r.db.BeginTx(ctx, opts)
}
