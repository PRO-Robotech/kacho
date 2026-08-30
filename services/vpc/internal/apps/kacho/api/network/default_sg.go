// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"context"
	"fmt"

	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// CreateDefaultSGUseCase — отдельный use-case для inline default-SG creation
// при Network.Create. Работает в УЖЕ открытой writer-TX (передается снаружи) —
// гарантирует atomic-семантику с Insert(Network). Сам TX не открывает и не
// commit'ит — это ответственность caller'а (`CreateNetworkUseCase.doCreate`).
//
// Вынесен в отдельный use-case, чтобы не раздувать `NetworkService.Create`.
// Композиция в одной writer-TX исключает orphan-ресурсы: caller открывает
// Writer, вставляет Network, передает ОТКРЫТЫЙ writer сюда; здесь — Insert(SG) +
// outbox-emit + SetDefaultSGID(Network) + outbox-emit; caller делает Commit.
// Либо весь композит виден (Commit), либо ничего (Abort на любой ошибке).
//
// Stateless (без полей) — конструктор `NewCreateDefaultSGUseCase()` сохраняем
// для parity с остальными use-case'ами и удобства мокинга в будущем.
type CreateDefaultSGUseCase struct{}

// NewCreateDefaultSGUseCase создает stateless CreateDefaultSGUseCase.
func NewCreateDefaultSGUseCase() *CreateDefaultSGUseCase {
	return &CreateDefaultSGUseCase{}
}

// Execute создает default-SG для только-что-вставленной Network и проставляет
// ее id как `Network.default_security_group_id`. Все DML и outbox-emit идут
// через переданный writer-TX (caller'а), что гарантирует atomic-семантику с
// Insert(Network) — либо все три DML видны, либо ни один (Abort/crash).
//
// Возвращает updated NetworkRecord с заполненным `default_security_group_id`.
// На любой ошибке возвращает уже обернутую через `mapRepoErr` gRPC-ошибку —
// caller просто пробрасывает ее наверх (worker превратит в Operation.error).
func (u *CreateDefaultSGUseCase) Execute(
	ctx context.Context,
	w Writer,
	network domain.Network,
) (*kachorepo.NetworkRecord, error) {
	// ID минтится в use-case-слое (не в domain-builder'е) — domain остаётся
	// чистым value-слоем без infra-зависимости на corelib/ids.
	sg := domain.NewDefaultSecurityGroup(ids.NewID(ids.PrefixSecurityGroup), network)
	sgRec, err := w.SecurityGroups().Insert(ctx, &sg)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	if err := w.Outbox().Emit(ctx, "SecurityGroup", sgRec.ID, sgRec.ProjectID, "CREATED", helpers.DomainToMap(sgRec)); err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
	}
	upd, err := w.Networks().SetDefaultSGID(ctx, network.ID, sgRec.ID)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	// Строки журнала о СЕТИ здесь нет намеренно (#1548).
	//
	// Она стояла и объявляла привязку группы отдельным изменением сети. Для
	// подписчика это было событие, которого арендатор не делал: он создавал сеть,
	// а получал её создание плюс правку — с состоянием, верным только до
	// следующего шага той же транзакции. Сеть объявляет ОДНОЙ строкой её
	// создатель, собрав её целиком (`create.go`, эмиссия после этой композиции).
	//
	// Своя строка ресурса, который этот use-case ЗАВОДИТ, остаётся выше и
	// обязательна: SecurityGroup — самостоятельный предмет, и его появление подписчик
	// узнаёт отсюда.
	return upd, nil
}
