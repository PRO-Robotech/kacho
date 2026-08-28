// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscription

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/pagetoken"
)

// drain вычитывает окно `(cursor, settled]` до конца и отдаёт то, что вызывающий
// вправе видеть. Возвращает новый курсор.
//
// # Почему окно, а не просто «больше курсора»
//
// Номер выдаётся счётчиком на вставке, а строка становится видимой на фиксации,
// поэтому порядок номеров и порядок фиксаций НЕЗАВИСИМЫ. Отдать номер, за
// которым ещё может появиться меньший, — значит потерять меньший навсегда:
// перечитывание идёт строго «больше курсора», и возобновление с выданной позиции
// воспроизводит ту же дыру. Граница устоявшегося (см. `watermark.go`)
// гарантирует обратное: к моменту отдачи события все меньшие номера уже
// определены — видимы либо потеряны откатом.
//
// # Курсор идёт по ПРОЧИТАННОЙ строке, а не по отправленной
//
// Батч читается целиком, затем сужается, затем отправляется. Позиция
// продвигается до последней ПРОЧИТАННОЙ строки: шла бы она за отправленными,
// полный батч невидимых вызывающему строк перечитывался бы вечно — тот же
// запрос, те же сто строк, ноль продвижения, и всё это внутри занятого слота.
// То же правило, по которому страничные списки выводят следующий курсор из
// последней ПРОСМОТРЕННОЙ строки.
func (s *Server) drain(
	ctx context.Context,
	conn *pgx.Conn,
	h *watermark,
	cursor int64,
	filter Filter,
	stream subscriptionv1.InternalSubscriptionService_SubscribeServer,
) (int64, error) {
	for {
		if ctx.Err() != nil {
			return cursor, nil
		}
		if h.settled <= cursor {
			return cursor, nil
		}

		rows, err := s.read(ctx, conn, cursor, h.settled, filter)
		if err != nil {
			if ctx.Err() != nil {
				return cursor, nil
			}
			// Отказ чтения — fail-closed. Недоступность источника НЕ означает
			// «событий нет»: пустая выдача была бы утверждением о мире.
			return cursor, status.Error(codes.Unavailable, "subscription backend unavailable")
		}
		if len(rows) == 0 {
			return h.settled, nil
		}

		// Право на ПРОЕКТ спрашивается заново — перед КАЖДОЙ порцией, которая
		// собирается уйти. См. [Server.regate].
		if err := s.regate(ctx, filter); err != nil {
			return cursor, err
		}

		scanned := rows[len(rows)-1].Position

		events, err := s.mapRows(rows, filter)
		if err != nil {
			return cursor, err
		}
		visible, err := s.narrow(ctx, rows, events)
		if err != nil {
			// Модель не ответила — это НЕ «да». Позиция не двигается, строки не
			// уходят.
			return cursor, err
		}
		for _, ev := range visible {
			if err := stream.Send(&subscriptionv1.SubscriptionMessage{
				Message: &subscriptionv1.SubscriptionMessage_Event{Event: ev},
			}); err != nil {
				return cursor, err
			}
		}
		cursor = scanned

		if len(rows) < readBatch {
			return cursor, nil
		}
	}
}

// regate переспрашивает стража проектной оси ПЕРЕД отправкой порции.
//
// # Почему на живом потоке, а не только при открытии
//
// Страж спрашивался один раз — при открытии, — и дальше поток жил сам. Это
// ровно тот класс, который корпус называет «контроль, действующий на выдаче, но
// не на предъявлении»: механизм присутствует, выглядит исполненным и не
// срабатывает никогда, потому что стоит не на том пути. От задержки
// распространения он отличается тем, что сходиться тут нечему: отзыв не
// приобретает читателя оттого, что прошло время.
//
// Пообъектное сужение ниже этой дыры НЕ закрывает, хотя выглядит так, будто
// закрывает. Оно спрашивает про КОРТЕЖ НА ОБЪЕКТЕ, а кортежи снимает
// реконсайлер iam из отозванной привязки — то есть окно между «право отозвано»
// и «строки перестали уходить» задаётся чужим конвейером и числом не названо.
// Страж оси спрашивает модель напрямую и отрицателен с момента отзыва.
//
// # Почему ПЕРЕД порцией, а не по таймеру
//
// Окно тогда есть СЛЕДСТВИЕ РЕШЕНИЯ, а не срока: после отзыва не уходит НИ ОДНОЙ
// порции, и величины, которую надо было бы выбрать и потом объяснять, здесь нет
// вовсе. Молчащий поток при этом не платит ничего — расход пропорционален
// объёму событий, а не числу открытых потоков.
//
// # Чего эта проверка НЕ делает
//
// Она не закрывает СОЕДИНЕНИЕ: поток остаётся открытым и перестаёт отдавать
// события. Закрытие соединения принадлежит тому, кто его открыл, — у
// сегодняшнего единственного потребителя это край (kacho#1022), и там оно
// сделано чтением отзыва. Сказано вслух, чтобы следующий читатель не принял
// «событий нет» за «потока нет».
//
// Fail-closed: неотвеченная модель НЕ есть «право есть» — отказ уходит наверх, и
// порция не отправляется.
func (s *Server) regate(ctx context.Context, filter Filter) error {
	if filter.ProjectID == "" {
		// Оси нет — сторожить нечего. Спрашивать здесь значило бы звать соседа с
		// вопросом, которого вызывающий не задавал.
		return nil
	}
	allowed, err := listnarrow.AllowedOnObject(ctx, s.cfg.Narrower,
		s.cfg.ProjectGate.ObjectType, s.cfg.ProjectGate.Action,
		s.cfg.ProjectGate.Relations, filter.ProjectID)
	if err != nil {
		return err
	}
	if !allowed {
		// Та же форма, что при открытии, и дословно та же, что у отсутствия
		// проекта: различимый текст выдал бы существование чужого проекта.
		return status.Errorf(codes.NotFound, s.cfg.ProjectGate.NotFoundFormat, filter.ProjectID)
	}
	return nil
}

