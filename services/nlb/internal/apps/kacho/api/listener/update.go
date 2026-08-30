// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/H-BF/corlib/pkg/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// UpdateUseCase — async Update Listener.
//
// Sync (handler-thread):
//  1. listener_id required.
//  2. repo.Reader Listener.Get (NotFound иначе).
//  3. update_mask discipline (единая для всех ресурсов, api-conventions):
//     - empty mask → full-object PATCH: применяются все mutable-поля из тела;
//     immutable из тела silently игнорируются (parity с loadbalancer/targetgroup).
//     - unknown field → InvalidArgument "field '<X>' is not recognised in update_mask".
//     - immutable field (load_balancer_id / protocol / port / project_id)
//     → InvalidArgument по конвенции Kachō
//     `"<field> is immutable after Listener.Create"`; у project_id к
//     каноническому тексту добавлен следующий шаг, потому что он у клиента
//     есть — глагол переноса владельца (#1671). Тексты — таблицей ниже.
//  4. Validate per-mask field (name regex, labels schema, etc).
//  5. default_target_group_id same-region precheck  — async-soft
//     либо sync; здесь делаем sync через kacho-nlb local TG.Get (same-DB
//     query); cross-region → FailedPrecondition фиксированный текст.
//  6. opsRepo.CreateWithPrincipal + operations.Run.
//
// Async worker (одна writer-TX, без status-CAS transition):
//  1. repo.Writer.Listeners.Update (OCC по expectedXmin) — concurrent-modify
//     между sync Get и worker-Update → FailedPrecondition.
//  2. outbox emit `nlb_listener:<id> UPDATED`.
//  3. FGA-register mirror-feed re-emit — ТОЛЬКО когда меняются labels (emitMirror);
//     иначе mirror-no-op.
//  4. Commit; operations-framework помечает Operation done (response=Listener).
//
// UPDATING как persisted-статус НЕ выставляется: одно-tx Update проще и атомарен,
// а UPDATING был бы лишь transient-проекцией in-flight Operation (caller поллит
// Operation.done). См. inline-комментарий в doUpdate.
type UpdateUseCase struct {
	repo    RepoFactory
	opsRepo OperationsRepo
	// checkClient — object-scoped authz-gate для caller-supplied `targetGroupId`
	// (per-RPC interceptor скоупит только сам Listener). nil НЕ означает
	// «пропустить»: отсутствие решателя — отказ (`Unavailable`). См. tg_ref.go.
	checkClient CheckClient
	logger      *slog.Logger
	registrar   Registrar
}

// NewUpdateUseCase — конструктор.
func NewUpdateUseCase(repo RepoFactory, opsRepo OperationsRepo, logger *slog.Logger) *UpdateUseCase {
	return &UpdateUseCase{repo: repo, opsRepo: opsRepo, logger: logger}
}

// WithRegistrar подключает sync-primary owner-tuple registrar. Смена меток меняет
// ПРОЕКЦИЮ, которую читает селектор владельца прав, поэтому обновлённое зеркало
// доставляется на пути запроса — как регистрация на Create. Durable-intent
// остаётся at-least-once backstop'ом, но ждать только его значит отдать ОТЗЫВ по
// снятию метки глубине очереди (замер соседнего сервиса 2026-08-05: 188–365 с при
// клиентском бюджете чтения-своих-записей 15 с). nil → sync-путь пропускается.
func (u *UpdateUseCase) WithRegistrar(r Registrar) *UpdateUseCase {
	u.registrar = r
	return u
}

// syncRegister — BEST-EFFORT sync-доставка mirror-intent'а после durable commit.
// Отказ ЛОГИРУЕТСЯ и ГЛОТАЕТСЯ: durable fga_register_outbox-intent + drainer —
// at-least-once backstop; Operation.done НЕ гейтится на видимость (ban #9).
func (u *UpdateUseCase) syncRegister(ctx context.Context, intent domain.FGARegisterIntent, intentVersion time.Time) {
	if u.registrar == nil {
		return
	}
	if err := u.registrar.Register(ctx, intent, intentVersion); err != nil {
		loggerOrDiscard(u.logger).Warn("Listener.Update sync mirror registration incomplete; register-drainer will reconcile",
			"err", err, "listener_id", intent.ResourceID)
	}
}

// WithCheckClient подключает object-scoped authz-gate для caller-supplied
// `targetGroupId` (`viewer` на `nlb_target_group:<id>`). Per-RPC interceptor
// авторизует caller'а только на самом Listener, поэтому TG репойнта — необойдённый
// caller-supplied объект (CWE-863). nil НЕ означает «пропустить»: отсутствие
// решателя — отказ (`Unavailable`). Возвращает self.
func (u *UpdateUseCase) WithCheckClient(c CheckClient) *UpdateUseCase {
	u.checkClient = c
	return u
}

