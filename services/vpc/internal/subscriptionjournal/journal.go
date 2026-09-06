// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package subscriptionjournal — объявление ЖУРНАЛА vpc для общего сервера потока
// изменений (`pkg/subscription`).
//
// # Что здесь есть и чего здесь нет
//
// Здесь только ЗНАЧЕНИЯ: где журнал лежит, каким каналом будит, как его строка
// становится событием общей формы. Курсора, границы устоявшегося, пределов,
// сужения по правам и порядка отказов здесь нет и быть не может — они
// принадлежат общему серверу и владельцу не выдаются.
//
// # Почему якорь проекта — КОЛОНКА, а не разбор нагрузки
//
// Общая форма допускает оба устройства, и выбор здесь сделан осознанно, тем же
// доводом, что у compute.
//
// Нагрузка события снятия у vpc несёт ОДИН идентификатор (`map[string]any{"id":
// id}` на всех пятнадцати путях снятия), проекта в ней нет. Значит разбор дал бы
// у снятий пустой якорь — а пустой якорь по контракту означает «предмет уровня
// аккаунта или кластера», то есть УТВЕРЖДЕНИЕ, ложное для сети, подсети и всякого
// проектного предмета vpc. Дальше подписка с осью `project_id` такие события не
// пропускала бы, и потребитель, снявший опрос, НИКОГДА не узнавал бы об удалении.
// Отказ этот наступает ТИХО: ни ошибки, ни пропуска в нумерации.
//
// Колонка `project_id` заведена миграцией `..._vpc_outbox_project_anchor` и
// заполняется в той же транзакции, что и ресурсная строка. Второе следствие того
// же выбора — ось отбирается ЗАПРОСОМ (частичный индекс по паре «якорь, номер»),
// а не после чтения: прочитано будет ровно то, что отдано.
//
// # У журнала vpc ДВА производителя, и это не деталь
//
// Строку пишет не только код: триггер `subnets_outbox_emit_route_table_change`
// вставляет `Subnet`/`UPDATED` при авто-привязке таблицы маршрутов, то есть
// события рождает САМА БАЗА. Оба производителя учтены — и словарём родов
// изменения (проба выводит его разбором обоих), и якорем (триггер научен писать
// колонку той же миграцией).
//
// # Каких видов здесь НЕТ, и почему это требование, а не пробел
//
// Журнал несёт десять видов, словарь называет ВОСЕМЬ. Вне словаря намеренно
// оставлены `AddressPool` и `AddressPoolNetworkDefault`: пул адресов —
// АДМИНСКИЙ ресурс уровня кластера, живущий только на внутренней поверхности
// (`InternalAddressPoolService`), и ПРОЕКТНОГО ИЗМЕРЕНИЯ у него нет по
// определению — `AddressPoolRecord` встраивает `domain.AddressPool`, а поля
// проекта нет ни там, ни там. Оси, по которой подписка сужает, у этих строк не
// существует, и одного этого достаточно.
//
// Отсюда следует не «мы забыли их добавить», а «их нельзя доставлять»: вопрос
// «вправе ли вызывающий видеть эту строку» задать НЕЧЕМ, а строка, которую нечем
// авторизовать, арендатору не отдаётся. Инфраструктурный предмет — адресные пулы
// платформы — остаётся на внутренней поверхности, как того требует правило о
// двух проекциях ресурса.
//
// # Чего это основание НЕ утверждает, и почему сказано вслух (#1494)
//
// Здесь стояло ВТОРОЕ основание — про отсутствие у пула типа объекта в модели
// прав, — и оно опровергнуто замером: `vpc_address_pool` объявлен в канонической
// модели и несёт четыре глагола. Тип есть, но помечен спящим (`# kacho:latent`):
// объектов пула у владельца прав не регистрирует никто, а его RPC гейтятся
// прямо на `cluster:cluster_root#system_admin`. То есть даже будь ось
// проекта, сужать было бы не по чему — но говорить об этом «типа нет» нельзя.
//
// Довод «типов такого домена в модели НОЛЬ» — рабочий признак
// непровязываемости, и он применяется там, где верен: у geo типов `geo_*`
// действительно ноль, и потому geo потоком не владеет
// (`pkg/subscription/doc.go`). Ложное употребление признака здесь обесценивало
// его там. Сверяется основание пробой `TestPoolExclusionGroundMatchesTheAuthzModel`.
package subscriptionjournal

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/types/known/anypb"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"
	_ "github.com/PRO-Robotech/kacho/services/vpc/internal/dto/toproto" // регистрация трансферов
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

