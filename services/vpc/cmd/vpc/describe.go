// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// describe.go — ОБЪЯВЛЕНИЕ kacho-vpc о себе для носителя контура
// (`pkg/servicehost`).
//
// # Что этот файл заменил
//
// До перевода композиционный корень собирал оба слушателя сам: две пары цепочек
// (`publicUnary`/`publicStream`/`internalUnary`/`internalStream`), свой
// конструктор звена решения о доступе, своя копия предиката гейтируемой мутации,
// своё звено верхней границы обработки. Порядок держался тем, что автор написал
// то же, что сосед, — и у vpc уже расходился: журнала доступа не было ни на
// одной полосе, а восстановление паники стояло САМЫМ ВНЕШНИМ, то есть исход
// паниковавшего вызова не видел никто.
//
// Здесь сервис приносит ЗНАЧЕНИЯ, и ни одного поля интерсепторного типа: цепочку
// невозможно принести, поэтому её невозможно собрать по-своему.

import (
	"fmt"
	"log/slog"
	"sort"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/authz/authziam"
	"github.com/PRO-Robotech/kacho/pkg/authz/catalogderive"
	"github.com/PRO-Robotech/kacho/pkg/authz/proxytuple"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/outbox/bootgate"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/check"
)

// hiddenObjectTypes — типы vpc, существование которых скрывает РАНТАЙМ.
//
// Перечень не выписан: он выводится из карты прав тем же вызовом, каким его
// получает носитель, надевая сверку существования на решателя
// (`catalogderive.ObjectScopedTypes`). Выписанный список был бы тем местом,
// которое забывают дополнить: новый ресурс приезжает в карту своей аннотацией, а
// в списке его нет — и отказ по нему молча перестал бы скрывать существование.
//
// Почему это НЕ «в каталоге нет строк hide_existence, значит скрывать нечего»:
// рантайм скрывает по `authz.HidesExistenceOnDeny` — по аннотации ИЛИ ПО ФОРМЕ
// (чтение объекта глаголом `v_get` у типа, за который есть чем говорить в
// таблице промахов владельцев). Аннотаций у vpc ноль, а скрывает он на семи
// `/Get`; ровно то же заявление стояло у storage и у nlb и оба раза падало
// тремя находками, как только судья и рантайм стали читать один предикат.
func hiddenObjectTypes() []string {
	scoped := catalogderive.ObjectScopedTypes(check.PermissionMap())
	out := make([]string, 0, len(scoped))
	for ot := range scoped {
		out = append(out, ot)
	}
	// Порядок детерминирован: перечень уезжает в дескриптор и в текст отказа, а
	// недетерминированный порядок делает несравнимыми два прогона одного старта.
	sort.Strings(out)
	return out
}

// hideExistenceForms — формы отказа для типов vpc, которые скрывает рантайм.
//
// Взяты у ПРОИЗВОДИТЕЛЯ (`authz.OwnerNotFoundFormat`), а не выписаны: носитель
// сверяет объявленное с тем, чем звено решения о доступе реально отвечает, и
// расхождение роняет старт. Собственная копия формы прошла бы конструктор и
// упала бы на подъёме — то есть позже и дороже.
//
// Отсутствие типа в таблице здесь — паника композиционного корня, а не пропуск:
// значит перечень пообъектных типов разошёлся с таблицей промахов владельцев, и
// поднимать процесс с неполной картой скрытия нельзя.
func hideExistenceForms() map[servicecontract.ObjectType]servicecontract.NotFoundFormat {
	out := map[servicecontract.ObjectType]servicecontract.NotFoundFormat{}
	for _, ot := range hiddenObjectTypes() {
		form, ok := authz.OwnerNotFoundFormat(ot)
		if !ok {
			panic("vpc: у типа " + ot + " нет голоса владельца в таблице промахов — " +
				"перечень пообъектных типов разошёлся с pkg/authz")
		}
		out[servicecontract.ObjectType(ot)] = servicecontract.NotFoundFormat(form)
	}
	return out
}

