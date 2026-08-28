// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal

import (
	"reflect"
	"sort"
	"testing"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/authzfilter"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// TestJournalIsAcceptedByTheCommonServer — объявление судится ТЕМ ЖЕ судьёй,
// который судит его на подъёме. Своей копии правил здесь нет намеренно.
func TestJournalIsAcceptedByTheCommonServer(t *testing.T) {
	if err := Journal().Validate(); err != nil {
		t.Fatalf("общий сервер ОТВЕРГ объявление журнала nlb — процесс не поднялся бы:\n%v", err)
	}
}

// TestChannelIsNotDerivedFromTheTableName — канал назван отдельно от таблицы.
//
// nlb — тот самый случай, ради которого общая форма держит их разными полями:
// таблица схемо-квалифицирована, канал нет. Вывод одного из другого работал бы у
// большинства владельцев и молча ошибался здесь — то есть там, где ошибку не ищут.
func TestChannelIsNotDerivedFromTheTableName(t *testing.T) {
	j := Journal()
	if j.Storage.Table != "kacho_nlb.nlb_outbox" {
		t.Fatalf("таблица журнала %q — объявление разошлось с миграцией 0001", j.Storage.Table)
	}
	if j.Channel != "nlb_outbox" {
		t.Fatalf("канал %q расходится с тем, в который шлёт триггер (`nlb_outbox`): "+
			"поток не будился бы вовсе и жил бы одним холостым перепросом", j.Channel)
	}
	if j.Channel == j.Storage.Table {
		t.Fatal("канал совпал с именем таблицы — если он ВЫВЕДЕН из неё, у nlb это неверно")
	}
}

// TestLoadBalancerKindDoesNotInheritItsOwnJournalWord — слово журнала и тип
// модели прав у балансировщика РАЗНЫЕ, и это проверяется.
//
// У двух видов из трёх они совпадают дословно, поэтому различие третьего не
// видно на глаз. Вид, унаследовавший собственное журнальное слово как тип
// объекта, спрашивал бы модель не о том объекте — и остался бы «зелёным»:
// проверка вернула бы отказ, событие просто не доставлялось бы.
func TestLoadBalancerKindDoesNotInheritItsOwnJournalWord(t *testing.T) {
	kinds := Journal().Mapping.Kinds

	lb, ok := kinds[kachorepo.OutboxResourceLoadBalancer]
	if !ok {
		t.Fatalf("вида %q в словаре нет", kachorepo.OutboxResourceLoadBalancer)
	}
	if lb.ObjectType == kachorepo.OutboxResourceLoadBalancer {
		t.Fatalf("тип объекта балансировщика взят из ЖУРНАЛЬНОГО слова (%q); "+
			"модель прав знает его как %q, и вопрос уходил бы про несуществующий тип",
			lb.ObjectType, authzfilter.ResourceTypeLoadBalancer)
	}
	if lb.ObjectType != authzfilter.ResourceTypeLoadBalancer {
		t.Fatalf("тип объекта %q расходится с производителем %q",
			lb.ObjectType, authzfilter.ResourceTypeLoadBalancer)
	}

	// Положительный контроль: у двух остальных видов слово и тип совпадают
	// законно, и проверка выше не должна объявлять это дефектом.
	for word, want := range map[string]string{
		kachorepo.OutboxResourceListener:    authzfilter.ResourceTypeListener,
		kachorepo.OutboxResourceTargetGroup: authzfilter.ResourceTypeTargetGroup,
	} {
		got, ok := kinds[word]
		if !ok {
			t.Fatalf("вида %q в словаре нет", word)
		}
		if got.ObjectType != want {
			t.Fatalf("тип объекта %q для вида %q расходится с производителем %q",
				got.ObjectType, word, want)
		}
	}
	if len(kinds) != 3 {
		t.Fatalf("видов в словаре %d, а ограничение базы перечисляет три", len(kinds))
	}
}

// TestChangeWordsCoverTheDatabaseConstraint — словарь родов изменения покрывает
// ВСЕ слова, разрешённые ограничением базы.
//
// Перечень взят у ограничения, а не у сегодняшних эмиттеров: слово вне словаря
// делает строку недоставляемой, и потеря эта тихая. Историческая строка
// долгоживущей базы — законный вход, а не край.
func TestChangeWordsCoverTheDatabaseConstraint(t *testing.T) {
	changes := Journal().Mapping.Changes
	// Дословно из `CHECK (action IN (...))`, миграция 0001.
	for _, word := range []string{"CREATED", "UPDATED", "DELETED", "MOVED", "FAILED"} {
		if changes[word] == subscriptionv1.SubscriptionEvent_CHANGE_UNSPECIFIED {
			t.Errorf("слово %q разрешено ограничением базы, но словарём не названо — "+
				"строка с ним не доставляется, и потеря эта тихая", word)
		}
	}
	if got := changes[kachorepo.OutboxActionMoved]; got != subscriptionv1.SubscriptionEvent_UPDATED {
		t.Errorf("переезд между проектами отдан родом %v, ожидалась правка: "+
			"снятием он читался бы подписчиком как удаление ресурса, который жив", got)
	}
	if got := changes[kachorepo.OutboxActionDeleted]; got != subscriptionv1.SubscriptionEvent_DELETED {
		t.Errorf("снятие отдано родом %v — подписчик не убрал бы строку", got)
	}
}

// TestStateIsAbsentBecauseTheJournalCarriesNone — состояния нет НИ У ОДНОГО рода,
// и это не сбой.
//
// Отрицание здесь не вакуумно: оно утверждает конкретную пару — отсутствие БЕЗ
// ошибки. Ошибка означала бы «состояние есть, но собрать не удалось», и следующий
// читатель чинил бы несуществующую поломку.
func TestStateIsAbsentBecauseTheJournalCarriesNone(t *testing.T) {
	for _, word := range []string{"CREATED", "UPDATED", "DELETED", "MOVED"} {
		got, err := state(subscription.Row{
			Change:  word,
			Payload: []byte(`{"id":"nlb-1234567890abcdefg","projectId":"prj-1234567890abcdefg"}`),
		})
		if err != nil {
			t.Errorf("род %q: отсутствие состояния объявлено ОШИБКОЙ (%v); это свойство "+
				"журнала, а не сбой сборки", word, err)
		}
		if got != nil {
			t.Errorf("род %q: отдано состояние из МИНИМАЛЬНОГО снимка. Подписчик вправе "+
				"читать непустую нагрузку как ПОЛНОЕ состояние и записал бы как факт, "+
				"что у ресурса нет ни меток, ни целей, ни адреса", word)
		}
	}
}

// TestProjectGateTakesItsRefusalFromTheProducer — форма отказа стража взята у
// производителя форм скрытия, а не сочинена здесь.
func TestProjectGateTakesItsRefusalFromTheProducer(t *testing.T) {
	gate, err := ProjectGate()
	if err != nil {
		t.Fatalf("страж не собрался: %v", err)
	}
	const owner = "Project %s not found"
	if gate.NotFoundFormat != owner {
		t.Fatalf("форма отказа стража %q расходится с формой промаха владельца %q",
			gate.NotFoundFormat, owner)
	}
}

// TestKindDictionaryIsWhatTheClientCanName — то, что nlb объявляет клиенту, есть
// словарь ТИПОВ ОБЪЕКТА, а слова его журнала наружу не выходят.
//
// Утверждение различающее ровно на одном виде из трёх: у слушателя и группы
// целей слово журнала и тип модели совпадают дословно, и на них проба была бы
// зелена при любом устройстве. У балансировщика они РАЗНЫЕ, и именно он
// показывает, какое из двух написаний едет клиенту.
func TestKindDictionaryIsWhatTheClientCanName(t *testing.T) {
	got := Journal().KindDictionary()
	want := []string{
		authzfilter.ResourceTypeListener,
		authzfilter.ResourceTypeLoadBalancer,
		authzfilter.ResourceTypeTargetGroup,
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("словарь видов nlb %q, ожидался %q (типы объекта, лексикографически)", got, want)
	}
	if got := Journal().KindDictionary(); len(got) != 3 {
		t.Fatalf("видов в словаре %d, а журнал объявляет три", len(got))
	}
	for _, journalWord := range []string{
		kachorepo.OutboxResourceLoadBalancer,
		kachorepo.OutboxResourceListener,
		kachorepo.OutboxResourceTargetGroup,
	} {
		if journalWord == kachorepo.OutboxResourceLoadBalancer {
			// Слово журнала балансировщика типом объекта НЕ является — значит
			// его присутствие в словаре было бы дефектом.
			for _, k := range got {
				if k == journalWord {
					t.Fatalf("слово ЖУРНАЛА %q попало в словарь клиента", journalWord)
				}
			}
		}
	}
}
