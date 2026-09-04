// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// licensemap.go — ОТОБРАЖЕНИЕ путь→лицензия: единственное место дерева, где
// сказано, под какой лицензией лежит файл по данному пути.
//
// # Почему отображение, а не одна константа
//
// До 2026-09-04 лицензионный гейт держал идентификатор ЛИТЕРАЛОМ
// (`const spdxMarker = "SPDX-License-Identifier: BUSL-1.1"`) и требовал его от
// каждого файла в области покрытия. Пока лицензия в дереве одна, литерал и
// отображение неразличимы; как только уровней становится больше одного, литерал
// начинает лгать в обе стороны сразу:
//
//   - файл с ЛЮБОЙ другой лицензией читается как файл БЕЗ лицензии — гейт
//     краснеет с текстом «missing SPDX header» на файле, у которого заголовок
//     есть. Разбор уводит не туда: чинят отсутствие там, где предмет —
//     несовпадение;
//   - обратное молчание: заголовок, не отвечающий своему пути, распознать
//     нечем — сравнивать не с чем.
//
// # Три уровня — следствие формы зависимости, а не вкус
//
// Реализация iam выносится отдельным продуктом и остаётся потребителем
// фундамента `pkg/` и контрактов `proto/`, которые остаются здесь. Копилефт
// §10 AGPL запрещает налагать на получателя дополнительные ограничения сверх
// самой лицензии, а BUSL-1.1 именно такова: она ограничивает использование до
// наступления даты перехода. Значит фундамент и контракты обязаны быть
// ПЕРМИССИВНЫМИ — иначе выбранная форма зависимости невыразима юридически при
// том, что технически она собирается.
//
//	pkg/           → Apache-2.0          фундамент, линкуется извне
//	proto/         → Apache-2.0          контракты, линкуются извне
//	services/iam/  → AGPL-3.0-or-later   вынесенный продукт
//	остальное      → BUSL-1.1            монорепо без изменений
//
// # Третья сторона — не пропуск, а объявленный уровень
//
// `proto/google/` — вендоренные контракты Google под собственным уведомлением
// Apache-2.0. Наш заголовок им не положен: он утверждал бы наше авторство над
// чужим текстом. Поэтому уровень несёт ПУСТОЙ идентификатор, а не отсутствует в
// таблице, — и его пустота наблюдаема: уровень, под который в дереве не попал
// НИ ОДИН файл, есть послабление без предмета, и гейт на нём краснеет
// (см. TestLicenseHeadersMatchTheirTier).
//
// # Разрешение пути — по САМОМУ ДЛИННОМУ совпавшему префиксу
//
// Не по порядку записей: порядок — свойство, которое правящий таблицу обязан
// помнить, а длина префикса выводится из самой записи. `proto/google/api/x.proto`
// принадлежит третьей стороне, а не контрактам, потому что её префикс длиннее,
// а не потому, что запись стоит выше.
package repohygiene

import (
	"path"
	"sort"
	"strings"
)

// Идентификаторы уровней. Держатся именованными константами, потому что их
// читают три разных места: заголовок файла, тело файла LICENSE и перепись.
const (
	licenseBUSL   = "BUSL-1.1"
	licenseApache = "Apache-2.0"
	licenseAGPL   = "AGPL-3.0-or-later"
)

// licenseTier — уровень лицензирования дерева.
type licenseTier struct {
	// Prefix — префикс rel-пути, ЗАКАНЧИВАЮЩИЙСЯ слэшем. Пустая строка —
	// умолчание всего дерева.
	Prefix string
	// Name — имя уровня, которым он назван в переписи и в находке.
	Name string
	// SPDX — идентификатор, который обязан стоять в заголовке файла этого
	// уровня. Пустая строка означает «файлы третьей стороны»: нашего заголовка
	// у них быть не должно.
	SPDX string
	// OwnLicenseFile — у корня уровня лежит СВОЙ файл LICENSE.
	OwnLicenseFile bool
}

// Root — каталог корня уровня в форме, в которой его отдаёт path.Dir: у
// умолчания это ".", у остальных — префикс без хвостового слэша.
func (t licenseTier) Root() string {
	if t.Prefix == "" {
		return "."
	}
	return strings.TrimSuffix(t.Prefix, "/")
}

