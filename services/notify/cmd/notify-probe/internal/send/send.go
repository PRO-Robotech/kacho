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
// берёт новый снимок, и параллельная постановка с номером старше, успевшая
// закоммититься, попала бы в него с `age(xmin) <= 0` рядом со своей строкой:
// строк стало бы две, и Send ответил бы INTERNAL («строк ленты этой транзакции
// 2») — ложный отказ, а не чужой id. Гонку держит проба
// TestParallelSendsGetTheirOwnIDs. Строк своей транзакции не ровно одна —
// внутренняя ошибка, а не «первая попавшаяся».
//
// # Цена: полный проход ленты на каждый Send
//
// У `age(xmin)` индекса нет, и запрос id своей строки — последовательный
// проход по всей таблице ленты пробы внутри транзакции постановки: цена Send
// растёт линейно с числом строк ленты, а не с числом своих (их одна). Это
// принято для пробы-источника: Send зовёт только сквозная проба стенда, и
// лента пробы растёт на строку за её прогон, — и не годится для источника с
// потоком постановок. Условие снятия — у фундамента: `feed.Put`
// возвращает id поставленной строки (`INSERT … RETURNING id` в
// `corelib/notify/feed`), и тогда этот запрос, уровень REPEATABLE READ и
// шапка выше снимаются одним изменением. Проверка условия: `go doc
// github.com/PRO-Robotech/corelib/notify/feed Put` — сигнатура, возвращающая
// id.
//
// # REPEATABLE READ и limits шаблона
//
// Уровень REPEATABLE READ безопасен, пока постановка ничего не обновляет: у
// шаблона без limits `feed.Put` только вставляет строку ленты и сигнал. Limits
// у шаблона сделали бы постановку обновлением счётчика окна (`INSERT … ON
// CONFLICT DO UPDATE`), и две параллельные постановки на одно окно под
// REPEATABLE READ давали бы 40001 (serialization failure); повтора транзакции
// у Send нет. Предпосылку «limits у probe-hello пусты» держит проба
// TestSentTemplateDeclaresNoLimits (инъекция limit → красный с именем шаблона).
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
const ownRowQuery = `SELECT id FROM ` + journal.FeedOutbox + ` WHERE age(xmin) <= 0`

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

// put — транзакция постановки: строка ленты, строка журнала, id своей строки.
func (s *Server) put(ctx context.Context, addr string) (string, error) {
	tx, err := journaltx.Begin(ctx, s.db, journaltx.NewOptions(s.source.Enabled()))
	if err != nil {
		return "", fmt.Errorf("начало транзакции постановки: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := notify.SendProbeHello(ctx, tx, notify.ProbeHelloAttrs{To: addr, Target: target}); err != nil {
		return "", fmt.Errorf("постановка probe-hello: %w", err)
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
