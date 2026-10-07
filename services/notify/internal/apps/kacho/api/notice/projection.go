// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package notice

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"

	"github.com/PRO-Robotech/kacho/services/notify/internal/notice/rules"
)

// Проекции контракта — одна функция на сообщение. Моменты усечены до секунды
// повторно на выходе (`api-timestamp-truncate`); незаданный момент — nil, и это
// значит «такого перехода не было», и только это (Р5).

func ts(t time.Time) *timestamppb.Timestamp { return timestamppb.New(t.UTC().Truncate(time.Second)) }

func tsp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return ts(*t)
}

// KindProto — вид в перечне контракта.
func KindProto(k rules.Kind) notifyv1.Notice_Kind {
	if v, ok := notifyv1.Notice_Kind_value[string(k)]; ok {
		return notifyv1.Notice_Kind(v)
	}
	return notifyv1.Notice_KIND_UNSPECIFIED
}

// KindOf — вид из перечня контракта; false — значение вне таблицы видов
// (`KIND_UNSPECIFIED` и числа, которых контракт не объявляет).
func KindOf(k notifyv1.Notice_Kind) (rules.Kind, bool) {
	if k == notifyv1.Notice_KIND_UNSPECIFIED {
		return "", false
	}
	name, ok := notifyv1.Notice_Kind_name[int32(k)]
	if !ok {
		return "", false
	}
	kind := rules.Kind(name)
	if _, known := rules.RuleOf(kind); !known {
		return "", false
	}
	return kind, true
}

func stateProto(s rules.State) notifyv1.Notice_State {
	if v, ok := notifyv1.Notice_State_value[string(s)]; ok {
		return notifyv1.Notice_State(v)
	}
	return notifyv1.Notice_STATE_UNSPECIFIED
}

func categoryProto(k rules.Kind) notifyv1.Notice_Category {
	r, ok := rules.RuleOf(k)
	if !ok {
		return notifyv1.Notice_CATEGORY_UNSPECIFIED
	}
	if v, ok := notifyv1.Notice_Category_value[string(r.Category)]; ok {
		return notifyv1.Notice_Category(v)
	}
	return notifyv1.Notice_CATEGORY_UNSPECIFIED
}

func refsProto(refs []Ref) []*notifyv1.AffectedResource {
	if len(refs) == 0 {
		return nil
	}
	out := make([]*notifyv1.AffectedResource, len(refs))
	for i, r := range refs {
		out[i] = &notifyv1.AffectedResource{Type: r.Type, Id: r.ID}
	}
	return out
}

func audienceProto(n Notice) *notifyv1.NoticeAudience {
	a := &notifyv1.NoticeAudience{AllAccounts: n.AllAccounts}
	for _, s := range n.Audience {
		switch s.Type {
		case ScopeAccount:
			a.AccountIds = append(a.AccountIds, s.ID)
		case ScopeProject:
			a.ProjectIds = append(a.ProjectIds, s.ID)
		}
	}
	return a
}

// Internal — внутренняя проекция (Р18). Счётчики строк доставки — строки
// адресатов и кандидатов стадии S2; в схеме S1 таких строк нет, и пустой набор
// значит «строк доставки нет», и только это.
func Internal(n Notice) *notifyv1.InternalNotice {
	out := &notifyv1.InternalNotice{
		Id:                n.ID,
		Kind:              KindProto(n.Kind),
		Category:          categoryProto(n.Kind),
		State:             stateProto(n.State),
		StartsAt:          ts(n.StartsAt),
		EndsAt:            tsp(n.EndsAt),
		StartedAt:         tsp(n.StartedAt),
		CompletedAt:       tsp(n.CompletedAt),
		CancelledAt:       tsp(n.CancelledAt),
		CreatedAt:         ts(n.CreatedAt),
		UpdatedAt:         ts(n.UpdatedAt),
		AffectedResources: refsProto(n.Affected),
		Audience:          audienceProto(n),
		CreatedBy:         n.CreatedBy,
		Revision:          n.Revision,
	}
	for _, at := range n.Reminders {
		out.Reminders = append(out.Reminders, &notifyv1.NoticeReminder{At: ts(at), Stage: string(rules.StageReminder)})
	}
	return out
}

// Public — публичная проекция (Р16). affected — уже суженные ссылки (Get) либо
// nil (списки поле не заполняют).
func Public(n Notice, affected []Ref) *notifyv1.Notice {
	return &notifyv1.Notice{
		Id:                n.ID,
		Kind:              KindProto(n.Kind),
		Category:          categoryProto(n.Kind),
		State:             stateProto(n.State),
		StartsAt:          ts(n.StartsAt),
		EndsAt:            tsp(n.EndsAt),
		StartedAt:         tsp(n.StartedAt),
		CompletedAt:       tsp(n.CompletedAt),
		CancelledAt:       tsp(n.CancelledAt),
		CreatedAt:         ts(n.CreatedAt),
		UpdatedAt:         ts(n.UpdatedAt),
		AffectedResources: refsProto(affected),
	}
}
