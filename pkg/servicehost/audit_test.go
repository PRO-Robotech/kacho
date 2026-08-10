// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// audit_test.go — доказательство того, что отказы старта, которым нужен
// СЛУЖИМЫЙ НАБОР RPC, способны упасть, и что падают они на существе.
//
// Все пробы гоняют ТУ ЖЕ функцию, что и носитель (`audit`), на синтетическом
// входе. Проба, повторяющая логику отказа своей копией, доказывала бы свойство
// копии.
//
// # Почему вход синтетический, а не «дескриптор переведённого сервиса»
//
// У первого переведённого сервиса производителя половины этих входов НЕТ вовсе:
// в каталоге его домена ноль строк `scope_filtered` и ноль скрывающих
// существование. Прогон на нём подтвердил бы только то, что отказ бывает, и
// молчал бы о том, отличает ли он законное от незаконного. Поэтому пары
// «инъекция + законный близнец» собираются здесь.
package servicehost

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
)

// lawfulCatalog — каталог домена-демо: два метода, ни один не сужается и ничего
// не скрывает. Форма повторяет то, что производит вывод из аннотаций.
func lawfulCatalog() catalogView {
	return catalogView{rows: map[servicecontract.MethodFQN]catalogRow{
		"/kacho.cloud.demo.v1.WidgetService/Get": {
			Method: "/kacho.cloud.demo.v1.WidgetService/Get",
			Domain: "kacho.cloud.demo.v1",
		},
		"/kacho.cloud.demo.v1.WidgetService/List": {
			Method: "/kacho.cloud.demo.v1.WidgetService/List",
			Domain: "kacho.cloud.demo.v1",
		},
	}}
}

// lawfulServed — служимый набор, в точности покрытый каталогом выше.
func lawfulServed() servedSet {
	return servedSet{methods: []servicecontract.MethodFQN{
		"/kacho.cloud.demo.v1.WidgetService/Get",
		"/kacho.cloud.demo.v1.WidgetService/List",
	}}
}

// lawfulMap — карта, которую реально спрашивает звено решения о доступе.
func lawfulMap() authz.RPCMap {
	return authz.RPCMap{
		"/kacho.cloud.demo.v1.WidgetService/Get":  {Public: true},
		"/kacho.cloud.demo.v1.WidgetService/List": {Public: true},
	}
}

// naAxes — дескриптор домена, у которого обе судимые каталогом оси объявлены
// неприменимыми. Это законно РОВНО ПОКА каталог молчит; как только в нём
// появляется соответствующая строка, заявление становится находкой.
func naAxes() servicecontract.Spec {
	return servicecontract.Spec{
		Service: "kacho-demo",
		Narrowers: servicecontract.NotApplicable[map[servicecontract.MethodFQN]servicecontract.ListNarrower](
			"ни один метод демо не сужается по правам"),
		HideExistence: servicecontract.NotApplicable[map[servicecontract.ObjectType]servicecontract.NotFoundFormat](
			"демо не скрывает существование ни одного типа"),
	}
}

// TestAuditAcceptsLawfulTree — положительный контроль. Первым намеренно: пока он
// красный, каждое отрицание ниже утверждает «отказано» по чужой причине.
func TestAuditAcceptsLawfulTree(t *testing.T) {
	c, err := audit(naAxes(), lawfulServed(), lawfulCatalog(), lawfulMap())
	if err != nil {
		t.Fatalf("законный набор отвергнут — все отрицания ниже вакуумны: %v", err)
	}
	if c.methods != 2 || c.domains != 1 {
		t.Fatalf("перепись не сходится: %+v", c)
	}
	t.Logf("перепись: %s", c)
}

func refusesAudit(t *testing.T, s servicecontract.Spec, sv servedSet, cat catalogView, m authz.RPCMap, mustName ...string) string {
	t.Helper()
	_, err := audit(s, sv, cat, m)
	if err == nil {
		t.Fatal("набор принят — отказ не способен упасть")
	}
	for _, want := range mustName {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("отказ не называет %q — по нему нечего чинить:\n%v", want, err)
		}
	}
	return err.Error()
}

// ── О2: служится RPC, у которого нет строки каталога его домена ─────────────

func TestO2_ServedMethodWithoutCatalogRowRefusesStart(t *testing.T) {
	sv := lawfulServed()
	sv.methods = append(sv.methods, "/kacho.cloud.demo.v1.WidgetService/Delete")
	msg := refusesAudit(t, naAxes(), sv, lawfulCatalog(), lawfulMap(),
		"/kacho.cloud.demo.v1.WidgetService/Delete")
	t.Logf("О2 (а) красный: %s", msg)
}

