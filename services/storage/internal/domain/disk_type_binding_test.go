// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
)

// Ревизия привязки класса к бэкенду. Приёмка STOR-P-13 (новая ревизия вытесняет
// прежнюю, прежняя не изменяется ни в одном поле) и STOR-P-14 (ровно одна
// действующая на пару «класс × зона» — держится частичной уникальностью в 0015).
//
// Несущее свойство — НЕИЗМЕНЯЕМОСТЬ, и она выражена ТИПОМ, а не уговором: ресурс
// ссылается на ревизию, под которой создан, поэтому правка класса физически не
// может задним числом изменить его свойства. Механизмом является ОТСУТСТВИЕ
// обновления, а значит проба обязана утверждать именно отсутствие.

func TestDiskTypeBinding_Validate(t *testing.T) {
	t.Parallel()

	// Положительный контроль: законная ревизия проходит.
	require.NoError(t, newValidBinding().Validate())

	// НОМЕР РЕВИЗИИ ДО ЗАПИСИ ЕЩЁ НЕ НАЗНАЧЕН, и ноль здесь означает именно это.
	//
	// Прежняя редакция пробы держала ноль в таблице отвергаемых — и закрепляла
	// противоречие, из-за которого глагол не работал НИ РАЗУ: номер назначает
	// регистрация (`COALESCE(max(revision),0)+1`), а присланный она отвергает
	// явно. Через любой вход получался отказ: ноль отвергал домен, ненулевое —
	// хранилище. Два правила об одном поле, взаимоисключающие.
	//
	// Проба обязана утверждать ВХОДНОЕ состояние, а не результат записи.
	unassigned := newValidBinding()
	unassigned.Revision = 0
	require.NoError(t, unassigned.Validate(),
		"ноль означает «номер ещё не назначен» — это законный вход регистрации")

	for name, mutate := range map[string]func(*domain.DiskTypeBinding){
		"без id":      func(b *domain.DiskTypeBinding) { b.ID = "" },
		"без класса":  func(b *domain.DiskTypeBinding) { b.DiskTypeID = "" },
		"без зоны":    func(b *domain.DiskTypeBinding) { b.ZoneID = "" },
		"без бэкенда": func(b *domain.DiskTypeBinding) { b.BackendID = "" },
		// Зеркало к утверждению выше: отрицательный номер — не «не назначен»,
		// а названный неверно, и остаётся отказом. Без этой пары послабление
		// читалось бы как «номер не проверяется вовсе».
		"отрицательная ревизия": func(b *domain.DiskTypeBinding) { b.Revision = -1 },
		"без пула": func(b *domain.DiskTypeBinding) { b.Locator.Pool = "" },
		"шаблон без подстановки":     func(b *domain.DiskTypeBinding) { b.Locator.NamespaceTemplate = "prj-shared" },
		"состояние вне словаря":      func(b *domain.DiskTypeBinding) { b.Status = "DELETED" },
		"отрицательный срок корзины": func(b *domain.DiskTypeBinding) { b.Capabilities.TrashTTLSeconds = -1 },
		"отрицательные числа qos":    func(b *domain.DiskTypeBinding) { b.QoS.BaselineIOPS = -1 },
		"потолок ниже базы":          func(b *domain.DiskTypeBinding) { b.QoS.MaxIOPS = 10 },
	} {
		b := newValidBinding()
		mutate(&b)
		require.Errorf(t, b.Validate(), "обязано отвергаться: %s", name)
	}
}

func TestDiskTypeBindingStatus_ClosedDictionary(t *testing.T) {
	t.Parallel()

	for _, st := range []domain.BindingStatus{domain.BindingStatusActive, domain.BindingStatusSuperseded} {
		b := newValidBinding()
		b.Status = st
		require.NoErrorf(t, b.Validate(), "состояние %q обязано приниматься", st)
	}
	// Пустое состояние — «ещё не назначено», а не «названо неверно»: назначает его
	// регистрация (вставляет действующее и вытесняет прежнюю ревизию). Держать его
	// в отвергаемых значило закреплять противоречие, из-за которого регистрация не
	// работала ни разу: вызывающий обязан был назвать величину, которую хранилище
	// всё равно назначает само и отвергает всякую другую.
	unassigned := newValidBinding()
	unassigned.Status = ""
	require.NoError(t, unassigned.Validate(),
		"пустое состояние — законный вход регистрации, она назначит действующее")

	// Зеркало: непустое по-прежнему обязано принадлежать словарю, иначе
	// послабление читалось бы как «состояние не проверяется вовсе».
	for _, bad := range []domain.BindingStatus{"active", "RETIRED", "ACTIVE "} {
		b := newValidBinding()
		b.Status = bad
		require.Errorf(t, b.Validate(), "состояние вне словаря обязано отвергаться: %q", bad)
	}
}

