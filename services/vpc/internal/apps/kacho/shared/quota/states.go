// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package quota

import (
	"context"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// Арендаторское чтение квот — задача #365, решение V2-7 приёмки
// `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md` (APPROVED).
//
// ПОЧЕМУ ЭТО ЖИВЁТ В ПОЛОСЕ, А НЕ ОТДЕЛЬНЫМ USE-CASE'ОМ. Чтению нужны ровно те
// же два источника, что и полосе: местные строки учёта и владелец величин на
// промахе. Отдельный тип с теми же полями означал бы второе место, знающее, как
// связаны эти два источника, — и оно разошлось бы с первым молча, потому что
// расхождение видно только на промахе, то есть на свежем проекте.

// States отдаёт квоты проекта такими, какими их читает арендатор: предел,
// потребление и источник победившей величины по каждому виду домена.
//
// ОТВЕТ НИКОГДА НЕ БЫВАЕТ ПУСТЫМ ПО УМОЛЧАНИЮ. Проект, ещё ничего не создавший,
// строк учёта не имеет by construction — их заводит первая мутация. Вернуть на
// это пустой набор значило бы сказать «квот нет», то есть «предела нет»: ровно
// обратное действительности, и прочитано оно будет как разрешение. Поэтому на
// пустоте величины резолвятся у владельца СИНХРОННО и отдаются с нулевым
// потреблением (`api-conventions.md` §«Пустое значение обязано означать „пусто“»).
//
// ЧТЕНИЕ НЕ ЗАПИСЫВАЕТ. Резолв на промахе не материализует строк — в отличие от
// `Admit`, где заведение строк есть часть предмета (место надо занимать). Здесь
// предмет иной: показать. Материализация на чтении означала бы, что список квот
// меняет состояние базы, и тогда «посмотреть» стало бы мутацией, которую никто
// не просил, а на fail-closed пути ещё и отказом там, где спрашивали только
// показать.
//
// ЦЕНА НАЗВАНА: чтение квот проекта, ещё ничего не создавшего, зависит от
// доступности владельца величин и отвечает отказом, когда тот недоступен. После
// первой мутации строки есть, и чтение локально. Предикат пересмотра — доля
// чтений, попадающих в промах, перестала быть пренебрежимой (V2-8).
func (g *Guard) States(ctx context.Context, projectID string) ([]kacho.QuotaState, error) {
	rd, err := g.repo.Reader(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rd.Close() }()

	states, err := rd.Quotas().ListStates(ctx, repo.QuotaCarrierProject, projectID)
	if err != nil {
		return nil, err
	}
	if len(states) > 0 {
		return states, nil
	}

	// Строк нет — собираем набор резолвом, ничего не записывая.
	limits, err := g.resolver.Resolve(ctx, projectID, g.service)
	if err != nil {
		return nil, g.peerRefusal(err, "iam.limit", projectID)
	}

	out := make([]kacho.QuotaState, 0, len(limits))
	for _, l := range limits {
		out = append(out, kacho.QuotaState{
			Kind: l.Kind,
			// Потребление нулевое не по умолчанию, а по факту: строк учёта нет,
			// значит ни одна вставка ещё не списывала место.
			Used:          0,
			Limit:         l.Value,
			SourceScope:   l.SourceScope,
			SourceScopeID: l.SourceScopeID,
			CarrierType:   repo.QuotaCarrierProject,
			CarrierID:     projectID,
		})
	}

	// Порядок задаётся здесь тем же признаком, что и на пути из базы (`ORDER BY
	// kind`): ответ владельца величин своего порядка не обещает, и без явной
	// сортировки набор свежего проекта шёл бы иначе, чем набор того же проекта
	// после первой мутации. Клиент увидел бы перестановку там, где ничего не
	// менялось.
	sortByKind(out)
	return out, nil
}

// sortByKind — единственное место, задающее порядок ответа на этом пути.
func sortByKind(states []kacho.QuotaState) {
	for i := 1; i < len(states); i++ {
		for j := i; j > 0 && states[j].Kind < states[j-1].Kind; j-- {
			states[j], states[j-1] = states[j-1], states[j]
		}
	}
}