// registeredObjectTypes — типы объектов модели прав, которые vpc регистрирует у
// владельца прав намерением из своей writer-транзакции.
//
// Источник тот же, что у скрытия: пообъектные типы карты прав. Совпадение не
// случайно и не подгонка — регистрируется ровно то, что адресуется пообъектно:
// на каждый такой ресурс пишется иерархическая привязка к проекту, и она же
// делает его видимым пообъектной проверке.
func registeredObjectTypes() []servicecontract.ObjectType {
	types := hiddenObjectTypes()
	out := make([]servicecontract.ObjectType, 0, len(types))
	for _, ot := range types {
		out = append(out, servicecontract.ObjectType(ot))
	}
	return out
}

// listByInstanceMethod — единственный метод vpc, чья авторизация переехала на
// уровень ДАННЫХ (`scope_filtered` в каталоге прав): инстансы называет
// вызывающий, а ответ касается интерфейсов с разными владельцами, поэтому
// единичного объекта, про который можно спросить заранее, у него нет by
// construction.
//
// Имя собирается из дескриптора службы, а не выписывается строкой: переименуют
// службу — не соберётся, а не разойдётся молча. Перечня сужаемых методов
// дескриптор не объявляет: его даёт каталог, и носитель сверяет проводку с ним в
// ОБЕ стороны (О3/О4) — потерянная проводка и лишняя одинаково роняют старт.
// subscriptionSubscribeFQN — полное имя ОБЩЕГО глагола подписки.
//
// Оно записано строкой, а не выведено из сгенерённого дескриптора служб: носитель
// сверяет проводку с КАТАЛОГОМ ПРАВ, где ключ — та же строка, и вывод её из
// другого источника сделал бы сверку тождественно истинной при расхождении.
const subscriptionSubscribeFQN servicecontract.MethodFQN = "/kacho.cloud.subscription.InternalSubscriptionService/Subscribe"

var listByInstanceMethod = servicecontract.MethodFQN(
	"/" + vpcv1.InternalNetworkInterfaceService_ServiceDesc.ServiceName + "/ListByInstance")

