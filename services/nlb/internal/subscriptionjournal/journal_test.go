// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/dto"
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

// TestListenerStateIsTheContractSubjectAndCarriesLabels — у вида `nlb_listener`
// событие несёт ПОЛНОЕ состояние предмета, и метки в нём есть.
//
// Утверждение сделано СРАВНЕНИЕМ С ЧТЕНИЕМ, а не перечислением полей: состояние
// обязано совпасть с тем, что по той же записи отдаёт обычный путь чтения. Список
// полей разошёлся бы с контрактом молча при первом же добавлении поля — а
// совпадение с трансфером чтения ломается ровно тогда, когда журнал заводит
// ВТОРУЮ проекцию ресурса.
func TestListenerStateIsTheContractSubjectAndCarriesLabels(t *testing.T) {
	rec := probeListenerRecord()

	var want *lbv1.Listener
	if err := dto.Transfer(dto.FromTo(*rec, &want)); err != nil {
		t.Fatalf("путь чтения не смог перенести запись в контракт: %v — сравнивать не с чем", err)
	}

	for _, word := range []string{kachorepo.OutboxActionCreated, kachorepo.OutboxActionUpdated} {
		got, absence, err := state(subscription.Row{
			Kind:    kachorepo.OutboxResourceListener,
			Change:  word,
			Payload: probeListenerPayload(t, rec),
		})
		if err != nil {
			t.Fatalf("род %q: состояние не собралось (%v)", word, err)
		}
		if got == nil {
			t.Fatalf("род %q: состояние НЕ отдано (причина %v) — клиентский отбор по меткам "+
				"для слушателя остаётся без источника", word, absence)
		}
		var have lbv1.Listener
		if err := got.UnmarshalTo(&have); err != nil {
			t.Fatalf("род %q: в конверте состояния не контракт слушателя: %v", word, err)
		}
		if !proto.Equal(want, &have) {
			t.Fatalf("род %q: состояние потока разошлось с чтением.\nчтение: %v\nпоток:  %v",
				word, want, &have)
		}
		if have.GetLabels()["env"] != "prod" {
			t.Fatalf("род %q: метки не доехали (%v) — клиентский отбор по меткам остался бы "+
				"без источника", word, have.GetLabels())
		}
	}
}

// TestListenerRemovalCarriesNoStateBecauseThereIsNoSubject — у снятия состояния
// нет и быть не может: предмета больше нет.
//
// Причина обязана быть НАЗВАНА: «не удалось собрать» означало бы неудавшуюся
// попытку там, где попытки не было, и звало бы подписчика перечитать снятый
// слушатель.
func TestListenerRemovalCarriesNoStateBecauseThereIsNoSubject(t *testing.T) {
	got, absence, err := state(subscription.Row{
		Kind:    kachorepo.OutboxResourceListener,
		Change:  kachorepo.OutboxActionDeleted,
		Payload: probeListenerPayload(t, probeListenerRecord()),
	})
	if err != nil {
		t.Fatalf("снятие: отсутствие состояния объявлено ОШИБКОЙ (%v)", err)
	}
	if got != nil {
		t.Fatal("снятие: отдано состояние предмета, которого больше нет")
	}
	if absence != subscription.StateNotProduced {
		t.Fatalf("снятие: причина отсутствия %v, ожидалась StateNotProduced", absence)
	}
}