// TestO2_PlatformServiceIsExemptAndNamed — законный близнец, и он несущий.
// Носитель служит не только доменные RPC: здоровье и рефлексия приезжают вместе
// с сервером, строк каталога у них нет и быть не может. Без ИМЕНОВАННОГО
// изъятия ни один процесс не поднялся бы вовсе; без ЗАКРЫТОГО перечня изъятие
// превратилось бы в корзину «всё, что не наше», куда однажды попадёт доменный RPC.
func TestO2_PlatformServiceIsExemptAndNamed(t *testing.T) {
	sv := lawfulServed()
	for name := range platformServices {
		sv.methods = append(sv.methods, servicecontract.MethodFQN("/"+name+"/Check"))
	}
	if _, err := audit(naAxes(), sv, lawfulCatalog(), lawfulMap()); err != nil {
		t.Fatalf("платформенная регистрация объявлена находкой: %v", err)
	}
}

// TestO2_UnknownNonKachoServiceIsNotExempt — обратная половина того же: изъятие
// работает по ЗАКРЫТОМУ ИМЕНИ, а не по признаку «не наш пакет». Иначе любой
// чужой пакет молча получал бы освобождение.
func TestO2_UnknownNonKachoServiceIsNotExempt(t *testing.T) {
	sv := lawfulServed()
	sv.methods = append(sv.methods, "/some.third.party.v1.Service/Do")
	refusesAudit(t, naAxes(), sv, lawfulCatalog(), lawfulMap(), "some.third.party.v1.Service")
}

// ── О9: у выведенного домена НОЛЬ строк каталога ────────────────────────────

func TestO9_DerivedDomainWithZeroCatalogRowsRefusesStart(t *testing.T) {
	sv := lawfulServed()
	sv.methods = append(sv.methods, "/kacho.cloud.ghost.v1.GhostService/Get")
	cat := lawfulCatalog()
	// Строка у метода есть, но домен не дал каталогу НИ ОДНОЙ записи: ровно тот
	// случай, когда «ноль целей» неотличим от «всё синхронно».
	cat.rows["/kacho.cloud.ghost.v1.GhostService/Get"] = catalogRow{
		Method: "/kacho.cloud.ghost.v1.GhostService/Get",
		Domain: "kacho.cloud.ghost.v1",
	}
	m := lawfulMap()
	m["/kacho.cloud.ghost.v1.GhostService/Get"] = authz.RPCEntry{Public: true}

	// Домен есть в служимом наборе и в строках, но карта, которую спрашивает
	// звено решения, о нём не знает ни одной записи.
	delete(m, "/kacho.cloud.ghost.v1.GhostService/Get")
	refusesAudit(t, naAxes(), sv, cat, m, "kacho.cloud.ghost.v1")
}

// TestO9_EveryDerivedDomainContributesRows — законный близнец: домен, давший
// карте хотя бы одну запись, находкой не является.
func TestO9_EveryDerivedDomainContributesRows(t *testing.T) {
	if _, err := audit(naAxes(), lawfulServed(), lawfulCatalog(), lawfulMap()); err != nil {
		t.Fatalf("домен со строками объявлен пустым: %v", err)
	}
}

// ── О3: каталог объявляет сужение, дескриптор сужателя не несёт ─────────────

func TestO3_DeclaredNarrowingWithoutWiringRefusesStart(t *testing.T) {
	cat := lawfulCatalog()
	row := cat.rows["/kacho.cloud.demo.v1.WidgetService/List"]
	row.ScopeFiltered = true
	cat.rows["/kacho.cloud.demo.v1.WidgetService/List"] = row

	msg := refusesAudit(t, naAxes(), lawfulServed(), cat, lawfulMap(),
		"/kacho.cloud.demo.v1.WidgetService/List", "Narrowers")
	t.Logf("О3 (а) красный: %s", msg)
}

// TestO3_NotApplicableIsJudgedByTheCatalog — то же зеркально и это ГЛАВНОЕ в
// О3: заявление «не применимо» истекает САМО. Пока каталог молчит — законно;
// появилась строка `scope_filtered` — то же самое заявление стало находкой,
// и никто не обязан о нём вспоминать.
func TestO3_NotApplicableIsJudgedByTheCatalog(t *testing.T) {
	// (б) каталог молчит → то же заявление принимается.
	if _, err := audit(naAxes(), lawfulServed(), lawfulCatalog(), lawfulMap()); err != nil {
		t.Fatalf("заявление «не применимо» отвергнуто при молчащем каталоге: %v", err)
	}
	// (а) каталог заговорил → то же заявление краснеет.
	cat := lawfulCatalog()
	row := cat.rows["/kacho.cloud.demo.v1.WidgetService/List"]
	row.ScopeFiltered = true
	cat.rows["/kacho.cloud.demo.v1.WidgetService/List"] = row
	refusesAudit(t, naAxes(), lawfulServed(), cat, lawfulMap(), "Narrowers")
}