const (
	// Table — таблица журнала, КВАЛИФИЦИРОВАННАЯ схемой.
	Table = "kacho_vpc.vpc_outbox"

	// Channel — канал пробуждения.
	//
	// Он НЕ выводится из имени таблицы, и vpc — ровно тот случай, ради которого
	// общая форма держит его отдельным полем: таблица здесь схемо-квалифицирована
	// (`kacho_vpc.vpc_outbox`), а канал триггера — нет (`pg_notify('vpc_outbox',
	// …)`, миграция 0001). Вывод одного из другого работал бы у большинства
	// владельцев и молча ошибался у этого.
	Channel = "vpc_outbox"
)

// Виды предметов журнала — слова ПРОИЗВОДИТЕЛЯ, ровно те, что стоят первым
// аргументом эмиссии. Их согласие с деревом закреплено пробой, выводящей перечень
// разбором вызовов, а не вторым рукописным списком.
const (
	KindNetwork          = "Network"
	KindSubnet           = "Subnet"
	KindSecurityGroup    = "SecurityGroup"
	KindRouteTable       = "RouteTable"
	KindAddress          = "Address"
	KindGateway          = "Gateway"
	KindNetworkInterface = "NetworkInterface"
	KindCidrGroup        = "CidrGroup"
)

// changeDeleted — слово владельца для снятия предмета.
const changeDeleted = "DELETED"

// Journal — объявление журнала vpc.
func Journal() subscription.Journal {
	return subscription.Journal{
		Channel: Channel,
		Storage: subscription.Storage{
			Table:          Table,
			PositionColumn: "sequence_no",
			KindColumn:     "resource_kind",
			IDColumn:       "resource_id",
			ChangeColumn:   "event_type",
			PayloadColumn:  "payload",
			ProjectColumn:  "project_id",
			Project:        subscription.ProjectInColumn,
			// Журнал ЧИСТИТСЯ, и обещание подписчику меняется ВМЕСТЕ с этим.
			//
			// Прежняя редакция объявляла [subscription.RetainsEverything] и
			// говорила прямо: «заведётся чистка — эта величина обязана поменяться
			// ВМЕСТЕ с ней, обещание удержания живёт ровно столько, сколько живёт
			// его основание». Основание снято задачей #1735: строка журнала
			// пишется на КАЖДОЙ мутации ресурса, темп задаёт арендатор, а снятия
			// строк не было ни на одном пути — рост был монотонным и вечным.
			//
			// Что теперь верно для подписчика: нижняя возобновимая позиция у
			// потока ЕСТЬ, она приходит в служебном сообщении открытия, и
			// возобновление с позиции ниже неё отвечает ЯВНЫМ отказом
			// `OutOfRange` с названной позицией — а не молча отдаёт неполное.
			// Окно объявлено платформой один раз ([subscription.JournalRetention]).
			Retention: subscription.RetainsFromEarliestRow,
			// Колонка срока — ПАРА к объявлению выше; предикат уборки строится по
			// ней. Отметку ставит умолчание колонки (`DEFAULT now()`, миграция
			// `0001_initial.sql`), то есть часами БАЗЫ — теми же, которыми судит
			// уборщик, поэтому слагаемого на разницу источников у порога нет.
			AgeColumn: "created_at",
		},
		Mapping: subscription.Mapping{
			// Тип объекта и действие берутся у ПРОИЗВОДИТЕЛЯ (`authzfilter`), а
			// не выписываются: второе написание чужого словаря расходится молча.
			// Действие — то же, которым сужается список этого вида, поэтому
			// видимость в потоке равна видимости в списке.
			Kinds: map[string]subscription.Kind{
				KindNetwork:          {ObjectType: authzfilter.ResourceTypeNetwork, Action: authzfilter.ActionNetworkList},
				KindSubnet:           {ObjectType: authzfilter.ResourceTypeSubnet, Action: authzfilter.ActionSubnetList},
				KindSecurityGroup:    {ObjectType: authzfilter.ResourceTypeSecurityGroup, Action: authzfilter.ActionSecurityGroupList},
				KindRouteTable:       {ObjectType: authzfilter.ResourceTypeRouteTable, Action: authzfilter.ActionRouteTableList},
				KindAddress:          {ObjectType: authzfilter.ResourceTypeAddress, Action: authzfilter.ActionAddressList},
				KindGateway:          {ObjectType: authzfilter.ResourceTypeGateway, Action: authzfilter.ActionGatewayList},
				KindNetworkInterface: {ObjectType: authzfilter.ResourceTypeNetworkInterface, Action: authzfilter.ActionNetworkInterfaceList},
				KindCidrGroup:        {ObjectType: authzfilter.ResourceTypeCidrGroup, Action: authzfilter.ActionCidrGroupList},
			},
			// Словарь родов изменения — ровно те слова, которыми пишут ОБА
			// производителя: код и триггер базы. Слово вне словаря делает строку
			// недоставляемой, и это громко: расхождение журнала с объявлением
			// есть дефект, а не свойство подписки.
			Changes: map[string]subscriptionv1.SubscriptionEvent_Change{
				"CREATED":     subscriptionv1.SubscriptionEvent_CREATED,
				"UPDATED":     subscriptionv1.SubscriptionEvent_UPDATED,
				changeDeleted: subscriptionv1.SubscriptionEvent_DELETED,
			},
			State: state,
		},
	}
}

