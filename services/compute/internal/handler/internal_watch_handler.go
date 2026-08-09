// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package handler — internal_watch_handler.go реализует
// kacho.cloud.compute.v1.InternalWatchService — internal RPC, поток событий из
// compute_outbox (Outbox pattern + LISTEN/NOTIFY wake-up). Handler НЕ выставлен
// через api-gateway external endpoint — слушает на cluster-internal порту.
// Структурно идентичен kacho-vpc/internal/handler/internal_watch_handler.go.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"

	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
)

// catchupBatchSize — сколько событий читаем за один SELECT при initial-catchup.
const catchupBatchSize = 100

// watchVisibilityBatchSize — максимум идентификаторов в ОДНОМ вопросе к модели
// прав. Контракт `AuthorizeService.BatchCheck` отвергает партию больше сотни, и
// это же ограничение записано в правиле платформы для авторизации на уровне
// данных. Держится не больше catchupBatchSize, поэтому один прочитанный батч
// журнала укладывается в один вопрос.
const watchVisibilityBatchSize = 100

// Сужение потока — тем же сужателем, что и списки.
//
// Прежде здесь стоял СВОЙ порт (`EventVisibility`), объявленный уже общего фильтра и
// требовавший на одну вещь больше — `Narrows()`. Причина была верной: общий фильтр
// при выключенном переключателе пропускал идентификаторы сквозь себя, и «фильтр
// подвешен» само по себе не являлось утверждением о сужении. Причина СНЯТА: сужатель
// больше не пропускает молча — посадка без модели отвечает отказом, а пропуск
// требуется объявить аварийным режимом. `Narrows()` при этом остался и читается
// здесь: у потока под ним нет второй линии, и он вправе требовать не «фильтр есть», а
// «фильтр сужает».
// InternalWatchHandler реализует computev1.InternalWatchServiceServer.
type InternalWatchHandler struct {
	computev1.UnimplementedInternalWatchServiceServer
	pool       *pgxpool.Pool
	dsn        string
	log        *slog.Logger
	streamSlot chan struct{}
	vis        *listnarrow.Narrower
}

// NewInternalWatchHandler создаёт handler. pool — для catchup-SELECT'ов; dsn —
// отдельный connection string для dedicated LISTEN-соединения (вне пула);
// maxStreams — лимит одновременных Watch-streams (0 → fallback 32); vis —
// per-row сужение потока по правам вызывающего (обязателен: nil → Watch
// отказывает, поток не открывается).
func NewInternalWatchHandler(pool *pgxpool.Pool, dsn string, log *slog.Logger, maxStreams int, vis *listnarrow.Narrower) *InternalWatchHandler {
	if maxStreams <= 0 {
		maxStreams = 32
	}
	return &InternalWatchHandler{
		pool:       pool,
		dsn:        dsn,
		log:        log,
		streamSlot: make(chan struct{}, maxStreams),
		vis:        vis,
	}
}

// Watch реализует server-stream подписки на события compute_outbox.
//
// # Кто получает строки
//
// Запрос не несёт ни субъектного, ни проектного предиката, а каждая строка — полный
// снимок ресурса (проект, имя, метки, метаданные, имя хоста, источник загрузки,
// пользовательские данные загрузки). Поэтому сужение делается на уровне ДАННЫХ, по
// правам вызывающего на каждую отдаваемую строку — единого объекта, о котором можно
// было бы задать один вопрос, у этого RPC не существует.
//
// Порядок в начале потока: сначала личность, потом наличие сужения, и только затем
// расход слота параллелизма и обращение к бэкенду. Оба отказа обязаны наступать до
// подключения — иначе вызывающий получает retryable-код на ввод, который валидным не
// станет, и отказ начинает зависеть от доступности БД.
func (h *InternalWatchHandler) Watch(req *computev1.WatchRequest, stream computev1.InternalWatchService_WatchServer) error {
	ctx := stream.Context()

	// Личность — безусловно. Пустой субъект НЕ является субъектом: за этим RPC нет
	// per-RPC Check, на который можно откатиться (он снят по построению, см.
	// internal/check/permission_map.go), поэтому неназванный вызывающий обязан
	// отбиваться здесь, а не «в боевом режиме».
	if _, subjErr := listnarrow.SubjectFromContext(ctx); subjErr != nil {
		h.log.Warn("watch refused: request names no caller")
		return status.Error(codes.PermissionDenied, "watch requires an authenticated caller")
	}

	// Сужение обязано существовать И действительно сужать. Общий фильтр при
	// выключенном мастер-переключателе (или без клиента к модели) пропускает
	// идентификаторы сквозь себя; для списочного RPC под этим остаётся per-RPC Check
	// на проектном ярусе, под этим потоком — ничего. Боевая стража отказывает в
	// старте на такой настройке (requireListFilter); здесь тот же запрет держится на
	// любом стенде.
	if h.vis == nil || !h.vis.Narrows() {
		h.log.Error("watch refused: per-row visibility filter is absent or does not narrow")
		return status.Error(codes.PermissionDenied, "watch is unavailable without per-event authorization")
	}

	select {
	case h.streamSlot <- struct{}{}:
		defer func() { <-h.streamSlot }()
	default:
		return status.Error(codes.ResourceExhausted, "too many concurrent watch streams (limit reached)")
	}

	cursor := req.GetFromSequenceNo()
	kinds := req.GetKinds()
	h.log.Info("watch stream started", "from_sequence_no", cursor, "kinds", kinds)

	connectCtx, connectCancel := context.WithTimeout(ctx, 2*time.Second)
	conn, err := pgx.Connect(connectCtx, h.dsn)
	connectCancel()
	if err != nil {
		return status.Error(codes.Unavailable, "watch backend unavailable")
	}
	defer func() {
		closeCtx, cancelClose := context.WithTimeout(context.Background(), 5*time.Second)
		_ = conn.Close(closeCtx)
		cancelClose()
	}()

	if _, err := conn.Exec(ctx, "LISTEN compute_outbox"); err != nil {
		return internalMapErr("watch listen failed", err)
	}
	defer func() {
		closeCtx, cancelClose := context.WithTimeout(context.Background(), 2*time.Second)
		_, _ = conn.Exec(closeCtx, "UNLISTEN compute_outbox")
		cancelClose()
	}()

	if newCursor, err := h.streamSince(ctx, conn, cursor, kinds, stream); err != nil {
		return err
	} else {
		cursor = newCursor
	}

	for {
		if err := ctx.Err(); err != nil {
			h.log.Info("watch stream cancelled", "err", err)
			return nil
		}
		waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, err := conn.WaitForNotification(waitCtx)
		cancel()
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				if ctx.Err() != nil {
					return nil
				}
				// timeout: re-poll анывай.
			} else {
				return status.Error(codes.Unavailable, "watch notification stream lost")
			}
		}
		if newCursor, err := h.streamSince(ctx, conn, cursor, kinds, stream); err != nil {
			return err
		} else {
			cursor = newCursor
		}
	}
}