// describe собирает ОБЪЯВЛЕНИЕ сервиса о себе.
//
// Стражей старта здесь нет ни одного — они живут в конструкторе дескриптора и в
// носителе. Задача функции: назвать величины и перевести конфигурацию vpc в общий
// словарь, ничего не досочиняя.
//
// Сужатель и загрузочный гейт передаются АРГУМЕНТАМИ, а не собираются здесь
// заново: проводка, объявленная дескриптором, обязана быть ТЕМ ЖЕ объектом, что
// сужает страницу в use-case'ах и чью готовность читает проба готовности пода.
// Собери их здесь второй раз — носитель сверял бы с каталогом экземпляр, которого
// на пути запроса нет, и «проводка есть» перестало бы означать «страница
// сужается».
func describe(
	cfg config.Config,
	mtlsCfg config.MTLSConfig,
	logger *slog.Logger,
	narrower *authzfilter.Narrower,
	gate *bootgate.Gate,
	existence servicecontract.ExistenceProbe,
	authzObserve func(read func() authz.Metrics),
	metricsReg prometheus.Registerer,
) (servicecontract.Descriptor, error) {
	mode, err := hostMode(cfg.AuthN.Mode)
	if err != nil {
		return servicecontract.Descriptor{}, err
	}

	// Транспорт слушателей и ребра решения о доступе строится ЗДЕСЬ, а судится
	// конструктором дескриптора — по ответу САМОГО ТРАНСПОРТА, а не по ручке:
	// сборщик креденшелов на невзведённой ручке отдаёт незашифрованные креды БЕЗ
	// ошибки, поэтому «ручка выглядит как угодно» этой проверке безразлично.
	publicCreds, err := grpcsrv.TLSServerTransportCreds(mtlsCfg.PublicServerMTLS)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("public listener tls creds: %w", err)
	}
	internalCreds, err := grpcsrv.TLSServerTransportCreds(mtlsCfg.InternalServerMTLS)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("internal listener tls creds: %w", err)
	}
	// Ребро решения о доступе идёт на ВНУТРЕННИЙ листенер kaname (:9091): там
	// живёт InternalIAMService.Check. Ручка его транспорта — та же
	// `IAM_AUTHZ_MTLS`, которой раньше дилился общий authz-conn, поэтому
	// дескриптор и проводка объявляют ОДНО ребро, а не два похожих.
	checkCreds, err := grpcclient.TLSClientTransportCreds(mtlsCfg.IAMAuthzMTLS)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("vpc→iam Check mTLS creds: %w", err)
	}

	// Потолок темпа и одновременности НА ВЫЗЫВАЮЩЕГО. Ручки читаются ОДНИМ
	// входом (Config.AdmissionKnobs), а необъявленность боевой посадки роняет
	// старт РАНЬШЕ — в Config.ValidateRequestRateLimits, где отказ называет
	// ручку. Здесь остаётся то, чего страж не решает: чем ограничен слушатель,
	// когда посадка молчит законно (вне боевого режима) — полом платформы.
	admissionPublic, admissionInternal := cfg.AdmissionKnobs()
	admission, err := servicecontract.AdmissionFromPosture(admissionPublic, admissionInternal)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("api-server.rate-limit: %w", err)
	}

	return servicecontract.New(servicecontract.Spec{
		Service: "kacho-vpc",
		Mode:    mode,
		Logger:  logger,

		Forwarders: servicecontract.Value(cfg.TrustedForwarders()),
		ForwarderKnobs: servicecontract.ForwarderKnobs{
			SANs:     "authz.trusted-forwarder-sans (env KACHO_VPC_AUTHZ__TRUSTED_FORWARDER_SANS)",
			TrustAny: "authz.trust-any-forwarder (env KACHO_VPC_AUTHZ__TRUST_ANY_FORWARDER)",
			OptIn:    cfg.AuthZ.TrustAnyForwarder,
		},

		// Домен доверия — ЗНАЧЕНИЕ: личность клиентского сертификата этот процесс
		// разбирает, и разбирает её ОТНОСИТЕЛЬНО домена. Читается из той же
		// функции, значение которой уезжает в пару звеньев извлечения личности,
		// поэтому «страж пропустил» ⟺ «домен реально объявлен».
		TrustDomain:     servicecontract.Value(cfg.TrustDomain()),
		TrustDomainKnob: "authz.trust-domain (env KACHO_VPC_AUTHZ__TRUST_DOMAIN)",

		Authz:     servicecontract.AuthzViaIAM,
		CheckEdge: servicecontract.NewPeerEdge(cfg.AuthZ.IAMEndpoint, checkCreds),
		// Перевод вопроса в контракт службы доступа приносит СЕРВИС: носитель
		// принадлежит фундаменту и чужого контракта не знает (приёмка K3-1,
		// раздел 7.2).
		PeerCheck:    authziam.NewCheckClient,
		CacheWindow:  cfg.AuthZ.CacheTTL,
		ClientBudget: cfg.AuthZ.CheckTimeout,

		// Приёмник величин кеша вердиктов: носитель строит кеш, а
		// диагностическую поверхность держит этот корень, и величины переходят
		// границу только здесь. Без него доля попаданий не выходит из процесса,
		// и «сколько даёт кеш» остаётся непроверяемым в обе стороны.
		AuthzObserve: authzObserve,

		// Реестр отдаёт композиционный корень, а серии задержки заводит носитель
		// своими руками: диагностическую поверхность держит корень, и другого
		// пути к ней у носителя нет. Разбор — у `servicecontract.Spec.Metrics`.
		Metrics: metricsReg,

		// Верхняя граница обработки ОДНОГО вызова. Величина — та же
		// `api-server.request-timeout`, что несло снятое звено vpc
		// (`internal/handler.UnaryTimeoutInterceptor`), и переезжает она без
		// изменения: обоснование живёт у ручки конфигурации, а не здесь.
		//
		// «Не применимо» у этой оси нет: вызов без срока держит соединение из
		// ограниченного пула столько, сколько выполняется его запрос, и MaxConns
		// таких вызовов отказывают весь сервис. Снятое звено при этом несло ветку
		// `timeout<=0 → passthrough` — ровно то место, где граница исчезала молча;
		// у носителя такой ветки нет, а неположительную величину отвергает
		// конструктор дескриптора.
		HandlingBudget: cfg.APIServer.RequestTimeout,

		// Срок жизни подписки — ВЕЛИЧИНА: vpc служит серверный поток.
		//
		// Изъятие здесь стояло дважды и дважды истекало ОТ ПОЯВЛЕНИЯ ПРЕДМЕТА.
		// Первый раз предметом был поток намерения исполнителю датаплейна; он был
		// снят целиком (kacho#400), и изъятие вернулось. Теперь предмет появился
		// снова и уже не уйдёт: vpc служит ПОДПИСКУ на изменения своих ресурсов
		// (`kacho.cloud.subscription.InternalSubscriptionService/Subscribe`,
		// kacho#1023) на внутреннем слушателе.
		//
		// Величина возвращается ОСОЗНАННО, как того и требовало изъятие: оно
		// объявляло, что при появлении потока старт откажет и назовёт метод — так
		// и произошло, проба переписи по СЛУЖИМОМУ набору назвала его поимённо.
		// Заявление судится набором, а не памятью автора (О11).
		//
		// Срок обязан заметно превосходить границу обработки одиночного вызова:
		// поток, закрывшийся раньше, чем доезжает первое событие догона, читался
		// бы подписчиком как «изменений нет». Отношение судит носитель контура и
		// роняет старт на негодной величине — здесь оно не переписывается второй
		// раз.
		StreamBudget: servicecontract.Value(cfg.APIServer.SubscriptionStreamBudget),

		// Бюджет отказов — ВЕЛИЧИНА, а не изъятие: решение о доступе vpc принимает
		// не у себя, а вопросом к kaname, — то есть сетевой сосед, которого
		// шторм отказов может уронить, у него ЕСТЬ, и на том же соединении живут
		// пообъектный сужатель и регистрация владельца. Изъятие («ронять некого»)
		// законно только у владельца модели, решающего в своём процессе.
		DenyBudget: servicecontract.Value(cfg.AuthZ.DenyRateLimitPerSec),

		// Ось потолка объявляется ВЕЛИЧИНОЙ, а не изъятием: слушатели выставлены
		// наружу. Провязку делает носитель — до него она жила в композиционном
		// корне ЭТОГО сервиса и была единственной на всю платформу.
		Admission: servicecontract.Value(admission),

		// Режим шифрования до своей БД читается из ТОЙ строки, что уходит в пул
		// (`cfg.DSN()`): sslmode приезжает и из `repository.postgres.ssl-mode`, и
		// из самого сырого URL. Сырое поле показало бы намерение, а не факт.
		DBSSLMode:     servicecontract.Value(coredb.SSLModeFromDSN(cfg.DSN())),
		PublicAddr:    cfg.APIServer.ListenAddress(),
		InternalAddr:  cfg.APIServer.InternalListenAddress(),
		PublicCreds:   publicCreds,
		InternalCreds: internalCreds,

		// ── оси ─────────────────────────────────────────────────────────────────

		// Эмиссия. vpc пишет владельцу прав ровно ОДНО отношение — иерархическую
		// привязку ресурса к проекту. Имя берётся у ПРИНИМАЮЩЕЙ стороны
		// (`pkg/authz/proxytuple`), которая владеет закрытым набором принимаемых
		// отношений: второе написание чужого закрытого набора расходится молча, и
		// расходится там, где это не видно, — отказ в правах дренаж читает как
		// временный, и очередь встаёт головой партиции навсегда.
		Emits: servicecontract.Value([]proxytuple.Relation{proxytuple.RelationProject}),

		// Регистрируемые типы объектов — пообъектные типы карты прав (см.
		// registeredObjectTypes): именно на них пишется привязка к проекту.
		Registers: servicecontract.Value(registeredObjectTypes()),

		// Проводка сужателя — ровно на те методы, которые каталог объявляет
		// сужаемыми, и их ДВА.
		//
		// Второй — общий глагол подписки. Он объявлен `scope_filtered`, то есть
		// пообъектной проверки на крае за ним НЕТ вовсе: сужение делает сам
		// владелец на каждой строке журнала. Поэтому отсутствие проводки означало
		// бы не «строже», а «без рубежа», и носитель контура отказывает в старте —
		// он это и сделал, когда подписка была смонтирована, а проводка ещё нет.
		//
		// Объект ТОТ ЖЕ, что сужает списочную страницу: видимость в потоке обязана
		// быть равна видимости в списке, а два разных сужателя об одном предмете
		// разошлись бы молча.
		Narrowers: servicecontract.Value(map[servicecontract.MethodFQN]servicecontract.ListNarrower{
			listByInstanceMethod:     narrower,
			subscriptionSubscribeFQN: narrower,
		}),

		// Скрытие существования у vpc ЕСТЬ, хотя ни один его RPC не помечен
		// аннотацией: рантайм скрывает и ПО ФОРМЕ (см. hiddenObjectTypes).
		// Формы взяты у производителя, а не выписаны.
		HideExistence: servicecontract.Value(hideExistenceForms()),

		// Порт сверки существования обязателен, раз скрытие объявлено выше: без
		// него отказ «есть, но не твой» и промах «нет такого» пришли бы одной
		// формой, и различить их снаружи стало бы невозможно.
		Existence: existence,

		// Происхождение намерений регистрации — ЗАПИСЬЮ, а не сверкой: строка
		// `kacho_vpc.fga_register_outbox` пишется ТОЙ ЖЕ writer-транзакцией, что
		// вставляет ресурс (один commit, без dual-write), поэтому первая доставка
		// доказана записью, а не выведена из часов в момент доставки.
		Delivery: servicecontract.Value(servicecontract.DeliveryWriterTransaction),

		// Загрузочный гейт мутаций. Предмет у него есть: vpc эмитит намерения
		// регистрации (см. Emits) и служит тенантские `Create` на семи ресурсах,
		// поэтому в окне, пока путь доставки не поднят, ресурс создавался бы БЕЗ
		// владельца. Гейт — ТОТ ЖЕ объект, чью готовность читает проба готовности
		// пода (`buildReadinessCheckers`), то есть «мутации отвергаются» и «под не
		// готов» — одно состояние, а не два.
		BootGate: servicecontract.Value[servicecontract.BootGate](gate),
	})
}

// hostMode переводит режим vpc в общий словарь посадки.
//
// Перевод, а не второй разбор строки: режим уже разобран и провалидирован
// `config.Load` → `Validate`, и заводить здесь второй разбор той же строки значило
// бы завести два места об одном предмете. Ветка `default` остаётся отказом —
// режим, которого нет в словаре, есть решение о посадке, принятое никем.
func hostMode(m config.Mode) (servicecontract.Mode, error) {
	switch m {
	case config.ModeDev:
		return servicecontract.ModeDev, nil
	case config.ModeProduction:
		return servicecontract.ModeProduction, nil
	case config.ModeProductionStrict:
		return servicecontract.ModeProductionStrict, nil
	default:
		return 0, fmt.Errorf("authn.mode %q: не переводится в посадку носителя "+
			"(ожидались dev|production|production-strict)", m)
	}
}
