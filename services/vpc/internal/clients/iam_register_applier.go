// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package clients — register-drainer FGA transactional-outbox.
//
// Декодирует одну строку fga_register_outbox в tuple и применяет его к FGA ЧЕРЕЗ
// kaname — модули в FGA напрямую не ходят. Apply — это один sync unary-вызов
// InternalIAMService.RegisterResource (event_type fga.register) либо
// UnregisterResource (fga.unregister); оба — Internal-only :9091 RPC, идемпотентные
// по контракту (повтор того же tuple → OK, НЕ AlreadyExists).
//
// Классификация ошибок (ее потребляет corelib outbox/drainer):
//   - OK                    → nil (drainer ставит sent_at).
//   - codes.InvalidArgument → ErrPermanent (malformed tuple = poison, без бесконечных
//     ретраев).
//   - codes.PermissionDenied → ErrPermanent (poison): отказ по правам терминален —
//     решение зависит от (вызывающий, отношение, объект), и повтор не меняет ни
//     одного из трёх. Держать его transient значило бы навсегда заклинить голову
//     партиции (подробный разбор — в godoc classifyRegisterErr ниже).
//   - все остальное (Unavailable, DeadlineExceeded, транспорт)
//     → transient (raw) → drainer ретраит с backoff; intent остается durable
//     (sent_at NULL) и НЕ теряется.
//
// Clean Architecture: этот адаптер (clients/) реализует port drainer'а Applier;
// импортирует grpc-stubs (iamv1) + чистый domain-тип tuple (fgaregister) — без pgx,
// без use-case/transport-слоя.
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

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/outbox/drainer"
	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/fgaregister"
)

// IAMRegisterRPC — узкое подмножество iamv1.InternalIAMServiceClient, нужное
// register-applier'у: только два FGA-proxy RPC. Полный InternalIAMServiceClient
// его удовлетворяет; test-fake реализует только эти два метода.
type IAMRegisterRPC interface {
	RegisterResource(ctx context.Context, in *iamv1.RegisterResourceRequest, opts ...grpc.CallOption) (*iamv1.RegisterResourceResponse, error)
	UnregisterResource(ctx context.Context, in *iamv1.UnregisterResourceRequest, opts ...grpc.CallOption) (*iamv1.UnregisterResourceResponse, error)
}

// iamRegisterRPC — неэкспортируемый alias для эргономики in-package тестов.
type iamRegisterRPC = IAMRegisterRPC

// FGARegisterPayload — payload-тип T для drainer'а. Это alias на чистый domain-тип
// fgaregister.Payload, чтобы repo-writer (который его эмитит) и drainer-applier
// (который его применяет) делили ровно одну форму. Несет tuple + mirror-feed
// (labels + parent_project_id + source_version).
type FGARegisterPayload = fgaregister.Payload

// DecodeFGARegisterPayload — Decoder[FGARegisterPayload] для corelib-drainer'а:
// разбирает payload одной строки fga_register_outbox в Payload (tuple + mirror-feed).
// Строка с голым Tuple декодируется с пустым mirror-feed. Malformed или неполный
// (пустые subject/relation/object) payload — баг на стороне вызывающего, ретрай его
// не починит → ErrPermanent (drainer poison'ит строку, а не ретраит вечно).
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
// клиента IAMRegisterRPC. eventType выбирает register или unregister. Register
// прокидывает mirror-feed (labels + parent_project_id + source_version), чтобы
// kaname материализовал resource_mirror для селектора; unregister прокидывает
// только идентичность tuple (+ source_version-tombstone).
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
				// Ресурсы vpc лежат под проектом, и глубже их иерархия не идёт —
				// цепь выводится из области ЭТОЙ ЖЕ доставки, ничего нового не
				// выдумывая. Аккаунт vpc на горячем пути не резолвит, и резолвить
				// не обязан: предка проекта достраивает принимающая сторона из
				// СВОЕЙ схемы (проект→аккаунт→кластер, миграция 740001 iam).
				//
				// Здесь стояло, что аккаунт достигается «с самого проекта его
				// собственным ребром». Такого ребра не писал никто, и утверждение
				// было ложным ровно в ту сторону, в какую заметить его нельзя:
				// верхний ярус выдач молча не находился (kacho#740).
				ParentChain:   ownerregister.ParentChain(nil, p.ParentProjectID, ""),
				SourceVersion: sourceVersionPB(p.SourceVersion),
			})
			return classifyRegisterErr(err)
		case fgaregister.EventUnregister:
			// source_version на unregister — это TOMBSTONE, а не «версия состояния».
			// iam сносит строку зеркала под `source_version <= $tombstone`, поэтому
			// unregister БЕЗ версии ('-infinity') не матчит ни одну версионированную
			// register-строку — зеркало пережило бы удаление ресурса, а
			// level-triggered реконсайлер продолжал бы ре-материализовать его tuple'ы.
			// Штамп берётся из той же outbox-строки (now() внутри writer-tx), поэтому
			// он заведомо не старше register'а, который отменяет. Паритет с compute/nlb.
			_, err := c.UnregisterResource(ctx, &iamv1.UnregisterResourceRequest{
				SubjectId:     p.SubjectID,
				Relation:      p.Relation,
				Object:        p.Object,
				SourceVersion: sourceVersionPB(p.SourceVersion),
			})
			return classifyRegisterErr(err)
		default:
			// Неизвестный event_type — баг вызывающего; применить нечего, poison.
			return errors.Join(drainer.ErrPermanent,
				fmt.Errorf("unknown fga register event_type %q", eventType))
		}
	}
}

// sourceVersionPB конвертирует монотонный source_version в proto-timestamp.
// Zero (строка без stamp'а) → nil; IAM трактует nil как -infinity — он никогда
// не выигрывает монотонное сравнение.
func sourceVersionPB(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

// classifyRegisterErr маппит ошибку IAM RPC на transient/permanent-контракт
// drainer'а. nil → nil (успех). InvalidArgument (malformed tuple ретраем не
// починить) и PermissionDenied → permanent (poison). Все остальное — Unavailable,
// транспорт, состояние пира — → transient (ретрай, intent остается durable).
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
	// Unavailable / DeadlineExceeded / Internal / транспорт → transient.
	return fmt.Errorf("iam register apply: %w", err)
}
