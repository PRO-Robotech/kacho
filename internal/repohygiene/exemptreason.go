// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// exemptreason.go — судья полосы «проверки нет»: словарь причин, требование по
// каждой причине и перечень координат энфорса.
//
// Судья вынесен из пробы в обычный файл НЕ ради красоты раскладки: его
// способность упасть доказывается инъекцией на синтетических записях, а
// инъекция, которая читает те же файлы дерева, что и сам гейт, проверяла бы
// дерево, а не судью. Разделение делает вход судьи управляемым — и потому
// проверяемым в обе стороны.
//
// Норма, предмет и границы — в шапке `exemptreason_test.go`.

import (
	"sort"
	"strconv"
	"strings"
)

// ExemptSentinelValue — значение поля `permission`, означающее «модель не
// спрашивают».
const ExemptSentinelValue = "<exempt>"

// Закрытый словарь причин. Значение вне словаря — находка: перечень, в который
// можно дописать своё слово, перестаёт различать что бы то ни было.
const (
	ReasonInternalListener = "INTERNAL_LISTENER"
	ReasonServiceSideAuthz = "SERVICE_SIDE_AUTHZ"
	ReasonSelfService      = "SELF_SERVICE"
	ReasonHandlerDecides   = "HANDLER_DECIDES"
)

// exemptReasonDictionary — закрытый словарь. Значение — то, что причина
// УТВЕРЖДАЕТ; требование по каждой причине выражено ниже предикатом, а не этим
// текстом.
var exemptReasonDictionary = map[string]string{
	ReasonInternalListener: "внутренний слушатель: mTLS и круг законных отправителей",
	ReasonServiceSideAuthz: "решение принимает сервис-владелец на данных страницы",
	ReasonSelfService:      "объекта нет by construction: вход и самообслуживание",
	ReasonHandlerDecides:   "область полиморфна либо владение решается предикатом у владельца",
}

// EnforcementSite — координата, где решение о доступе ДЕЙСТВИТЕЛЬНО принимается.
//
// Перечень требуется по трём причинам из четырёх: у внутреннего слушателя
// требование выражено самим именем службы и координаты не нуждается. Записи
// САМОИСТЕКАЮТ в обе стороны: строка перечня без живой записи каталога —
// находка (ей больше нечего называть), запись каталога без строки — тоже
// (причина заявлена, а место не названо).
// Седьмым ушло чтение одной роли (#973): его причина называла «решение
// принимает сервис своим Check», тогда как решения не принимал никто — каталог
// ролей читает любой аутентифицированный, а сужение делает обработчик на
// данных. Гейт это и назвал: строка перечня осталась без живой записи каталога.
// Шесть списков iam ушли отсюда вместе со своим освобождением (#914): они
// переведены в полосу сужения на данных, где решение принимает тот же владелец,
// но край при этом ОБЯЗАН извлечь принципала. Строка перечня без живой записи
// каталога — находка, поэтому запись и координата снимаются одним изменением.
//
// ЧЕТЫРЕ КООРДИНАТЫ СЛУЖБЫ ДОСТУПА СНЯТЫ ВМЕСТЕ СО СВОИМ ПРЕДМЕТОМ (вынос
// `services/iam` отдельным продуктом). Записи каталога у этих методов ЖИВЫ — край
// по-прежнему их маршрутизирует, контракт по-прежнему лежит в `proto/`, — а вот
// код, принимающий решение, уехал в чужое дерево. Координата, которую здесь ни
// при каком состоянии диска не резолвить, есть утверждение о том, чего не
// существует; держать её значило бы обещать проверку, которой нет.
//
// Требование при этом НЕ снято, а АДРЕСОВАНО ВЛАДЕЛЬЦУ: полоса, чья часть
// объявлена внешне-исходной (`productnaming.SourcesInThisTree`), судится в
// репозитории владельца, а здесь считается ОТДЕЛЬНЫМ ЧИСЛОМ переписи. Молчаливый
// пропуск был бы неотличим от «осмотрели и сошлось». Послабление ИСТЕКАЕТ САМО:
// вернётся часть в это дерево — ведомость разреза перестанет называть её внешней,
// и координата снова станет обязательной; исчезнут записи из каталога — истечёт и
// предмет.
var EnforcementSite = map[string]string{
	"kacho.cloud.operation.OperationService/Get":    "gateway/internal/opsproxy/proxy.go",
	"kacho.cloud.operation.OperationService/Cancel": "gateway/internal/opsproxy/proxy.go",
}

// ExemptCatalogRow — запись каталога прав вместе с копией, из которой прочитана.
type ExemptCatalogRow struct {
	FQN          string `json:"fqn"`
	Permission   string `json:"permission"`
	ExemptReason string `json:"exempt_reason"`
	Source       string `json:"-"`
}