// Mutable update_mask paths (single source of truth).
var listenerMutableMaskPaths = map[string]struct{}{
	"name":                    {},
	"description":             {},
	"labels":                  {},
	"default_target_group_id": {},
	// NLB-1b EXPAND (additive): repoint the wired target group (LIVE-mutable).
	"target_group_id": {},
}

// Immutable update_mask paths (in mask → InvalidArgument with фиксированный текст).
// VIP консолидирован на LoadBalancer: address_id/ip_version/subnet_id/region_id
// сняты с листенера (proto reserved), поэтому в immutable-списке их больше нет —
// неизвестный путь → "field '<x>' is not recognised in update_mask".
//
// Таблица путь→ТЕКСТ, а не набор путей под общим форматом (#1671). Причин две, и
// вторая сильнее первой. Первая: у области владения есть следующий шаг, а у
// остальных трёх полей его нет, поэтому один формат на всех выразить это не
// может. Вторая: пока текст собирался форматом, полоса слушателя была НЕВИДИМА
// переписи отказов по литералу — она находила две полосы из трёх и молчала о
// третьей. Полосы сведены к одной форме записи, общей с балансировщиком и
// группой целей, именно затем, чтобы распознаватель видел их все.
var listenerImmutableMaskPaths = map[string]string{
	"load_balancer_id": "load_balancer_id is immutable after Listener.Create",
	"protocol":         "protocol is immutable after Listener.Create",
	"port":             "port is immutable after Listener.Create",
	// Собственного глагола переноса у слушателя нет и не заводится: его проект
	// денормализован с балансировщика, и Move владельца переставляет слушателей
	// каскадом, в той же транзакции. Отказ называет глагол ВЛАДЕЛЬЦА — обещать
	// `ListenerService.Move` значило бы объявить возможность, которой нет.
	"project_id": "project_id is immutable after Listener.Create; " +
		"use NetworkLoadBalancerService.Move on the parent load balancer",
}