// streamSince читает события из compute_outbox с sequence_no > cursor (и
// resource_kind ∈ kinds, если задан), СУЖАЕТ прочитанный батч до прав subject'а и
// шлёт оставшееся в stream.
//
// # Курсор идёт по ПРОЧИТАННОЙ строке, а не по отправленной
//
// Батч читается целиком, затем сужается, затем отправляется — вопрос к модели не
// задаётся с открытым курсором БД. Позиция продвигается до последней ПРОЧИТАННОЙ
// строки: если бы она шла за отправленными, полный батч невидимых вызывающему строк
// перечитывался бы вечно (тот же запрос, те же сто строк, ноль продвижения — цикл
// внутри занятого слота). То же правило, по которому страничные списки выводят
// `next_page_token` из последней просмотренной строки: полный обход не пропускает
// ничего, но порция законно приходит частичной.
func (h *InternalWatchHandler) streamSince(
	ctx context.Context,
	conn *pgx.Conn,
	cursor int64,
	kinds []string,
	stream computev1.InternalWatchService_WatchServer,
) (int64, error) {
	for {
		args := []any{cursor}
		var kindFilter string
		if len(kinds) > 0 {
			kindFilter = " AND resource_kind = ANY($2)"
			args = append(args, kinds)
		}
		q := fmt.Sprintf(`
			SELECT sequence_no, resource_kind, resource_id, event_type, payload, created_at
			FROM compute_outbox
			WHERE sequence_no > $1%s
			ORDER BY sequence_no ASC
			LIMIT %d
		`, kindFilter, catchupBatchSize)

		rows, err := conn.Query(ctx, q, args...)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return cursor, nil
			}
			return cursor, internalMapErr("query outbox", err)
		}
		batch := make([]*computev1.Event, 0, catchupBatchSize)
		scanned := cursor
		for rows.Next() {
			var seq int64
			var kind, id, eventType string
			var payloadJSON []byte
			var createdAt time.Time
			if err := rows.Scan(&seq, &kind, &id, &eventType, &payloadJSON, &createdAt); err != nil {
				rows.Close()
				return cursor, internalMapErr("scan outbox", err)
			}
			payloadStruct, err := jsonBytesToStruct(payloadJSON)
			if err != nil {
				h.log.Warn("watch: bad payload JSON", "sequence_no", seq, "err", err)
				payloadStruct = &structpb.Struct{Fields: map[string]*structpb.Value{}}
			}
			batch = append(batch, &computev1.Event{
				SequenceNo:   seq,
				ResourceKind: kind,
				ResourceId:   id,
				EventType:    eventType,
				Payload:      payloadStruct,
				CreatedAt:    timestamppb.New(createdAt),
			})
			scanned = seq
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return cursor, internalMapErr("outbox iter", err)
		}

		visible, err := h.narrowToSubject(ctx, batch)
		if err != nil {
			// Модель не ответила — это НЕ «да». Позиция не двигается, строки не уходят.
			return cursor, err
		}
		for _, ev := range visible {
			if err := stream.Send(ev); err != nil {
				return cursor, err
			}
		}
		cursor = scanned

		if len(batch) < catchupBatchSize {
			return cursor, nil
		}
	}
}

