// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// describe_test.go — что утверждает ОБЪЯВЛЕНИЕ vpc о себе.
//
// Предмет здесь — КОНСТРУКТОР дескриптора: он судит поля по себе (объявлена ли
// ось, положительна ли величина, сходится ли форма отказа с производителем).
// Половина отказов носителя так не проверяется — им нужен СЛУЖИМЫЙ набор,
// снятый у самих серверов после регистрации; их закрывает `carrier_start_test.go`,
// и без него дескриптор мог бы нести ложное заявление (ровно так у двух соседей
// проходило изъятие «скрывать нечего» при рантайме, который скрывает).

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/authz/catalogderive"
	"github.com/PRO-Robotech/kacho/pkg/outbox/bootgate"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/check"
)

// probeExistence — порт сверки существования для проб. Реальный живёт на пуле, а
// пула здесь нет: предмет — что порт ПРИНЕСЁН и что ось скрытия его требует, а не
// что он отвечает.
type probeExistence struct{}

func (probeExistence) ObjectExists(_ context.Context, _, _ string) (bool, error) { return false, nil }

// describeCfg — посадка vpc в минимально-полной форме: всё, что судит конструктор
// дескриптора, задано, и ничего сверх.
//
// Режим — dev, и это выбор, а не упрощение: боевая посадка требует ПРОВЕРЕННОГО
// транспорта на обоих слушателях и на ребре решения о доступе, то есть настоящей
// тройки сертификатов на диске. Что боевой режим их требует, проверяет сам
// конструктор дескриптора (`pkg/servicecontract`, отказ О8) — на своих фикстурах
// и без чтения файлов. Здесь предмет другой: что ОБЪЯВЛЯЕТ о себе vpc. Заводить
// ради этого генерацию сертификатов значило бы сделать пробу заложницей файловой
// системы и повторить чужую проверку.
func describeCfg(t *testing.T) (config.Config, config.MTLSConfig) {
	t.Helper()
	var c config.Config
	c.AuthN.Mode = config.ModeDev
	c.APIServer.Endpoint = "tcp://0.0.0.0:9090"
	c.APIServer.InternalEndpoint = "tcp://0.0.0.0:9091"
	c.APIServer.RequestTimeout = 30 * time.Second
	c.Repository.Postgres.URL = "postgres://u@h:5432/db"
	c.Repository.Postgres.SSLMode = "require"
	c.AuthZ.IAMEndpoint = "kacho-iam-internal:9091"
	c.AuthZ.CacheTTL = 5 * time.Second
	c.AuthZ.CheckTimeout = 2 * time.Second
	c.AuthZ.DenyRateLimitPerSec = 100
	c.AuthZ.TrustedForwarderSANs = []string{"spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"}

	var m config.MTLSConfig
	return c, m
}

// Дескриптор принимается конструктором.
//
// Положительный контроль ко всем отрицаниям ниже: без него они зеленели бы и от
// «конструктор отвергает вообще всё».
func TestDescriptorOfCompletePostureIsAccepted(t *testing.T) {
	cfg, mtls := describeCfg(t)
	desc, err := describe(cfg, mtls, discardLogger(), buildListFilter(cfg, nil, discardLogger()),
		bootgate.New(bootgate.Config{RequireIAM: true, Service: "kacho-vpc"}), probeExistence{},
		probeAuthzObserve)
	require.NoError(t, err, "полная посадка обязана приниматься — иначе процесс не поднялся бы")
	require.True(t, desc.Accepted())
}

// Ребро решения о доступе объявляется ЯВНО, и незаданный адрес — отказ на ЛЮБОЙ
// посадке, а не только в боевой.
//
// Это строже снятой проводки: прежде пустой адрес означал «в dev поднимаемся без
// проверки прав, в production — фатально», то есть исход зависел от режима.
// Теперь ребро есть свойство дескриптора, и режим на него не влияет.
func TestUndeclaredDecisionEdgeRefusesInEveryPosture(t *testing.T) {
	for _, mode := range []config.Mode{config.ModeDev, config.ModeProduction, config.ModeProductionStrict} {
		t.Run(mode.String(), func(t *testing.T) {
			cfg, mtls := describeCfg(t)
			cfg.AuthN.Mode = mode
			cfg.AuthZ.IAMEndpoint = ""
			_, err := describe(cfg, mtls, discardLogger(), buildListFilter(cfg, nil, discardLogger()),
				bootgate.New(bootgate.Config{RequireIAM: true, Service: "kacho-vpc"}), probeExistence{},
				probeAuthzObserve)
			require.Error(t, err, "ребро решения о доступе не объявлено — процесс не поднимается")
			assert.Contains(t, err.Error(), "CheckEdge")
		})
	}
}

