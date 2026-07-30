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
)

// catchupBatchSize — сколько событий читаем за один SELECT при initial-catchup.
const catchupBatchSize = 100

// watchVisibilityBatchSize — максимум идентификаторов в ОДНОМ вопросе к модели
// прав. Контракт `AuthorizeService.BatchCheck` отвергает партию больше сотни, и
// это же ограничение записано в правиле платформы для авторизации на уровне
// данных. Держится не больше catchupBatchSize, поэтому один прочитанный батч
// журнала укладывается в один вопрос.
const watchVisibilityBatchSize = 100

// EventVisibility — то, чем поток сужается до прав вызывающего.
//
// Порт УЖЕ, чем authzfilter.Filter, и требует на одну вещь больше — Narrows().
// Причина в том, что общий фильтр при выключенном мастер-переключателе (или без
// клиента к модели) возвращает идентификаторы КАК ЕСТЬ. Для списочного RPC это
// защитимо: под ним остаётся per-RPC Check на проектном ярусе. Под этим потоком
// не остаётся ничего, поэтому «фильтр подвешен» само по себе не является
// утверждением о сужении, и спросить об этом нужно отдельно.
type EventVisibility interface {
	// FilterVisibleIDs возвращает подмножество ids, видимое subject'у. err != nil —
	// fail-closed: вызывающий ОБЯЗАН пробросить ошибку, а не отдать строки.
	FilterVisibleIDs(ctx context.Context, subject, resourceType, action string, ids []string) ([]string, error)
	// Narrows сообщает, что фильтр действительно сужает выдачу, а не пропускает
	// идентификаторы сквозь себя (выключен / без клиента к модели / отдаёт
	// нефильтрованное на ошибке).
	Narrows() bool
}

// InternalWatchHandler реализует computev1.InternalWatchServiceServer.
type InternalWatchHandler struct {
	computev1.UnimplementedInternalWatchServiceServer
	pool       *pgxpool.Pool
	dsn        string
	log        *slog.Logger
	streamSlot chan struct{}
	vis        EventVisibility
}

// NewInternalWatchHandler создаёт handler. pool — для catchup-SELECT'ов; dsn —
// отдельный connection string для dedicated LISTEN-соединения (вне пула);
// maxStreams — лимит одновременных Watch-streams (0 → fallback 32); vis —
// per-row сужение потока по правам вызывающего (обязателен: nil → Watch
// отказывает, поток не открывается).
func NewInternalWatchHandler(pool *pgxpool.Pool, dsn string, log *slog.Logger, maxStreams int, vis EventVisibility) *InternalWatchHandler {
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
func (h *InternalWatchHandler) Watch(req *computev1.WatchRequest, stream computev1.InternalWatchService_WatchServer) error {
	ctx := stream.Context()

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

	if newCursor, err := h.streamSince(ctx, conn, cursor, kinds, "", stream); err != nil {
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
		if newCursor, err := h.streamSince(ctx, conn, cursor, kinds, "", stream); err != nil {
			return err
		} else {
			cursor = newCursor
		}
	}
}

// streamSince читает все события из compute_outbox с sequence_no > cursor (и
// resource_kind ∈ kinds, если задан) и шлёт их в stream.
func (h *InternalWatchHandler) streamSince(
	ctx context.Context,
	conn *pgx.Conn,
	cursor int64,
	kinds []string,
	subject string,
	stream computev1.InternalWatchService_WatchServer,
) (int64, error) {
	_ = subject // RED stage: not yet consulted.
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
		count := 0
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
			ev := &computev1.Event{
				SequenceNo:   seq,
				ResourceKind: kind,
				ResourceId:   id,
				EventType:    eventType,
				Payload:      payloadStruct,
				CreatedAt:    timestamppb.New(createdAt),
			}
			if err := stream.Send(ev); err != nil {
				rows.Close()
				return cursor, err
			}
			cursor = seq
			count++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return cursor, internalMapErr("outbox iter", err)
		}
		if count < catchupBatchSize {
			return cursor, nil
		}
	}
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