// read вычитывает одну порцию окна, применяя те оси, которые отбираются запросом.
//
// Ось `project_id` уходит в запрос ТОЛЬКО у владельца, чей журнал несёт колонку;
// у остальных якорь даёт отображение, и ось отбирается после него — по-прежнему
// СЕРВЕРОМ (см. [Server.mapRows]).
func (s *Server) read(
	ctx context.Context,
	conn *pgx.Conn,
	cursor, settled int64,
	filter Filter,
) ([]Row, error) {
	st := s.cfg.Journal.Storage

	projectExpr := "''"
	if st.Project == ProjectInColumn {
		projectExpr = st.ProjectColumn
	}

	args := []any{cursor, settled}
	var where strings.Builder
	if len(filter.Kinds) > 0 {
		args = append(args, filter.Kinds)
		fmt.Fprintf(&where, " AND %s = ANY($%d)", st.KindColumn, len(args))
	}
	if len(filter.IDs) > 0 {
		args = append(args, filter.IDs)
		fmt.Fprintf(&where, " AND %s = ANY($%d)", st.IDColumn, len(args))
	}
	if filter.ProjectID != "" && st.Project == ProjectInColumn {
		args = append(args, filter.ProjectID)
		fmt.Fprintf(&where, " AND %s = $%d", st.ProjectColumn, len(args))
	}

	q := fmt.Sprintf(`
		SELECT %s, %s, %s, %s, %s, %s
		FROM %s
		WHERE %s > $1 AND %s <= $2%s
		ORDER BY %s ASC
		LIMIT %d`,
		st.PositionColumn, st.KindColumn, st.IDColumn, projectExpr, st.ChangeColumn, st.PayloadColumn,
		st.Table,
		st.PositionColumn, st.PositionColumn, where.String(),
		st.PositionColumn, readBatch)

	pgRows, err := conn.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer pgRows.Close()

	out := make([]Row, 0, readBatch)
	for pgRows.Next() {
		var r Row
		if err := pgRows.Scan(&r.Position, &r.Kind, &r.ID, &r.ProjectID, &r.Change, &r.Payload); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := pgRows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// mapRows превращает строки в события общей формы, сохраняя ПОРЯДОК.
//
// Соответствие позиционное: `events[i]` отвечает `rows[i]`, а недоставляемая
// строка даёт `nil`. Отбрасывать её здесь нельзя — сужение по правам
// сопоставляет батч со строками по индексу, и сдвиг разъехался бы молча.
//
// # Три исхода, и каждый назван
//
//   - вид вне закрытого словаря владельца ЛИБО род изменения вне его словаря —
//     строка НЕДОСТАВЛЯЕМА: вопрос о её видимости задать нечем, а род изменения
//     подписчику не назвать. Громко в лог: это расхождение журнала владельца с
//     его же объявлением, а не свойство подписки;
//   - якорь не добылся — строка НЕДОСТАВЛЯЕМА по той же причине: решение о
//     показе принять не из чего. Fail-closed;
//   - состояние не собралось — событие ДОСТАВЛЯЕТСЯ с признаком «состояния не
//     будет» и названной причиной. Пустая нагрузка вместо состояния не
//     выразима формой by construction, и это тот случай, ради которого ветвление
//     носителя и заведено.
func (s *Server) mapRows(rows []Row, filter Filter) ([]*subscriptionv1.SubscriptionEvent, error) {
	m := s.cfg.Journal.Mapping
	out := make([]*subscriptionv1.SubscriptionEvent, len(rows))

	var undeliverable int
	for i, row := range rows {
		if _, ok := m.Kinds[row.Kind]; !ok {
			undeliverable++
			continue
		}
		change, ok := m.Changes[row.Change]
		if !ok {
			undeliverable++
			continue
		}

		project := row.ProjectID
		if m.Anchor != nil {
			anchored, err := m.Anchor(row)
			if err != nil {
				s.log.Error("subscription: row is undeliverable — the project anchor could not be derived",
					"position", row.Position, "kind", row.Kind, "err", err)
				undeliverable++
				continue
			}
			project = anchored
		}
		// Ось, отобранная не запросом, отбирается здесь — СЕРВЕРОМ. Клиенту
		// доотбор по якорной оси не передаётся ни при каком устройстве журнала.
		if filter.ProjectID != "" && project != filter.ProjectID {
			continue
		}

		ev := &subscriptionv1.SubscriptionEvent{
			Position:   pagetoken.EncodeSubscriptionPosition(pagetoken.SubscriptionPosition{Settled: row.Position}),
			Kind:       row.Kind,
			ResourceId: row.ID,
			ProjectId:  project,
			Change:     change,
		}
		state, err := m.State(row)
		switch {
		case err != nil:
			s.log.Warn("subscription: state is unavailable for this event",
				"position", row.Position, "kind", row.Kind, "err", err)
			ev.Carrier = &subscriptionv1.SubscriptionEvent_StateUnavailable_{
				StateUnavailable: &subscriptionv1.SubscriptionEvent_StateUnavailable{
					Reason: subscriptionv1.SubscriptionEvent_StateUnavailable_NOT_SERIALIZABLE,
				},
			}
		case state == nil:
			// Отображение вернуло отсутствие без ошибки. Пустое состояние не
			// отдаётся НИКОГДА: подписчик вправе читать непустую нагрузку как
			// ПОЛНОЕ состояние предмета, и пустой объект солгал бы ему.
			ev.Carrier = &subscriptionv1.SubscriptionEvent_StateUnavailable_{
				StateUnavailable: &subscriptionv1.SubscriptionEvent_StateUnavailable{
					Reason: subscriptionv1.SubscriptionEvent_StateUnavailable_NOT_SERIALIZABLE,
				},
			}
		default:
			ev.Carrier = &subscriptionv1.SubscriptionEvent_State{State: state}
		}
		out[i] = ev
	}

	if undeliverable > 0 {
		s.log.Warn("subscription: rows dropped — the journal carries kinds or changes the owner did not declare",
			"dropped", undeliverable, "table", s.cfg.Journal.Storage.Table)
	}
	return out, nil
}

// narrow оставляет только те события, которые вызывающий вправе видеть,
// СОХРАНЯЯ порядок: журнал упорядочен по номеру, и перестановка сломала бы
// монотонность потока.
//
// Вопрос задаётся партиями не больше [visibilityBatch] и группируется по типу
// объекта: у модели спрашивают про однотипные идентификаторы. Ошибка любой
// партии прекращает обработку батча ЦЕЛИКОМ — частично сужённый батч отдавать
// нельзя.
func (s *Server) narrow(
	ctx context.Context,
	rows []Row,
	events []*subscriptionv1.SubscriptionEvent,
) ([]*subscriptionv1.SubscriptionEvent, error) {
	// Требование заявляется ТАМ, ГДЕ строки используются, а не только у входа, и
	// заявляется ЦЕЛИКОМ. Половина условия была бы хуже целого отсутствия
	// проверки: она закрывала бы ШУМНЫЙ подслучай (нет сужателя — паника) и
	// оставляла ТИХИЙ (сужатель есть, но пропускает всё насквозь).
	if s.cfg.Narrower == nil || !s.cfg.Narrower.Narrows() {
		return nil, status.Error(codes.PermissionDenied,
			"subscription is unavailable without per-event authorization")
	}

	byKind := make(map[string][]string, len(s.cfg.Journal.Mapping.Kinds))
	for i, ev := range events {
		if ev == nil {
			continue
		}
		byKind[rows[i].Kind] = append(byKind[rows[i].Kind], rows[i].ID)
	}

	allowed := make(map[string]map[string]struct{}, len(byKind))
	for kind, ids := range byKind {
		binding := s.cfg.Journal.Mapping.Kinds[kind]
		seen := make(map[string]struct{}, len(ids))
		for start := 0; start < len(ids); start += visibilityBatch {
			end := start + visibilityBatch
			if end > len(ids) {
				end = len(ids)
			}
			visible, err := listnarrow.IDs(ctx, s.cfg.Narrower,
				binding.ObjectType, binding.Action, ids[start:end])
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return nil, err
				}
				return nil, err
			}
			for _, id := range visible {
				seen[id] = struct{}{}
			}
		}
		allowed[kind] = seen
	}

	out := make([]*subscriptionv1.SubscriptionEvent, 0, len(events))
	for i, ev := range events {
		if ev == nil {
			continue
		}
		if _, ok := allowed[rows[i].Kind][rows[i].ID]; ok {
			out = append(out, ev)
		}
	}
	return out, nil
}
