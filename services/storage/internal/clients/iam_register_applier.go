// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package clients — register-drainer FGA transactional-outbox (storage→iam).
//
// Декодирует одну строку kacho_storage.fga_register_outbox в owner-tuple и применяет
// его к FGA ЧЕРЕЗ kaname — storage в FGA напрямую не ходит. Apply — один sync
// unary-вызов InternalIAMService.RegisterResource (event_type fga.register) либо
// UnregisterResource (fga.unregister); оба — Internal-only :9091 RPC, идемпотентные
// по контракту (повтор того же tuple → OK, НЕ AlreadyExists).
//
// Классификация ошибок (её потребляет corelib outbox/drainer):
//   - OK                     → nil (drainer ставит sent_at).
//   - codes.InvalidArgument  → ErrPermanent (malformed tuple = poison, без вечных ретраев).
//   - codes.PermissionDenied → ErrPermanent. Отказ по правам НЕ временный: решение
//     зависит от (вызывающий, отношение, объект), повтор не меняет ни одного из трёх,
//     поэтому идентичный запрос пройти не может. Классификация «transient» здесь не
//     покупала будущий успех, а заклинивала ГОЛОВУ партиции (партиция — ресурс), из-за
//     чего снятие регистрации, стоящее в очереди за регистрацией, не доезжало — грант
//     переживал удаление ресурса. Подробный разбор размена и его пары с redrive —
//     на classifyRegisterErr.
//   - всё остальное (Unavailable, DeadlineExceeded, транспорт) → transient (ретрай с
//     backoff; intent остаётся durable, sent_at NULL, не теряется).
//
// Clean Architecture: адаптер (clients/) реализует port drainer'а Applier; импортирует
// grpc-stubs (iamv1) + чистый domain-тип tuple (fgaregister) — без pgx, без
// use-case/transport-слоя.
package clients

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/outbox/drainer"
	"github.com/PRO-Robotech/kacho/pkg/ownerregister"

	"github.com/PRO-Robotech/kacho/services/storage/internal/fgaregister"
)

// IAMRegisterRPC — узкое подмножество iamv1.InternalIAMServiceClient, нужное
// register-applier'у и sync-registrar'у: только два FGA-proxy RPC. Полный
// InternalIAMServiceClient его удовлетворяет; test-fake реализует только эти два.
type IAMRegisterRPC interface {
	RegisterResource(ctx context.Context, in *iamv1.RegisterResourceRequest, opts ...grpc.CallOption) (*iamv1.RegisterResourceResponse, error)
	UnregisterResource(ctx context.Context, in *iamv1.UnregisterResourceRequest, opts ...grpc.CallOption) (*iamv1.UnregisterResourceResponse, error)
}

// FGARegisterPayload — payload-тип T для drainer'а: alias на чистый domain-тип
// fgaregister.Payload, чтобы repo-writer (emit) и drainer-applier (apply) делили
// ровно одну форму.
type FGARegisterPayload = fgaregister.Payload

// DecodeFGARegisterPayload — Decoder[FGARegisterPayload] для corelib-drainer'а:
// разбирает payload одной строки в Payload. Malformed или неполный (пустые
// subject/relation/object) payload — баг вызывающего, ретрай не починит →
// ErrPermanent (drainer отравляет строку, не ретраит вечно).
func DecodeFGARegisterPayload(payload []byte) (FGARegisterPayload, error) {
	p, err := fgaregister.Decode(payload)
	if err != nil {
		return p, errors.Join(drainer.ErrPermanent, fmt.Errorf("decode fga register payload: %w", err))
	}
	if !p.Valid() {
		return p, errors.Join(drainer.ErrPermanent,
			fmt.Errorf("incomplete fga register payload: subject_id/relation/object required"))
	}
	return p, nil
}

