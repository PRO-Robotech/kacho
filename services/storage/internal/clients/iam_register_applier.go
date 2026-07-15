// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package clients — register-drainer FGA transactional-outbox (storage→iam).
//
// Декодирует одну строку kacho_storage.fga_register_outbox в owner-tuple и применяет
// его к FGA ЧЕРЕЗ kacho-iam — storage в FGA напрямую не ходит. Apply — один sync
// unary-вызов InternalIAMService.RegisterResource (event_type fga.register) либо
// UnregisterResource (fga.unregister); оба — Internal-only :9091 RPC, идемпотентные
// по контракту (повтор того же tuple → OK, НЕ AlreadyExists).
//
// Классификация ошибок (её потребляет corelib outbox/drainer):
//   - OK                     → nil (drainer ставит sent_at).
//   - codes.InvalidArgument  → ErrPermanent (malformed tuple = poison, без вечных ретраев).
//   - codes.PermissionDenied → transient: отсутствующий grant fga_writer@iam_fgaproxy:system
//     — вопрос порядка provisioning'а, лечится после SA-grant-миграции; ретрай даёт
//     owner-tuple осесть, поэтому НЕ poison (как в vpc/compute/nlb).
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

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/outbox/drainer"

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
				SourceVersion:   sourceVersionPB(p.SourceVersion),
			})
			return classifyRegisterErr(err)
		case fgaregister.EventUnregister:
			_, err := c.UnregisterResource(ctx, &iamv1.UnregisterResourceRequest{
				SubjectId: p.SubjectID,
				Relation:  p.Relation,
				Object:    p.Object,
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
// drainer'а. nil → nil. InvalidArgument → permanent (poison). Всё остальное —
// включая PermissionDenied (grant fga_writer ещё не засеян), Unavailable, транспорт
// — → transient (ретрай, intent durable: fail-closed, но не теряется).
func classifyRegisterErr(err error) error {
	if err == nil {
		return nil
	}
	if status.Code(err) == codes.InvalidArgument {
		return errors.Join(drainer.ErrPermanent, fmt.Errorf("iam register apply: %w", err))
	}
	return fmt.Errorf("iam register apply: %w", err)
}
