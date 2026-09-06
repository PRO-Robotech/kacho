// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// describe.go — ОБЪЯВЛЕНИЕ kacho-nlb о себе для носителя контура
// (`pkg/servicehost`).
//
// # Что этот файл заменил
//
// До перевода композиционный корень собирал оба слушателя сам: две пары цепочек,
// свой конструктор звена решения о доступе, свой литерал бюджета отказов, своя
// копия предиката гейтируемой мутации. Порядок звеньев держался тем, что автор
// написал то же, что сосед, — и уже расходился: журнал доступа у nlb не вёлся
// вовсе, а верхней границы обработки вызова не было ни на одном листенере.
//
// Здесь сервис приносит ЗНАЧЕНИЯ, и ни одного поля интерсепторного типа: цепочку
// невозможно принести, поэтому её невозможно собрать по-своему.

import (
	"fmt"
	"strings"

	"log/slog"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/authz/authziam"
	"github.com/PRO-Robotech/kacho/pkg/authz/proxytuple"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/outbox/bootgate"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// describe собирает дескриптор процесса.
//
// Стражей старта здесь нет ни одного — они живут в конструкторе дескриптора и в
// носителе. Задача этой функции: назвать величины и перевести конфигурацию nlb в
// общий словарь, ничего не досочиняя.
// hideExistenceForms — формы отказа для типов nlb, которые скрывает рантайм.
//
// Взяты у производителя (`authz.OwnerNotFoundFormat`): носитель сверяет
// объявленное с тем, чем звено реально отвечает, и расхождение роняет старт.
// Отсутствие типа в таблице — паника корня, а не пропуск: значит перечень
// разошёлся с таблицей промахов владельцев, и поднимать процесс с неполной картой
// скрытия нельзя.
func hideExistenceForms() map[servicecontract.ObjectType]servicecontract.NotFoundFormat {
	out := map[servicecontract.ObjectType]servicecontract.NotFoundFormat{}
	for _, ot := range []string{"nlb_network_load_balancer", "nlb_listener", "nlb_target_group"} {
		form, ok := authz.OwnerNotFoundFormat(ot)
		if !ok {
			panic("nlb: у типа " + ot + " нет голоса владельца в таблице промахов — " +
				"перечень скрывающих типов разошёлся с pkg/authz")
		}
		out[servicecontract.ObjectType(ot)] = servicecontract.NotFoundFormat(form)
	}
	return out
}

func describe(
	cfg *config.Config,
	logger *slog.Logger,
	narrower *authzfilter.Narrower,
	gate *bootgate.Gate,
	existence servicecontract.ExistenceProbe,
	authzObserve func(read func() authz.Metrics),
	metricsReg prometheus.Registerer,
) (servicecontract.Descriptor, error) {
	mode, err := hostMode(cfg)
	if err != nil {
		return servicecontract.Descriptor{}, err
	}

	// Транспорт слушателей и ребра решения о доступе строится ЗДЕСЬ, а судится
	// конструктором дескриптора — по ответу самого транспорта, а не по ручке:
	// сборщик креденшелов на невзведённой ручке отдаёт незашифрованные креды БЕЗ
	// ошибки, поэтому «ручка выглядит как угодно» этой проверке безразлично.
	//
	// Слушателей два, а серверный сертификат у nlb ОДИН (`mtls.server`), поэтому
	// обе половины дескриптора получают один и тот же транспорт — асимметрии здесь
	// не существует, и объявлять её двумя ручками значило бы завести различие,
	// которого нет в развёртывании.
	serverCreds, err := grpcsrv.TLSServerTransportCreds(cfg.MTLS.Server)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("build server TLS creds: %w", err)
	}
	// Ребро решения о доступе идёт на ВНУТРЕННИЙ листенер kaname: там живёт
	// InternalIAMService.Check. Ручка его транспорта — `mtls.iam-register`, та же,
	// что закрывает соединение дренажа регистраций (это одно соединение). Адрес
	// резолвится ровно тем же правилом, что в таблице соединений (`peerDialSpecs`,
	// строка `iam-internal`): иначе дескриптор и проводка объявляли бы разные рёбра.
	checkCreds, err := grpcclient.TLSClientTransportCreds(cfg.MTLS.IAMRegister)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("build nlb→iam Check mTLS creds: %w", err)
	}
	checkAddr := firstNonEmpty(cfg.ExtAPI.IAM.InternalAddr, cfg.ExtAPI.IAM.Addr)

	// Потолок темпа и одновременности НА ВЫЗЫВАЮЩЕГО: величины посадки там, где
	// она их назвала, и пол платформы там, где молчит. Сборка стоит ДО
	// дескриптора намеренно — негодный набор обязан назвать СЛУШАТЕЛЯ, а не
	// приехать в общий список находок безымянным.
	admission, err := servicecontract.AdmissionFromPosture(cfg.APIServer.RateLimit.Public, cfg.APIServer.RateLimit.Internal)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("api-server.rate-limit: %w", err)
	}

	return servicecontract.New(servicecontract.Spec{
		Service: "kacho-nlb",
		Mode:    mode,
		Logger:  logger,

		Forwarders: servicecontract.Value(cfg.TrustedForwarders()),
		ForwarderKnobs: servicecontract.ForwarderKnobs{
			SANs:     "authz.trusted-forwarder-sans (env KACHO_NLB_AUTHZ__TRUSTED_FORWARDER_SANS)",
			TrustAny: "authz.trust-any-forwarder (env KACHO_NLB_AUTHZ__TRUST_ANY_FORWARDER)",
			OptIn:    cfg.Authz.TrustAnyForwarder,
		},

		// Домен доверия — ЗНАЧЕНИЕ: личность клиентского сертификата этот процесс
		// разбирает, и разбирает её ОТНОСИТЕЛЬНО домена. Читается из той же
		// функции, значение которой уезжает в пару звеньев извлечения личности,
		// поэтому «страж пропустил» ⟺ «домен реально объявлен».
		TrustDomain:     servicecontract.Value(cfg.TrustDomain()),
		TrustDomainKnob: "authz.trust-domain (env KACHO_NLB_AUTHZ__TRUST_DOMAIN)",

		Authz:     servicecontract.AuthzViaIAM,
		CheckEdge: servicecontract.NewPeerEdge(checkAddr, checkCreds),
		// Перевод вопроса в контракт службы доступа приносит СЕРВИС: носитель
		// принадлежит фундаменту и чужого контракта не знает (приёмка K3-1,
		// раздел 7.2).
		PeerCheck:    authziam.NewCheckClient,
		CacheWindow:  cfg.Authz.Cache.TTL,
		ClientBudget: cfg.Authz.IAM.RequestTimeout,
		// Приёмник величин кеша вердиктов: носитель строит кеш, а
		// диагностическую поверхность держит этот корень, и величины переходят
		// границу только здесь. Без него доля попаданий не выходит из процесса,
		// и «сколько даёт кеш» остаётся непроверяемым в обе стороны.
		AuthzObserve: authzObserve,

		// Реестр приходит отсюда же: серии задержки заводит носитель своими
		// руками, а диагностическую поверхность держит композиционный корень.
		// Разбор решения — у `servicecontract.Spec.Metrics`.
		Metrics: metricsReg,

		// Верхняя граница обработки вызова и бюджет отказов — обе ВЕЛИЧИНЫ, обе с
		// обоснованием у своих ручек конфигурации (`api-server.handling-budget`,
		// `authz.deny-budget-per-sec`). «Не применимо» здесь незаконно: решение о
		// доступе nlb принимает не у себя, а вопросом к kaname, — то есть
		// сетевой сосед, которого шторм отказов может уронить, у него ЕСТЬ.
		HandlingBudget: cfg.APIServer.HandlingBudget,
		DenyBudget:     servicecontract.Value(cfg.Authz.DenyBudgetPerSec),

		// Ось потолка объявляется ВЕЛИЧИНОЙ, а не изъятием: слушатели выставлены
		// наружу, и «потолка не надо» означало бы, что один вызывающий вправе
		// занять сервис чтением.
		Admission: servicecontract.Value(admission),

		// Срок жизни подписки — ВЕЛИЧИНА, и предмет у неё снова есть.
		//
		// Здесь стояло ИЗЪЯТИЕ, и оно было верным: собственный стрим сервиса снят
		// вместе со своим контрактом, потребителя у него не было. Сервис
		// служит поток снова — но уже ОБЩИЙ (`pkg/subscription`), один
		// на платформу, а не свой. Изъятие стало бы ложью о дереве ровно в тот
		// момент, когда глагол зарегистрирован, поэтому меняется вместе с ним.
		//
		// Величина принадлежит ПОСАДКЕ и приезжает ручкой: срок жизни потока —
		// то, что оператор обязан уметь подрезать, не пересобирая образ. Носитель
		// сам судит её отношение к границе обработки одиночного вызова.
		StreamBudget: servicecontract.Value(cfg.APIServer.SubscriptionStreamBudget),

		DBSSLMode:     servicecontract.Value(coredb.SSLModeFromDSN(cfg.Repository.Postgres.URL)),
		PublicAddr:    hostPort(cfg.APIServer.Endpoint),
		InternalAddr:  hostPort(cfg.APIServer.InternalEndpoint),
		PublicCreds:   serverCreds,
		InternalCreds: serverCreds,

		// ── оси ─────────────────────────────────────────────────────────────────

		// Эмиссия. nlb пишет владельцу прав ровно ОДНО отношение — иерархическую
		// привязку ресурса к проекту. Перепись, а не память: `domain.FGAProjectTuple`
		// — единственный строитель кортежа в не-тестовом дереве сервиса
		// (`git grep -n 'domain\.FGA[A-Za-z]*Tuple(' services/nlb/internal | grep -v _test`
		// даёт 10 вхождений, все его).
		Emits: servicecontract.Value([]proxytuple.Relation{proxytuple.RelationProject}),

		// Регистрируемые типы объектов модели прав — три ресурса домена. Тот же
		// предикат разбивает 10 вхождений на 4 балансировщика, 3 листенера и 3
		// целевые группы, и других типов среди них нет.
		Registers: servicecontract.Value([]servicecontract.ObjectType{
			servicecontract.ObjectType(domain.FGAObjectTypeLoadBalancer),
			servicecontract.ObjectType(domain.FGAObjectTypeListener),
			servicecontract.ObjectType(domain.FGAObjectTypeTargetGroup),
		}),

		// Проводка сужателя списочной выдачи. Перечня сужаемых методов дескриптор
		// не несёт — его даёт каталог прав, и носитель сверяет проводку с ним в ОБЕ
		// стороны (О3/О4). Имена собираются из дескрипторов служб, а не выписываются
		// строкой: переименуют службу — не соберётся, а не разойдётся молча.
		Narrowers: servicecontract.Value(map[servicecontract.MethodFQN]servicecontract.ListNarrower{
			listMethod(lbv1.NetworkLoadBalancerService_ServiceDesc.ServiceName): narrower,
			listMethod(lbv1.ListenerService_ServiceDesc.ServiceName):            narrower,
			listMethod(lbv1.TargetGroupService_ServiceDesc.ServiceName):         narrower,
			// Общий поток изменений (`pkg/subscription`). Имя собрано
			// из дескриптора ОБЩЕЙ службы тем же способом, что и остальные три:
			// переименуют — не соберётся, а не разойдётся молча.
			//
			// Сужатель — ТОТ ЖЕ, что у списков, и это несущее: за глаголом потока
			// пообъектной проверки на крае нет вовсе (`scope_filtered`), поэтому
			// отсутствующая или чужая проводка означает не «строже», а «без
			// рубежа».
			servicecontract.MethodFQN("/" + subscriptionv1.InternalSubscriptionService_ServiceDesc.ServiceName +
				"/Subscribe"): narrower,
		}),

		// Скрытие существования у nlb не объявлено НИ НА ОДНОМ методе: в каталоге
		// прав домена `loadbalancer` нет ни одной строки `hide_existence`
		// (`git grep -n hide_existence proto/kacho/cloud/loadbalancer/v1/` — пусто).
		// Заявление ИСТЕКАЕТ САМО: появится первая такая строка — носитель откажет
		// в старте (О5) и назовёт тип, для которого не объявлена форма отказа.
		// Поэтому же сервис не приносит порта проверки существования: спрашивать
		// его было бы некому, и это отдельный отказ конструктора.
		// Скрытие существования у nlb ЕСТЬ, хотя аннотаций в каталоге нет ни одной:
		// рантайм скрывает и ПО ФОРМЕ — чтение объекта глаголом `v_get` у типа, за
		// который есть чем говорить. Такими оказываются балансировщик, слушатель и
		// группа целей.
		//
		// Прежняя редакция объявляла ось неприменимой, ссылаясь на отсутствие строк
		// `hide_existence`. Довод был верен про аннотации и ЛОЖЕН про исполнение —
		// ровно то же заявление стояло у storage и держалось лишь потому, что судья
		// читал аннотацию, а не предикат рантайма.
		//
		// Формы взяты у производителя, а не выписаны: копия разошлась бы с ним тем
		// самым способом, который отказ и ловит.
		HideExistence: servicecontract.Value(hideExistenceForms()),

		// Порт сверки существования обязателен, раз скрытие объявлено: без него
		// «объекта нет» и «есть, но не твой» пришли бы одной формой.
		Existence: existence,

		// Происхождение намерений регистрации — записью, а не сверкой: строка
		// очереди пишется ТОЙ ЖЕ writer-транзакцией, что и сам ресурс
		// (`repo/kacho/pg.fgaRegisterEmitter.Emit` исполняется на `pgx.Tx` писателя),
		// поэтому первая доставка доказана записью, а не выведена из часов в момент
		// доставки.
		Delivery: servicecontract.Value(servicecontract.DeliveryWriterTransaction),

		// Загрузочный гейт мутаций. Предмет у него есть: nlb эмитит намерения
		// регистрации (см. Emits) и служит тенантские `Create` на трёх ресурсах,
		// поэтому в окне, пока путь доставки не поднят, ресурс создавался бы БЕЗ
		// владельца. Гейт — тот же объект, чью готовность читает проба готовности
		// пода (`buildReadinessCheckers`), то есть «мутации отвергаются» и «под не
		// готов» — одно состояние, а не два.
		BootGate: servicecontract.Value[servicecontract.BootGate](gate),
	})
}

