// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package nodeownership — узел берёт машину в работу и отпускает её.
package nodeownership

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

// Repo — порт владения машиной.
type Repo interface {
	ClaimInstance(ctx context.Context, b repo.NodeBinding, graceAfterExpiry time.Duration) (*repo.NodeBinding, error)
	ReleaseInstance(ctx context.Context, instanceID, nodeID string) error
}

// Service — use-case владения.
type Service struct {
	repo  Repo
	grace time.Duration
}

// GraceAfterExpiry — запас сверх истечения аренды, до которого перехват НЕ
// законен.
//
// Величина названа здесь, а не подобрана в условии запроса, потому что она —
// решение о безопасности, а не настройка производительности. Узел обязан
// остановить машину до истечения аренды ПО СВОИМ ЧАСАМ; запас покрывает
// расхождение часов и время самой остановки. Меньший запас означал бы перехват
// у узла, который ещё не успел отпустить том, — то есть ровно двух писателей,
// от которых аренда и защищает.
const GraceAfterExpiry = 2 * time.Minute

// NewService собирает use-case.
func NewService(r Repo) *Service { return &Service{repo: r, grace: GraceAfterExpiry} }

// maxNodeIDLen — предел длины идентификатора узла; зеркалит ограничение схемы.
// Значение, которое база не примет, обязано быть отвергнуто здесь и по имени
// поля: отказ хранилища не называет поля, и по нему нельзя понять, что не так.
const maxNodeIDLen = 253

// ClaimReq — вход обмена владением.
type ClaimReq struct {
	InstanceID string
	NodeID     string
	SequenceNo int64
	LeaseUntil time.Time
}

// Binding — действующая привязка после обмена.
type Binding struct {
	NodeID     string
	SequenceNo int64
	LeaseUntil time.Time
}

// maxLeaseAhead — насколько вперёд узел вправе просить аренду.
//
// Без предела узел мог бы попросить аренду на годы вперёд и стать
// невытесняемым: его отказ оставил бы машину непереносимой навсегда, а
// восстановление потребовало бы правки базы руками.
const maxLeaseAhead = time.Hour

// Claim берёт машину в работу либо отказывает по состоянию.
//
// Продление аренды — ТОТ ЖЕ обмен, поэтому отдельного метода у use-case нет:
// «просто продлить» было бы записью без условия и возвращало бы владение тому,
// у кого его отобрали.
func (s *Service) Claim(ctx context.Context, req ClaimReq) (Binding, error) {
	if err := corevalidate.ResourceID("Instance", ids.PrefixInstanceHyphen, req.InstanceID); err != nil {
		return Binding{}, err
	}
	if l := len(req.NodeID); l == 0 || l > maxNodeIDLen {
		return Binding{}, serviceerr.InvalidArg("node_id", "nodeId is required and must be at most 253 characters")
	}
	if req.SequenceNo <= 0 {
		return Binding{}, serviceerr.InvalidArg("delivery_sequence_no",
			"deliverySequenceNo must be positive: zero would name a delivery that does not exist")
	}
	if req.LeaseUntil.IsZero() {
		return Binding{}, serviceerr.InvalidArg("lease_until",
			"leaseUntil is required: a claim without an expiry would hold the instance forever")
	}
	now := time.Now()
	if !req.LeaseUntil.After(now) {
		return Binding{}, serviceerr.InvalidArg("lease_until",
			"leaseUntil must be in the future: an already-expired lease claims nothing")
	}
	if req.LeaseUntil.After(now.Add(maxLeaseAhead)) {
		return Binding{}, serviceerr.InvalidArg("lease_until",
			"leaseUntil is too far ahead: a lease nobody can outlive makes the instance unmovable")
	}

	got, err := s.repo.ClaimInstance(ctx, repo.NodeBinding{
		InstanceID:   req.InstanceID,
		NodeID:       req.NodeID,
		ClaimedSeqNo: req.SequenceNo,
		LeaseUntil:   req.LeaseUntil,
	}, s.grace)
	switch {
	case errors.Is(err, repo.ErrHeldByAnotherNode):
		// Отказ по СОСТОЯНИЮ, а не по вводу: тот же запрос после истечения чужой
		// аренды будет законным. И текст не называет держателя — идентификатор
		// узла инфра-чувствителен, а спрашивающий его узел уже знает, что
		// проиграл.
		return Binding{}, status.Error(codes.FailedPrecondition,
			"instance is held by another node with a live lease")
	case err != nil:
		return Binding{}, serviceerr.MapRepoErr(err)
	}
	return Binding{NodeID: got.NodeID, SequenceNo: got.ClaimedSeqNo, LeaseUntil: got.LeaseUntil}, nil
}

// Release отпускает машину.
//
// Идемпотентен: отпустить уже отпущенную — успех. Агент, повторяющий отпускание
// после обрыва ответа, не должен получать отказ на действие, которое исполнено.
func (s *Service) Release(ctx context.Context, instanceID, nodeID string) error {
	if err := corevalidate.ResourceID("Instance", ids.PrefixInstanceHyphen, instanceID); err != nil {
		return err
	}
	if l := len(nodeID); l == 0 || l > maxNodeIDLen {
		return serviceerr.InvalidArg("node_id", "nodeId is required and must be at most 253 characters")
	}
	if err := s.repo.ReleaseInstance(ctx, instanceID, nodeID); err != nil {
		return serviceerr.MapRepoErr(err)
	}
	return nil
}
