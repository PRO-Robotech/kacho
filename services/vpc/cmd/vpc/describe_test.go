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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/authz/catalogderive"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/pkg/outbox/bootgate"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/check"
	vpcpg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

// probeExistence — порт сверки существования для проб. Реальный живёт на пуле, а
// пула здесь нет: предмет — что порт ПРИНЕСЁН и что ось скрытия его требует, а не
// что он отвечает.
type probeExistence struct{}

func (probeExistence) ObjectExists(_ context.Context, _, _ string) (bool, error) { return false, nil }

// ProbeableTypes — охват ДЕЛЕГИРУЕТСЯ настоящей пробе сервиса.
//
// Подделка не вправе быть снисходительнее продукта: объяви она свой перечень —
// и сверка охвата на старте (`servicehost`, О5в) судила бы фикстуру вместо
// пробы, то есть молчала бы ровно там, где таблица настоящей разошлась с картой
// прав сервиса (задача продукта #1931).
func (probeExistence) ProbeableTypes() []string { return (&vpcpg.ExistenceProbe{}).ProbeableTypes() }

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
	// Величины подписки — те же, что ставит объявление умолчаний. Ноль здесь не
	// умолчание, а «не объявлено»: общий сервер такую посадку отвергает на
	// подъёме, и проба судила бы отказ конструктора вместо своего предмета.
	c.APIServer.SubscriptionStreamBudget = time.Hour
	c.APIServer.SubscriptionMaxStreams = 16
	c.APIServer.SubscriptionIdlePoll = 2 * time.Second
	c.Repository.Postgres.URL = "postgres://u@h:5432/db"
	c.Repository.Postgres.SSLMode = "require"
	c.AuthZ.IAMEndpoint = "kaname-internal:9091"
	c.AuthZ.CacheTTL = 5 * time.Second
	c.AuthZ.CheckTimeout = 2 * time.Second
	c.AuthZ.DenyRateLimitPerSec = 100
	c.AuthZ.TrustedForwarderSANs = []string{"spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"}
	// Домен доверия — величина установки, и конструктор дескриптора требует её
	// названной: процесс, не назвавший домена, своим не признаёт никого.
	c.AuthZ.TrustDomainName = "kacho.cloud"

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
		probeAuthzObserve, prometheus.NewRegistry())
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
				probeAuthzObserve, prometheus.NewRegistry())
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
		probeAuthzObserve, prometheus.NewRegistry())
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
		probeAuthzObserve, prometheus.NewRegistry())
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

// Срок жизни подписки объявлен ВЕЛИЧИНОЙ, потому что поток служится.
//
// Проба — GREEN-половина пары, чья RED-половина сработала сама: ось стояла
// изъятием («серверных потоков не служит»), и появление подписки сделало
// предпосылку ложной. Изъятие истекло ОТ ПОЯВЛЕНИЯ ПРЕДМЕТА, назвав метод
// поимённо, — ровно так, как обещало.
//
// Утверждается ТРОЙКА, и ни одно из трёх не выводится из другого:
//
//	(а) ось несёт величину, а не изъятие — иначе дескриптор молчал бы о сроке
//	    потока, который служит;
//	(б) величина заметно превосходит границу обработки одиночного вызова —
//	    поток, закрывшийся раньше первого события догона, читался бы подписчиком
//	    как «изменений нет»;
//	(в) служимых потоков РОВНО ОДИН и это ИМЕННО подписка — перепись идёт по
//	    ТЕМ ЖЕ регистраторам, что поднимают процесс, поэтому второй поток,
//	    заведённый мимо этой оси, здесь покраснеет.
//
// «Ноль потоков» обязано быть отличимо от «ноль прочитанных дескрипторов»,
// поэтому перепись называет и число осмотренных методов.
func TestStreamBudgetIsAValueBecauseTheSubscriptionStreamIsServed(t *testing.T) {
	cfg, mtls := describeCfg(t)
	desc, err := describe(cfg, mtls, discardLogger(), buildListFilter(cfg, nil, discardLogger()),
		bootgate.New(bootgate.Config{RequireIAM: true, Service: "kacho-vpc"}), probeExistence{},
		probeAuthzObserve, prometheus.NewRegistry())
	require.NoError(t, err)

	budget, hasValue := desc.Spec().StreamBudget.Get()
	require.True(t, hasValue,
		"поток служится, а ось объявлена изъятием: дескриптор молчит о сроке жизни подписки")
	assert.Equal(t, cfg.APIServer.SubscriptionStreamBudget, budget,
		"величина обязана приезжать из объявления сервиса, а не зашиваться второй раз")
	assert.Greater(t, budget, cfg.APIServer.RequestTimeout,
		"срок потока не превосходит границы обработки одиночного вызова: поток закрылся бы "+
			"раньше, чем доезжает первое событие догона, и подписчик прочёл бы штатное "+
			"закрытие как «изменений нет»")

	_, na := desc.Spec().StreamBudget.NotApplicableBecause()
	assert.False(t, na, "изъятие при служимом потоке — заявление, ложное по построению")

	// Предпосылка величины — переписью по СЛУЖИМОМУ набору, и набор берётся у ТЕХ
	// ЖЕ регистраторов, что поднимают процесс: своя копия перечня разошлась бы с
	// боевой проводкой молча.
	svcs := emptyServices()
	subscribe, serr := buildSubscriptionServer(cfg, narrowtest.AllowingAll(), discardLogger())
	require.NoError(t, serr, "сервер потока не собрался — перепись судила бы неполный набор")
	probe := grpc.NewServer()
	registerPublicServices(probe, svcs, nil)
	registerInternalServices(probe, svcs, subscribe)

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
	assert.Equal(t, []string{"kacho.cloud.subscription.InternalSubscriptionService/Subscribe"}, streaming,
		"служимые потоки разошлись с тем, ради чего объявлена величина "+
			"(осмотрено методов: %d)", methods)
	assert.Equal(t, 1, streams, "потоков служится %d: %v", streams, streaming)
}

