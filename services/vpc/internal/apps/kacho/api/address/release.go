// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package address

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/fgaregister"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// ReleaseLeaseInput — предъявление владения арендой.
//
// `owned` здесь нет намеренно: ветку «удалить адрес» либо «оставить адрес
// арендатора» выбирает vpc по СВОЕЙ колонке address_references.owned. Признак,
// принесённый потребителем, был бы вторым местом об одном предмете — и
// разошёлся бы молча.
type ReleaseLeaseInput struct {
	ProjectID    string
	AddressID    string
	ReferrerType string
	ReferrerID   string
}

// ReleaseOwnedAddressUseCase — снятие аренды по предъявлению владения ею.
//
// # Почему это отдельный глагол, а не пара «снять ссылку» + «удалить адрес»
//
// Пара анкорит право ПООБЪЕКТНО, на самом адресе, а пообъектный отказ платформа
// обязана делать неотличимым от отсутствия объекта (анти-оракульное скрытие).
// Тогда у вызывающего не остаётся ответа, из которого следует «аренды нет», — и
// он всё равно его выводил, строя на нём необратимый шаг.
//
// Здесь право анкорится на ПРОЕКТЕ — том же якоре, что у создания аренды, —
// поэтому пообъектной пробы существования нет, а значит нет и полосы скрытия:
// неоднозначность не различается, а перестаёт порождаться.
//
// # Fail-closed здесь означает НЕ ОСВОБОЖДАТЬ
//
// Ошибка в сторону «не освободили» оставляет аренду занятой: пул истощается,
// это заметно и подбирается, пока строка потребителя цела. Ошибка в другую
// сторону вернула бы в свободный список адрес, который у владельца всё ещё
// занят, и второй арендатор получил бы адрес, работающий у первого. Первое
// чинится, второе — пересечение арендаторов. Поэтому любой неразрешённый исход
// (отказ хранилища, недоступность) обязан быть ОТКАЗОМ, а не «уже снято».
//
// # Синхронный, в отличие от создания
//
// Исход обязан приехать полем ответа; форма Operation унесла бы его за опрос
// публичного OperationService — за ту самую поверхность, с которой глагол
// уходит. Отступление от «мутации возвращают Operation» записано в
// services/vpc/docs/engineering/architecture/07-known-divergences.md.
type ReleaseOwnedAddressUseCase struct {
	repo Repo
}

// NewReleaseOwnedAddressUseCase создаёт ReleaseOwnedAddressUseCase.
func NewReleaseOwnedAddressUseCase(r Repo) *ReleaseOwnedAddressUseCase {
	return &ReleaseOwnedAddressUseCase{repo: r}
}

// Execute снимает аренду и возвращает НАЗВАННЫЙ исход.
//
// Вся работа — одна writer-TX: снятие ссылки со сверкой пары и проекта, сброс
// `used`, удаление строки, возврат IP в свободный список, outbox и намерение
// снятия owner-tuple. Срыв на любом шаге откатывает всё: частичного исхода, из
// которого потом выводят «наверное сделано», не существует by construction.
func (u *ReleaseOwnedAddressUseCase) Execute(
	ctx context.Context, in ReleaseLeaseInput,
) (kachorepo.LeaseOutcome, error) {
	// Формат — первым стейтментом, до любого обращения к хранилищу.
	if err := corevalidate.ResourceID("address", ids.PrefixAddress, in.AddressID); err != nil {
		return "", err
	}
	// `corevalidate.ResourceID` пустую строку ПРОПУСКАЕТ — required это отдельная
	// ответственность вызывающего. Без неё пустое значение уехало бы в сверку и
	// вернулось утверждением о ресурсе, которого вызывающий не называл.
	switch {
	case in.ProjectID == "":
		return "", status.Error(codes.InvalidArgument, "project_id: required")
	case in.AddressID == "":
		return "", status.Error(codes.InvalidArgument, "address_id: required")
	case in.ReferrerType == "":
		return "", status.Error(codes.InvalidArgument, "referrer_type: required")
	case in.ReferrerID == "":
		return "", status.Error(codes.InvalidArgument, "referrer_id: required")
	}

	w, err := u.repo.Writer(ctx)
	if err != nil {
		return "", serviceerr.MapRepoErr(err)
	}
	defer w.Abort()

	res, err := w.Addresses().ReleaseLease(ctx, kachorepo.LeaseReleaseRequest{
		AddressID:    in.AddressID,
		ProjectID:    in.ProjectID,
		ReferrerType: in.ReferrerType,
		ReferrerID:   in.ReferrerID,
	})
	if err != nil {
		return "", serviceerr.MapRepoErr(err)
	}

	if res.Outcome == kachorepo.LeaseReleased {
		if err := u.finishRelease(ctx, w, in.AddressID, res.Deleted); err != nil {
			return "", err
		}
	}

	if err := w.Commit(); err != nil {
		return "", serviceerr.MapRepoErr(err)
	}
	return res.Outcome, nil
}

// finishRelease — хвост удаления адреса в ТОЙ ЖЕ транзакции: возврат аренды в
// пул, событие и снятие owner-tuple.
//
// Порядок и условия — те же, что у публичного удаления адреса: координаты
// берутся из УДАЛЁННОЙ строки (свежий snapshot), а не из чтения до транзакции,
// иначе IP, выданный между чтением и записью, утёк бы.
func (u *ReleaseOwnedAddressUseCase) finishRelease(
	ctx context.Context, w Writer, addressID string, deleted *kachorepo.AddressRecord,
) error {
	if deleted == nil {
		// Владелец объявил RELEASED и не отдал строку — это дефект хранилища, а
		// не состояние: молчаливое продолжение потеряло бы возврат аренды в пул.
		return status.Error(codes.Internal, "internal error")
	}
	if deleted.ExternalIpv6 != nil && deleted.ExternalIpv6.Address != "" {
		if frr := w.Addresses().FreeExternalIPv6(ctx, addressID); frr != nil {
			return serviceerr.MapRepoErr(fmt.Errorf("%w: free external ipv6: %v", repo.ErrInternal, frr))
		}
	}
	if deleted.ExternalIpv4 != nil && deleted.ExternalIpv4.Address != "" && deleted.ExternalIpv4.AddressPoolID != "" {
		if rerr := w.Addresses().ReturnIPToFreelist(ctx, deleted.ExternalIpv4.AddressPoolID, deleted.ExternalIpv4.Address); rerr != nil {
			return serviceerr.MapRepoErr(fmt.Errorf("%w: return ip to freelist: %v", repo.ErrInternal, rerr))
		}
	}
	if err := w.Outbox().Emit(ctx, "Address", addressID, deleted.ProjectID, "DELETED", map[string]any{"id": addressID}); err != nil {
		return serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
	}
	if rerr := w.FGARegister().EmitUnregister(ctx, fgaregister.RegisterIntent(
		fgaregister.ProjectHierarchy(deleted.ProjectID, "vpc_address", addressID),
	)); rerr != nil {
		return serviceerr.MapRepoErr(fmt.Errorf("%w: fga unregister intent: %v", repo.ErrInternal, rerr))
	}
	return nil
}