// TestListenerRowOfTheOldShapeIsNotPassedOffAsFullState — строка, записанная ДО
// обогащения, состоянием НЕ становится.
//
// Журнал nlb не чистится (`RetainsEverything`), а подписчик вправе открыть поток
// с начала — значит МИНИМАЛЬНЫЕ снимки прежней формы доезжают до сборщика и
// сегодня. Разбор их молча НЕ отвергнет: `encoding/json` сопоставляет имена без
// учёта регистра, поэтому `id`/`name`/`protocol`/`port`/`status` прежней нагрузки
// попали бы в поля записи, а `project_id`, родитель, отметка создания и МЕТКИ —
// нет. Получился бы разбираемый, переносимый и ЛОЖНЫЙ предмет: слушатель без
// проекта и без меток. Поэтому полноту объявляет КОНВЕРТ, а не удача разбора.
func TestListenerRowOfTheOldShapeIsNotPassedOffAsFullState(t *testing.T) {
	old := []byte(`{"id":"nlb-l-1","project_id":"prj-1","parent_resource_id":"nlb-1",` +
		`"name":"front","protocol":"TCP","port":443,"status":"ACTIVE"}`)

	got, absence, err := state(subscription.Row{
		Kind:    kachorepo.OutboxResourceListener,
		Change:  kachorepo.OutboxActionUpdated,
		Payload: old,
	})
	if err != nil {
		t.Fatalf("строка прежней формы объявлена СБОЕМ сборки (%v) — сбоя не было, "+
			"состояние в ней просто не производилось", err)
	}
	if got != nil {
		var have lbv1.Listener
		_ = got.UnmarshalTo(&have)
		t.Fatalf("минимальный снимок отдан как ПОЛНОЕ состояние (%v): подписчик записал бы "+
			"как факт, что у слушателя нет ни проекта, ни меток", &have)
	}
	if absence != subscription.StateNotProduced {
		t.Fatalf("причина отсутствия %v, ожидалась StateNotProduced", absence)
	}
}

// TestKindsWithoutTheirOwnStateSayItIsNotProduced — у двух остальных видов nlb
// состояния НЕТ, и это по-прежнему сказано ПРИЗНАКОМ.
//
// Отрицание здесь не вакуумно: рядом стоит положительный контроль (слушатель
// выше), поэтому «состояния нет» отличимо от «сборщик сломан целиком». Оно
// утверждает тройку — отсутствие, отсутствие ошибки И названную причину:
// неназванная уехала бы клиенту как `REASON_UNSPECIFIED`, то есть «владелец
// забыл».
func TestKindsWithoutTheirOwnStateSayItIsNotProduced(t *testing.T) {
	for _, kind := range []string{
		kachorepo.OutboxResourceLoadBalancer,
		kachorepo.OutboxResourceTargetGroup,
	} {
		for _, word := range []string{"CREATED", "UPDATED", "DELETED", "MOVED"} {
			got, absence, err := state(subscription.Row{
				Kind:    kind,
				Change:  word,
				Payload: []byte(`{"id":"nlb-1234567890abcdefg","projectId":"prj-1234567890abcdefg"}`),
			})
			if err != nil {
				t.Errorf("вид %q род %q: отсутствие состояния объявлено ОШИБКОЙ (%v); это "+
					"свойство журнала, а не сбой сборки", kind, word, err)
			}
			if got != nil {
				t.Errorf("вид %q род %q: отдано состояние из МИНИМАЛЬНОГО снимка. Подписчик "+
					"вправе читать непустую нагрузку как ПОЛНОЕ состояние и записал бы как "+
					"факт, что у ресурса нет ни меток, ни целей, ни адреса", kind, word)
			}
			if absence != subscription.StateNotProduced {
				t.Errorf("вид %q род %q: причина отсутствия %v, ожидалась StateNotProduced — "+
					"неназванная причина доезжает клиенту как «владелец забыл назвать»",
					kind, word, absence)
			}
		}
	}
}

// probeListenerRecord — запись слушателя, заполненная ЦЕЛИКОМ.
//
// Целиком — потому что предмет проб «полное состояние»: запись с пустыми полями
// зеленела бы на сборщике, который их теряет.
func probeListenerRecord() *kachorepo.ListenerRecord {
	rec := &kachorepo.ListenerRecord{
		CreatedAt: time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 8, 30, 12, 30, 0, 0, time.UTC),
		Xmin:      "4242",
	}
	rec.ID = domain.ResourceID("nlb-l-1234567890abcd")
	rec.ProjectID = domain.ProjectID("prj-1234567890abcdef")
	rec.LoadBalancerID = domain.ResourceID("nlb-1234567890abcdef")
	rec.RegionID = domain.RegionID("ru-central1")
	rec.Name = domain.LbName("front")
	rec.Description = domain.LbDescription("витрина")
	rec.Labels = domain.LabelsFromMap(map[string]string{"env": "prod"})
	rec.Protocol = domain.ProtoTCP
	rec.Port = domain.LbPort(443)
	rec.Status = domain.ListenerStatusActive
	return rec
}

