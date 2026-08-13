// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package realization — приём отчётов узла о том, что он наблюдает.
package realization

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"
)

// Repo — порт записи наблюдаемого состояния.
type Repo interface {
	ApplyObservedState(ctx context.Context, rep repo.ObservedReport) (bool, int64, error)
}

// Service — use-case приёма отчётов.
type Service struct {
	repo Repo
}

// NewService собирает use-case.
func NewService(r Repo) *Service { return &Service{repo: r} }

// observedStates — закрытый набор принимаемых значений.
//
// Он совпадает с ограничением схемы поэлементно и проверяется здесь, ДО записи,
// ради имени поля в отказе: значение, которое база не примет, иначе вернулось бы
// отказом хранилища, по которому нельзя понять, что именно не так.
var observedStates = map[string]struct{}{
	"OBSERVED_STARTING":                {},
	"OBSERVED_RUNNING":                 {},
	"OBSERVED_STOPPING":                {},
	"OBSERVED_STOPPED":                 {},
	"OBSERVED_TERMINATED_UNEXPECTEDLY": {},
}

// maxReasonLen — предел пояснения. Причина адресована человеку, а не разбору;
// без предела узел мог бы писать в нашу базу произвольный объём.
const maxReasonLen = 1024

// Report — вход одного отчёта.
type Report struct {
	InstanceID string
	State      string
	SequenceNo int64
	ObservedAt time.Time
	Reason     string
}

// Result — исход приёма.
type Result struct {
	Applied    bool
	CurrentSeq int64
}

// Apply принимает отчёт узла.
//
// # Почему время узла не подменяется нашим
//
// Узел мог наблюдать факт заметно раньше, чем сумел о нём сообщить. Подстановка
// нашего времени стёрла бы эту разницу — единственный след того, что связь была
// потеряна. Поэтому пустое время отвергается по имени поля, а не заполняется
// умолчанием: умолчание здесь означало бы «наблюдение было сейчас», чего мы не
// знаем.
//
// # Отчёт из будущего отвергается
//
// Часы узла нам не подчиняются, и небольшое расхождение законно. Но время,
// опережающее наше существенно, означает не расхождение часов, а неверный
// источник времени; записав его, мы получили бы наблюдение, которое навсегда
// свежее любого следующего.
func (s *Service) Apply(ctx context.Context, rep Report) (Result, error) {
	if err := corevalidate.ResourceID("Instance", ids.PrefixInstanceHyphen, rep.InstanceID); err != nil {
		return Result{}, err
	}
	if _, ok := observedStates[rep.State]; !ok {
		return Result{}, serviceerr.InvalidArg("observed_state",
			"observedState is required and must name a state the node can observe")
	}
	if rep.SequenceNo <= 0 {
		return Result{}, serviceerr.InvalidArg("delivery_sequence_no",
			"deliverySequenceNo must be positive: zero would name a delivery that does not exist")
	}
	if rep.ObservedAt.IsZero() {
		return Result{}, serviceerr.InvalidArg("observed_at",
			"observedAt is required: substituting our own clock would erase the delay we need to see")
	}
	if rep.ObservedAt.After(time.Now().Add(clockSkewAllowance)) {
		return Result{}, serviceerr.InvalidArg("observed_at",
			"observedAt is too far in the future for a clock difference")
	}
	if len(rep.Reason) > maxReasonLen {
		return Result{}, serviceerr.InvalidArg("reason", "reason must be at most 1024 bytes")
	}

	applied, current, err := s.repo.ApplyObservedState(ctx, repo.ObservedReport{
		InstanceID: rep.InstanceID,
		State:      rep.State,
		SequenceNo: rep.SequenceNo,
		ObservedAt: rep.ObservedAt,
		Reason:     rep.Reason,
	})
	switch {
	case errors.Is(err, repo.ErrUnknownDelivery):
		// Расхождение между отправленным и сообщённым — состояние, а не ввод:
		// то же самое поле в другой момент было бы законным.
		return Result{}, status.Error(codes.FailedPrecondition,
			"report references a delivery that was never emitted for this instance")
	case err != nil:
		return Result{}, serviceerr.MapRepoErr(err)
	}
	return Result{Applied: applied, CurrentSeq: current}, nil
}

// clockSkewAllowance — допуск на расхождение часов узла с нашими.
//
// Величина названа здесь, а не подобрана в условии: она — решение про то,
// насколько мы доверяем чужим часам, и читать её надо там, где принимают отчёт.
const clockSkewAllowance = 5 * time.Minute
