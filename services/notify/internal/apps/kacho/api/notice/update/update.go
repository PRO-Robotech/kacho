// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package update — use-case `InternalNoticeService.Update`: перенос моментов
// извещения в SCHEDULED (приёмка NTF-5 Р5; замысел issue-2924 З5 п.4, п.5, З7).
package update

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	operationv1 "github.com/PRO-Robotech/corelib/api/corelib/operation"
	"github.com/PRO-Robotech/corelib/operations/operationspb"
	corevalidate "github.com/PRO-Robotech/corelib/validate"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"

	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice"
	"github.com/PRO-Robotech/kacho/services/notify/internal/notice/rules"
)

// immutable — поля, которые Update не меняет никогда, в написании текста
// отказа (Р5): проверяются ДО известного набора, чтобы отказ назвал
// неизменяемость, а не «неизвестное поле» (`api-gotcha-immutable-first`).
var immutable = map[string]string{
	"kind":               "kind",
	"audience":           "audience",
	"affected_resources": "affectedResources",
}

// known — изменяемые поля маски в обеих формах имени.
var known = map[string]struct{}{
	"starts_at": {}, "startsAt": {},
	"ends_at": {}, "endsAt": {},
}

// UseCase — перенос моментов.
type UseCase struct {
	store notice.Store
	now   notice.Clock
	lead  time.Duration
}

// New — use-case над хранилищем, часами notify и ручкой
// KACHO_NOTIFY_NOTICE_REMINDER_LEAD (напоминание MAINTENANCE, З7 п.1, З20).
func New(store notice.Store, now notice.Clock, lead time.Duration) *UseCase {
	return &UseCase{store: store, now: now, lead: lead}
}

func invalid(format string, a ...any) error {
	return status.Errorf(codes.InvalidArgument, format, a...)
}

// Execute: форма id → маска (неизменяемые, затем известный набор) → значения
// («в будущем» — часами notify; прочее судят ограничения записи) → одна
// транзакция записи с условием на состояние, вид и неравенство значений.
// Вид до записи не читается: он в условии записи (З5 п.5).
func (u *UseCase) Execute(ctx context.Context, req *notifyv1.UpdateNoticeRequest) (*operationv1.Operation, error) {
	id := req.GetNoticeId()
	if err := notice.ValidateID(id); err != nil {
		return nil, err
	}
	starts, ends, full, err := fields(req.GetUpdateMask().GetPaths())
	if err != nil {
		return nil, err
	}
	now := notice.Seconds(u.now())
	in := notice.UpdateInput{ID: id, Now: now, Lead: u.lead}
	if in.StartsAt, err = moment("startsAt", req.GetStartsAt(), starts, full); err != nil {
		return nil, err
	}
	if in.StartsAt != nil && !in.StartsAt.After(now) {
		return nil, invalid("startsAt: must be in the future")
	}
	if in.EndsAt, err = moment("endsAt", req.GetEndsAt(), ends, full); err != nil {
		return nil, err
	}
	p, err := notice.Caller(ctx)
	if err != nil {
		return nil, err
	}
	t := rules.TransitionOf(rules.VerbUpdate)
	fin, done := notice.Capture(notice.DoneFinisher("Update notice",
		&notifyv1.UpdateNoticeMetadata{NoticeId: id}, now, p))
	if _, err := u.store.Update(ctx, in, p, fin); err != nil {
		return nil, notice.Refusal(id, rules.VerbUpdate, t, err)
	}
	return operationspb.ToProto(done()), nil
}

// fields разбирает маску: неизменяемое поле — отказ с его именем; неизвестное —
// отказ с его именем (`api-mask-unknown`); пустая маска — полная правка
// изменяемых полей тела (`api-mask-empty-full-patch`), full = true.
func fields(paths []string) (starts, ends, full bool, err error) {
	if len(paths) == 0 {
		return true, true, true, nil
	}
	for _, p := range paths {
		if name, ok := immutable[corevalidate.CanonFieldName(p)]; ok {
			return false, false, false, invalid("%s is immutable after Notice.Create", name)
		}
	}
	for _, p := range paths {
		if _, ok := known[p]; !ok {
			return false, false, false, invalid("update_mask: unknown field %s", p)
		}
		switch corevalidate.CanonFieldName(p) {
		case "starts_at":
			starts = true
		case "ends_at":
			ends = true
		}
	}
	return starts, ends, false, nil
}

// moment — значение изменяемого момента. Поле вне маски — не меняется (nil).
// Поле, названное маской и незаданное, — `<field>: required`; при полной правке
// незаданное поле тела не меняется.
func moment(field string, ts *timestamppb.Timestamp, set, full bool) (*time.Time, error) {
	if !set {
		return nil, nil
	}
	if ts == nil {
		if full {
			return nil, nil
		}
		return nil, invalid("%s: required", field)
	}
	if err := ts.CheckValid(); err != nil {
		return nil, invalid("%s: invalid timestamp", field)
	}
	v := notice.Seconds(ts.AsTime())
	return &v, nil
}