// watchObjectTypes — resource_kind журнала → тип объекта в модели прав.
//
// Перечисление закрытое, и это часть гейта: kind, которого здесь нет, не имеет типа
// объекта, поэтому вопрос «вправе ли вызывающий видеть эту строку» задать НЕЛЬЗЯ, и
// строка не доставляется. Блочное хранение ушло из compute (миграция 0021 дропнула
// disks/images/snapshots), так что оставшиеся в журнале `Disk`/`Image`/`Snapshot` —
// ровно этот случай: compute ими больше не владеет и об их видимости не судит.
// Действие несётся В ТОЙ ЖЕ записи, а не берётся одно на всех: иначе добавленный
// позже kind унаследовал бы чужой глагол, и модель судила бы о нём по неверному
// действию, оставаясь при этом «зелёной».
var watchObjectTypes = map[string]struct{ objectType, action string }{
	"Instance": {authzfilter.ResourceTypeInstance, authzfilter.ActionInstanceRead},
}

// narrowToSubject оставляет из батча только строки, которые subject вправе видеть,
// СОХРАНЯЯ порядок (журнал упорядочен по sequence_no — перестановка сломала бы
// монотонность потока).
//
// Вопрос задаётся партиями не больше watchVisibilityBatchSize и группируется по типу
// объекта: у модели спрашивают про однотипные идентификаторы. Ошибка любой партии
// прекращает обработку батча целиком — частично сужённый батч отдавать нельзя.
// Субъект больше НЕ ПАРАМЕТР: он приходит контекстом, тем же путём и той же
// функцией, что у списков. Дублировать его аргументом значило бы завести второй
// ответ на один вопрос — и разъехаться они смогли бы молча.
func (h *InternalWatchHandler) narrowToSubject(
	ctx context.Context,
	batch []*computev1.Event,
) ([]*computev1.Event, error) {
	if len(batch) == 0 {
		return nil, nil
	}
	// Требование заявляется ТАМ, ГДЕ строки используются, а не только у входа в RPC, и
	// заявляется ЦЕЛИКОМ — то же условие, что у входа (строка выше в Watch), а не его
	// половина. Watch проверяет фильтр до открытия потока, поэтому сейчас сюда иначе не
	// попасть, но именно в такой форме дыры и возвращаются: страж стоит у одной двери,
	// позже появляется второй вызывающий цикла чтения.
	//
	// Половина условия была бы хуже целого отсутствия проверки, потому что закрывала бы
	// ШУМНЫЙ подслучай и оставляла ТИХИЙ. Отсутствующий фильтр разыменовался бы в
	// панику — непрозрачный INTERNAL, ноль строк. Подвешенный, но не сужающий (снят
	// мастер-переключатель, нет клиента к модели, мягкий проход на ошибке) возвращает
	// идентификаторы КАК ЕСТЬ (authzfilter.FGAFilter.FilterVisibleIDs / handleErr) — то
	// есть отдал бы весь журнал молча, под кодом, который выглядит фильтрующим.
	if h.vis == nil || !h.vis.Narrows() {
		return nil, status.Error(codes.PermissionDenied, "watch is unavailable without per-event authorization")
	}

	// Идентификаторы по kind'у; kind без записи в таблице сюда не попадает и тем
	// самым отсекается.
	byKind := make(map[string][]string, len(watchObjectTypes))
	dropped := 0
	for _, ev := range batch {
		if _, ok := watchObjectTypes[ev.GetResourceKind()]; !ok {
			dropped++
			continue
		}
		byKind[ev.GetResourceKind()] = append(byKind[ev.GetResourceKind()], ev.GetResourceId())
	}
	if dropped > 0 {
		h.log.Warn("watch: rows dropped — resource kind has no object type in the permission model",
			"dropped", dropped)
	}

	allowed := make(map[string]struct{}, len(batch))
	for kind, ids := range byKind {
		mapping := watchObjectTypes[kind]
		for start := 0; start < len(ids); start += watchVisibilityBatchSize {
			end := start + watchVisibilityBatchSize
			if end > len(ids) {
				end = len(ids)
			}
			visible, err := listnarrow.IDs(ctx, h.vis, mapping.objectType, mapping.action, ids[start:end])
			if err != nil {
				return nil, err
			}
			for _, id := range visible {
				allowed[id] = struct{}{}
			}
		}
	}

	out := make([]*computev1.Event, 0, len(allowed))
	for _, ev := range batch {
		if _, ok := watchObjectTypes[ev.GetResourceKind()]; !ok {
			continue
		}
		if _, ok := allowed[ev.GetResourceId()]; ok {
			out = append(out, ev)
		}
	}
	return out, nil
}

// jsonBytesToStruct декодирует raw JSON-bytes (object) в structpb.Struct.
func jsonBytesToStruct(raw []byte) (*structpb.Struct, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" {
		return &structpb.Struct{Fields: map[string]*structpb.Value{}}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return structpb.NewStruct(m)
}
