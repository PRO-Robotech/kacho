// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal

import (
	"strings"
	"testing"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// probeEndpointBase — то же умолчание, что несёт ручка посадки
// (`KACHO_REGISTRY_ENDPOINT_BASE`) и конструктор use-case.
const probeEndpointBase = "registry.kacho.local"

// TestJournalIsAcceptedByTheCommonServer — объявление проходит суд общего
// сервера. Отказ здесь — отказ ПОДЪЁМА, и обнаруживать его первым запросом в
// бою нельзя.
func TestJournalIsAcceptedByTheCommonServer(t *testing.T) {
	if err := Journal(probeEndpointBase).Validate(); err != nil {
		t.Fatalf("объявление отвергнуто общим сервером: %v", err)
	}
}

// TestChannelIsNotDerivedFromTheTableName — канал назван ОТДЕЛЬНО, и у реестра
// это не украшение: таблица схемо-квалифицирована, канал — нет.
func TestChannelIsNotDerivedFromTheTableName(t *testing.T) {
	if !strings.Contains(Table, ".") {
		t.Fatalf("таблица %q не квалифицирована схемой — предмет пробы исчез", Table)
	}
	if Channel == Table {
		t.Fatalf("канал совпал с именем таблицы %q: `pg_notify` квалифицированного имени "+
			"не принимает, и вывод одного из другого молча промахнулся бы", Table)
	}
	if strings.Contains(Channel, ".") {
		t.Fatalf("канал %q квалифицирован схемой — `pg_notify` такое имя не разрешает", Channel)
	}
}

// TestTheChannelIsNotTheOneOfTheRightsQueue — канал ресурсного журнала НЕ тот,
// которым будят дренаж прав.
//
// Совпадение каналов разбудило бы чужого читателя на каждой строке: дренаж прав
// читает СВОЮ таблицу по голове партиции и на пустой выборке уходит обратно —
// то есть отказа не будет, будет холостая работа на каждую мутацию реестра.
func TestTheChannelIsNotTheOneOfTheRightsQueue(t *testing.T) {
	const rightsQueueChannel = "kacho_registry_outbox" // миграция 0001, `registry_outbox_notify`
	if Channel == rightsQueueChannel {
		t.Fatalf("ресурсный журнал будит канал очереди прав %q", rightsQueueChannel)
	}
}

// TestKindDictionaryNamesTheRegistryAndOnlyIt — словарь видов назван ОДНИМ
// предметом, и это решение, а не недоделка.
//
// Строку в базе реестра имеет ТОЛЬКО реестр: репозиторий и тег живут в zot, и
// транзакции, к которой можно пристегнуть эмиссию, у них нет. Вид, о видимости
// которого нельзя спросить модель прав, не доставляется — поток по нему молчал
// бы, оставаясь «зелёным».
func TestKindDictionaryNamesTheRegistryAndOnlyIt(t *testing.T) {
	got := Journal(probeEndpointBase).KindDictionary()
	if len(got) != 1 || got[0] != domain.FGAObjectTypeRegistry {
		t.Fatalf("словарь видов %v, ожидался ровно один — %q", got, domain.FGAObjectTypeRegistry)
	}
}

// TestKindTakesItsWordsFromTheProducers — тип объекта и действие взяты у
// производителей, а не выписаны здесь вторым написанием чужого словаря.
func TestKindTakesItsWordsFromTheProducers(t *testing.T) {
	binding, ok := Journal(probeEndpointBase).Mapping.Kinds[JournalWordRegistry]
	if !ok {
		t.Fatalf("слова журнала %q нет в словаре видов", JournalWordRegistry)
	}
	if binding.ObjectType != domain.FGAObjectTypeRegistry {
		t.Errorf("тип объекта %q, у производителя %q", binding.ObjectType, domain.FGAObjectTypeRegistry)
	}
	if binding.Action != domain.ActionRegistryList {
		t.Errorf("действие %q, у производителя %q", binding.Action, domain.ActionRegistryList)
	}
}

// TestProjectGateTakesItsRefusalFromTheProducer — форма отказа стража оси взята
// у производителя форм скрытия, а не сочинена здесь.
func TestProjectGateTakesItsRefusalFromTheProducer(t *testing.T) {
	gate, err := ProjectGate()
	if err != nil {
		t.Fatalf("страж не собрался: %v", err)
	}
	want, ok := authz.OwnerNotFoundFormat("project")
	if !ok {
		t.Fatal("у производителя нет формы отсутствия проекта — предмет пробы исчез")
	}
	if gate.NotFoundFormat != want {
		t.Errorf("форма отказа %q, у производителя %q", gate.NotFoundFormat, want)
	}
	if gate.ObjectType != "project" {
		t.Errorf("тип объекта стража %q, ожидался project", gate.ObjectType)
	}
	if len(gate.Relations) == 0 {
		t.Error("страж без отношений: ось осталась бы без суждения")
	}
}

// TestRemovalCarriesNoState — снятие отдаётся БЕЗ состояния, и род изменения
// спрашивается ЯВНО.
//
// Вывод «состояния нет, значит это снятие» сработал бы и на настоящем сбое
// разбора, и два разных исхода стали бы неразличимы.
func TestRemovalCarriesNoState(t *testing.T) {
	st, absence, err := Journal(probeEndpointBase).Mapping.State(subscription.Row{
		Kind:      JournalWordRegistry,
		ID:        "reg-0000000000000001",
		ProjectID: "prj-0000000000000001",
		Change:    changeDeleted,
		Payload:   []byte(`{"id":"reg-0000000000000001"}`),
	})
	if err != nil {
		t.Fatalf("снятие отдано с отказом: %v — отсутствие состояния у снятия есть свойство журнала, а не сбой", err)
	}
	if st != nil {
		t.Fatalf("снятие принесло состояние: подписчик прочитал бы почти пустой реестр как ПОЛНОЕ состояние предмета")
	}
	if absence != subscription.StateNotProduced {
		t.Fatalf("причина отсутствия у снятия %v, ожидалась StateNotProduced: неназванная "+
			"доезжает клиенту как REASON_UNSPECIFIED, и подписчик пошёл бы ПЕРЕЧИТЫВАТЬ "+
			"реестр, которого больше нет", absence)
	}
}

// TestStateAbsenceIsNamedByTheOwner — три исхода [stateWithEndpoint] различимы, и
// причину отсутствия называет ВЛАДЕЛЕЦ.
//
// # Почему все три сразу
//
// Порознь каждое утверждение вакуумно: «причина названа» зеленеет на владельце,
// который никогда не отдаёт состояния, «состояние собрано» — на владельце,
// который никогда его не теряет. Различает только таблица.
//
// # Что различают причины у ПОДПИСЧИКА
//
// `NOT_PRODUCED` — идти за предметом не нужно, его нет. `NOT_SERIALIZABLE` —
// разумно перечитать. Сведи их в одну, и половина подписчиков будет перечитывать
// снятые реестры вечно, а другая — молча терять живые.
func TestStateAbsenceIsNamedByTheOwner(t *testing.T) {
	state := Journal(probeEndpointBase).Mapping.State
	row := func(change, payload string) subscription.Row {
		return subscription.Row{
			Kind:      JournalWordRegistry,
			ID:        "reg-0000000000000001",
			ProjectID: "prj-0000000000000001",
			Change:    change,
			Payload:   []byte(payload),
		}
	}

	// Сборка удалась: причина НЕ называется — иначе владелец противоречит сам
	// себе, и общий сервер обязан на это пожаловаться.
	const live = `{"id":"reg-0000000000000001","project_id":"prj-0000000000000001",` +
		`"name":"probe","status":"ACTIVE","created_at":"2026-08-29T10:00:00Z",` +
		`"default_visibility":"PRIVATE","region_id":"reg-ru-1","placement_type":"REGIONAL"}`
	st, absence, err := state(row(changeCreated, live))
	if err != nil {
		t.Fatalf("законная нагрузка не собралась: %v — положительный контроль обязан "+
			"проходить, иначе отрицания зеленеют на всём сломанном", err)
	}
	if st == nil {
		t.Fatal("законная нагрузка дала пустое состояние без отказа")
	}
	if absence != subscription.StateAbsenceUnnamed {
		t.Errorf("при СОБРАННОМ состоянии названа причина его отсутствия (%v): объявление "+
			"владельца противоречит себе", absence)
	}

	// Настоящий отказ сборки: причину даёт СЕРВЕР (`NOT_SERIALIZABLE`). Назови её
	// владелец — поломка объявилась бы свойством журнала, и единственный её след
	// погас бы.
	st, absence, err = state(row(changeCreated, `{`))
	if err == nil {
		t.Fatal("испорченная нагрузка разобралась без отказа — отрицание ниже было бы вакуумным")
	}
	if st != nil {
		t.Error("при отказе сборки отдано состояние")
	}
	if absence != subscription.StateAbsenceUnnamed {
		t.Errorf("отказ сборки назван причиной %v: настоящая поломка объявлена свойством журнала", absence)
	}
}

// TestChangeDictionaryNamesEveryWordWithItsSubject — три рода изменения, каждый
// со своим смыслом у подписчика.
func TestChangeDictionaryNamesEveryWordWithItsSubject(t *testing.T) {
	changes := Journal(probeEndpointBase).Mapping.Changes
	for word, want := range map[string]subscriptionv1.SubscriptionEvent_Change{
		changeCreated: subscriptionv1.SubscriptionEvent_CREATED,
		changeUpdated: subscriptionv1.SubscriptionEvent_UPDATED,
		changeDeleted: subscriptionv1.SubscriptionEvent_DELETED,
	} {
		if got := changes[word]; got != want {
			t.Errorf("род %q отдан как %v, ожидалось %v", word, got, want)
		}
	}
}