// ГЕЙТ неизменяемости. У типа не должно существовать ни одного метода, способного
// изменить его поля: единственный переход (в SUPERSEDED) производит НОВОЕ значение.
//
// Проверяется механически: метод способен изменить приёмник только при указательном
// приёмнике, поэтому набор методов у *T обязан совпадать с набором у T.
func TestDiskTypeBinding_HasNoMutatingMethods(t *testing.T) {
	t.Parallel()

	value := reflect.TypeOf(domain.DiskTypeBinding{})
	pointer := reflect.PointerTo(value)

	// Предпосылка гейта, проверяемая самим гейтом: у ревизии нет полей ссылочного
	// типа — ни на верхнем уровне, ни во вложенных структурах. Появись срез или
	// карта, метод с приёмником-ЗНАЧЕНИЕМ правил бы их содержимое на месте, и
	// равенство наборов методов перестало бы означать неизменяемость. Тогда этот
	// гейт остался бы зелёным, утверждая свойство, которого больше нет.
	assertNoReferenceFields(t, value, value.Name())

	// Положительный контроль к равенству ниже: методы у типа есть. Без него
	// равенство «0 == 0» выполнялось бы вакуумно на типе вообще без поведения.
	require.Positive(t, value.NumMethod(), "у ревизии обязаны быть методы, иначе равенство наборов вакуумно")

	valueMethods := map[string]bool{}
	for i := range value.NumMethod() {
		valueMethods[value.Method(i).Name] = true
	}
	var mutators []string
	for i := range pointer.NumMethod() {
		if name := pointer.Method(i).Name; !valueMethods[name] {
			mutators = append(mutators, name)
		}
	}
	require.Emptyf(t, mutators,
		"методы с указательным приёмником способны изменить ревизию: %v. "+
			"Ревизия неизменяема — переход состояния обязан производить новое значение", mutators)
}

// Единственный законный переход: в SUPERSEDED. Он производит новое значение и не
// трогает исходное — «прежняя строка не изменена ни в одном поле» (STOR-P-13).
func TestDiskTypeBinding_SupersedeProducesNewValue(t *testing.T) {
	t.Parallel()

	active := newValidBinding()
	superseded := active.Supersede()

	require.Equal(t, domain.BindingStatusSuperseded, superseded.Status)
	require.Equal(t, domain.BindingStatusActive, active.Status, "исходная ревизия обязана остаться нетронутой")
	require.True(t, active.IsActive())
	require.False(t, superseded.IsActive())

	// Переход меняет РОВНО статус: приравняв статусы, получаем то же значение.
	// Утверждение поимённо не перечисляет поля — иначе новое поле молча выпало бы
	// из-под проверки.
	sameStatus := superseded
	sameStatus.Status = active.Status
	require.Equal(t, active, sameStatus, "переход обязан менять только состояние")

	// Повтор перехода — тот же результат: вытеснение уже вытесненной ревизии
	// случается при повторе доставки и не должно быть отказом.
	require.Equal(t, superseded, superseded.Supersede())
}

// Шаблон единицы изоляции арендатора обязан НЕСТИ подстановку проекта. Шаблон без
// неё выглядит настроенным, а на деле сводит всех арендаторов класса в одно
// пространство имён у бэкенда: шумный сосед ничем не ограничен, а любая ошибка в
// правах на стороне хранилища сразу межарендная.
func TestBindingLocator_NamespaceTemplateCarriesProject(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"", "{projectId}", "prj-{projectId}", "kacho-{projectId}-block"} {
		loc := domain.BindingLocator{Pool: "kacho-block-balanced", NamespaceTemplate: ok}
		require.NoErrorf(t, loc.Validate(), "шаблон обязан приниматься: %q", ok)
	}
	for _, bad := range []string{"prj-shared", "kacho", "{project_id}", "{projectID}"} {
		loc := domain.BindingLocator{Pool: "kacho-block-balanced", NamespaceTemplate: bad}
		require.Errorf(t, loc.Validate(), "шаблон без подстановки обязан отвергаться: %q", bad)
	}
}

