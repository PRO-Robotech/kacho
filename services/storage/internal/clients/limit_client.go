// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
	"github.com/PRO-Robotech/kacho/pkg/quota/quotaiam"
	"github.com/PRO-Robotech/kacho/pkg/retry"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/quota"
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
// разрешения, из которых верным окажется одно. Владелец типа хранит уже
// РАЗРЕШЁННУЮ величину, и `Resolve` — единственный её источник.
//
// ПОЧЕМУ ВНУТРЕННИЙ СЛУШАТЕЛЬ И ПОЧЕМУ НОВОГО РЕБРА НЕТ. Величины — админская
// поверхность: пять глаголов правки стоят под `system_admin`, а два чтения для
// владельцев — под узким отношением читателя квот. На внешнем слушателе их нет и
// быть не должно (`security.md` §Internal-vs-external). Соединение берётся то же,
// которым storage уже спрашивает права и регистрирует owner-tuple, — ребро
// `storage → iam` существует, и работа его не заводит.

// LimitClient — типизированный клиент резолва величин.
type LimitClient struct {
	cli iamv1.InternalLimitServiceClient
}

// Compile-time проверка: клиент удовлетворяет порту совещательной полосы.
var _ quota.LimitResolver = (*LimitClient)(nil)

// NewLimitClient создаёт клиента поверх conn ВНУТРЕННЕГО слушателя kaname.
func NewLimitClient(conn grpc.ClientConnInterface) *LimitClient {
	return &LimitClient{cli: iamv1.NewInternalLimitServiceClient(conn)}
}

// Resolve возвращает разрешённые величины по всем видам названного домена.
//
// Перечень видов приезжает ОТВЕТОМ, а не спрашивается по одному: каталог видов
// принадлежит платформе, и владелец типа не держит его копии. Заведут четвёртый
// вид — он приедет сюда сам, без правки этого файла; держи владелец свой список,
// четвёртый вид молча не материализовался бы, а «строки нет» означает отказ.
//
// Личность вызывающего пробрасывается в исходящие метаданные: без неё сосед
// увидит анонимный вызов и откажет — при том что решение он принимает по
// служебной учётке владельца, состоящей в группе читателей квот.
func (c *LimitClient) Resolve(ctx context.Context, scopeID, service string) ([]quota.ResolvedLimit, error) {
	if c.cli == nil {
		return nil, status.Error(codes.Unavailable, "storage→iam InternalLimitService not configured")
	}
	var out []quota.ResolvedLimit
	err := retry.OnUnavailable(ctx, func(ctx context.Context) error {
		cctx, cancel := context.WithTimeout(ctx, peerCallTimeout)
		defer cancel()
		resp, rerr := c.cli.Resolve(auth.PropagateOutgoing(cctx), &iamv1.ResolveLimitsRequest{
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
	return quotaiam.NewDelta(c.cli, 0).ListChangedSince(ctx, cursor, pageSize)
}