// TestO3_WiredNarrowerSatisfiesTheCatalog — вторая законная сторона: проводка
// предъявлена ровно на объявленный каталогом метод → молчание.
func TestO3_WiredNarrowerSatisfiesTheCatalog(t *testing.T) {
	cat := lawfulCatalog()
	row := cat.rows["/kacho.cloud.demo.v1.WidgetService/List"]
	row.ScopeFiltered = true
	cat.rows["/kacho.cloud.demo.v1.WidgetService/List"] = row

	s := naAxes()
	s.Narrowers = servicecontract.Value(map[servicecontract.MethodFQN]servicecontract.ListNarrower{
		"/kacho.cloud.demo.v1.WidgetService/List": stubNarrower(),
	})
	if _, err := audit(s, lawfulServed(), cat, lawfulMap()); err != nil {
		t.Fatalf("предъявленная проводка объявлена находкой: %v", err)
	}
}

// ── О4: дескриптор несёт сужатель на метод, которого каталог таковым не звал ──

// TestO4_WiringWithoutDeclarationRefusesStart — зеркальная половина О3, и без
// неё ПРОПАЖА ПРОВОДКИ невидима: сужатель, потерянный при переносе слоёв, не
// краснеет нигде, потому что каталог о нём не спрашивает.
func TestO4_WiringWithoutDeclarationRefusesStart(t *testing.T) {
	s := naAxes()
	s.Narrowers = servicecontract.Value(map[servicecontract.MethodFQN]servicecontract.ListNarrower{
		"/kacho.cloud.demo.v1.WidgetService/Get": stubNarrower(),
	})
	msg := refusesAudit(t, s, lawfulServed(), lawfulCatalog(), lawfulMap(),
		"/kacho.cloud.demo.v1.WidgetService/Get", "Narrowers")
	t.Logf("О4 (а) красный: %s", msg)
}

// TestO4_NilNarrowerIsNotWiring — проводка, объявленная нулевым указателем, —
// не проводка. Без этой половины «сужатель предъявлен» удовлетворялось бы
// записью карты, которая на запросе паникует.
func TestO4_NilNarrowerIsNotWiring(t *testing.T) {
	cat := lawfulCatalog()
	row := cat.rows["/kacho.cloud.demo.v1.WidgetService/List"]
	row.ScopeFiltered = true
	cat.rows["/kacho.cloud.demo.v1.WidgetService/List"] = row

	s := naAxes()
	s.Narrowers = servicecontract.Value(map[servicecontract.MethodFQN]servicecontract.ListNarrower{
		"/kacho.cloud.demo.v1.WidgetService/List": nil,
	})
	refusesAudit(t, s, lawfulServed(), cat, lawfulMap(), "Narrowers")
}

// ── О5: каталог называет тип среди скрывающих, форма отказа не объявлена ────

func TestO5_HiddenTypeWithoutRefusalFormRefusesStart(t *testing.T) {
	cat := lawfulCatalog()
	row := cat.rows["/kacho.cloud.demo.v1.WidgetService/Get"]
	row.HideExistence = true
	row.ObjectType = "demo_widget"
	cat.rows["/kacho.cloud.demo.v1.WidgetService/Get"] = row

	msg := refusesAudit(t, naAxes(), lawfulServed(), cat, lawfulMap(), "demo_widget", "HideExistence")
	t.Logf("О5 (а) красный: %s", msg)
}

// TestO5_DeclaredFormSatisfiesTheCatalog — законный близнец.
func TestO5_DeclaredFormSatisfiesTheCatalog(t *testing.T) {
	cat := lawfulCatalog()
	row := cat.rows["/kacho.cloud.demo.v1.WidgetService/Get"]
	row.HideExistence = true
	row.ObjectType = "demo_widget"
	cat.rows["/kacho.cloud.demo.v1.WidgetService/Get"] = row

	s := naAxes()
	s.HideExistence = servicecontract.Value(map[servicecontract.ObjectType]servicecontract.NotFoundFormat{
		"demo_widget": "Widget %s not found",
	})
	if _, err := audit(s, lawfulServed(), cat, lawfulMap()); err != nil {
		t.Fatalf("объявленная форма отказа объявлена находкой: %v", err)
	}
}