// Верхняя граница обработки вызова не имеет умолчания и не имеет «не применимо».
//
// Величина переехала со снятого звена vpc, и переехать молча она не может: у
// снятого была ветка «не задано → пропускаем», и ровно в ней граница исчезала.
func TestHandlingBudgetHasNoDefaultAndNoExemption(t *testing.T) {
	cfg, mtls := describeCfg(t)
	cfg.APIServer.RequestTimeout = 0
	_, err := describe(cfg, mtls, discardLogger(), buildListFilter(cfg, nil, discardLogger()),
		bootgate.New(bootgate.Config{RequireIAM: true, Service: "kacho-vpc"}), probeExistence{},
		probeAuthzObserve)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HandlingBudget")
}

// Величина верхней границы обработки — ТА ЖЕ, что читала снятая проводка.
//
// vpc был единственным, кто эту границу имел, и он же единственный, кто мог её
// потерять при переезде: «граница есть» и «граница та же» — разные утверждения, и
// второе не следует из первого. Проба связывает поле дескриптора с ручкой
// конфигурации напрямую, поэтому подмена величины (умолчанием библиотеки, чужой
// ручкой, круглым числом «на глаз») краснеет здесь, а не наблюдается на стенде.
func TestHandlingBudgetIsTheSameQuantityTheRetiredLinkRead(t *testing.T) {
	cfg, mtls := describeCfg(t)
	cfg.APIServer.RequestTimeout = 17123 * time.Millisecond // заведомо не круглое
	desc, err := describe(cfg, mtls, discardLogger(), buildListFilter(cfg, nil, discardLogger()),
		bootgate.New(bootgate.Config{RequireIAM: true, Service: "kacho-vpc"}), probeExistence{},
		probeAuthzObserve)
	require.NoError(t, err)
	assert.Equal(t, cfg.APIServer.RequestTimeout, desc.Spec().HandlingBudget,
		"граница обработки обязана быть ровно api-server.request-timeout, а не производной от неё")
}

// Формы отказа взяты у ПРОИЗВОДИТЕЛЯ и покрывают КАЖДЫЙ пообъектный тип карты.
//
// Два утверждения сразу, и оба нужны: (1) множество совпадает с выведенным из
// карты прав — значит новый ресурс попадает под скрытие в момент появления, а не
// когда кто-нибудь вспомнит дополнить список; (2) каждая форма дословно равна
// той, которой отвечает звено решения о доступе, — выписанная копия разошлась бы
// с ним молча, и отказ стал бы отличим от настоящего промаха владельца.
func TestHideExistenceFormsCoverEveryObjectScopedTypeAndComeFromTheProducer(t *testing.T) {
	scoped := catalogderive.ObjectScopedTypes(check.PermissionMap())
	require.NotEmpty(t, scoped, "«ноль форм» обязано отличаться от «ноль прочитанного»")

	forms := hideExistenceForms()
	require.Len(t, forms, len(scoped),
		"перечень скрывающих типов обязан совпадать с пообъектными типами карты прав")

	for ot := range scoped {
		form, ok := forms[servicecontract.ObjectType(ot)]
		require.Truef(t, ok, "у пообъектного типа %s нет объявленной формы отказа", ot)
		owner, known := authz.OwnerNotFoundFormat(ot)
		require.Truef(t, known, "у типа %s нет голоса владельца в таблице промахов", ot)
		assert.Equal(t, owner, string(form), "форма обязана быть дословно той, которой отвечает звено")
		assert.Equal(t, 1, strings.Count(string(form), "%s"),
			"форма несёт ровно одну подстановку идентификатора — иначе она отличима от промаха владельца")
	}
	t.Logf("перепись: пообъектных типов %d, объявленных форм %d", len(scoped), len(forms))
}

// Иерархические якоря в скрывающие НЕ затягиваются.
//
// Вывод может ошибиться в обе стороны, и вторая тише: попади сюда `project` или
// `cluster`, отказ на СПИСКЕ начал бы отвечать «не найдено» вместо «нет доступа»,
// сообщая о коллекции то, чего о ней и не скрывают. Проба унаследована от снятого
// адаптера решателя (`internal/check`), где тот же вывод жил рядом с ним.
func TestHiddenTypesAreResourcesNotHierarchyAnchors(t *testing.T) {
	want := []string{
		"vpc_address", "vpc_cidr_group", "vpc_gateway", "vpc_network",
		"vpc_network_interface", "vpc_route_table", "vpc_security_group", "vpc_subnet",
	}
	assert.Equal(t, want, hiddenObjectTypes(),
		"скрывающие типы — ресурсы домена, и только они")
}