// hostMode отдаёт режим nlb носителю контура.
//
// Перевода здесь БОЛЬШЕ НЕТ, и это не упрощение записи. Пока перевод был, у
// сервиса был и свой словарь значений: он объявлял два режима из трёх, и третий
// (`production-strict`) не мог доехать до носителя даже будучи написанным в
// профиле — отказ приходил раньше, на разборе. Тип теперь общий, поэтому
// переводить нечего, а расхождение словарей невыразимо by construction.
//
// Отказ остаётся ровно один и на своём месте: неразобранное значение отвергает
// `Validate`, и процесс до этой функции не доходит. Нулевой режим сюда всё же
// приезжает при пустой строке — носитель отвергнет его сам («посадка не задана»),
// и второй отказ об одном предмете здесь не заводится.
func hostMode(cfg *config.Config) (servicecontract.Mode, error) {
	return cfg.Mode(), nil
}

// hostPort снимает схему с эндпоинта конфигурации (`tcp://0.0.0.0:9090` →
// `0.0.0.0:9090`). Схему уже проверил `Validate` (только `tcp`), поэтому здесь
// именно снятие префикса, а не разбор.
func hostPort(endpoint string) string {
	return strings.TrimPrefix(strings.TrimSpace(endpoint), "tcp://")
}

// listMethod собирает полное имя списочного метода службы в той форме, в какой его
// передаёт grpc-go и в какой его ключует каталог прав.
func listMethod(serviceName string) servicecontract.MethodFQN {
	return servicecontract.MethodFQN("/" + serviceName + "/List")
}