// TestO5_FormWithoutSingleVerbIsRefused — форма отказа обязана нести РОВНО один
// `%s` под идентификатор. Скрытие существования работает ровно настолько,
// насколько текст неотличим от настоящего промаха владельца; форма без
// подстановки или с двумя подстановками даёт отличимый текст, то есть оракул.
func TestO5_FormWithoutSingleVerbIsRefused(t *testing.T) {
	cat := lawfulCatalog()
	row := cat.rows["/kacho.cloud.demo.v1.WidgetService/Get"]
	row.HideExistence = true
	row.ObjectType = "demo_widget"
	cat.rows["/kacho.cloud.demo.v1.WidgetService/Get"] = row

	for _, bad := range []servicecontract.NotFoundFormat{
		"Widget not found",
		"Widget %s %s not found",
		"Widget %d not found",
	} {
		s := naAxes()
		s.HideExistence = servicecontract.Value(map[servicecontract.ObjectType]servicecontract.NotFoundFormat{
			"demo_widget": bad,
		})
		refusesAudit(t, s, lawfulServed(), cat, lawfulMap(), "demo_widget")
	}
}

// TestO5_FormForATypeTheCatalogNeverHidesIsRefused — самоистечение и здесь:
// объявленная форма отказа, которой больше нечего скрывать, — находка. Иначе
// запись переживает свой предмет и утверждает, что платформа отвечает голосом
// владельца там, где владельца уже нет.
func TestO5_FormForATypeTheCatalogNeverHidesIsRefused(t *testing.T) {
	s := naAxes()
	s.HideExistence = servicecontract.Value(map[servicecontract.ObjectType]servicecontract.NotFoundFormat{
		"demo_ghost": "Ghost %s not found",
	})
	refusesAudit(t, s, lawfulServed(), lawfulCatalog(), lawfulMap(), "demo_ghost")
}

// ── перепись: «ноль находок» обязано быть отличимо от «ноль прочитанного» ────

// TestAuditRefusesAnEmptyServedSet — ноль служимых методов есть ОТКАЗ, а не
// успех. Раскатка в ноль целей печатала одиннадцать «пропущено» и выходила
// успехом; здесь этот исход невозможен.
func TestAuditRefusesAnEmptyServedSet(t *testing.T) {
	refusesAudit(t, naAxes(), servedSet{}, lawfulCatalog(), lawfulMap(), "ноль")
}

// TestCensusIsAssertedNotAssumed — перепись обязана называть объём осмотренного.
func TestCensusIsAssertedNotAssumed(t *testing.T) {
	c, err := audit(naAxes(), lawfulServed(), lawfulCatalog(), lawfulMap())
	if err != nil {
		t.Fatalf("законный набор отвергнут: %v", err)
	}
	s := c.String()
	for _, want := range []string{"методов", "доменов"} {
		if !strings.Contains(s, want) {
			t.Fatalf("перепись не называет %q: %q", want, s)
		}
	}
}

// ── вывод домена из имени службы (К-2) ──────────────────────────────────────

// TestDomainIsDerivedNotDeclared — домен снимается с имени зарегистрированной
// службы. Разбор НЕ вправе вернуть пустую строку молча: иначе «не разобрали» и
// «у домена ноль строк» стали бы одним и тем же исходом.
func TestDomainIsDerivedNotDeclared(t *testing.T) {
	for method, want := range map[servicecontract.MethodFQN]string{
		"/kacho.cloud.geo.v1.RegionService/Get":       "kacho.cloud.geo.v1",
		"/kacho.cloud.operation.OperationService/Get": "kacho.cloud.operation",
	} {
		got, err := domainOf(method)
		if err != nil {
			t.Fatalf("domainOf(%q) отказал: %v", method, err)
		}
		if got != want {
			t.Fatalf("domainOf(%q) = %q, ждали %q", method, got, want)
		}
	}
	for _, bad := range []servicecontract.MethodFQN{"", "/", "NoSlash", "/NoDot/Get", "/trailing."} {
		if got, err := domainOf(bad); err == nil {
			t.Fatalf("domainOf(%q) молча вернул %q вместо отказа — «не разобрали» стало бы неотличимо от «ноль строк»", bad, got)
		}
	}
}

// stubNarrower — проводка, отличная от нулевого указателя. Её ответы здесь не
// проверяются: О3/О4 требуют НАЛИЧИЕ проводки, а не её правильность (правильность
// — предмет конформанса, и это названо остаточным риском, а не закрыто).
func stubNarrower() servicecontract.ListNarrower {
	return listnarrow.New(nil, listnarrow.Config{})
}