// Сцепка двух мест об одном предмете: домен решает, ЧТО такое законный шаблон, а
// подстановку исполняет помощник порта. Разъедься они — домен принимал бы шаблон,
// который у бэкенда не подставляется, и изоляция исчезла бы молча. Проба падает
// именно на расхождении и называет его причиной.
func TestBindingLocator_TemplateAgreesWithBackendNaming(t *testing.T) {
	t.Parallel()

	const projectID = "prj-4kq2n8xr1vm0d3c7f"

	loc := domain.BindingLocator{Pool: "kacho-block-balanced", NamespaceTemplate: "prj-{projectId}"}
	require.NoError(t, loc.Validate())
	got := blockbackend.NamespaceOfProject(loc.NamespaceTemplate, projectID)
	require.Contains(t, got, projectID, "подстановка обязана произойти — домен принял шаблон именно за это")
	require.NotEqual(t, loc.NamespaceTemplate, got)

	// Отрицание в паре: шаблон, который домен отвергает, у порта действительно
	// НЕ подставляется — то есть отвергается не из вкусовщины.
	const shared = "prj-shared"
	require.Error(t, (domain.BindingLocator{Pool: "p", NamespaceTemplate: shared}).Validate())
	require.Equal(t, shared, blockbackend.NamespaceOfProject(shared, projectID),
		"именно поэтому шаблон и отвергается: подстановки нет, пространство имён общее на всех арендаторов")
}

func TestBindingQoS_Validate(t *testing.T) {
	t.Parallel()

	// Положительный контроль: числа из целевого вида класса проходят.
	require.NoError(t, domain.BindingQoS{
		BaselineIOPS: 3000, IOPSPerGiB: 30, MaxIOPS: 80000,
		BaselineThroughputMiBps: 125, ThroughputPerGiBMiBps: 0.5, MaxThroughputMiBps: 1000,
	}.Validate())

	// Ничего не объявлено — законно: ноль означает «класс не называет числа», а не
	// «ограничение равно нулю».
	require.NoError(t, domain.BindingQoS{}.Validate())

	for name, q := range map[string]domain.BindingQoS{
		"отрицательная база":           {BaselineIOPS: -1},
		"отрицательный наклон":         {IOPSPerGiB: -1},
		"отрицательный потолок":        {MaxIOPS: -1},
		"отрицательная база полосы":    {BaselineThroughputMiBps: -1},
		"отрицательный наклон полосы":  {ThroughputPerGiBMiBps: -0.5},
		"отрицательный потолок полосы": {MaxThroughputMiBps: -1},
		"потолок ниже базы":            {BaselineIOPS: 3000, MaxIOPS: 100},
		"потолок полосы ниже базы":     {BaselineThroughputMiBps: 125, MaxThroughputMiBps: 10},
		"наклон полосы не число":       {ThroughputPerGiBMiBps: math.NaN()},
		"наклон полосы без предела":    {ThroughputPerGiBMiBps: math.Inf(1)},
	} {
		require.Errorf(t, q.Validate(), "обязано отвергаться: %s", name)
	}
}

