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

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/authz/proxytuple"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/outbox/bootgate"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"

	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/compute/internal/config"
)

// watchMethod — единственный метод compute, чья авторизация переехала на уровень
// ДАННЫХ (`scope_filtered` в каталоге прав): поток журнала изменений не называет
// ни одного ресурса, поэтому единичного объекта, про который можно спросить
// заранее, у него нет by construction — сужение идёт на КАЖДУЮ отдаваемую строку.
//
// Имя собирается из дескриптора службы, а не выписывается строкой: переименуют
// службу — не соберётся, а не разойдётся молча. Перечня сужаемых методов
// дескриптор НЕ объявляет: его даёт каталог, и носитель сверяет проводку с ним в
// обе стороны (О3/О4) — потерянная проводка и лишняя одинаково роняют старт.
var watchMethod = servicecontract.MethodFQN(
	"/" + computev1.InternalWatchService_ServiceDesc.ServiceName + "/Watch")

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
// Замер по карте прав compute (`internal/check.PermissionMap`): пообъектный тип
// из неё выводится РОВНО один — `compute_instance`; типы машины и внутреннего
// каталога гейтятся кластерным ярусом, объекта у них нет.
//
// Формы взяты у ПРОИЗВОДИТЕЛЯ (`authz.OwnerNotFoundFormat`), а не выписаны:
// отвечает всегда его таблица, поэтому выписанная копия разошлась бы с
// действительностью — и дескриптор бы это расхождение узаконил. Отсутствие типа
// в таблице — паника корня, а не пропуск: значит перечень скрывающих типов
// разошёлся с таблицей промахов, и поднимать процесс с неполной картой скрытия
// нельзя.
func hideExistenceForms() map[servicecontract.ObjectType]servicecontract.NotFoundFormat {
	out := map[servicecontract.ObjectType]servicecontract.NotFoundFormat{}
	for _, ot := range []string{authzfilter.ResourceTypeInstance} {
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
	narrower *authzfilter.Narrower,
	gate *bootgate.Gate,
	existence servicecontract.ExistenceProbe,
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

	return servicecontract.New(servicecontract.Spec{
		Service: "kacho-compute",
		Mode:    mode,
		Logger:  logger,

		Forwarders: cfg.TrustedForwarders(),
		ForwarderKnobs: servicecontract.ForwarderKnobs{
			SANs:     "KACHO_COMPUTE_AUTHZ_TRUSTED_FORWARDER_SANS",
			TrustAny: "KACHO_COMPUTE_AUTHZ_TRUST_ANY_FORWARDER",
			OptIn:    cfg.AuthZTrustAnyForwarder,
		},

		Authz:        servicecontract.AuthzViaIAM,
		CheckEdge:    servicecontract.NewPeerEdge(cfg.AuthZIAMGRPCAddr, checkCreds),
		CacheWindow:  cfg.AuthZCacheTTL,
		ClientBudget: cfg.AuthZCheckTimeout,

		// Верхняя граница обработки вызова и бюджет отказов — обе ВЕЛИЧИНЫ, обе с
		// обоснованием у своих ручек конфигурации. «Не применимо» здесь незаконно:
		// решение о доступе compute принимает не у себя, а вопросом к kacho-iam, —
		// то есть сетевой сосед, которого шторм отказов может уронить, у него ЕСТЬ.
		HandlingBudget: cfg.HandlingBudget,
		DenyBudget:     servicecontract.Value(cfg.AuthZDenyBudgetPerSec),

		// Срок жизни подписки — ВЕЛИЧИНА, а не изъятие: compute служит серверный
		// стрим `InternalWatchService/Watch` (провязан в registerInternalServices).
		// Изъятие здесь было бы ложным заявлением, и носитель уронил бы старт (О11),
		// назвав метод.
		//
		// Величина обязана ЗАМЕТНО превосходить границу обработки одиночного вызова
		// (30 с): у подписки свой срок — она живёт, пока живёт наблюдатель, — и
		// потолок запроса рвал бы её каждые полминуты, причём клиент видел бы это
		// сетевым сбоем, а не нашим отказом. Час — это ПОТОЛОК на случай забытого
		// наблюдателя, а не цель: здоровая подписка закрывается раньше своим
		// контекстом. Предмет потолка назван у ручки: каждый стрим держит выделенное
		// соединение под LISTEN (KACHO_COMPUTE_WATCH_MAX_STREAMS, 32), поэтому
		// брошенные подписки исчерпывают лимит и новые наблюдатели не подключаются.
		StreamBudget: servicecontract.Value(cfg.WatchStreamBudget),

		DBSSLMode:     coredb.SSLModeFromDSN(cfg.DSN()),
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
		Narrowers: servicecontract.Value(map[servicecontract.MethodFQN]servicecontract.ListNarrower{
			watchMethod: narrower,
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