// ProjectGate — страж оси `project_id`.
//
// Без него вызывающий, назвавший недоступный ему проект, получил бы ОТКРЫТЫЙ
// поток, молчащий вечно: ни одна строка не прошла бы построчного сужения, и это
// читалось бы как «изменений нет».
//
// Форма отказа берётся у ПРОИЗВОДИТЕЛЯ форм скрытия, а не сочиняется здесь: она
// обязана быть неотличима от настоящего промаха владельца проектов, иначе
// подписка становится способом узнать существование чужого проекта. Отсутствие
// формы у производителя — отказ сборки, а не молчаливое умолчание: страж без
// формы отвечал бы отличимым текстом, то есть ровно тем, что закрывает.
func ProjectGate() (subscription.ProjectGate, error) {
	const projectObjectType = "project"
	form, ok := authz.OwnerNotFoundFormat(projectObjectType)
	if !ok {
		return subscription.ProjectGate{}, fmt.Errorf(
			"subscriptionjournal: у типа %q нет формы отсутствия у производителя (pkg/authz): "+
				"страж оси project_id отвечал бы текстом, отличимым от промаха владельца, "+
				"то есть выдавал бы существование чужого проекта", projectObjectType)
	}
	return subscription.ProjectGate{
		ObjectType: projectObjectType,
		// Действие и отношение — те же, которыми владелец проектов гейтит своё
		// чтение (`iam.v1.ProjectService/Get` в каталоге прав): вопрос «вправе ли
		// он видеть этот проект» обязан быть ТЕМ ЖЕ вопросом, а не похожим.
		Action:         "iam.projects.get",
		Relations:      []string{"v_get"},
		NotFoundFormat: form,
	}, nil
}

