// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package subscriptionjournal — объявление ЖУРНАЛА compute для общего сервера
// потока изменений (`corelib/subscription`).
//
// # Что здесь есть и чего здесь нет
//
// Здесь только ЗНАЧЕНИЯ: где журнал лежит, каким каналом будит, как его строка
// становится событием общей формы. Курсора, границы устоявшегося, пределов,
// сужения по правам и порядка отказов здесь нет и быть не может — они
// принадлежат общему серверу и владельцу не выдаются. Появись у compute
// возможность принести своё вместо любого из них, механизм перестал бы быть
// общим, оставшись общим по имени.
//
// # Почему якорь проекта — КОЛОНКА, а не разбор нагрузки
//
// Общая форма допускает оба устройства, и выбор здесь сделан осознанно.
//
// Нагрузка события удаления у compute несёт идентификатор и снимок имени
// (ветки Delete репозиториев), проекта в ней нет. Значит разбор нагрузки
// дал бы у удалений пустой якорь — а пустой якорь по контракту означает «предмет
// уровня аккаунта или кластера», то есть УТВЕРЖДЕНИЕ, ложное для машины. Дальше
// подписка с осью `project_id` такие события не пропускала бы, и потребитель,
// снявший поллинг, НИКОГДА не узнавал бы об удалении — держал бы удалённые
// строки вечно. Отказ этот наступает ТИХО: ни ошибки, ни пропуска в нумерации.
//
// Ровно это и называет контракт формы у поля `project_id`: якорь стоит полем
// оболочки, потому что «для события удаления это несущее — обратиться не к чему».
// Колонка `project_id` заведена миграцией `..._compute_outbox_project_anchor` и
// заполняется в той же транзакции, что и ресурсная строка.
//
// Второе следствие того же выбора — ось отбирается ЗАПРОСОМ (частичный индекс по
// паре «якорь, номер»), а не после чтения: прочитано будет ровно то, что отдано.
package subscriptionjournal

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"
	"github.com/PRO-Robotech/corelib/authz"
	"github.com/PRO-Robotech/corelib/subscription"
	"github.com/PRO-Robotech/kacho/pkg/feedjournal"
	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/protoconv"
)

const (
	// Table — таблица журнала. Имя без схемы: журнал compute лежит в `public`.
	Table = "compute_outbox"

	// Channel — канал пробуждения. Назван ОТДЕЛЬНО от таблицы, а не выведен из
	// её имени: у compute они совпадают, у соседа с квалифицированной таблицей —
	// нет, и вывод одного из другого молча ошибался бы именно там.
	Channel = "compute_outbox"

	// JournalWordInstance — слово, которым РЕПОЗИТОРИЙ записывает вид строки в
	// колонку `resource_kind` (`emitCompute(..., "Instance", ...)`).
	//
	// Имя названо словом ЖУРНАЛА, а не «видом», намеренно: клиенту этот предмет
	// едет как `compute_instance` — тип объекта модели прав, единственное
	// написание вида на всё дерево (`corelib/subscription`, `Journal.KindDictionary`).
	// Прежнее имя (`KindInstance`) утверждало обратное и было бы прочитано
	// следующим как то, что клиент пишет в ось `kinds`.
	//
	// Блочное хранение ушло из compute миграцией 0021, и `Disk` / `Image` /
	// `Snapshot` среди видов больше не значатся — их владелец kacho-storage.
	JournalWordInstance = "Instance"

	// JournalWordPlacementGroup — слово журнала группы размещения
	// (`emitCompute(..., "PlacementGroup", ...)`); клиенту едет
	// `compute_placement_group`.
	JournalWordPlacementGroup = "PlacementGroup"

	// JournalWordGuestAccessKey — слово журнала гостевого ключа
	// (`emitCompute(..., "GuestAccessKey", ...)`); клиенту едет
	// `compute_guest_access_key`.
	JournalWordGuestAccessKey = "GuestAccessKey"

	// changeDeleted — слово владельца для снятия предмета.
	changeDeleted = "DELETED"
)

