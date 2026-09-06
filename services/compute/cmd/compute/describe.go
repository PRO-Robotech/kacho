// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// describe.go — ОБЪЯВЛЕНИЕ kacho-compute о себе для носителя контура
// (`pkg/servicehost`).
//
// # Что этот файл заменил
//
// До перевода композиционный корень собирал оба слушателя сам: две пары цепочек,
// своя фабрика звена решения о доступе, своё звено загрузочного гейта мутаций,
// своя проводка извлечения личности на каждый листенер. Порядок звеньев держался
// тем, что автор написал то же, что сосед, — и уже расходился: журнала доступа у
// compute не было ни на одной полосе, верхней границы обработки вызова не было
// вовсе, а срок вопроса о правах и бюджет отказов брались умолчанием библиотеки,
// то есть были числами, которых никто не выбирал.
//
// Здесь сервис приносит ЗНАЧЕНИЯ, и ни одного поля интерсепторного типа: цепочку
// невозможно принести, поэтому её невозможно собрать по-своему.

import (
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/authz/authziam"
	"github.com/PRO-Robotech/kacho/pkg/authz/proxytuple"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/outbox/bootgate"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"

	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/compute/internal/config"
)

// hideExistenceForms — формы отказа для типов compute, которые скрывает рантайм.
//
// Скрытие судится ПРЕДИКАТОМ РАНТАЙМА (`authz.HidesExistenceOnDeny`), а не
// наличием строки `hide_existence` в каталоге: рантайм скрывает по аннотации ИЛИ
// ПО ФОРМЕ — чтение объекта глаголом `v_get` у типа, за который есть чем
// говорить. У compute аннотаций нет ни одной, а по форме скрывает
// `InstanceService/Get`, и объявить ось неприменимой здесь было бы ложным
// заявлением: ровно такое стояло у storage и у nlb, и оба раза отвергалось
// носителем, как только судья и исполнение свели к одному предикату.
//
// Пообъектных типов у compute ДВА — `compute_instance` и
// `compute_guest_access_key`; типы машины и внутреннего каталога гейтятся
// кластерным ярусом, объекта у них нет. Перечень берётся из
// `authzfilter.PerObjectTypes`, а не выписывается здесь: выписанный, он пережил
// бы появление следующего типа и оставил бы его без формы скрытия.
//
// Формы взяты у ПРОИЗВОДИТЕЛЯ (`authz.OwnerNotFoundFormat`), а не выписаны:
// отвечает всегда его таблица, поэтому выписанная копия разошлась бы с
// действительностью — и дескриптор бы это расхождение узаконил. Отсутствие типа
// в таблице — паника корня, а не пропуск: значит перечень скрывающих типов
// разошёлся с таблицей промахов, и поднимать процесс с неполной картой скрытия
// нельзя.
// subscriptionSubscribeFQN — полное имя общего глагола подписки.
//
// Оно записано строкой, а не выведено из сгенерённого дескриптора служб: носитель
// сверяет проводку с КАТАЛОГОМ ПРАВ, где ключ — та же строка, и вывод её из
// другого источника сделал бы сверку тождественно истинной при расхождении.
const subscriptionSubscribeFQN servicecontract.MethodFQN = "/kacho.cloud.subscription.InternalSubscriptionService/Subscribe"

func hideExistenceForms() map[servicecontract.ObjectType]servicecontract.NotFoundFormat {
	out := map[servicecontract.ObjectType]servicecontract.NotFoundFormat{}
	for _, ot := range authzfilter.PerObjectTypes {
		form, ok := authz.OwnerNotFoundFormat(ot)
		if !ok {
			panic("compute: у типа " + ot + " нет голоса владельца в таблице промахов — " +
				"перечень скрывающих типов разошёлся с pkg/authz")
		}
		out[servicecontract.ObjectType(ot)] = servicecontract.NotFoundFormat(form)
	}
	return out
}

