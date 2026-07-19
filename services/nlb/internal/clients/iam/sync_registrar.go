// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// sync_registrar.go — sync-primary owner-tuple registrar over kacho-iam
// InternalIAMService.RegisterResource. Mirrors the vpc SyncRegistrar
// (services/vpc/internal/clients/iam_sync_registrar.go) but with BEST-EFFORT
// (never fail-closed) semantics — see the type doc below.
//
// nlb был единственным ресурс-сервисом БЕЗ sync-registrar: Create эмитил только
// `fga_register_outbox`-intent → async register-drainer → kacho-iam
// RegisterResource → reconciler. Owner/project-grant создателя становился виден
// с большим лагом → первый create→immediate-Get/Update своего ресурса ловил
// transient 403/404 (read-your-writes окно), что раздувало newman
// `retry_until_authorized`-busy-wait. Этот registrar регистрирует containment/
// owner-tuple СИНХРОННО сразу после durable commit ресурса, закрывая окно —
// но durable outbox-intent + register-drainer остаются at-least-once backstop'ом.
package iam

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	iampb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Registrar — порт синхронной owner-tuple регистрации, потребляемый create-
// use-case'ами (loadbalancer/listener/targetgroup). Реализация — *SyncRegistrar
// поверх InternalIAMService.RegisterResource. Определён здесь (adapter-пакет)
// в соответствии с конвенцией nlb: port-интерфейсы peer-адаптеров живут в
// clients/iam и алиасятся в use-case ports.go (ProjectClient/CheckClient — так же).
//
// Семантика вызывающего — BEST-EFFORT: use-case вызывает Register ПОСЛЕ durable
// commit ресурса и его `fga_register_outbox`-intent'а; ошибку ЛОГИРУЕТ и
// ГЛОТАЕТ — НЕ пробрасывает, НЕ гейтит Operation.done, НЕ фейлит Operation.
// Гейт видимости owner-tuple на done = phantom-ресурс (ban #9) — запрещён.
type Registrar interface {
	// Register синхронно регистрирует owner/containment-tuple'ы intent'а в
	// kacho-iam. Возвращает НЕ-nil только на transient-сбоях (iam недоступен и
	// т.п.), чтобы вызывающий залогировал best-effort; proxy-rejection'ы
	// (не-registrable relations) — не ошибка (см. SyncRegistrar.Register).
	Register(ctx context.Context, intent domain.FGARegisterIntent) error
}

// SyncRegistrar реализует Registrar поверх RegisterResourceClient (тот же mTLS-
// conn к kacho-iam :9091, что и register-drainer). Идемпотентен: повторная
// регистрация того же tuple (sync + async оба сработали) → OK (iam
// RegisterResource idempotent, read-delta).
type SyncRegistrar struct {
	cli     RegisterResourceClient
	timeout time.Duration
}

// NewSyncRegistrar собирает registrar поверх RegisterResourceClient. timeout по
// умолчанию 5s на один RegisterResource — create-worker идёт на background-ctx
// без дедлайна, поэтому ограничиваем per-call здесь (architecture.md per-call
// deadline). nil cli → Register no-op (dev/no-iam: остаётся только async drainer).
func NewSyncRegistrar(cli RegisterResourceClient) *SyncRegistrar {
	return &SyncRegistrar{cli: cli, timeout: 5 * time.Second}
}

// Register синхронно регистрирует каждый tuple intent'а через kacho-iam
// RegisterResource с per-call deadline, форвардя mirror-поля (labels/parent) и
// монотонный source_version (см. ниже). НЕ короткозамыкается: атакует ВСЕ tuple'ы
// даже при ошибке на предыдущем (containment `project`-tuple — первый в intent'е —
// всегда попадёт в попытку). Возвращает объединённую ошибку ТОЛЬКО transient-
// сбоев (classifySyncRegisterErr); proxy-rejection не-registrable relation'ов —
// benign (не всплывает). nil cli → no-op.
func (s *SyncRegistrar) Register(ctx context.Context, intent domain.FGARegisterIntent) error {
	if s.cli == nil {
		return nil
	}
	// PropagateOutgoing: iam-side principal/identity extractor видит реальный
	// caller-ctx. Идентичность для least-priv fgaproxy-gate — из mTLS client-cert.
	ctx = auth.PropagateOutgoing(ctx)

	// source_version штампуется now(): sync-путь идёт ПОСЛЕ commit ресурса, а
	// outbox-emitter штампует intent DB-clock'ом ВНУТРИ writer-TX (до commit) →
	// now() >= DB-stamp (монотонно). kacho-iam resource_mirror применяет
	// last-SOURCE-state-wins: sync (больший source_version) выигрывает, а
	// последующий async re-apply (меньший DB-stamp) — stale no-op (без лишней
	// перезаписи зеркала). Зеркалит vpc SyncRegistrar.
	sv := timestamppb.Now()

	var errs []error
	for _, t := range intent.Tuples {
		cctx, cancel := context.WithTimeout(ctx, s.timeout)
		_, err := s.cli.RegisterResource(cctx, &iampb.RegisterResourceRequest{
			SubjectId:       t.SubjectID,
			Relation:        t.Relation,
			Object:          t.Object,
			TraceId:         intent.ResourceID,
			Labels:          intent.Labels,
			ParentProjectId: intent.ParentProjectID,
			ParentAccountId: intent.ParentAccountID,
			SourceVersion:   sv,
		})
		cancel()
		if cerr := classifySyncRegisterErr(err); cerr != nil {
			errs = append(errs, fmt.Errorf("owner-tuple %s: %w", t.Object, cerr))
		}
	}
	return errors.Join(errs...)
}

// classifySyncRegisterErr классифицирует per-tuple RegisterResource-ответ для
// BEST-EFFORT sync-пути:
//
//   - nil / AlreadyExists → nil (применён либо idempotent OK — повторный tuple);
//   - PermissionDenied / InvalidArgument → nil (BENIGN): tuple НЕ registrable
//     через iam-proxy — либо не-containment relation (nlb эмитит creator `admin`
//     и parent-link `load_balancer`, а least-priv proxy-policy принимает только
//     {project,account,parent,owner}), либо malformed. Async register-drainer его
//     ТОЖЕ не применяет (identical), поэтому это НЕ сбой видимости, а ожидаемый
//     отказ policy — всплытие наружу дало бы per-create лог-шум. Containment
//     `project`-tuple accepted, его успех и закрывает read-your-writes окно.
//   - transient (Unavailable/DeadlineExceeded/Internal/…) → raw: iam не обработал
//     → use-case логирует best-effort, register-drainer досведёт из durable outbox.
//
// Замечание про сокрытие: use-case всё равно ГЛОТАЕТ любой возврат (best-effort),
// поэтому классификация влияет ТОЛЬКО на то, что попадёт в лог. Authz-мисконфиг
// (даже `project`-tuple → PermissionDenied) authoritative всплывает через
// register-drainer retry/poison-метрики — sync-путь опционален by design.
func classifySyncRegisterErr(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err // non-status (raw transport) — transient
	}
	switch st.Code() {
	case codes.OK, codes.AlreadyExists:
		return nil
	case codes.PermissionDenied, codes.InvalidArgument:
		return nil // benign proxy-rejection — см. doc
	default:
		return err // transient — всплывает для best-effort лога
	}
}

// Compile-time check.
var _ Registrar = (*SyncRegistrar)(nil)