// Публичные способности КЛАССА выводятся пересечением по его ДЕЙСТВУЮЩИМ ревизиям.
// Консервативно: класс предлагается в нескольких зонах, зоны могут обслуживаться
// разными бэкендами, и объявить способность, которой нет в одной из зон, значит
// пообещать отказ — арендатор напишет код по каталогу и получит его в проде.
func TestPublicCapabilities_IntersectsActiveRevisions(t *testing.T) {
	t.Parallel()

	const class = "block-balanced"

	full := domain.BindingCapabilities{
		Snapshots: true, CloneFromSnapshot: true, CloneFromImage: true,
		OnlineGrow: true, MultiAttach: true, EncryptionAtRest: true,
		CloneKeepsParent: true, TrashTTLSeconds: 86400,
	}

	// Одна ревизия — класс объявляет ровно то, что она несёт.
	one := bindingWith(class, "ru-central1-a", 1, full)
	require.Equal(t, domain.Capabilities{
		Snapshots: true, CloneFromSnapshot: true, CloneFromImage: true,
		OnlineGrow: true, MultiAttach: true, EncryptionAtRest: true,
	}, domain.PublicCapabilities(class, []domain.DiskTypeBinding{one}))

	// Две согласные — тот же ответ, пересечение ничего не сужает.
	two := bindingWith(class, "ru-central1-b", 1, full)
	require.Equal(t,
		domain.PublicCapabilities(class, []domain.DiskTypeBinding{one}),
		domain.PublicCapabilities(class, []domain.DiskTypeBinding{one, two}),
		"согласные ревизии не сужают ответ")

	// Две расходящиеся — пересечение УЖЕ: способность, которой нет во второй зоне,
	// классом не объявляется.
	narrow := full
	narrow.MultiAttach = false
	narrow.EncryptionAtRest = false
	diverging := bindingWith(class, "ru-central1-c", 1, narrow)
	got := domain.PublicCapabilities(class, []domain.DiskTypeBinding{one, diverging})
	require.False(t, got.MultiAttach, "способность, отсутствующая в одной зоне, не объявляется")
	require.False(t, got.EncryptionAtRest)
	require.True(t, got.Snapshots, "положительный контроль: общая способность остаётся объявленной")

	// Ноль ревизий — класс не объявляет ничего. Пустой каталог законен: пока
	// привязки нет, обещать нечего.
	require.Equal(t, domain.Capabilities{}, domain.PublicCapabilities(class, nil))

	// Вытесненная ревизия в выводе НЕ участвует: она описывает то, что обещали
	// прежним ресурсам, а не то, что предлагается сейчас.
	stale := bindingWith(class, "ru-central1-a", 1, full).Supersede()
	require.Equal(t, domain.Capabilities{},
		domain.PublicCapabilities(class, []domain.DiskTypeBinding{stale}),
		"вытесненная ревизия ничего не объявляет")
	require.False(t,
		domain.PublicCapabilities(class, []domain.DiskTypeBinding{diverging, stale}).MultiAttach,
		"вытесненная ревизия не вправе РАСШИРИТЬ объявленное действующей")

	// Ревизия чужого класса не участвует вовсе — иначе выборка, отданная страницей
	// на несколько классов, молча смешала бы обещания разных классов.
	foreign := bindingWith("block-fast", "ru-central1-a", 1, domain.BindingCapabilities{})
	require.Equal(t,
		domain.PublicCapabilities(class, []domain.DiskTypeBinding{one}),
		domain.PublicCapabilities(class, []domain.DiskTypeBinding{one, foreign}),
		"ревизия другого класса не сужает ответ этого")
}

// assertNoReferenceFields — рекурсивная проверка предпосылки гейта неизменяемости.
func assertNoReferenceFields(t *testing.T, typ reflect.Type, path string) {
	t.Helper()
	for i := range typ.NumField() {
		f := typ.Field(i)
		switch f.Type.Kind() {
		case reflect.Slice, reflect.Map, reflect.Pointer, reflect.Chan, reflect.Func, reflect.Interface:
			t.Fatalf("%s.%s — поле ссылочного типа %s: равенство наборов методов "+
				"перестаёт означать неизменяемость (приёмник-значение правит содержимое на месте)",
				path, f.Name, f.Type)
		case reflect.Struct:
			if f.Type.PkgPath() == typ.PkgPath() {
				assertNoReferenceFields(t, f.Type, path+"."+f.Name)
			}
		default:
		}
	}
}

func bindingWith(diskTypeID, zoneID string, revision int32, caps domain.BindingCapabilities) domain.DiskTypeBinding {
	b := newValidBinding()
	b.DiskTypeID = diskTypeID
	b.ZoneID = zoneID
	b.Revision = revision
	b.Capabilities = caps
	return b
}

func newValidBinding() domain.DiskTypeBinding {
	return domain.DiskTypeBinding{
		ID:         "dtb-2n5q8s1v4x7z0b3ef",
		DiskTypeID: "block-balanced",
		ZoneID:     "ru-central1-a",
		BackendID:  "sb-7k2m9p4r1t8w3y6zb",
		Revision:   1,
		Locator: domain.BindingLocator{
			Pool:              "kacho-block-balanced",
			NamespaceTemplate: "prj-{projectId}",
		},
		Capabilities: domain.BindingCapabilities{
			Snapshots: true, CloneFromSnapshot: true, CloneFromImage: true,
			CloneKeepsParent: true, OnlineGrow: true, EncryptionAtRest: true,
			TrashTTLSeconds: 86400,
		},
		QoS: domain.BindingQoS{
			BaselineIOPS: 3000, IOPSPerGiB: 30, MaxIOPS: 80000,
			BaselineThroughputMiBps: 125, ThroughputPerGiBMiBps: 0.5, MaxThroughputMiBps: 1000,
		},
		Status: domain.BindingStatusActive,
	}
}