// Run — sync validate + spawn worker. Errors mapped to gRPC codes inline.
func (u *UpdateUseCase) Run(ctx context.Context, req *lbv1.UpdateListenerRequest) (*operations.Operation, error) {
	id := req.GetListenerId()
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "listener_id required")
	}
	if err := validateListenerID(id); err != nil {
		return nil, err
	}

	mask := req.GetUpdateMask().GetPaths()
	if err := validateListenerMask(mask); err != nil {
		return nil, err
	}

	// Load current row (verifies existence; needed for same-region precheck +
	// merge for partial Update).
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	cur, err := rd.Listeners().Get(ctx, id)
	if err != nil {
		_ = rd.Close()
		return nil, mapDomainErr(err)
	}
	expectedXmin := cur.Xmin // OCC snapshot for the worker-side Update.

	// Apply mask-driven mutations on a copy of current domain entity. Empty mask →
	// full-object PATCH: apply пропускает все mutable-поля (parity с
	// loadbalancer.applyUpdateMask / targetgroup.applyUpdateMaskTG).
	next := cur.Listener
	apply := func(field string) bool {
		if len(mask) == 0 {
			return true
		}
		for _, p := range mask {
			if p == field {
				return true
			}
		}
		return false
	}
	tgRegionCheckNeeded := false
	tgIDToCheck := ""
	// Имя на правке судит ЕДИНСТВЕННАЯ функция дерева (validate.NameOnUpdate):
	// пять исходов маски и значения. Ветка, ради которой она здесь, —
	// ПОЛНАЯ правка с пустым именем: в proto3 пропущенное и пустое поле
	// неразличимы, поэтому пустое там означает «не прислали», и имя остаётся
	// прежним. До этого оно записывалось пустым и умирало на Validate() как
	// «name is required» — то есть правку описания полным PATCH'ем нельзя было
	// сделать вовсе, не назвав заодно имя.
	applyName, nerr := corevalidate.NameOnUpdate("name", mask, req.GetName())
	if nerr != nil {
		_ = rd.Close()
		return nil, nerr
	}
	if applyName {
		next.Name = domain.LbName(req.GetName())
	}
	if apply("description") {
		d := domain.LbDescription(req.GetDescription())
		if err := d.Validate(); err != nil {
			_ = rd.Close()
			return nil, err
		}
		next.Description = d
	}
	if apply("labels") {
		lbls := domain.LabelsFromMap(req.GetLabels())
		if err := domain.ValidateLabels(lbls); err != nil {
			_ = rd.Close()
			return nil, err
		}
		next.Labels = lbls
	}
	// NLB-1b EXPAND (additive): target_group_id and the legacy default_target_group_id
	// both map to the listener's TG reference. Only a field present in the mask is
	// applied; target_group_id takes precedence when both are applied and non-empty.
	// An applied field with an empty value clears the reference.
	applyTG := apply("target_group_id")
	applyDTG := apply("default_target_group_id")
	if applyTG || applyDTG {
		tg := ""
		switch {
		case applyTG && req.GetTargetGroupId() != "":
			tg = req.GetTargetGroupId()
		case applyDTG && req.GetDefaultTargetGroupId() != "":
			tg = req.GetDefaultTargetGroupId()
		}
		if tg == "" {
			next.DefaultTargetGroupID = option.ValueOf[domain.ResourceID]{}
		} else {
			next.DefaultTargetGroupID = option.MustNewOption(domain.ResourceID(tg))
			tgIDToCheck = tg
			tgRegionCheckNeeded = true
		}
	}
	// Owning-project + authz + same-region precheck for the (re)wired target group.
	// The referenced TG must belong to the listener's OWN project: repointing at a
	// victim project's TargetGroup would forward this LB's traffic to the victim's
	// targets (CWE-639 — the interceptor scopes only the LISTENER object). An
	// unresolved reference (missing OR foreign-project) reports the repo's own
	// not-found verbatim, so the two stay indistinguishable (security.md #6); the
	// composite FK (0023) is the atomic backstop for the precheck→worker race.
	if tgRegionCheckNeeded {
		tg, terr := lookupWiredTargetGroup(ctx, rd.TargetGroups(), tgIDToCheck, string(cur.ProjectID))
		_ = rd.Close()
		if terr != nil {
			if errors.Is(terr, errTargetGroupUnresolved) {
				return nil, targetGroupMissErr(tgIDToCheck)
			}
			return nil, mapDomainErr(terr)
		}
		// Object-scoped authz AFTER the owning-project resolve — a cross-project id
		// must stay hidden as a miss (a PermissionDenied would confirm it exists).
		if err := checkTargetGroupViewer(ctx, u.checkClient, tgIDToCheck); err != nil {
			return nil, err
		}
		if tg.RegionID != cur.RegionID {
			return nil, status.Errorf(codes.FailedPrecondition,
				"default target group region %s does not match listener region %s",
				tg.RegionID, cur.RegionID)
		}
	} else {
		_ = rd.Close()
	}

	// Re-validate the merged domain entity (defence in depth — partial fields
	// already validated above; this catches cross-field invariants).
	if err := next.Validate(); err != nil {
		return nil, err
	}

	// Create Operation row.
	op, err := operations.NewFromContext(ctx,
		ids.PrefixOperationNLB,
		fmt.Sprintf("Update listener %s", string(next.Name)),
		&lbv1.UpdateListenerMetadata{ListenerId: id},
	)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	principal := operations.PrincipalFromContext(ctx)
	if err := u.opsRepo.CreateWithPrincipal(ctx, op, principal); err != nil {
		return nil, mapDomainErr(err)
	}

	// (parity with LB/TG update.go labelsInMask):
	// re-emit the FGA-register mirror-feed (carrying the NEW labels) ONLY when
	// labels change — labels in mask, or empty mask (full PATCH always reapplies
	// labels). A non-labels Update is a mirror no-op (skip the intent). Full
	// label removal → mirror.upsert with empty labels (NOT Unregister) — the
	// listener still lives; this stales label selectors without dropping the
	// resource registration.
	emitMirror := listenerLabelsInMask(mask)

	// Snapshot inputs into worker closure to avoid handler-ctx capture.
	snap := next
	operations.Run(ctx, u.opsRepo, op.ID, func(workerCtx context.Context) (*anypb.Any, error) {
		return u.doUpdate(workerCtx, snap, expectedXmin, emitMirror)
	})
	return &op, nil
}

// listenerLabelsInMask reports whether the Update touches labels: explicit
// "labels" in the mask, or an empty mask (full-object PATCH reapplies all mutable
// fields). Parity with loadbalancer.labelsInMask / targetgroup.labelsInMaskTG.
func listenerLabelsInMask(mask []string) bool {
	if len(mask) == 0 {
		return true
	}
	for _, p := range mask {
		if p == "labels" {
			return true
		}
	}
	return false
}

// validateListenerMask — verifies every path is one of mutable; rejects
// immutable + unknown.
func validateListenerMask(paths []string) error {
	for _, p := range paths {
		if msg, ok := listenerImmutableMaskPaths[p]; ok {
			return status.Errorf(codes.InvalidArgument, "%s", msg)
		}
		if _, ok := listenerMutableMaskPaths[p]; ok {
			continue
		}
		return status.Errorf(codes.InvalidArgument,
			"field '%s' is not recognised in update_mask", p)
	}
	return nil
}