// NewIAMRegisterApplier строит corelib-drainer Applier[FGARegisterPayload] поверх
// IAMRegisterRPC. eventType выбирает register или unregister. Register прокидывает
// mirror-feed (labels + parent_project_id + source_version); unregister — только
// идентичность tuple (+ source_version-tombstone).
func NewIAMRegisterApplier(c IAMRegisterRPC) drainer.Applier[FGARegisterPayload] {
	return func(ctx context.Context, eventType string, p FGARegisterPayload) error {
		switch eventType {
		case fgaregister.EventRegister:
			_, err := c.RegisterResource(ctx, &iamv1.RegisterResourceRequest{
				SubjectId:       p.SubjectID,
				Relation:        p.Relation,
				Object:          p.Object,
				Labels:          p.Labels,
				ParentProjectId: p.ParentProjectID,
				// Цепь предков идёт ОБОИМИ путями доставки — этим и синхронным.
				// Проведи её одним, и повтор из очереди стёр бы её пустой: приёмная
				// сторона заменяет набор рёбер объекта целиком, поэтому доставки
				// одного намерения обязаны нести одно содержание.
				//
				// Ресурсы storage лежат под проектом, глубже иерархия не идёт —
				// цепь выводится из области ЭТОЙ ЖЕ доставки, ничего нового не
				// выдумывая.
				ParentChain:   ownerregister.ParentChain(nil, p.ParentProjectID, ""),
				SourceVersion: sourceVersionPB(p.SourceVersion),
			})
			return classifyRegisterErr(err)
		case fgaregister.EventUnregister:
			// source_version на unregister — TOMBSTONE, а не «версия состояния». iam
			// сносит строку зеркала под `source_version <= $tombstone`, поэтому
			// unregister БЕЗ версии ('-infinity') не матчит версионированную
			// register-строку: зеркало пережило бы удаление Volume/Snapshot, а
			// level-triggered реконсайлер продолжал бы ре-материализовать его tuple'ы.
			// Штамп — из той же outbox-строки (now() внутри writer-tx), т.е. заведомо
			// не старше отменяемого register'а. Паритет с compute/nlb.
			_, err := c.UnregisterResource(ctx, &iamv1.UnregisterResourceRequest{
				SubjectId:     p.SubjectID,
				Relation:      p.Relation,
				Object:        p.Object,
				SourceVersion: sourceVersionPB(p.SourceVersion),
			})
			return classifyRegisterErr(err)
		default:
			// Неизвестный event_type — баг вызывающего; применить нечего → poison.
			return errors.Join(drainer.ErrPermanent,
				fmt.Errorf("unknown fga register event_type %q", eventType))
		}
	}
}

// sourceVersionPB конвертирует монотонный source_version в proto-timestamp. Zero
// (строка без stamp'а) → nil; IAM трактует nil как -infinity.
func sourceVersionPB(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

// classifyRegisterErr маппит ошибку IAM RPC на transient/permanent-контракт
// drainer'а. nil → nil. InvalidArgument и PermissionDenied → permanent (poison).
// Всё остальное (Unavailable, транспорт, состояние пира) → transient (ретрай,
// intent durable: fail-closed, но не теряется).
//
// Отказ по правам терминален, а не временен. Решение об авторизации зависит от
// (вызывающий, отношение, объект), и повтор не меняет ни одного из трёх, поэтому
// идентичный повтор пройти не может. «Transient» здесь не покупает будущий успех:
// дренаж намеренно держит временную строку на единицу НИЖЕ порога отравления,
// поэтому она никогда не покидает блокирующий набор claim-запроса, и ни одна
// последующая строка её партиции не клеймится. Партиция — это ресурс, а снятие
// регистрации стоит в очереди ЗА регистрацией: заклиненная голова означает грант,
// переживший удаление ресурса. Отравление, наоборот, отказывает закрыто:
// отвергнутая запись не состоялась, партиция разблокирована. Само по себе оно НЕ
// самоисцеляется — недоставленная регистрация оставляет ресурс без mirror-строки в
// kaname, а значит без owner-tuple, — поэтому идёт в паре с периодическим
// redrive-бэкстопом, который превращает отравление в ограниченную паузу, а не в
// безвозвратную потерю.
func classifyRegisterErr(err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.InvalidArgument, codes.PermissionDenied:
		return errors.Join(drainer.ErrPermanent, fmt.Errorf("iam register apply: %w", err))
	}
	return fmt.Errorf("iam register apply: %w", err)
}
