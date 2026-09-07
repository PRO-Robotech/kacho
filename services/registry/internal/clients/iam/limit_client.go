// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package iam

import (
	"context"
	"time"

	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
	"github.com/PRO-Robotech/kacho/pkg/quota/quotaiam"
	"github.com/PRO-Robotech/kacho/pkg/retry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/quota"
)

// Клиент владельца величин: `InternalLimitService.Resolve` на ВНУТРЕННЕМ
// слушателе kaname.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), V2-1 и DoD S4 п.1.
//
// ПОЧЕМУ ОТДЕЛЬНОЕ СОЕДИНЕНИЕ ОТ `Client`. Тот держит conn к ПУБЛИЧНОМУ
// слушателю (:9090, `ProjectService.Get`); величины живут на ВНУТРЕННЕМ (:9091)
// — админская поверхность, которой на внешнем нет и быть не должно
// (`security.md` §Internal-vs-external). Один conn на оба означал бы, что один
// из двух вызовов уходит не туда, и обнаружилось бы это ответом «метод не
// реализован» — сообщением, по которому причину не восстановить.

// limitResolveTimeout — per-call deadline резолва величин.
//
// Тот же порядок, что у соседнего вызова этого пакета: резолв стоит на
// request-path создания, и неотвечающий сосед обязан упереться в срок, а не
// висеть (`architecture.md` §«Per-call deadline на КАЖДОМ внешнем вызове»).
const limitResolveTimeout = 5 * time.Second

// LimitClient — типизированный клиент резолва величин.
type LimitClient struct {
	conn grpc.ClientConnInterface
}

// Compile-time проверка: клиент удовлетворяет порту совещательной полосы.
var _ quota.LimitResolver = (*LimitClient)(nil)

// NewLimitClient создаёт клиента поверх conn ВНУТРЕННЕГО слушателя kaname.
//
// nil conn → nil клиент: сборка без соседа величин законна (полоса тогда не
// собирается), и «полосы нет» обязано быть отличимо от «полоса есть и молчит».
func NewLimitClient(conn grpc.ClientConnInterface) *LimitClient {
	if conn == nil {
		return nil
	}
	return &LimitClient{conn: conn}
}

// Resolve возвращает разрешённые величины по всем видам названного домена.
//
// Перечень видов приезжает ОТВЕТОМ, а не спрашивается по одному: каталог видов
// принадлежит платформе, и владелец типа не держит его копии. Заведут новый вид
// — он приедет сюда сам, без правки этого файла.
//
// Личность вызывающего пробрасывается в исходящие метаданные: без неё сосед
// увидит анонимный вызов и откажет — при том что решение он принимает по
// служебной учётке владельца, состоящей в группе читателей квот.
func (c *LimitClient) Resolve(ctx context.Context, scopeID, service string) ([]quota.ResolvedLimit, error) {
	ctx, cancel := context.WithTimeout(auth.PropagateOutgoing(ctx), limitResolveTimeout)
	defer cancel()

	cli := iamv1.NewInternalLimitServiceClient(c.conn)
	var out []quota.ResolvedLimit
	err := retry.OnUnavailable(ctx, func(ctx context.Context) error {
		resp, rerr := cli.Resolve(ctx, &iamv1.ResolveLimitsRequest{
			ScopeId: scopeID,
			Service: service,
		})
		if rerr != nil {
			return rerr
		}
		limits := resp.GetLimits()
		out = make([]quota.ResolvedLimit, 0, len(limits))
		for _, l := range limits {
			out = append(out, quota.ResolvedLimit{
				Kind:          l.GetKind(),
				Value:         l.GetValue(),
				Carrier:       l.GetCarrier(),
				SourceScope:   scopeName(l.GetSourceScope()),
				SourceScopeID: l.GetSourceScopeId(),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// scopeName переводит область контракта в строку, которую хранит строка учёта.
//
// Перевод, а не приведение числа к тексту: столбец `source_scope` ограничен
// схемой тремя значениями, и попади туда «SCOPE_UNSPECIFIED» или число, строка
// не вставилась бы вовсе — материализация падала бы на всём проекте из-за одного
// невнятного ответа соседа. Необъявленная область поэтому переводится в пустую
// строку, а не в выдуманное значение: пусть отвергает ограничение схемы, называя
// предмет.
func scopeName(s iamv1.Limit_Scope) string {
	switch s {
	case iamv1.Limit_DEFAULT:
		return "DEFAULT"
	case iamv1.Limit_ACCOUNT:
		return "ACCOUNT"
	case iamv1.Limit_PROJECT:
		return "PROJECT"
	}
	return ""
}

// ListChangedSince тянет ДЕЛЬТУ изменений величин — то, чем снимок догоняет
// авторитет.
//
// Тело — ОБЩЕЕ (`pkg/quota/quotaiam`): перевод дельты доменного не несёт ничего,
// и пять копий разошлись бы на переводе области, то есть там, где расхождение
// молча меняет, какие строки снимка правит администратор.
func (c *LimitClient) ListChangedSince(
	ctx context.Context, cursor string, pageSize int32,
) ([]corequota.Change, string, error) {
	return quotaiam.NewDelta(iamv1.NewInternalLimitServiceClient(c.conn), limitResolveTimeout).ListChangedSince(ctx, cursor, pageSize)
}
