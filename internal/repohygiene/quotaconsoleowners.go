// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Судящая часть гейта «витрина квот спрашивает ровно тех, у кого есть что
// показать проекту». Вынесена из пробы, чтобы инъекция гоняла ЕЁ, а не свою
// копию логики: проба, повторяющая разбор, доказывала бы свойство копии.

// QuotaConsoleCensus — объём осмотренного. Печатается ВСЕГДА: без него «находок
// нет» неотличимо от «ничего не прочитано». Обе половины названы порознь —
// одно суммарное число скрыло бы ровно тот случай, ради которого гейт заведён.
type QuotaConsoleCensus struct {
	// Kinds — сколько видов прочитано в каталоге величин ВСЕГО.
	Kinds int
	// ProjectKinds — из них носителем которых является ПРОЕКТ.
	ProjectKinds int
	// CatalogueDomains — доменов в каталоге; ProjectDomains — из них тех, у кого
	// есть хотя бы один вид-носитель проекта.
	CatalogueDomains, ProjectDomains int
	// PageOwners — записей в перечне витрины.
	PageOwners int
}

func (c QuotaConsoleCensus) String() string {
	return fmt.Sprintf(
		"осмотрено: видов каталога %d (из них носитель ПРОЕКТ — %d), доменов каталога %d "+
			"(из них с project-видом — %d); записей в перечне витрины %d",
		c.Kinds, c.ProjectKinds, c.CatalogueDomains, c.ProjectDomains, c.PageOwners)
}

var (
	// Запись каталога величин: `{"vpc.network", CarrierProject}` либо
	// `{"vpc.network.subnet", "vpc.network"}` — носителем бывает и РОДИТЕЛЬСКИЙ
	// РЕСУРС, и такая запись проектной не является: она отвечает на вопрос
	// «сколько детей в ОДНОМ родителе», а не «сколько их у проекта».
	quotaKindRe = regexp.MustCompile(`\{"([a-zA-Z][\w.]*)",\s*(Carrier\w+|"[a-zA-Z][\w.]*")\}`)
	// Запись перечня витрины: `{ domain: "vpc", path: "/vpc/v1/quotas" },`.
	quotaOwnerRe = regexp.MustCompile(`\{\s*domain:\s*"([a-z][\w-]*)"\s*,\s*path:\s*"([^"]+)"\s*\}`)
)

// quotaCatalogueToConsole — имя домена в каталоге против имени в консоли и на
// крае. Соответствие объявлено ЗДЕСЬ и явно, а не выведено из совпадения имён:
// вывод по совпадению молча пропустил бы ровно этот домен, и гейт отчитался бы
// «ноль находок», не осмотрев его. Оба написания настоящие, и ни одно не
// выводится из другого (`polyrepo.md` §«Ключ домена и имя каталога»).
var quotaCatalogueToConsole = map[string]string{"loadbalancer": "nlb"}

// ProjectCarryingDomains — домены, у которых есть ХОТЯ БЫ ОДИН вид, списываемый
// с ПРОЕКТА, в именах консоли.
func ProjectCarryingDomains(limitSrc string) (map[string]string, QuotaConsoleCensus) {
	var cen QuotaConsoleCensus
	out := map[string]string{} // домен консоли → первый его project-вид
	seen := map[string]bool{}
	for _, m := range quotaKindRe.FindAllStringSubmatch(limitSrc, -1) {
		kind, carrier := m[1], m[2]
		cen.Kinds++
		dom := strings.SplitN(kind, ".", 2)[0]
		if !seen[dom] {
			seen[dom] = true
			cen.CatalogueDomains++
		}
		if carrier != "CarrierProject" {
			continue
		}
		cen.ProjectKinds++
		name := dom
		if alias, ok := quotaCatalogueToConsole[dom]; ok {
			name = alias
		}
		if _, ok := out[name]; !ok {
			out[name] = kind
		}
	}
	cen.ProjectDomains = len(out)
	return out, cen
}

// PageQuotaOwners — перечень витрины: домен → путь чтения.
func PageQuotaOwners(pageSrc string) map[string]string {
	block := pageSrc
	if i := strings.Index(block, "const QUOTA_OWNERS"); i >= 0 {
		block = block[i:]
		if j := strings.Index(block, "] as const;"); j >= 0 {
			block = block[:j]
		}
	}
	out := map[string]string{}
	for _, m := range quotaOwnerRe.FindAllStringSubmatch(block, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// AuditQuotaConsoleOwners — сверка В ОБЕ СТОРОНЫ.
//
// Домен, списывающий с ПРОЕКТА и не спрошенный витриной, — находка: его
// арендатор упирается в предел, которого не видит. Запись витрины, за которой
// нет ни одного project-вида, — находка тише и потому опаснее: страница
// показывает потолок, под который ничего не считается.
func AuditQuotaConsoleOwners(limitSrc, pageSrc string) ([]string, QuotaConsoleCensus) {
	charging, cen := ProjectCarryingDomains(limitSrc)
	owners := PageQuotaOwners(pageSrc)
	cen.PageOwners = len(owners)

	var findings []string
	names := make([]string, 0, len(charging))
	for d := range charging {
		names = append(names, d)
	}
	sort.Strings(names)
	for _, d := range names {
		if _, ok := owners[d]; !ok {
			findings = append(findings, fmt.Sprintf(
				"%s — списывает с ПРОЕКТА (вид %q), но витрина квот его не спрашивает: "+
					"арендатор упирается в предел, которого не видит. Добавь запись в "+
					"QUOTA_OWNERS (ui-future/shared/src/pages/QuotasPage.tsx)", d, charging[d]))
		}
	}
	names = names[:0]
	for d := range owners {
		names = append(names, d)
	}
	sort.Strings(names)
	for _, d := range names {
		if _, ok := charging[d]; !ok {
			findings = append(findings, fmt.Sprintf(
				"%s — витрина квот его спрашивает (%s), но НИ ОДНОГО вида с носителем "+
					"ПРОЕКТ у него в каталоге величин нет: страница называет потолок, под "+
					"который ничего не считается", d, owners[d]))
		}
	}
	return findings, cen
}