// probeListenerPayload — нагрузка строки журнала, собранная ТЕМ ЖЕ строителем,
// каким её собирает эмиттер.
//
// Своя копия формы здесь была бы кругом: обе стороны брали бы её из пробы, и
// расхождение эмиттера со сборщиком осталось бы невидимым.
func probeListenerPayload(t *testing.T, rec *kachorepo.ListenerRecord) []byte {
	t.Helper()
	raw, err := json.Marshal(kachorepo.StateEnvelope(rec))
	if err != nil {
		t.Fatalf("нагрузка не собралась: %v", err)
	}
	return raw
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

// TestTargetGroupStateIsTheContractSubjectAndCarriesTargetsWithState — у вида
// `nlb_target_group` событие несёт ПОЛНОЕ состояние группы, и оно совпадает с
// тем, что отдаёт чтение.
//
// # Почему цели названы отдельно
//
// Публичная проекция группы строится НЕ из её строки, а из набора целей С
// СОСТОЯНИЕМ. Строка, собранная из одной лишь записи группы, разобралась бы,
// перенеслась в контракт и объявила группу БЕЗ ЦЕЛЕЙ — то есть дала бы предмет,
// отличный от ответа `Get`, причём отличие читалось бы подписчиком как факт
// («все цели удалены»), а не как неполнота.
//
// # Почему сверка идёт с ЧТЕНИЕМ, а не со списком полей
//
// Перечень полей здесь был бы вторым отображением записи в контракт: он
// разошёлся бы с первым молча. Сверяется то же, что отдаёт `Get`, — одним и тем
// же трансфером.
func TestTargetGroupStateIsTheContractSubjectAndCarriesTargetsWithState(t *testing.T) {
	rec := probeTargetGroupRecord()

	var want *lbv1.TargetGroup
	if err := dto.Transfer(dto.FromTo(*rec, &want)); err != nil {
		t.Fatalf("эталон чтения не собрался: %v", err)
	}

	for _, word := range []string{
		kachorepo.OutboxActionCreated,
		kachorepo.OutboxActionUpdated,
		kachorepo.OutboxActionMoved,
	} {
		got, absence, err := state(subscription.Row{
			Kind:    kachorepo.OutboxResourceTargetGroup,
			Change:  word,
			Payload: probeTargetGroupPayload(t, rec),
		})
		if err != nil {
			t.Fatalf("род %q: состояние не собралось (%v)", word, err)
		}
		if got == nil {
			t.Fatalf("род %q: состояние НЕ отдано (причина %v) — клиентский отбор по меткам "+
				"для группы остаётся без источника", word, absence)
		}
		var have lbv1.TargetGroup
		if err := got.UnmarshalTo(&have); err != nil {
			t.Fatalf("род %q: в конверте состояния не контракт группы: %v", word, err)
		}
		if !proto.Equal(want, &have) {
			t.Fatalf("род %q: состояние потока разошлось с чтением.\nчтение: %v\nпоток:  %v",
				word, want, &have)
		}
		if len(have.GetTargets()) == 0 {
			t.Fatalf("род %q: состояние пришло БЕЗ целей. Пустой набор читается подписчиком "+
				"как «целей нет», а не как «это событие поле не заполняет», и клиент, ведущий "+
				"состояние, предложит создать их заново", word)
		}
		if have.GetLabels()["env"] != "prod" {
			t.Fatalf("род %q: метки не доехали (%v) — клиентский отбор по меткам остался бы "+
				"без источника", word, have.GetLabels())
		}
	}
}

// TestTargetGroupRemovalCarriesNoStateBecauseThereIsNoSubject — у снятия группы
// состояния нет и быть не может.
func TestTargetGroupRemovalCarriesNoStateBecauseThereIsNoSubject(t *testing.T) {
	got, absence, err := state(subscription.Row{
		Kind:    kachorepo.OutboxResourceTargetGroup,
		Change:  kachorepo.OutboxActionDeleted,
		Payload: probeTargetGroupPayload(t, probeTargetGroupRecord()),
	})
	if err != nil {
		t.Fatalf("снятие: отсутствие состояния объявлено ОШИБКОЙ (%v)", err)
	}
	if got != nil {
		t.Fatal("снятие: отдано состояние предмета, которого больше нет")
	}
	if absence != subscription.StateNotProduced {
		t.Fatalf("снятие: причина отсутствия %v, ожидалась StateNotProduced", absence)
	}
}

// probeTargetGroupRecord — запись группы, заполненная ЦЕЛИКОМ, включая набор
// целей С СОСТОЯНИЕМ: запись с пустыми полями зеленела бы на сборщике, который
// их теряет.
func probeTargetGroupRecord() *kachorepo.TargetGroupRecord {
	rec := &kachorepo.TargetGroupRecord{
		CreatedAt: time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 8, 30, 12, 30, 0, 0, time.UTC),
		Xmin:      "4242",
	}
	rec.ID = domain.ResourceID("tgr-1234567890abcde")
	rec.ProjectID = domain.ProjectID("prj-1234567890abcdef")
	rec.RegionID = domain.RegionID("ru-central1")
	rec.Name = domain.LbName("back")
	rec.Description = domain.LbDescription("бэкенды")
	rec.Labels = domain.LabelsFromMap(map[string]string{"env": "prod"})
	rec.Status = domain.TargetGroupStatusActive
	rec.Port = 8080
	rec.DeregistrationDelay = domain.LbDuration(300 * time.Second)
	rec.HealthCheck = domain.HealthCheck{
		Interval:           domain.DefaultHealthInterval,
		Timeout:            domain.DefaultHealthTimeout,
		UnhealthyThreshold: domain.DefaultUnhealthyThreshold,
		HealthyThreshold:   domain.DefaultHealthyThreshold,
		TCP:                &domain.HealthCheckTCP{Port: 80},
	}
	drainedAt := time.Date(2026, 8, 30, 12, 15, 0, 0, time.UTC)
	// Две цели, и одна из них СЛИВАЕТСЯ: без lifecycle-состояния она неотличима
	// от обычной, а именно ради этого различия проекция строится из набора С
	// состоянием.
	rec.TargetStates = []kachorepo.TargetRecord{
		{
			Target:        domain.Target{ExternalIP: &domain.TargetExternalIP{Address: "203.0.113.10"}, Weight: 100},
			ID:            "tgt-live",
			TargetGroupID: string(rec.ID),
			Status:        kachorepo.TargetStatusActive,
			CreatedAt:     rec.CreatedAt,
			UpdatedAt:     rec.UpdatedAt,
		},
		{
			Target:         domain.Target{ExternalIP: &domain.TargetExternalIP{Address: "203.0.113.11"}, Weight: 50},
			ID:             "tgt-draining",
			TargetGroupID:  string(rec.ID),
			Status:         kachorepo.TargetStatusDraining,
			DrainStartedAt: &drainedAt,
			CreatedAt:      rec.CreatedAt,
			UpdatedAt:      rec.UpdatedAt,
		},
	}
	for _, ts := range rec.TargetStates {
		rec.Targets = append(rec.Targets, ts.Target)
	}
	return rec
}

// probeTargetGroupPayload — нагрузка строки журнала, собранная ТЕМ ЖЕ
// строителем, каким её собирает эмиттер.
func probeTargetGroupPayload(t *testing.T, rec *kachorepo.TargetGroupRecord) []byte {
	t.Helper()
	raw, err := json.Marshal(kachorepo.TargetGroupStatePayload(rec))
	if err != nil {
		t.Fatalf("нагрузка не собралась: %v", err)
	}
	return raw
}