// describe собирает дескриптор процесса.
//
// Стражей старта здесь нет ни одного — они живут в конструкторе дескриптора и в
// носителе. Задача этой функции: назвать величины и перевести конфигурацию
// compute в общий словарь, ничего не досочиняя.
//
// Сужатель и загрузочный гейт передаются АРГУМЕНТАМИ, а не собираются здесь
// заново: проводка, объявленная дескриптором, обязана быть ТЕМ ЖЕ объектом, что
// сужает страницу в обработчиках и чью готовность читает проба готовности пода.
// Собери их здесь второй раз — носитель сверял бы с каталогом экземпляр,
// которого на пути запроса нет, и «проводка есть» перестало бы означать «строка
// сужается».
func describe(
	cfg config.Config,
	logger *slog.Logger,
	// listFilter — пообъектный сужатель, ТОТ ЖЕ, что сужает страницы списков в
	// обработчиках. Он приезжает ПАРАМЕТРОМ, а не строится здесь: носитель
	// сверяет с каталогом ЭКЗЕМПЛЯР проводки, и собранный здесь второй раз
	// означал бы, что дескриптор объявил один сужатель, а на пути запроса стоит
	// другой.
	listFilter *listnarrow.Narrower,
	gate *bootgate.Gate,
	existence servicecontract.ExistenceProbe,
	authzObserve func(read func() authz.Metrics),
	metricsReg prometheus.Registerer,
) (servicecontract.Descriptor, error) {
	mode, err := servicecontract.ParseMode(cfg.AuthMode)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("KACHO_COMPUTE_AUTH_MODE: %w", err)
	}

	// Транспорт слушателей и ребра решения о доступе строится ЗДЕСЬ, а судится
	// конструктором дескриптора — по ответу САМОГО ТРАНСПОРТА, а не по ручке:
	// сборщик креденшелов на невзведённой ручке отдаёт незашифрованные креды БЕЗ
	// ошибки, поэтому процесс поднимался, отчитывался «проверка прав включена», и
	// каждый вопрос о доступе уходил по открытому каналу.
	publicCreds, err := grpcsrv.TLSServerTransportCreds(cfg.PublicServerMTLS)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("public listener tls creds: %w", err)
	}
	internalCreds, err := grpcsrv.TLSServerTransportCreds(cfg.InternalServerMTLS)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("internal listener tls creds: %w", err)
	}
	checkCreds, err := grpcclient.TLSClientTransportCreds(cfg.IAMAuthzMTLS)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("compute→iam Check mTLS creds: %w", err)
	}

	// Потолок темпа и одновременности НА ВЫЗЫВАЮЩЕГО: величины посадки там, где
	// она их назвала, и пол платформы там, где молчит. Сборка стоит ДО
	// дескриптора намеренно — негодный набор обязан назвать СЛУШАТЕЛЯ, а не
	// приехать в общий список находок безымянным.
	admission, err := servicecontract.AdmissionFromPosture(cfg.AdmissionPublic, cfg.AdmissionInternal)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("KACHO_COMPUTE_ADMISSION_*: %w", err)
	}

	return servicecontract.New(servicecontract.Spec{
		Service: "kacho-compute",
		Mode:    mode,
		Logger:  logger,

		Forwarders: servicecontract.Value(cfg.TrustedForwarders()),
		ForwarderKnobs: servicecontract.ForwarderKnobs{
			SANs:     "KACHO_COMPUTE_AUTHZ_TRUSTED_FORWARDER_SANS",
			TrustAny: "KACHO_COMPUTE_AUTHZ_TRUST_ANY_FORWARDER",
			OptIn:    cfg.AuthZTrustAnyForwarder,
		},

		// Домен доверия — ЗНАЧЕНИЕ: личность клиентского сертификата этот процесс
		// разбирает, и разбирает её ОТНОСИТЕЛЬНО домена. Читается из той же
		// функции, значение которой уезжает в пару звеньев извлечения личности,
		// поэтому «страж пропустил» ⟺ «домен реально объявлен».
		TrustDomain:     servicecontract.Value(cfg.TrustDomain()),
		TrustDomainKnob: "KACHO_COMPUTE_AUTHZ_TRUST_DOMAIN",

		Authz:     servicecontract.AuthzViaIAM,
		CheckEdge: servicecontract.NewPeerEdge(cfg.AuthZIAMGRPCAddr, checkCreds),
		// Перевод вопроса в контракт службы доступа приносит СЕРВИС: носитель
		// принадлежит фундаменту и чужого контракта не знает (приёмка K3-1,
		// раздел 7.2).
		PeerCheck:    authziam.NewCheckClient,
		CacheWindow:  cfg.AuthZCacheTTL,
		ClientBudget: cfg.AuthZCheckTimeout,
		// Приёмник величин кеша вердиктов: носитель строит кеш, а
		// диагностическую поверхность держит этот корень, и величины переходят
		// границу только здесь. Без него доля попаданий не выходит из процесса,
		// и «сколько даёт кеш» остаётся непроверяемым в обе стороны.
		AuthzObserve: authzObserve,

		// Реестр приходит из корня по той же причине: серии задержки заводит
		// носитель своими руками, а поверхность, которую скребут, держит этот
		// корень. Разбор решения — у `servicecontract.Spec.Metrics`.
		Metrics: metricsReg,

		// Верхняя граница обработки вызова и бюджет отказов — обе ВЕЛИЧИНЫ, обе с
		// обоснованием у своих ручек конфигурации. «Не применимо» здесь незаконно:
		// решение о доступе compute принимает не у себя, а вопросом к kaname, —
		// то есть сетевой сосед, которого шторм отказов может уронить, у него ЕСТЬ.
		HandlingBudget: cfg.HandlingBudget,
		DenyBudget:     servicecontract.Value(cfg.AuthZDenyBudgetPerSec),

		// Ось потолка объявляется ВЕЛИЧИНОЙ, а не изъятием: слушатели выставлены
		// наружу, и «потолка не надо» означало бы, что один вызывающий вправе
		// занять сервис чтением. Изъятие законно только у внутрипроцессной
		// фикстуры, и на боевой посадке дескриптор его отвергает.
		Admission: servicecontract.Value(admission),

		// Срок жизни серверного стрима — ВЕЛИЧИНА, и предмет у неё снова есть.
		//
		// Здесь стояло ИЗЪЯТИЕ, и оно было верным: подписка на журнал изменений
		// была снята вместе со своей поверхностью, потому что подписчика у неё не
		// было ни одного дня. Сервис служит поток снова — но уже ОБЩИЙ
		// (`pkg/subscription`), один на платформу, а не свой. Изъятие
		// стало бы ложью о дереве ровно в тот момент, когда глагол
		// зарегистрирован, поэтому меняется вместе с ним, а не после.
		//
		// Величина принадлежит ПОСАДКЕ и приезжает ручкой: срок жизни потока —
		// то, что оператор обязан уметь подрезать, не пересобирая образ. Носитель
		// сам судит её отношение к границе обработки одиночного вызова и роняет
		// старт, если поток закрывался бы раньше первого события догона.
		StreamBudget: servicecontract.Value(cfg.SubscriptionStreamBudget),

		DBSSLMode:     servicecontract.Value(coredb.SSLModeFromDSN(cfg.DSN())),
		PublicAddr:    ":" + cfg.GrpcPort,
		InternalAddr:  ":" + cfg.InternalGrpcPort,
		PublicCreds:   publicCreds,
		InternalCreds: internalCreds,

		// ── оси ─────────────────────────────────────────────────────────────────

		// Эмиссия: одно отношение иерархии владения — `project:<id> #project
		// @compute_instance:<id>`. Имя берётся у ПРИНИМАЮЩЕЙ стороны
		// (`pkg/authz/proxytuple`), которая владеет закрытым набором принимаемых
		// отношений: второе написание чужого закрытого набора расходится молча, и
		// расходится там, где это не видно — отказ в правах дренаж читает как
		// временный, и очередь встаёт головой партиции навсегда.
		Emits: servicecontract.Value([]proxytuple.Relation{proxytuple.RelationProject}),

		// Регистрируемый тип объектов модели прав — один: машина. Тип машины
		// (`MachineType`) собственного объекта не заводит — это глобальный каталог,
		// гейтящийся кластерным ярусом, и владельца у его строк нет.
		Registers: servicecontract.Value([]servicecontract.ObjectType{
			servicecontract.ObjectType(authzfilter.ResourceTypeInstance),
		}),

		// Проводка сужателя — ровно на тот метод, который каталог объявляет
		// сужаемым. Он же единственный серверный стрим сервиса: за ним пообъектной
		// проверки на крае нет вовсе, поэтому отсутствующий сужатель означал бы не
		// «строже», а «без рубежа».
		//
		// Сужаемый метод у сервиса ОДИН — общий поток изменений
		// (`pkg/subscription`). Здесь стояла ПУСТАЯ карта, и она была
		// верным утверждением ровно пока сервис не служил потока: прежний,
		// собственный, был снят вместе со своей поверхностью. Носитель сверяет
		// проводку с каталогом В ОБЕ СТОРОНЫ, поэтому карта и перечень сужаемых
		// записей каталога обязаны сойтись — лишний сужатель роняет старт так же,
		// как потерянный, и пустая карта при служимом потоке роняет его сама.
		//
		// Сужатель — ТОТ ЖЕ объект, что отдан обработчикам списков: за потоком нет
		// пообъектной проверки на крае, откатываться не на что, и второй экземпляр
		// означал бы, что поток сужается не тем, чем сужаются списки.
		//
		// Публичные списки сужаются НЕ здесь: у них пообъектная проверка стоит
		// в самих обработчиках (`registerPublicServices` отдаёт им `listFilter`).
		Narrowers: servicecontract.Value(map[servicecontract.MethodFQN]servicecontract.ListNarrower{
			subscriptionSubscribeFQN: listFilter,
		}),

		// Скрытие существования у compute ЕСТЬ, хотя аннотаций в каталоге нет ни
		// одной: рантайм скрывает и ПО ФОРМЕ. Разбор — у hideExistenceForms.
		HideExistence: servicecontract.Value(hideExistenceForms()),

		// Порт сверки существования обязателен, раз скрытие объявлено: без него
		// «объекта нет» и «есть, но не твой» пришли бы одной формой, и различить их
		// снаружи стало бы невозможно — то есть скрытие превратилось бы в оракул
		// наоборот.
		Existence: existence,

		// Происхождение намерений регистрации — записью, а не сверкой: строка
		// очереди пишется ТОЙ ЖЕ writer-транзакцией, что вставляет машину
		// (`repo.InstanceRepo.Insert` эмитит намерение на `pgx.Tx` писателя),
		// поэтому первая доставка доказана записью, а не выведена из часов в момент
		// доставки.
		Delivery: servicecontract.Value(servicecontract.DeliveryWriterTransaction),

		// Загрузочный гейт мутаций. Предмет у него есть: compute эмитит намерения
		// регистрации (см. Emits) и служит тенантский `Instance.Create`, поэтому в
		// окне, пока путь доставки не поднят, машина создавалась бы БЕЗ владельца.
		// Гейт — тот же объект, чью готовность читает проба готовности пода
		// (`buildReadinessCheckers`), то есть «мутации отвергаются» и «под не готов»
		// — одно состояние, а не два.
		BootGate: servicecontract.Value[servicecontract.BootGate](gate),
	})
}
