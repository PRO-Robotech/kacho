// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// rolescopechainstaysempty.go — САМОИСТЕКАЮЩИЙ гейт решения «меточную ось
// сужать НЕ НАДО» (задача продукта #1913, приёмка
// `services/iam/docs/engineering/acceptance/role-withdrawal-has-a-producer.md`
// §2.8, §10 шаг 8).
//
// # Что здесь стережётся — ФАКТ О ДЕРЕВЕ, на котором стоит решение
//
// Приёмка решила НЕ сужать меточную ось выдачи по живости роли, и решение стоит
// целиком на одном факте: у роли МОДУЛЯ цепь областей ПУСТА, поэтому меточная
// выдача не достаёт её ни живую, ни снятую. Факт измерен и верен сегодня —
// и НЕ ЗАЩИЩЁН НИЧЕМ:
//
//   - роль модуля всегда кластерного яруса, потому что `owner_module` и
//     `cluster_id` пишет ОДИН оператор, а не ограничение схемы (`CHECK`,
//     связывающего владельца с ярусом, нет — задача продукта #2020);
//   - у роли кластерного яруса цепь пуста, потому что ни одна ветвь
//     производителя звеньев её не отбирает: ветви берут `account_id` и
//     `project_id`, а у роли модуля они пусты оба.
//
// Значит производителя звена для `iam_role` может завести любая соседняя работа.
// Появится он — решение §2.8 станет неверным, а доступ вернётся МОЛЧА: сказать
// об этом будет нечему.
//
// Вынести гейт преемником значило бы принять решение и отложить единственное,
// что делает его безопасным, — то есть оставить решение БЕЗ ДЕРЖАТЕЛЯ. Поэтому
// он заводится вместе с решением.
//
// # ЧТО СЧИТАЕТСЯ НАХОДКОЙ — ИСТОЧНИК ветви, а не имя типа
//
// Ветвей, производящих звено для `iam_role`, две, и обе ЗАКОННЫ:
//
//	(5a) роль АККАУНТА  → источник `account_id`
//	(5b) роль ПРОЕКТА   → источник `project_id`
//
// Они взаимоисключающи ограничением `roles_definition_tier_xor`, и роль модуля
// (кластерный ярус) не попадает ни в одну — законно. Находка — ТРЕТЬЯ ветвь: та,
// что производит звено для `iam_role`, не спрашивая ни одного из двух ярусных
// столбцов. Судить по ИМЕНИ ТИПА нельзя: у `iam_role` есть и аккаунтные роли,
// у которых предок обязан быть, — то же различение, которое проводит сама
// миграция производителя.
//
// # ДИАГНОСТИКА — ЧАСТЬ СВОЙСТВА, А НЕ УКРАШЕНИЕ
//
// Текст находки обязан сказать, что закрывать тогда надо по ВСЕМ ТРЁМ АРМАМ
// выдачи (якорь · имена · метки), а не по меточному. Без этой строки следующий
// починит одну ветвь из трёх и решит, что закрыл: цепью объекта гейтятся все три
// арма, и сузить меточный значит не сузить две трети.
//
// # ЧЕГО РАЗБОР НЕ ВИДИТ — НАЗВАНО, А НЕ СПРЯТАНО
//
//  1. **звено, приехавшее ЗЕРКАЛОМ ресурса.** Писатель зеркала
//     (`resource_mirror`) кладёт звенья из цепи предков, которую ему ПОДАЛИ, и
//     типа не знает вовсе: он одинаков для всех. Появление звена этим путём
//     разбор текста поймать не может by construction — его поймает только
//     наблюдение за строками. Это ограничение, а не пропуск: гейт стережёт
//     ОБЪЯВЛЕННЫХ производителей, и их сегодня столько же, сколько ветвей;
//  2. **запрос, собранный из кусков в рантайме.** Формы в дереве нет;
//  3. **производителя вне дерева iam.** Таблица принадлежит iam, и писатель
//     извне означал бы, что предмет переехал целиком.
package repohygiene

import (
	"regexp"
	"strings"
)

// roleScopeChainType — тип объекта модели прав, чью цепь областей стережёт гейт.
const roleScopeChainType = "iam_role"

// roleScopeChainTable — таблица звеньев цепи. Ветвь, её не называющая, к
// предмету отношения не имеет: `'iam_role'` встречается и в словаре типов, и в
// перечислениях, и производителем звена от этого не становится.
const roleScopeChainTable = "resource_parent_edge"

// roleScopeChainTierSources — ярусные столбцы, из которых звено роли берётся
// ЗАКОННО.
//
// Перечень закрытый и короткий by construction: ярусов у роли три, и третий
// (кластерный) звена не даёт намеренно. Расширять его — значит менять решение
// §2.8, а не чинить гейт.
var roleScopeChainTierSources = []string{"account_id", "project_id"}