// licenseTiers — само отображение. Порядок записей на разрешение НЕ влияет.
var licenseTiers = []licenseTier{
	{Prefix: "proto/google/", Name: "третья сторона", SPDX: ""},
	{Prefix: "pkg/", Name: "фундамент", SPDX: licenseApache, OwnLicenseFile: true},
	{Prefix: "proto/", Name: "контракты", SPDX: licenseApache, OwnLicenseFile: true},
	{Prefix: "services/iam/", Name: "вынесенный продукт", SPDX: licenseAGPL, OwnLicenseFile: true},
	{Prefix: "", Name: "монорепо", SPDX: licenseBUSL, OwnLicenseFile: true},
}

// licenseTierFor — уровень, которому принадлежит путь: самый длинный совпавший
// префикс. Умолчание с пустым префиксом совпадает всегда, поэтому результат
// определён для любого пути и второго значения не требуется.
func licenseTierFor(rel string) licenseTier {
	best := -1
	for i, t := range licenseTiers {
		if t.Prefix != "" && !strings.HasPrefix(rel, t.Prefix) {
			continue
		}
		if best < 0 || len(t.Prefix) > len(licenseTiers[best].Prefix) {
			best = i
		}
	}
	return licenseTiers[best]
}

// licenseTierNames — имена уровней в порядке убывания длины префикса. Нужен
// переписи: она печатает объём осмотренного ПО КАЖДОМУ уровню отдельно, и
// порядок строк не должен зависеть от обхода карты.
func licenseTierNames() []string {
	tiers := append([]licenseTier(nil), licenseTiers...)
	sort.Slice(tiers, func(i, j int) bool { return len(tiers[i].Prefix) > len(tiers[j].Prefix) })
	out := make([]string, 0, len(tiers))
	for _, t := range tiers {
		out = append(out, t.Name)
	}
	return out
}

// licenseTextMarkers — по чему узнаётся ТЕЛО файла лицензии. Идентификатор в
// заголовке файла и текст лицензии — разные утверждения, и расходятся они
// молча: файл, названный Apache-2.0 и содержащий текст BUSL, валиден для всякого
// читателя, кроме юриста.
var licenseTextMarkers = map[string][]string{
	licenseBUSL:   {"Business Source License 1.1"},
	licenseApache: {"Apache License", "Version 2.0, January 2004"},
	licenseAGPL:   {"GNU AFFERO GENERAL PUBLIC LICENSE", "Version 3, 19 November 2007"},
}

// licenseSubjectAliases — каталоги, чьё имя НЕ совпадает с идентификатором
// компонента. Запись здесь заводится только под такое расхождение; каталог,
// чьё имя совпадает, в карте не появляется.
var licenseSubjectAliases = map[string]string{
	"gateway": "kacho-api-gateway",
}

// licenseFileWant — чего гейт ждёт от файла LICENSE по данному пути.
type licenseFileWant struct {
	// SPDX — какая это лицензия.
	SPDX string
	// Subject — идентификатор предмета в строке `Licensed Work:`. Пустой у
	// форм, где параметра предмета нет вовсе (Apache-2.0, AGPL-3.0): требовать
	// его от них значило бы требовать строки, которой в лицензии не бывает.
	Subject string
	// Tier — уровень, из которого выведено ожидание; идёт в текст находки.
	Tier string
}

// licenseFileWantFor — чего обязан быть файл LICENSE по этому пути. Второй
// результат false означает «правило этого места не знает» — это находка, а не
// пропуск: иначе первый же файл лицензии в новом каталоге выпал бы из
// наблюдения молча.
func licenseFileWantFor(rel string) (licenseFileWant, bool) {
	dir := path.Dir(rel)
	tier := licenseTierFor(rel)

	// Корень уровня: лицензия уровня, предмет — только у формы BUSL.
	if tier.OwnLicenseFile && tier.Root() == dir {
		want := licenseFileWant{SPDX: tier.SPDX, Tier: tier.Name}
		if tier.SPDX == licenseBUSL {
			want.Subject = "kacho"
		}
		return want, true
	}

	// Компонент внутри уровня BUSL: предмет выводится из пути механически —
	// новый сервис под services/ карты НЕ требует.
	if tier.SPDX == licenseBUSL {
		if alias, ok := licenseSubjectAliases[dir]; ok {
			return licenseFileWant{SPDX: licenseBUSL, Subject: alias, Tier: tier.Name}, true
		}
		if svc, ok := strings.CutPrefix(dir, "services/"); ok && svc != "" && !strings.Contains(svc, "/") {
			return licenseFileWant{SPDX: licenseBUSL, Subject: "kacho-" + svc, Tier: tier.Name}, true
		}
	}
	return licenseFileWant{}, false
}
