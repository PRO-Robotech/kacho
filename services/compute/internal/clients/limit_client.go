// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

import (
	"context"
	"time"

	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
	"github.com/PRO-Robotech/kacho/pkg/quota/quotaiam"
	"github.com/PRO-Robotech/kacho/pkg/retry"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// Клиент владельца величин: `InternalLimitService.Resolve` на ВНУТРЕННЕМ
// слушателе kaname.
//
// ПОЧЕМУ СТАРШИНСТВО РАЗРЕШАЕТСЯ ТАМ, А НЕ ЗДЕСЬ. Правило PROJECT > ACCOUNT >
// DEFAULT требует знать аккаунт проекта, а владелец типа его не знает — у него
// только зеркало, и то заводимое из этого же обращения. Зеркалить СЫРЫЕ строки
// пределов к себе и разрешать старшинство у себя значило бы завести второе место
// разрешения, из которых верным окажется одно. Владелец типа хранит уже
// РАЗРЕШЁННУЮ величину, и `Resolve` — единственный её источник.
//
// ПОЧЕМУ ВНУТРЕННИЙ СЛУШАТЕЛЬ. Величины — админская поверхность: глаголы правки
// стоят под `system_admin`, а два чтения для владельцев — под узким отношением
// читателя квот. На внешнем слушателе их нет и быть не должно
// — внутренний контур, не внешний.
//
// ПОЧЕМУ ОТВЕТ НЕ КЕШИРУЕТСЯ. Резолв зовётся один раз на проект — при
// материализации; дальше решение принимается по местной строке учёта. Кеш
// экономил бы вызов, которого и так нет, и вводил бы второе место, где живёт
// величина, с собственным сроком устаревания.

// limitCallTimeout — предел времени одной попытки резолва.
//
// Собственный per-call deadline на КАЖДОЙ попытке: peer, который жив, но не
// отвечает, обязан биться таймаутом, а не висеть до входящего контекста.
const limitCallTimeout = 5 * time.Second

// LimitClient — типизированный клиент резолва величин.
type LimitClient struct {
	cli     iamv1.InternalLimitServiceClient
	timeout time.Duration
}

// NewLimitClient создаёт клиента поверх conn ВНУТРЕННЕГО слушателя kaname.
func NewLimitClient(conn grpc.ClientConnInterface) *LimitClient {
	return &LimitClient{cli: iamv1.NewInternalLimitServiceClient(conn), timeout: limitCallTimeout}
}

// NewLimitClientWith — seam для проб с подставным клиентом.
func NewLimitClientWith(cli iamv1.InternalLimitServiceClient) *LimitClient {
	return &LimitClient{cli: cli, timeout: limitCallTimeout}
}

// Resolve возвращает разрешённые величины по ВСЕМ видам сервиса.
//
// По всем разом, а не по одному запрошенному: строки учёта материализуются
// целиком на домен, иначе новый вид не появился бы у уже живущих проектов и
// потребовал бы дозаписи беклогом.
//
// Область-победитель переводится в строку ЗДЕСЬ, у границы: дальше по коду
// живёт текст, который понимает схема (`DEFAULT`/`ACCOUNT`/`PROJECT`), а не
// перечисление чужого пакета. Неизвестное значение перечисления НЕ подменяется
// умолчанием: подмена записала бы в строку учёта область, которой владелец не
// называл, и personal-перекрытие стало бы неотличимо от унаследованного.
func (c *LimitClient) Resolve(ctx context.Context, scopeID, service string) ([]ports.ResolvedLimit, error) {
	var out []ports.ResolvedLimit
	err := retry.OnUnavailable(ctx, func(ctx context.Context) error {
		callCtx, cancel := context.WithTimeout(ctx, c.timeout)
		defer cancel()
		resp, rerr := c.cli.Resolve(auth.PropagateOutgoing(callCtx), &iamv1.ResolveLimitsRequest{
			ScopeId: scopeID,
			Service: service,
		})
		if rerr != nil {
			return rerr
		}
		limits := resp.GetLimits()
		out = make([]ports.ResolvedLimit, 0, len(limits))
		for _, l := range limits {
			scope, ok := limitScopeName(l.GetSourceScope())
			if !ok {
				// Область, которой этот сервис не знает, — не повод записать
				// умолчание: схема учёта приняла бы только три известных значения,
				// и подмена сделала бы отказ схемы похожим на исправную работу.
				continue
			}
			out = append(out, ports.ResolvedLimit{
				Kind:          l.GetKind(),
				Value:         l.GetValue(),
				Carrier:       l.GetCarrier(),
				SourceScope:   scope,
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

// limitScopeName переводит область-победитель в текст, который понимает схема.
//
// Отображение ЯВНОЕ, а не `String()` перечисления: имя в контракте и значение в
// столбце — два разных предмета, и связать их именем значило бы сделать
// переименование в контракте молчаливой сменой данных.
func limitScopeName(s iamv1.Limit_Scope) (string, bool) {
	switch s {
	case iamv1.Limit_DEFAULT:
		return "DEFAULT", true
	case iamv1.Limit_ACCOUNT:
		return "ACCOUNT", true
	case iamv1.Limit_PROJECT:
		return "PROJECT", true
	default:
		return "", false
	}
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