// roleScopeChainBranchRe — начало ветви, производящей звено для `iam_role`.
//
// Форма взята у производителя дословно (`SELECT 'iam_role'::text, …`), а не
// угадана: приведение типа здесь обязательно — `UNION ALL` требует согласия
// типов, и без него ветвь не собирается вовсе.
var roleScopeChainBranchRe = regexp.MustCompile(
	`(?i)select\s+'` + roleScopeChainType + `'\s*::\s*text`)

// roleScopeChainDirectRe — ПРЯМОЙ посев звена значениями, без источника.
//
// Отдельный признак, потому что у такой строки предиката нет вовсе: она не
// спрашивает ни одного ярусного столбца и потому не может быть ни (5a), ни (5b).
// Всякое её появление — находка независимо от текста рядом.
var roleScopeChainDirectRe = regexp.MustCompile(
	`(?is)insert\s+into\s+(?:kaname\.)?` + roleScopeChainTable + `\b[^;]*?values[^;]*?'` +
		roleScopeChainType + `'`)

// RoleScopeChainSite — координата одной находки.
type RoleScopeChainSite struct {
	File string
	Line int
	// What — начало ветви, дословно: читателю нужно место, а не пересказ.
	What string
}

// RoleScopeChainCensus — объём осмотренного одним файлом.
type RoleScopeChainCensus struct {
	// Statements — операторов, называющих таблицу звеньев, прочитано.
	Statements int
	// Branches — ветвей, производящих звено для `iam_role`, прочитано.
	Branches int
	// TierSourced — из них взявших звено у ярусного столбца. Печатается ОТДЕЛЬНО:
	// «находок 0» при «ветвей 0» означает, что распознаватель потерял предмет, а
	// не что дерево чисто.
	TierSourced int
}

// ScanRoleScopeChain разбирает один файл — SQL миграции либо прод-исходник Go.
//
// Разбор текстовый, и это названо прямо: у SQL в этом дереве нет разборщика, а
// разбор Go дал бы узлы литералов, внутри которых лежит тот же текст. Цена
// текстового разбора закрыта тем, что признак ветви взят у производителя
// дословно, а не собран по слову.
func ScanRoleScopeChain(path, src string) (found []RoleScopeChainSite, census RoleScopeChainCensus) {
	lower := strings.ToLower(src)
	if !strings.Contains(lower, roleScopeChainTable) {
		return nil, census
	}
	census.Statements = strings.Count(lower, roleScopeChainTable)

	for _, m := range roleScopeChainBranchRe.FindAllStringIndex(src, -1) {
		census.Branches++
		branch := roleScopeChainBranchOf(src, m[0])
		if roleScopeChainSourcedByTier(branch) {
			census.TierSourced++
			continue
		}
		found = append(found, RoleScopeChainSite{
			File: path,
			Line: roleScopeChainLineOf(src, m[0]),
			What: roleScopeChainFirstLine(branch),
		})
	}

	for _, m := range roleScopeChainDirectRe.FindAllStringIndex(src, -1) {
		census.Branches++
		found = append(found, RoleScopeChainSite{
			File: path,
			Line: roleScopeChainLineOf(src, m[0]),
			What: roleScopeChainFirstLine(src[m[0]:m[1]]),
		})
	}
	return found, census
}

// roleScopeChainBranchOf — текст ветви от её начала до следующей границы.
//
// Границей служит `UNION ALL` либо конец оператора: именно ими производитель
// разделяет ветви, и брать шире значило бы засчитать ветви СОСЕДА за источник
// этой — то есть молчать там, где сосед законен, а эта нет.
func roleScopeChainBranchOf(src string, from int) string {
	rest := src[from:]
	end := len(rest)
	for _, sep := range []string{"UNION ALL", "union all", ";"} {
		if i := strings.Index(rest, sep); i >= 0 && i < end {
			end = i
		}
	}
	return rest[:end]
}

// roleScopeChainSourcedByTier — берёт ли ветвь звено у ярусного столбца роли.
func roleScopeChainSourcedByTier(branch string) bool {
	lower := strings.ToLower(branch)
	for _, src := range roleScopeChainTierSources {
		if strings.Contains(lower, src) {
			return true
		}
	}
	return false
}

// roleScopeChainLineOf — номер строки по смещению в тексте. Координата обязана
// быть точной: читатель идёт по ней в файл.
func roleScopeChainLineOf(src string, off int) int {
	if off > len(src) {
		off = len(src)
	}
	return 1 + strings.Count(src[:off], "\n")
}

// roleScopeChainFirstLine — первая непустая строка отрезка, обрезанная по длине.
func roleScopeChainFirstLine(seg string) string {
	for _, line := range strings.Split(seg, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if len(t) > 120 {
			t = t[:120] + "…"
		}
		return t
	}
	return strings.TrimSpace(seg)
}
