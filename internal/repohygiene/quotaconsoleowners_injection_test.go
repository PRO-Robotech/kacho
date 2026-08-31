// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/internal/repohygiene"
)

// Доказательство того, что гейт витрины квот СПОСОБЕН упасть — и что падает он
// на существе, а не на форме.
//
// ФИКСТУРА ПРИВЯЗАНА К ДЕРЕВУ, А НЕ К ПАМЯТИ АВТОРА: обе стороны берутся из
// закоммиченных файлов, дефект в них ВОЗВРАЩАЕТСЯ, и каждая проба сперва
// утверждает, что предмет её правки в дереве ЕСТЬ. Синтетика доказывала бы
// свойство вчерашнего дерева.
//
// ПРОГОНОВ ТРИ, и третий обязателен (`testing.md` §«Гейт на класс», п. 2в):
// без него молчание гейта на контроле неотличимо от молчания мёртвого.

func quotaSources(t *testing.T) (limitSrc, pageSrc string) {
	t.Helper()
	root := repoRootFor(t)
	l, err := os.ReadFile(filepath.Join(root, "services", "iam", "internal", "domain", "limit.go")) // #nosec G304
	require.NoError(t, err)
	p, err := os.ReadFile(filepath.Join(root, "ui-future", "shared", "src", "pages", "QuotasPage.tsx")) // #nosec G304
	require.NoError(t, err)
	return string(l), string(p)
}

// Прогон 1 — КОНТРОЛЬ: дерево как есть, гейт молчит, и обход не пуст.
func TestQCO_Run1_ControlIsSilentAndNotEmpty(t *testing.T) {
	t.Parallel()
	limitSrc, pageSrc := quotaSources(t)

	f, cen := repohygiene.AuditQuotaConsoleOwners(limitSrc, pageSrc)
	require.Empty(t, f, "гейт покраснел на дереве как есть — контроль опровергнут")
	require.NotZero(t, cen.ProjectDomains, "доменов с project-видом ноль — обход пуст, молчание беспредметно")
	require.Equal(t, cen.ProjectDomains, cen.PageOwners,
		"на контроле множества обязаны совпадать: %d против %d", cen.ProjectDomains, cen.PageOwners)
}

// Прогон 2 — СНЯТА ЗАПИСЬ ВИТРИНЫ: домен списывает с проекта, а витрина его не
// спрашивает. Это и есть предмет, которым #412 заводилась.
func TestQCO_Run2_OwnerDroppedFromThePageIsAFinding(t *testing.T) {
	t.Parallel()
	limitSrc, pageSrc := quotaSources(t)

	const dropped = `  { domain: "storage", path: "/storage/v1/quotas" },` + "\n"
	require.Containsf(t, pageSrc, dropped,
		"фикстура беспредметна: записи %q в витрине нет — форма перечня сменилась, "+
			"и проба доказывала бы свойство вчерашнего дерева", strings.TrimSpace(dropped))

	f, cen := repohygiene.AuditQuotaConsoleOwners(limitSrc, strings.Replace(pageSrc, dropped, "", 1))
	require.NotEmpty(t, f, "снятый владелец не дал находки — гейт не способен упасть на своём предмете")
	require.Len(t, f, 1, "снятие ОДНОЙ записи дало %d находок — инъекция роняет не только проверяемое: %v", len(f), f)
	require.Contains(t, f[0], "storage")
	require.Contains(t, f[0], "витрина квот его не спрашивает")
	// Прибавка обязана менять ПЕРЕПИСЬ, а не только число находок.
	require.Equal(t, cen.ProjectDomains-1, cen.PageOwners,
		"перепись не заметила снятой записи: %d против %d", cen.ProjectDomains, cen.PageOwners)
}

// Прогон 3 — ЗАПИСЬ БЕЗ ЕДИНОГО PROJECT-ВИДА: обратная сторона, и она тише.
// Ровно её даёт наивная правка «добавить iam шестым владельцем»: страница
// назвала бы потолок, под который ничего не считается, — все девять видов iam
// носятся аккаунтом, личностью либо родительским принципалом.
func TestQCO_Run3_PageEntryWithoutAnyProjectKindIsAFinding(t *testing.T) {
	t.Parallel()
	limitSrc, pageSrc := quotaSources(t)

	const anchor = `  { domain: "vpc", path: "/vpc/v1/quotas" },` + "\n"
	require.Containsf(t, pageSrc, anchor, "фикстура беспредметна: якоря %q в витрине нет", strings.TrimSpace(anchor))
	// Контроль предпосылки: у iam действительно НЕТ ни одного project-вида —
	// иначе эта проба доказывала бы обратное тому, что объявляет.
	byDom, _ := repohygiene.ProjectCarryingDomains(limitSrc)
	require.NotContains(t, byDom, "iam",
		"у iam появился вид с носителем ПРОЕКТ — предпосылка пробы устарела, и её надо "+
			"перемерить, а не подгонять")

	added := strings.Replace(pageSrc, anchor, anchor+`  { domain: "iam", path: "/iam/v1/quotas" },`+"\n", 1)
	f, _ := repohygiene.AuditQuotaConsoleOwners(limitSrc, added)
	require.NotEmpty(t, f, "запись без единого project-вида не дала находки — обратная сторона сверки мертва")
	require.Len(t, f, 1, "добавление ОДНОЙ записи дало %d находок — инъекция роняет не только проверяемое: %v", len(f), f)
	require.Contains(t, f[0], "iam")
	require.Contains(t, f[0], "потолок, под который ничего не считается")
}

// Законный близнец: имя домена в каталоге и в консоли РАЗНОЕ, и соответствие
// объявлено явно. Без него `loadbalancer` был бы вечной находкой в обе стороны,
// и гейт отключили бы первым.
func TestQCO_CatalogueAliasIsHonouredNotGuessed(t *testing.T) {
	t.Parallel()
	limitSrc, _ := quotaSources(t)

	byDom, _ := repohygiene.ProjectCarryingDomains(limitSrc)
	require.Contains(t, byDom, "nlb",
		"домен балансировщика не приведён к имени консоли: в каталоге он `loadbalancer`, "+
			"и без объявленного соответствия гейт краснел бы на исправном дереве")
	require.NotContains(t, byDom, "loadbalancer",
		"каталожное имя доехало до сверки как есть — соответствие применено не ко всем видам")
}

// Законный близнец: носителем бывает РОДИТЕЛЬСКИЙ РЕСУРС, и такой вид проектным
// не является. Без этого различения `vpc.network.subnet` считался бы
// project-видом, а мерка гейта перестала бы отличать «сколько у проекта» от
// «сколько в одном родителе».
func TestQCO_ParentCarriedKindIsNotCountedAsProjectCarried(t *testing.T) {
	t.Parallel()

	const src = `var countableKinds = []CountableKind{
	{"vpc.network", CarrierProject},
	{"vpc.network.subnet", "vpc.network"},
	{"iam.account", CarrierIdentity},
	{"iam.project", CarrierAccount},
}`
	byDom, cen := repohygiene.ProjectCarryingDomains(src)
	require.Equal(t, 4, cen.Kinds, "прочитано видов %d вместо четырёх", cen.Kinds)
	require.Equal(t, 1, cen.ProjectKinds,
		"носителем ПРОЕКТ признано %d видов вместо одного: родительский либо аккаунтный "+
			"носитель принят за проектный", cen.ProjectKinds)
	require.Equal(t, map[string]string{"vpc": "vpc.network"}, byDom)
}