// Проводка сужателя — на КАЖДЫЙ метод, авторизуемый на уровне данных, и их два.
//
// Имя собирается из дескриптора службы, поэтому переименование службы ломает
// сборку, а не расходится молча. Совпадение с каталогом сверяет носитель в обе
// стороны (О3/О4); здесь — что проводка полна и что это ТОТ ЖЕ объект, который
// сужает страницу в use-case'ах.
//
// # Почему методов ДВА, а собственный у vpc по-прежнему один
//
// `check.ScopeFilteredRPCs()` читает СВОЙ контракт домена, и там сужаемый метод
// один — списочный. Второй приходит из ОБЩЕГО контракта подписки: глагол
// объявлен `scope_filtered` однажды на всю платформу, и каждый владелец журнала
// монтирует ЕГО ЖЕ. Поэтому перечень домена остаётся единичным, а проводка —
// двойной, и путать эти два числа нельзя.
//
// # Почему сужатель ОДИН объект на оба
//
// Видимость в потоке обязана быть равна видимости в списке. Два разных сужателя
// об одном предмете разошлись бы молча — и разошлись бы именно там, где
// расхождение означает выдачу строки тому, кто не вправе её видеть.
func TestNarrowerIsWiredToEveryScopeFilteredMethod(t *testing.T) {
	own := check.ScopeFilteredRPCs()
	require.Len(t, own, 1, "у СОБСТВЕННОГО контракта vpc ровно один метод, авторизуемый на уровне данных")
	require.Equal(t, string(listByInstanceMethod), own[0])

	cfg, mtls := describeCfg(t)
	narrower := buildListFilter(cfg, nil, discardLogger())
	desc, err := describe(cfg, mtls, discardLogger(), narrower,
		bootgate.New(bootgate.Config{RequireIAM: true, Service: "kacho-vpc"}), probeExistence{},
		probeAuthzObserve, prometheus.NewRegistry())
	require.NoError(t, err)

	wired, ok := desc.Spec().Narrowers.Get()
	require.True(t, ok)
	require.Len(t, wired, 2,
		"проводка обязана покрывать и свой списочный метод, и общий глагол подписки: "+
			"за сужаемым методом пообъектной проверки на крае нет вовсе, поэтому "+
			"непровязанный означает не «строже», а «без рубежа»")
	assert.Same(t, narrower, wired[listByInstanceMethod],
		"дескриптор обязан объявлять ТОТ ЖЕ объект, что сужает страницу на пути запроса")
	assert.Same(t, narrower, wired[subscriptionSubscribeFQN],
		"поток обязан сужаться ТЕМ ЖЕ объектом, что и список: иначе видимость в потоке "+
			"расходится с видимостью в списке — молча")
}

// probeAuthzObserve — приёмник величин кеша вердиктов для проб КОНСТРУКТОРА.
//
// Заглушка здесь законна: предмет этих проб — что судит конструктор дескриптора,
// а не куда уезжают величины. Настоящий приёмник, чей вызов носителем
// утверждается, стоит в пробе подъёма (`carrier_start_test.go`): там его пропажа
// красит пробу, здесь — не может по построению.
func probeAuthzObserve(func() authz.Metrics) {}