// state — состояние предмета события.
//
// # Почему снятие отдаётся БЕЗ состояния, и это не потеря
//
// Нагрузка снятия несёт один идентификатор — полного состояния в ней нет и быть
// не может: предмета больше нет. Собрать из неё ресурс значило бы отдать
// подписчику почти пустую сеть, а контракт формы разрешает читать НЕПУСТУЮ
// нагрузку как ПОЛНОЕ состояние предмета. Подписчик записал бы пустые поля как
// факт: имя исчезло, метки исчезли, блоки исчезли.
//
// Поэтому род изменения спрашивается ЯВНО, а не выводится из бедности нагрузки:
// вывод из бедности сработал бы и на настоящем сбое разбора, и два разных исхода
// стали бы неразличимы.
//
// Для снятия подписчику довольно оболочки: вид, идентификатор, якорь проекта и
// род изменения — этого хватает, чтобы убрать строку из своего состояния.
//
// # Почему причина отсутствия у снятия — «НЕ ПРОИЗВОДИТСЯ», а не «не удерживается»
//
// Журнал vpc состояние НЕСЁТ — у семи видов из восьми оно собирается полностью, и
// именно поэтому причина обязана быть названа ПОСТРОЧНО, а не на весь журнал.
// Снятие — единственный род, у которого предмета больше нет by construction:
// попытки собрать не было, и повтор её не изменит. Это и есть [subscription.StateNotProduced].
//
// [subscription.StateNotRetained] («больше не удерживается») здесь неверна и была
// бы не оттенком, а ложью — но ОСНОВАНИЕ у этого теперь другое, чем было.
//
// Прежняя редакция выводила её неверность из того, что журнал не чистится вовсе
// ([subscription.RetainsEverything] строкой выше). Основание снято задачей #1735:
// журнал ЧИСТИТСЯ, окно удержания конечно. Вывод при этом уцелел, и вот почему:
// уборка снимает СТРОКУ ЦЕЛИКОМ, а не состояние внутри доставляемой строки.
// Подписчик, чья позиция ушла под окно, получает ЯВНЫЙ отказ `OutOfRange` и в
// эту функцию не попадает вовсе; а всякая строка, которая до неё доехала,
// удержана. Значит «больше не удерживается» не описывает НИ ОДНУ строку,
// приходящую сюда, — и подписчик, прочитавший это, заключил бы, что опоздал за
// состоянием, которого никто не собирал.
func state(r subscription.Row) (*anypb.Any, subscription.StateAbsence, error) {
	if r.Change == changeDeleted {
		return nil, subscription.StateNotProduced, nil
	}

	// Разбор и перенос выписаны ПОВИДОВО, а не сведены в один обобщённый вызов,
	// и это следствие чужого решения, а не многословие: набор трансферов
	// объявлен ЗАКРЫТЫМ union-ограничением (`dto.Transferrable`), поэтому
	// обобщённый параметр ему не удовлетворяет by construction. Закрытость там
	// намеренная — она требует объявить трансфер, а не получить его выводом, —
	// и ломать её ради краткости здесь значило бы открыть чужой инвариант.
	switch r.Kind {
	case KindNetwork:
		var rec kachorepo.NetworkRecord
		var pb *vpcv1.Network
		if err := decode(r, &rec); err != nil {
			return nil, subscription.StateAbsenceUnnamed, err
		}
		if err := dto.Transfer(dto.FromTo(rec, &pb)); err != nil {
			return nil, subscription.StateAbsenceUnnamed, transferFailed(r, err)
		}
		return packed(anypb.New(pb))
	case KindSubnet:
		var rec kachorepo.SubnetRecord
		var pb *vpcv1.Subnet
		if err := decode(r, &rec); err != nil {
			return nil, subscription.StateAbsenceUnnamed, err
		}
		if err := dto.Transfer(dto.FromTo(rec, &pb)); err != nil {
			return nil, subscription.StateAbsenceUnnamed, transferFailed(r, err)
		}
		return packed(anypb.New(pb))
	case KindSecurityGroup:
		var rec kachorepo.SecurityGroupRecord
		var pb *vpcv1.SecurityGroup
		if err := decode(r, &rec); err != nil {
			return nil, subscription.StateAbsenceUnnamed, err
		}
		if err := dto.Transfer(dto.FromTo(rec, &pb)); err != nil {
			return nil, subscription.StateAbsenceUnnamed, transferFailed(r, err)
		}
		return packed(anypb.New(pb))
	case KindRouteTable:
		var rec kachorepo.RouteTableRecord
		var pb *vpcv1.RouteTable
		if err := decode(r, &rec); err != nil {
			return nil, subscription.StateAbsenceUnnamed, err
		}
		if err := dto.Transfer(dto.FromTo(rec, &pb)); err != nil {
			return nil, subscription.StateAbsenceUnnamed, transferFailed(r, err)
		}
		return packed(anypb.New(pb))
	case KindAddress:
		var rec kachorepo.AddressRecord
		var pb *vpcv1.Address
		if err := decode(r, &rec); err != nil {
			return nil, subscription.StateAbsenceUnnamed, err
		}
		if err := dto.Transfer(dto.FromTo(rec, &pb)); err != nil {
			return nil, subscription.StateAbsenceUnnamed, transferFailed(r, err)
		}
		return packed(anypb.New(pb))
	case KindGateway:
		var rec kachorepo.GatewayRecord
		var pb *vpcv1.Gateway
		if err := decode(r, &rec); err != nil {
			return nil, subscription.StateAbsenceUnnamed, err
		}
		if err := dto.Transfer(dto.FromTo(rec, &pb)); err != nil {
			return nil, subscription.StateAbsenceUnnamed, transferFailed(r, err)
		}
		return packed(anypb.New(pb))
	case KindNetworkInterface:
		var rec kachorepo.NetworkInterfaceRecord
		var pb *vpcv1.NetworkInterface
		if err := decode(r, &rec); err != nil {
			return nil, subscription.StateAbsenceUnnamed, err
		}
		if err := dto.Transfer(dto.FromTo(rec, &pb)); err != nil {
			return nil, subscription.StateAbsenceUnnamed, transferFailed(r, err)
		}
		return packed(anypb.New(pb))
	case KindCidrGroup:
		var rec kachorepo.CidrGroupRecord
		var pb *vpcv1.CidrGroup
		if err := decode(r, &rec); err != nil {
			return nil, subscription.StateAbsenceUnnamed, err
		}
		if err := dto.Transfer(dto.FromTo(rec, &pb)); err != nil {
			return nil, subscription.StateAbsenceUnnamed, transferFailed(r, err)
		}
		return packed(anypb.New(pb))
	}
	// Вид вне словаря сюда не доходит — сервер отсеивает такую строку раньше,
	// потому что авторизовать её нечем. Ветка оставлена как ОТКАЗ, а не как
	// пустое состояние: молчаливый `nil` здесь означал бы «предмет снят», и
	// подписчик убрал бы из своего состояния живую строку.
	return nil, subscription.StateAbsenceUnnamed, fmt.Errorf("вид %q вне словаря журнала vpc: состояние собрать не из чего", r.Kind)
}