// Journal — объявление журнала compute.
//
// feedEnabled — флаг ленты модуля (`KACHO_COMPUTE_NOTIFICATIONS_ENABLED`), прочитанный
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
			// его основание». Основание снято: строка журнала
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
			// Словарь видов ЗАКРЫТ в обе стороны: вид вне его отвергается на
			// открытии, а строка вне его не доставляется — вопрос о видимости
			// задать нечем.
			//
			// СЛЕВА — слово ХРАНИЛИЩА (что лежит в колонке), СПРАВА — то, что
			// едет клиенту. Клиенту едет ТИП ОБЪЕКТА: написание вида одно на всё
			// дерево, и берётся оно у ПРОИЗВОДИТЕЛЯ (`authzfilter`), а не
			// выписывается — второе написание чужого словаря расходится молча.
			// Здесь оно и разошлось бы виднее всего: слово хранилища у этого
			// журнала — `Instance`, с заглавной и без домена, то есть ни на что
			// в дереве не похоже.
			//
			// Действие у каждого вида — то, которым сужается его список, поэтому
			// видимость в потоке равна видимости в списке.
			//
			// Форма имени и якорь объявлены у каждого вида (NTF-3, З2): все три
			// вида compute живут в проекте, и имя у каждого — DNS-метка (форму
			// держит ограничение `<таблица>_name_check` схемы). Значит строка
			// снятия обязана нести снимок имени под ключом
			// [subscription.NamePayloadKey] — его кладёт удаляющий оператор
			// репозитория из `RETURNING`, а не чтение до удаления.
			Kinds: map[string]subscription.Kind{
				JournalWordInstance: {
					ObjectType: authzfilter.ResourceTypeInstance,
					Action:     authzfilter.ActionInstanceRead,
					NameForm:   subscription.NameFormDNS,
					Scope:      subscription.ScopeProject,
				},
				JournalWordPlacementGroup: {
					ObjectType: authzfilter.ResourceTypePlacementGroup,
					Action:     authzfilter.ActionPlacementGroupRead,
					NameForm:   subscription.NameFormDNS,
					Scope:      subscription.ScopeProject,
				},
				JournalWordGuestAccessKey: {
					ObjectType: authzfilter.ResourceTypeGuestAccessKey,
					Action:     authzfilter.ActionGuestAccessKeyRead,
					NameForm:   subscription.NameFormDNS,
					Scope:      subscription.ScopeProject,
				},
			},
			// Словарь родов изменения — ровно те слова, которыми пишет
			// `emitCompute`. Слово вне словаря делает строку недоставляемой, и
			// это громко: расхождение журнала с объявлением есть дефект, а не
			// свойство подписки.
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
// Нагрузка удаления несёт идентификатор и снимок имени — полного состояния в
// ней нет и быть не может: предмета больше нет. Собрать из неё состояние вида
// значило бы отдать подписчику почти пустой предмет, а контракт формы разрешает
// читать НЕПУСТУЮ нагрузку как ПОЛНОЕ состояние предмета. Подписчик записал бы
// пустые поля как факт: имя исчезло, зона исчезла, метки исчезли.
//
// Поэтому род изменения спрашивается ЯВНО, а не выводится из бедности нагрузки:
// вывод из бедности сработал бы и на настоящем сбое сериализации, и два разных
// исхода стали бы неразличимы.
//
// Для снятия подписчику довольно оболочки: вид, идентификатор, якорь проекта и
// род изменения — этого хватает, чтобы убрать строку из своего состояния.
func state(r subscription.Row) (*anypb.Any, subscription.StateAbsence, error) {
	if r.Change == changeDeleted {
		// Причина НАЗВАНА: предмета больше нет, собирать было нечего, попытки не
		// было. «Не удалось сериализовать» здесь означало бы неудавшуюся попытку —
		// и звало бы подписчика перечитать снятую машину.
		return nil, subscription.StateNotProduced, nil
	}

	// Нагрузка записана тем же кодированием, каким читается (`domainToMap` —
	// обход `encoding/json` по доменной структуре), поэтому обратный ход
	// симметричен by construction. Проба этого не предполагает, а проверяет.
	//
	// Тип собираемого состояния выбирается ВИДОМ строки: нагрузка группы,
	// разобранная в машину, дала бы машину с одним именем и пустыми полями, и
	// подписчик записал бы их как факт. Вид вне перечня — отказ сборки, а не
	// умолчание: словарь видов закрыт, и такую строку сервер не доставляет.
	var msg proto.Message
	switch r.Kind {
	case JournalWordInstance:
		var in domain.Instance
		if err := json.Unmarshal(r.Payload, &in); err != nil {
			return nil, subscription.StateAbsenceUnnamed, fmt.Errorf("разбор нагрузки журнала: %w", err)
		}
		msg = protoconv.Instance(&in)
	case JournalWordPlacementGroup:
		var g domain.PlacementGroup
		if err := json.Unmarshal(r.Payload, &g); err != nil {
			return nil, subscription.StateAbsenceUnnamed, fmt.Errorf("разбор нагрузки журнала: %w", err)
		}
		msg = protoconv.PlacementGroup(&g)
	case JournalWordGuestAccessKey:
		var k domain.GuestAccessKey
		if err := json.Unmarshal(r.Payload, &k); err != nil {
			return nil, subscription.StateAbsenceUnnamed, fmt.Errorf("разбор нагрузки журнала: %w", err)
		}
		msg = protoconv.GuestAccessKey(&k)
	default:
		return nil, subscription.StateAbsenceUnnamed, fmt.Errorf("вид %q вне словаря журнала compute", r.Kind)
	}
	// НАСТОЯЩИЙ отказ сборки выше (состояние есть, собрать не удалось) получает
	// причину от сервера (`NOT_SERIALIZABLE`), и она остаётся отличимой от
	// «состояния не бывает» — действия у них противоположные.
	//
	// Тип НАЗВАН: `Any` несёт имя типа на проводе, и ключи нагрузки суть поля
	// контракта владельца. Свободной структуры здесь нет намеренно — её ключи
	// производились бы от имён идентификаторов Go, и обычный внутренний
	// рефактор молча ломал бы публичную нагрузку, а `buf breaking` этого не
	// увидел бы by construction.
	packed, err := anypb.New(msg)
	if err != nil {
		return nil, subscription.StateAbsenceUnnamed, err
	}
	return packed, subscription.StateAbsenceUnnamed, nil
}
