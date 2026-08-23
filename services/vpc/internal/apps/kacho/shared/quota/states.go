// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package quota

import (
	"context"

	"github.com/PRO-Robotech/kacho/pkg/quota/quotaread"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// Арендаторское чтение квот — задача #365, решение V2-7 приёмки
// `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md` (APPROVED);
// перенесено на общую полосу задачей #412.
//
// # Что здесь БОЛЬШЕ не живёт и почему
//
// Прежде тело чтения стояло здесь целиком: резолв на промахе, отбор по носителю,
// нулевое потребление, порядок по виду, перевод отказа соседа. Оно доменно-
// независимо ВСЁ, кроме имени схемы и токена каталога, — и пока владелец был
// один, копия и оригинал были одним и тем же файлом. С появлением второго
// владельца копия перестала быть бесплатной: пять тел разошлись бы на промахе,
// то есть на проекте, который ещё ничего не создавал, — там, где расхождение не
// видит ни один вызывающий, потому что на живом проекте все пять отвечают
// одинаково.
//
// Осталось ровно то, что доменно: как этот владелец добирается до своих строк.

// stateRows — переходник от транзакционного чтения этого владельца к порту
// общей полосы.
//
// Существует потому, что у vpc строки читаются ВНУТРИ read-транзакции (у
// соседей — прямо из пула), и открытие транзакции есть частность владельца, а не
// свойство чтения квот. Переходник ничего не решает: он открывает, спрашивает и
// закрывает.
type stateRows struct{ repo Repo }

func (s stateRows) ListStates(
	ctx context.Context, carrierType, carrierID string,
) ([]quotaread.State, error) {
	rd, err := s.repo.Reader(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rd.Close() }()
	return rd.Quotas().ListStates(ctx, carrierType, carrierID)
}

// States отдаёт квоты проекта такими, какими их читает арендатор: предел,
// потребление и источник победившей величины по каждому виду домена.
//
// Тело — у общей полосы (`quotaread.Band`); её договор и цена названы там.
// Здесь остаётся сборка: чем читаются строки, у кого спрашиваются величины,
// каким токеном каталога и от чьего имени называется отказ.
func (g *Guard) States(ctx context.Context, projectID string) ([]kacho.QuotaState, error) {
	band, err := quotaread.NewBand(stateRows{repo: g.repo}, g.resolver, g.service, serviceDomain)
	if err != nil {
		return nil, err
	}
	// Носитель называется ЯВНО: полоса отвечает про того, о ком спросили, и вид,
	// считаемый в другом носителе, в этот ответ не попадает. Договор и цена —
	// у самой полосы.
	return band.States(ctx, quotaread.ProjectCarrier(projectID))
}

// serviceDomain — имя сервиса, называющее ИСТОЧНИК ответа в машинном признаке.
//
// Совпадает здесь с токеном каталога, и это совпадение, а не тождество: у
// соседа-балансировщика каталог знает виды как `loadbalancer`, а ответ на
// проводе обязан называть источником `nlb`. Литерал стоит отдельно именно
// поэтому.
const serviceDomain = "vpc"

// Ссылка на носитель учёта — чтобы смена его имени ломала сборку здесь, а не
// молча меняла адресацию строк.
var _ = repo.QuotaCarrierProject