// packed переводит исход упаковки в тройку общей формы.
//
// Отказ упаковки — НАСТОЯЩИЙ отказ сборки: состояние есть, собрать не удалось.
// Причину такому исходу даёт сервер (`NOT_SERIALIZABLE`), и владелец её не
// называет: назвал бы — и свойство журнала стало бы неотличимо от поломки.
func packed(a *anypb.Any, err error) (*anypb.Any, subscription.StateAbsence, error) {
	if err != nil {
		return nil, subscription.StateAbsenceUnnamed, err
	}
	return a, subscription.StateAbsenceUnnamed, nil
}

// decode — разбор нагрузки в запись репозитория.
//
// Нагрузка записана тем же кодированием, каким читается (`helpers.DomainToMap` —
// обход `encoding/json` по записи), поэтому обратный ход симметричен by
// construction. Проба этого не предполагает, а проверяет.
func decode(r subscription.Row, dst any) error {
	if err := json.Unmarshal(r.Payload, dst); err != nil {
		return fmt.Errorf("разбор нагрузки журнала (%s %s): %w", r.Kind, r.ID, err)
	}
	return nil
}

// transferFailed — отказ переноса записи в контракт.
//
// Перенос идёт ТЕМ ЖЕ трансфером, которым отвечает обычное чтение, а не своим
// вторым отображением: второе разошлось бы с первым молча, и подписчик получал
// бы ресурс, отличный от того, что отдаёт `Get`.
func transferFailed(r subscription.Row, err error) error {
	return fmt.Errorf("перенос записи в контракт (%s %s): %w", r.Kind, r.ID, err)
}
