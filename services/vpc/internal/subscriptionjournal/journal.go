// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package subscriptionjournal — объявление ЖУРНАЛА vpc для общего сервера потока
// изменений (`corelib/subscription`).
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
// # Пул адресов — вид уровня кластера (NTF-3, Р2, З2)
//
// Журнал несёт девять видов, и словарь называет все девять. Пул адресов —
// админский ресурс уровня кластера: проектного измерения у него нет
// (`AddressPoolRecord` встраивает `domain.AddressPool`, поля проекта нет ни там,
// ни там), поэтому его вид объявлен `ScopeCluster` — строка пула пишется без
// якоря, а запись с якорем функция фундамента отвергает. Видимость события
// пула решает модель прав поштучно (`v_get` на `vpc_address_pool`), как и у
// проектных видов; подписка с осью проекта строк пула не отбирает — у них оси
// нет.
//
// Привязка пула по умолчанию к сети собственного вида не имеет: у неё нет типа
// в модели прав и нет читателя. Глаголы привязки и снятия привязки пишут
// событие правки самого пула (`AddressPool` `UPDATED`), а строк прежнего
// отдельного вида привязки журнал больше не получает (NTF3-62).
//
// # Форма имени и якорь у каждого вида
//
// Каждый вид объявляет, есть ли у него имя формы DNS-метки (`NameForm`), и где
// он живёт (`Scope`). У всех девяти видов vpc имя — DNS-метка (форму держит
// ограничение `<таблица>_name_check` схемы либо самопроверяющийся тип имени
// домена), поэтому строка снятия обязана нести снимок имени под ключом
// [subscription.NamePayloadKey] — его даёт `RETURNING` удаляющего оператора
// репозитория, а не чтение до удаления.
package subscriptionjournal

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/types/known/anypb"

	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"
	"github.com/PRO-Robotech/corelib/authz"
	"github.com/PRO-Robotech/corelib/subscription"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/feedjournal"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
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
	KindAddressPool      = "AddressPool"
)

// changeDeleted — слово владельца для снятия предмета.
const changeDeleted = "DELETED"

// Journal — объявление журнала vpc.
//
// feedEnabled — флаг ленты модуля (`KACHO_VPC_NOTIFICATIONS_ENABLED`), прочитанный
// загрузчиком конфигурации один раз; то же значение корень отдаёт писателям
// журнала (`journaltx.Options`). Вид ленты объявляется ровно при включённом
// флаге, прочие виды от него не зависят (NTF3-65, NTF3-67; замысел З11).
func Journal(feedEnabled bool) subscription.Journal {
	j := subscription.Journal{
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
			// Инициатор и время строки — колонки журнала (NTF-3, Р2, З2):
			// инициатора кладёт умолчание колонки из настройки транзакции
			// помощника `journaltx` (миграция `..._journal_initiator.sql`), время —
			// умолчание `now()` колонки `created_at`, то есть время транзакции
			// изменения, а не часы процесса. Событие несёт оба значения.
			InitiatorColumn:  "initiator",
			OccurredAtColumn: "created_at",
		},
		Mapping: subscription.Mapping{
			// Тип объекта и действие берутся у ПРОИЗВОДИТЕЛЯ (`authzfilter`), а
			// не выписываются: второе написание чужого словаря расходится молча.
			// Действие — то же, которым гейтится список этого вида, поэтому
			// видимость в потоке равна видимости в списке.
			Kinds: map[string]subscription.Kind{
				KindNetwork: {
					ObjectType: authzfilter.ResourceTypeNetwork,
					Action:     authzfilter.ActionNetworkList,
					NameForm:   subscription.NameFormDNS,
					Scope:      subscription.ScopeProject,
				},
				KindSubnet: {
					ObjectType: authzfilter.ResourceTypeSubnet,
					Action:     authzfilter.ActionSubnetList,
					NameForm:   subscription.NameFormDNS,
					Scope:      subscription.ScopeProject,
				},
				KindSecurityGroup: {
					ObjectType: authzfilter.ResourceTypeSecurityGroup,
					Action:     authzfilter.ActionSecurityGroupList,
					NameForm:   subscription.NameFormDNS,
					Scope:      subscription.ScopeProject,
				},
				KindRouteTable: {
					ObjectType: authzfilter.ResourceTypeRouteTable,
					Action:     authzfilter.ActionRouteTableList,
					NameForm:   subscription.NameFormDNS,
					Scope:      subscription.ScopeProject,
				},
				KindAddress: {
					ObjectType: authzfilter.ResourceTypeAddress,
					Action:     authzfilter.ActionAddressList,
					NameForm:   subscription.NameFormDNS,
					Scope:      subscription.ScopeProject,
				},
				KindGateway: {
					ObjectType: authzfilter.ResourceTypeGateway,
					Action:     authzfilter.ActionGatewayList,
					NameForm:   subscription.NameFormDNS,
					Scope:      subscription.ScopeProject,
				},
				KindNetworkInterface: {
					ObjectType: authzfilter.ResourceTypeNetworkInterface,
					Action:     authzfilter.ActionNetworkInterfaceList,
					NameForm:   subscription.NameFormDNS,
					Scope:      subscription.ScopeProject,
				},
				KindCidrGroup: {
					ObjectType: authzfilter.ResourceTypeCidrGroup,
					Action:     authzfilter.ActionCidrGroupList,
					NameForm:   subscription.NameFormDNS,
					Scope:      subscription.ScopeProject,
				},
				KindAddressPool: {
					ObjectType: authzfilter.ResourceTypeAddressPool,
					Action:     authzfilter.ActionAddressPoolList,
					NameForm:   subscription.NameFormDNS,
					Scope:      subscription.ScopeCluster,
				},
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
	// Вид ленты извещений — ровно при включённом флаге модуля (NTF3-65, NTF3-67).
	feedjournal.Declare(j.Mapping.Kinds, feedEnabled)
	return j
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
// Нагрузка снятия несёт идентификатор и снимок имени — полного состояния в ней
// нет и быть не может: предмета больше нет. Собрать из неё ресурс значило бы отдать
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
// Журнал vpc состояние НЕСЁТ — у каждого вида оно собирается из нагрузки, и
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
	case KindAddressPool:
		// Нагрузка пула — доменный снимок одной формы для пути записи и посева
		// стенда (З8; сличение — `seedaddresspoolparity`): отметки создания в
		// ней нет. Перенос идёт тем же трансфером, что ответ `Get`, но отметка
		// на событии снята, а не подставлена нулём: нулевое время прочиталось
		// бы фактом «пул создан в 1970-м», а отсутствие поля честно говорит, что
		// журнал его не несёт.
		var d domain.AddressPool
		var pb *vpcv1.AddressPool
		if err := decode(r, &d); err != nil {
			return nil, subscription.StateAbsenceUnnamed, err
		}
		if err := dto.Transfer(dto.FromTo(kachorepo.AddressPoolRecord{AddressPool: d}, &pb)); err != nil {
			return nil, subscription.StateAbsenceUnnamed, transferFailed(r, err)
		}
		pb.CreatedAt = nil
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