// ExemptJudgement — вердикт судьи вместе с объёмом осмотренного.
type ExemptJudgement struct {
	Total    int
	Exempt   int
	ByReason map[string]int
	// SiteJudgedHere — полос, у которых причина требует координаты И владелец
	// живёт в ЭТОМ дереве: единственные, о которых молчание что-то значит.
	SiteJudgedHere int
	// SiteDeferredToOwner — полос, чей владелец объявлен внешне-исходным.
	// Отдельным числом, а не вычетом: пропуск, не названный числом, неотличим
	// от «осмотрели и сошлось».
	SiteDeferredToOwner int
	Findings            []string
}

// Census — объём осмотренного одной строкой. Печатается ВСЕГДА: «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
func (j ExemptJudgement) Census() string {
	reasons := make([]string, 0, len(j.ByReason))
	for r, n := range j.ByReason {
		name := r
		if name == "" {
			name = "<без причины>"
		}
		reasons = append(reasons, name+"="+strconv.Itoa(n))
	}
	sort.Strings(reasons)
	return "осмотрено: записей " + strconv.Itoa(j.Total) +
		", из них `<exempt>` " + strconv.Itoa(j.Exempt) +
		"; координата энфорса: судится здесь " + strconv.Itoa(j.SiteJudgedHere) +
		", отложено владельцу в чужом дереве " + strconv.Itoa(j.SiteDeferredToOwner) +
		"; по причинам: " + strings.Join(reasons, " ")
}

// serviceShortName — короткое имя службы из полного имени RPC.
func serviceShortName(fqn string) string {
	s := fqn
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// JudgeExemptLane выносит вердикт по набору записей каталога.
//
// `sites` — перечень координат энфорса, `siteExists` — предикат существования
// координаты в дереве. Оба параметрами, а не глобальными: инъекция обязана
// управлять входом судьи, иначе она проверяла бы дерево, а не судью.
func JudgeExemptLane(
	rows []ExemptCatalogRow,
	sites map[string]string,
	siteExists func(string) bool,
	ownerOutsideTree func(fqn string) bool,
) ExemptJudgement {
	j := ExemptJudgement{ByReason: map[string]int{}}
	claimed := map[string]bool{}

	for _, r := range rows {
		j.Total++
		where := r.FQN
		if r.Source != "" {
			where += " (" + r.Source + ")"
		}

		if r.Permission != ExemptSentinelValue {
			if r.ExemptReason != "" {
				j.Findings = append(j.Findings,
					"не-освобождённая запись несёт причину освобождения: "+where)
			}
			continue
		}

		j.Exempt++
		j.ByReason[r.ExemptReason]++

		if r.ExemptReason == "" {
			j.Findings = append(j.Findings, "полоса `<exempt>` без причины: "+where)
			continue
		}
		if _, ok := exemptReasonDictionary[r.ExemptReason]; !ok {
			j.Findings = append(j.Findings,
				"причина вне закрытого словаря: "+r.FQN+" → "+r.ExemptReason+
					" (словарь закрыт намеренно; причины «глобальный справочник» в нём НЕТ — "+
					"публичное чтение справочника выражается системной выдачей, а не освобождением)")
			continue
		}

		if r.ExemptReason == ReasonInternalListener {
			// Требование причины «внутренний слушатель»: служба и вправду
			// внутренняя. Иначе полоса, задуманная как «сюда попадают по
			// сертификату», прикрывала бы публичный вызов.
			if !strings.HasPrefix(serviceShortName(r.FQN), "Internal") {
				j.Findings = append(j.Findings,
					"причина «внутренний слушатель» у НЕвнутренней службы: "+where)
			}
			continue
		}

		// Остальные три причины утверждают, что решение принимает КОД. Значит у
		// них обязана быть живая координата этого кода — но только если сам код
		// лежит в ЭТОМ дереве. У части, объявленной внешне-исходной, координата
		// не резолвится и резолвиться не может; требование адресуется её
		// владельцу, а здесь считается отдельным числом.
		if ownerOutsideTree != nil && ownerOutsideTree(r.FQN) {
			j.SiteDeferredToOwner++
			if named, ok := sites[r.FQN]; ok {
				j.Findings = append(j.Findings, "перечень координат называет метод, чей "+
					"владелец объявлен внешне-исходным: "+r.FQN+" → "+named+
					" — координата здесь не резолвится ни при каком состоянии дерева")
				claimed[r.FQN] = true
			}
			continue
		}
		j.SiteJudgedHere++
		site, ok := sites[r.FQN]
		if !ok {
			j.Findings = append(j.Findings, "причина "+r.ExemptReason+
				" заявлена, а координата энфорса не названа: "+where)
			continue
		}
		claimed[r.FQN] = true
		if !siteExists(site) {
			j.Findings = append(j.Findings,
				"координата энфорса не резолвится: "+r.FQN+" → "+site)
		}
	}

	// Самоистечение перечня: запись, которой больше нечего называть, — находка.
	for fqn := range sites {
		if !claimed[fqn] {
			j.Findings = append(j.Findings, "перечень координат называет метод, у которого нет "+
				"живой записи `<exempt>` с причиной, требующей координаты: "+fqn)
		}
	}

	sort.Strings(j.Findings)
	return j
}