// doUpdate — worker-side flow. When emitMirror is true (labels changed),
// the FGA-register mirror-feed intent is written in the SAME
// writer-tx as the resource UPDATE (no dual-write); the emitter
// stamps a monotonic source_version so IAM applies the mirror last-source-wins.
func (u *UpdateUseCase) doUpdate(ctx context.Context, next domain.Listener, expectedXmin string, emitMirror bool) (*anypb.Any, error) {
	// Transient UPDATING status guard. CAS handles concurrent Delete (status
	// already DELETING → FailedPrecondition; client sees фиксированный текст).
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		w.Abort()
	}()

	// We don't lock status to UPDATING in DB transition (one-tx Update is
	// simpler + atomic). UPDATING is a transient projection of in-flight
	// Operation — caller polls Operation.done, sees done=true with the new
	// row. This mirrors kacho-vpc Network.Update flow.
	updated, err := w.Listeners().Update(ctx, &next, expectedXmin)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	if err := w.Outbox().Emit(ctx,
		kachorepo.OutboxResourceListener, string(updated.ID), string(updated.ProjectID),
		kachorepo.OutboxActionUpdated, kachorepo.ListenerStatePayload(updated),
	); err != nil {
		return nil, mapDomainErr(fmt.Errorf("%w: outbox emit listener UPDATED: %v", domain.ErrInternal, err))
	}
	// refresh the IAM resource_mirror with the current
	// labels in the SAME writer-tx (gated, upsert-not-unregister,
	// atomic). Label removal → upsert with empty labels, which stales the γ
	// label selector while keeping the listener registered.
	// Намерение строится ОДИН раз и доставляется обеими доставками — этой и
	// дренажом той же строки. Прежде его собирали дважды: очередь получала одно
	// значение, синхронный вызов — второе, построенное заново, и разойтись они
	// могли бы молча. Объявление стоит ВНЕ ветки, потому что доставка идёт после
	// коммита — то есть за пределами блока, где намерение эмитится.
	var (
		intent        domain.FGARegisterIntent
		intentVersion time.Time
	)
	if emitMirror {
		intent = listenerMirrorIntent(updated)
		var eerr error
		intentVersion, eerr = w.FGARegisterOutbox().Emit(ctx, domain.FGAEventRegister, intent)
		if eerr != nil {
			return nil, mapDomainErr(fmt.Errorf("%w: fga register-intent emit: %v", domain.ErrInternal, err))
		}
	}
	if err := w.Commit(); err != nil {
		return nil, mapDomainErr(err)
	}
	if emitMirror {
		u.syncRegister(ctx, intent, intentVersion)
	}
	committed = true
	return marshalListener(updated)
}

// listenerMirrorIntent builds the mirror-feed register-intent for an
// UPDATED Listener: the project-hierarchy tuple (re-register is idempotent in IAM)
// carrying the refreshed labels + parent-project so kacho-iam updates its
// resource_mirror. No creator tuple — Update never re-assigns ownership; this is a
// pure labels-refresh feed (parity with lbMirrorIntent / tgMirrorIntent). Empty
// labels (full removal) is a valid upsert payload — it stales the label selector
// without unregistering the listener. source_version is stamped by the
// outbox emitter from the DB clock inside the writer-tx.
//
// THE TUPLE MUST BE THE PROJECT ONE, and that is the whole substance of this feed.
// kacho-iam writes resource_mirror only as a side effect of RegisterResource, and
// it guards that proxy write-path with a least-privilege rule over the whole tuple
// (pkg/authz/proxytuple.ValidateTuple) evaluated BEFORE the mirror UPSERT. nlb's
// parent-link relation `load_balancer` is not accepted by it (see
// listenerRegisterIntent). Feeding the refresh through the
// parent-link alone made every labels-Update a PermissionDenied that dropped the
// labels payload on the floor: the mirror kept the labels a listener had at
// creation, so clearing the label an ARM_LABELS grant selects on revoked nothing
// (T31-LBLREVOKE-NLB-LISTENER-04 `lsn-post-revoke-deny` stayed {"allowed":true}
// indefinitely — not a consistency lag). The project tuple is registrable, so the
// refresh actually lands, and it is the same tuple the Create path registers — a
// re-register is idempotent in IAM.
func listenerMirrorIntent(l *kachorepo.ListenerRecord) domain.FGARegisterIntent {
	id := string(l.ID)
	return domain.FGARegisterIntent{
		Kind:       "Listener",
		ResourceID: id,
		Tuples: []domain.FGATuple{
			domain.FGAProjectTuple(domain.FGAObjectTypeListener, id, string(l.ProjectID)),
		},
		Labels:          domain.LabelsToMap(l.Labels),
		ParentProjectID: string(l.ProjectID),
	}
}