// Срок жизни подписки объявлен ИЗЪЯТИЕМ: серверных потоков vpc не служит.
//
// Ось меняла форму дважды, и оба раза — от появления и исчезновения ОДНОГО
// предмета. Изъятие стояло здесь изначально; истекло, когда завели поток
// намерения исполнителю датаплейна; вернулось, когда весь шов сняли целиком
// вместе с этим потоком (kacho#400) — исполнителя не существует.
//
// Проверяется ровно то, что делает изъятие честным: (а) величины НЕТ — иначе
// дескриптор утверждал бы срок подписки при нулевом наборе подписок; (б) причина
// НЕПУСТА — пустая причина объявлением не является, и конструктор её отвергает;
// (в) служимый набор действительно не несёт серверных потоков — это и есть
// предпосылка изъятия, и судится она набором, а не памятью автора (О11).
//
// Появится поток снова — предпосылка станет ложной, и проба покраснеет ЗДЕСЬ, а
// не на подъёме стенда.
func TestStreamBudgetIsExemptBecauseNoServerStreamIsServed(t *testing.T) {
	cfg, mtls := describeCfg(t)
	desc, err := describe(cfg, mtls, discardLogger(), buildListFilter(cfg, nil, discardLogger()),
		bootgate.New(bootgate.Config{RequireIAM: true, Service: "kacho-vpc"}), probeExistence{},
		probeAuthzObserve)
	require.NoError(t, err)

	_, hasValue := desc.Spec().StreamBudget.Get()
	assert.False(t, hasValue,
		"величина при отсутствии служимых потоков — срок для подписки, которой нет")

	because, na := desc.Spec().StreamBudget.NotApplicableBecause()
	require.True(t, na, "ось обязана быть объявлена изъятием")
	assert.NotEmpty(t, because, "изъятие без причины объявлением не является")

	// Предпосылка изъятия — переписью по СЛУЖИМОМУ набору, а не утверждением о
	// нём, и набор берётся у ТЕХ ЖЕ регистраторов, что поднимают процесс: своя
	// копия перечня разошлась бы с боевой проводкой молча.
	//
	// «Ноль потоков» обязано быть отличимо от «ноль прочитанных дескрипторов»,
	// поэтому перепись называет и число осмотренных методов.
	svcs := emptyServices()
	probe := grpc.NewServer()
	registerPublicServices(probe, svcs, nil)
	registerInternalServices(probe, svcs)

	var streams, methods int
	var streaming []string
	for name, info := range probe.GetServiceInfo() {
		for _, m := range info.Methods {
			methods++
			if m.IsServerStream || m.IsClientStream {
				streams++
				streaming = append(streaming, name+"/"+m.Name)
			}
		}
	}
	require.Positive(t, methods, "перепись прочла ноль методов — судить не о чем")
	assert.Zero(t, streams,
		"при объявленном изъятии служится потоков: %v. Заявление ложно — "+
			"величину надо вернуть осознанно (осмотрено методов: %d)", streaming, methods)
}

// Проводка сужателя — ровно на тот метод, который каталог объявляет сужаемым.
//
// Имя собирается из дескриптора службы, поэтому переименование службы ломает
// сборку, а не расходится молча. Совпадение с каталогом сверяет носитель в обе
// стороны (О3/О4); здесь — что проводка одна и что это ТОТ ЖЕ объект, который
// сужает страницу в use-case'ах.
func TestNarrowerIsWiredToTheOneScopeFilteredMethod(t *testing.T) {
	scopeFiltered := check.ScopeFilteredRPCs()
	require.Len(t, scopeFiltered, 1, "у vpc ровно один метод, авторизуемый на уровне данных")
	require.Equal(t, string(listByInstanceMethod), scopeFiltered[0])

	cfg, mtls := describeCfg(t)
	narrower := buildListFilter(cfg, nil, discardLogger())
	desc, err := describe(cfg, mtls, discardLogger(), narrower,
		bootgate.New(bootgate.Config{RequireIAM: true, Service: "kacho-vpc"}), probeExistence{},
		probeAuthzObserve)
	require.NoError(t, err)

	wired, ok := desc.Spec().Narrowers.Get()
	require.True(t, ok)
	require.Len(t, wired, 1)
	assert.Same(t, narrower, wired[listByInstanceMethod],
		"дескриптор обязан объявлять ТОТ ЖЕ объект, что сужает страницу на пути запроса")
}

// probeAuthzObserve — приёмник величин кеша вердиктов для проб КОНСТРУКТОРА.
//
// Заглушка здесь законна: предмет этих проб — что судит конструктор дескриптора,
// а не куда уезжают величины. Настоящий приёмник, чей вызов носителем
// утверждается, стоит в пробе подъёма (`carrier_start_test.go`): там его пропажа
// красит пробу, здесь — не может по построению.
func probeAuthzObserve(func() authz.Metrics) {}
