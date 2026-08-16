// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

import (
	"context"

	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
	"github.com/PRO-Robotech/kacho/pkg/retry"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/quota"
)

// Клиент владельца величин: `InternalLimitService.Resolve` на ВНУТРЕННЕМ
// слушателе kacho-iam.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), V2-1 и DoD S2 п.5.
//
// ПОЧЕМУ СТАРШИНСТВО РАЗРЕШАЕТСЯ ТАМ, А НЕ ЗДЕСЬ. Правило PROJECT > ACCOUNT >
// DEFAULT требует знать аккаунт проекта, а владелец типа его не знает — у него
// только зеркало, и то заводимое из этого же обращения. Зеркалить СЫРЫЕ строки
// пределов к себе и разрешать старшинство у себя значило бы завести второе место
// разрешения, из которых верным окажется одно. Владелец типа хранит уже
// РАЗРЕШЁННУЮ величину, и `Resolve` — единственный её источник.
//
// ПОЧЕМУ ВНУТРЕННИЙ СЛУШАТЕЛЬ. Величины — админская поверхность: пять глаголов
// правки стоят под `system_admin`, а два чтения для владельцев — под узким
// отношением читателя квот. На внешнем слушателе их нет и быть не должно
// (`security.md` §Internal-vs-external).

// LimitClient — типизированный клиент резолва величин.
type LimitClient struct {
	cli iamv1.InternalLimitServiceClient
}

// Compile-time проверка: клиент удовлетворяет порту совещательной полосы.
var _ quota.LimitResolver = (*LimitClient)(nil)

// NewLimitClient создаёт клиента поверх conn ВНУТРЕННЕГО слушателя kacho-iam.
func NewLimitClient(conn grpc.ClientConnInterface) *LimitClient {
	return &LimitClient{cli: iamv1.NewInternalLimitServiceClient(conn)}
}

// Resolve возвращает разрешённые величины по всем видам названного домена.
//
// Перечень видов приезжает ОТВЕТОМ, а не спрашивается по одному: каталог видов
// принадлежит платформе, и владелец типа не держит его копии. Заведут девятый
// вид — он приедет сюда сам, без правки этого файла; держи владелец свой список,
// девятый вид молча не материализовался бы, а «строки нет» означает отказ.
//
// Личность вызывающего пробрасывается в исходящие метаданные: без неё сосед
// увидит анонимный вызов и откажет — при том что решение он принимает по
// служебной учётке владельца, состоящей в группе читателей квот.
func (c *LimitClient) Resolve(ctx context.Context, scopeID, service string) ([]quota.ResolvedLimit, error) {
	var out []quota.ResolvedLimit
	err := retry.OnUnavailable(ctx, func(ctx context.Context) error {
		cctx, cancel := peerCallCtx(ctx, defaultPeerCallTimeout)
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
// Курсор непрозрачен и приезжает от владельца величин; пустая строка означает
// «с начала времён», то есть ровно то, что нужно проекции, ни разу не тянувшей
// дельту. Следующий курсор владелец возвращает ВСЕГДА, включая пустую страницу:
// тянущий, продвигавший курсор только на непустых страницах, перечитывал бы один
// и тот же хвост вечно, как только догнал.
//
// Область переводится тем же переводом, что и на резолве, и НЕ приводится к
// умолчанию: неизвестная область здесь означает, что сосед сказал то, чего мы не
// понимаем, и синхронизатор обязан отказаться, а не догадаться. Пустая строка
// невалидна для `quota.Change`, поэтому такой ответ остановит проход, не сдвинув
// курсор, — и это громче, чем применить догадку.
func (c *LimitClient) ListChangedSince(
	ctx context.Context, cursor string, pageSize int32,
) ([]corequota.Change, string, error) {
	var (
		out  []corequota.Change
		next string
	)
	err := retry.OnUnavailable(ctx, func(ctx context.Context) error {
		cctx, cancel := peerCallCtx(ctx, defaultPeerCallTimeout)
		defer cancel()
		resp, rerr := c.cli.ListChangedSince(auth.PropagateOutgoing(cctx),
			&iamv1.ListChangedLimitsRequest{Cursor: cursor, PageSize: int64(pageSize)})
		if rerr != nil {
			return rerr
		}
		changes := resp.GetChanges()
		out = make([]corequota.Change, 0, len(changes))
		for _, ch := range changes {
			l := ch.GetLimit()
			out = append(out, corequota.Change{
				Kind:      l.GetKind(),
				Scope:     corequota.Scope(scopeName(l.GetScope())),
				ScopeID:   l.GetScopeId(),
				Value:     l.GetValue(),
				Revision:  l.GetRevision(),
				Withdrawn: ch.GetWithdrawn(),
			})
		}
		next = resp.GetNextCursor()
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return out, next, nil
}
