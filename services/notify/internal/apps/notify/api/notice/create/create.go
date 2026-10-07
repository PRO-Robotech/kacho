// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package create — use-case `InternalNoticeService.Create`, приём заявки
// (приёмка NTF-5 Р4, Р10 шаг 1; замысел issue-2924 З3 п.1).
//
// notify-api проверяет вход синхронно, в порядке Р4, до записи, и одной
// транзакцией пишет заявку создания и операцию done=false (Ф1). Справочник он
// не зовёт: области проверяет у владельца notify-sender (Р10 шаги 2–6).
package create

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	operationv1 "github.com/PRO-Robotech/corelib/api/corelib/operation"
	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/operations/operationspb"
	corevalidate "github.com/PRO-Robotech/corelib/validate"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"

	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/notice"
	"github.com/PRO-Robotech/kacho/services/notify/internal/notice/rules"
)

// UseCase — приём заявки Create.
type UseCase struct {
	store notice.Store
	now   notice.Clock
}

// New — use-case над хранилищем и часами notify.
func New(store notice.Store, now notice.Clock) *UseCase {
	return &UseCase{store: store, now: now}
}

// Execute проверяет вход, пишет заявку с операцией done=false и отдаёт
// операцию. Момент приёма — часы notify, усечённые до секунды один раз (З3 п.1).
func (u *UseCase) Execute(ctx context.Context, req *notifyv1.CreateNoticeRequest) (*operationv1.Operation, error) {
	now := notice.Seconds(u.now())
	r, err := Validate(req, now)
	if err != nil {
		return nil, err
	}
	p, err := notice.Caller(ctx)
	if err != nil {
		return nil, err
	}
	r.NoticeID = ids.NewHyphenID(ids.PrefixNoticeHyphen)
	r.CreatedBy = notice.Subject(p)
	r.AcceptedAt = now

	op, err := notice.NewOperation("Create notice", &notifyv1.CreateNoticeMetadata{NoticeId: r.NoticeID},
		r.NoticeID, now, p)
	if err != nil {
		return nil, notice.StorageFailure()
	}
	r.OperationID = op.ID
	if err := u.store.Accept(ctx, r, op, p); err != nil {
		return nil, notice.StorageFailure()
	}
	return operationspb.ToProto(&op), nil
}

func invalid(format string, a ...any) error {
	return status.Errorf(codes.InvalidArgument, format, a...)
}

// moment — момент входа: незадан — nil; неверный — отказ с именем поля.
func moment(field string, ts *timestamppb.Timestamp) (*time.Time, error) {
	if ts == nil {
		return nil, nil
	}
	if err := ts.CheckValid(); err != nil {
		return nil, invalid("%s: invalid timestamp", field)
	}
	v := notice.Seconds(ts.AsTime())
	return &v, nil
}

// Validate — проверки входа Create в порядке Р4: вид и поля вида → аудитория
// (форма, число, обязательность элементов, форма id) → затронутые ресурсы
// (число, тип, обязательность id, форма id). Первый нарушенный пункт даёт
// ответ. now — момент приёма, по нему судится «в будущем».
func Validate(req *notifyv1.CreateNoticeRequest, now time.Time) (notice.CreateRequest, error) {
	var r notice.CreateRequest

	kind, ok := notice.KindOf(req.GetKind())
	if !ok {
		return r, invalid("kind: required")
	}
	rule, _ := rules.RuleOf(kind)
	r.Kind = kind

	startsAt, err := moment("startsAt", req.GetStartsAt())
	if err != nil {
		return r, err
	}
	endsAt, err := moment("endsAt", req.GetEndsAt())
	if err != nil {
		return r, err
	}
	switch rule.StartsAt {
	case rules.Required:
		if startsAt == nil {
			return r, invalid("startsAt: required")
		}
		if !startsAt.After(now) {
			return r, invalid("startsAt: must be in the future")
		}
	case rules.Forbidden:
		if startsAt != nil {
			return r, invalid("startsAt: not allowed for kind %s", kind)
		}
	}
	switch rule.EndsAt {
	case rules.Required:
		if endsAt == nil {
			return r, invalid("endsAt: required")
		}
		if !endsAt.After(*startsAt) {
			return r, invalid("endsAt: must be after startsAt")
		}
	case rules.Forbidden:
		if endsAt != nil {
			return r, invalid("endsAt: not allowed for kind %s", kind)
		}
	}
	if !rule.Affected && len(req.GetAffectedResources()) > 0 {
		return r, invalid("affectedResources: not allowed for kind %s", kind)
	}
	r.StartsAt, r.EndsAt = startsAt, endsAt

	if err := validateAudience(req.GetAudience(), kind, rule, &r); err != nil {
		return r, err
	}
	if err := validateAffected(req.GetAffectedResources(), &r); err != nil {
		return r, err
	}
	return r, nil
}

func validateAudience(a *notifyv1.NoticeAudience, kind rules.Kind, rule rules.KindRule, r *notice.CreateRequest) error {
	forms := 0
	if a.GetAllAccounts() {
		forms++
	}
	if len(a.GetAccountIds()) > 0 {
		forms++
	}
	if len(a.GetProjectIds()) > 0 {
		forms++
	}
	switch {
	case forms == 0:
		return invalid("audience: required")
	case forms > 1:
		return invalid("audience: exactly one of allAccounts, accountIds, projectIds")
	}
	if a.GetAllAccounts() {
		if !rule.AllAccounts {
			return invalid("audience.allAccounts: not allowed for kind %s", kind)
		}
		r.AllAccounts = true
		return nil
	}
	if ids := a.GetAccountIds(); len(ids) > 0 {
		if err := scopeIDs("audience.accountIds", notice.ScopeAccount, ids); err != nil {
			return err
		}
		r.AccountIDs = append([]string(nil), ids...)
		return nil
	}
	ids := a.GetProjectIds()
	if err := scopeIDs("audience.projectIds", notice.ScopeProject, ids); err != nil {
		return err
	}
	r.ProjectIDs = append([]string(nil), ids...)
	return nil
}

// scopeIDs — число, обязательность каждого элемента (отдельной проверкой ДО
// формы: проверка формы пустую строку пропускает, `api-resourceid-empty`), форма.
func scopeIDs(field, scopeType string, list []string) error {
	if len(list) > rules.MaxAudienceScopes {
		return invalid("%s: at most %d items", field, rules.MaxAudienceScopes)
	}
	for i, id := range list {
		if id == "" {
			return invalid("%s[%d]: required", field, i)
		}
	}
	for _, id := range list {
		if err := corevalidate.ResourceID(scopeType, "", id); err != nil {
			return err
		}
	}
	return nil
}

func validateAffected(list []*notifyv1.AffectedResource, r *notice.CreateRequest) error {
	if len(list) > rules.MaxAffectedResources {
		return invalid("affectedResources: at most %d items", rules.MaxAffectedResources)
	}
	for i, ref := range list {
		if !rules.IsTenantResourceType(ref.GetType()) {
			return invalid("affectedResources[%d].type: %s is not a tenant resource type", i, ref.GetType())
		}
	}
	for i, ref := range list {
		if ref.GetId() == "" {
			return invalid("affectedResources[%d].id: required", i)
		}
	}
	refs := make([]notice.Ref, 0, len(list))
	for _, ref := range list {
		if err := corevalidate.ResourceID(ref.GetType(), "", ref.GetId()); err != nil {
			return err
		}
		refs = append(refs, notice.Ref{Type: ref.GetType(), ID: ref.GetId()})
	}
	r.Affected = refs
	return nil
}
