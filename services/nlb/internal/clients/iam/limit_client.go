// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package iam

import (
	"context"
	"time"

	"google.golang.org/grpc"

	iampb "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
	"github.com/PRO-Robotech/kacho/pkg/quota/quotaiam"
	"github.com/PRO-Robotech/kacho/pkg/retry"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/quota"
)

// Клиент владельца величин: `InternalLimitService.Resolve` на ВНУТРЕННЕМ
// слушателе kaname.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), V2-1 и DoD S4 п.1.
//
// ПОЧЕМУ СТАРШИНСТВО РАЗРЕШАЕТСЯ ТАМ, А НЕ ЗДЕСЬ. Правило PROJECT > ACCOUNT >
// DEFAULT требует знать аккаунт проекта, а владелец типа его не знает — у него
// только зеркало, и то заводимое из этого же обращения. Зеркалить СЫРЫЕ строки
// пределов к себе и разрешать старшинство у себя значило бы завести второе место
// разрешения, из которых верным окажется одно.
//
// ПОЧЕМУ ВНУТРЕННИЙ СЛУШАТЕЛЬ. Величины — админская поверхность: глаголы правки
// стоят под `system_admin`, а чтения для владельцев — под узким отношением
// читателя квот. На внешнем слушателе их нет и быть не должно
// (`security.md` §Internal-vs-external).

// DefaultLimitResolveTimeout — per-call deadline резолва величин.
//
// Тот же порядок, что у соседних вызовов этого пакета: резолв стоит на
// request-path создания, и неотвечающий сосед обязан упереться в срок, а не
// висеть (`architecture.md` §«Per-call deadline на КАЖДОМ внешнем вызове»).
const DefaultLimitResolveTimeout = 5 * time.Second

// LimitClient — типизированный клиент резолва величин.
type LimitClient struct {
	cli     iampb.InternalLimitServiceClient
	timeout time.Duration
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
	return &LimitClient{
		cli:     iampb.NewInternalLimitServiceClient(conn),
		timeout: DefaultLimitResolveTimeout,
	}
}

// Resolve возвращает разрешённые величины по всем видам названного домена.
//
// Перечень видов приезжает ОТВЕТОМ, а не спрашивается по одному: каталог видов
// принадлежит платформе, и владелец типа не держит его копии. Заведут новый вид
// — он приедет сюда сам, без правки этого файла; держи владелец свой список,
// новый вид молча не материализовался бы, а «строки нет» означает отказ.
//
// Личность вызывающего пробрасывается в исходящие метаданные: без неё сосед
// увидит анонимный вызов и откажет — при том что решение он принимает по
// служебной учётке владельца, состоящей в группе читателей квот.
func (c *LimitClient) Resolve(ctx context.Context, scopeID, service string) ([]quota.ResolvedLimit, error) {
	var out []quota.ResolvedLimit
	ctx, cancel := context.WithTimeout(auth.PropagateOutgoing(ctx), c.timeout)
	defer cancel()

	err := retry.OnUnavailable(ctx, func(ctx context.Context) error {
		resp, rerr := c.cli.Resolve(ctx, &iampb.ResolveLimitsRequest{
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
// предмет, а не молчаливо принимает несуществующую область.
func scopeName(s iampb.Limit_Scope) string {
	switch s {
	case iampb.Limit_DEFAULT:
		return "DEFAULT"
	case iampb.Limit_ACCOUNT:
		return "ACCOUNT"
	case iampb.Limit_PROJECT:
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
	return quotaiam.NewDelta(c.cli, c.timeout).ListChangedSince(ctx, cursor, pageSize)
}
