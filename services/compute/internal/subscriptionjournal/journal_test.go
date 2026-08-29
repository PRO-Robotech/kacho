// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal

import (
	"encoding/json"
	"sort"
	"testing"
	"time"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
)

// TestJournalIsAcceptedByTheCommonServer — объявление судится ТЕМ ЖЕ судьёй,
// который судит его на подъёме.
//
// Своей копии правил здесь нет намеренно: копия разошлась бы с общим сервером
// молча, и объявление, которое он отвергнет в бою, оставалось бы зелёным.
func TestJournalIsAcceptedByTheCommonServer(t *testing.T) {
	if err := Journal().Validate(); err != nil {
		t.Fatalf("общий сервер ОТВЕРГ объявление журнала compute — процесс не поднялся бы:\n%v", err)
	}
}

// TestProjectAnchorIsAColumnNotAPayloadParse — ось проекта отбирается ЗАПРОСОМ.
//
// Это не вкус раскладки, а условие того, что удаления доезжают: нагрузка снятия
// несёт один идентификатор, поэтому разбор нагрузки дал бы у неё пустой якорь —
// «предмет уровня аккаунта», — и подписка с осью `project_id` такие события
// молча не пропускала бы.
func TestProjectAnchorIsAColumnNotAPayloadParse(t *testing.T) {
	j := Journal()
	if j.Storage.Project != subscription.ProjectInColumn {
		t.Fatalf("якорь проекта объявлен не колонкой (%v): у события снятия нагрузка "+
			"несёт один идентификатор, и разбор дал бы пустой якорь — то есть "+
			"утверждение «предмет уровня аккаунта», ложное для машины",
			j.Storage.Project)
	}
	if j.Storage.ProjectColumn == "" {
		t.Fatal("колонка якоря не названа")
	}
	if j.Mapping.Anchor != nil {
		t.Fatal("объявлено ДВА источника якоря — колонка и отображение; они разойдутся молча")
	}
}

// TestKindsCarryTheProducersOwnAuthzWords — тип объекта и действие взяты у
// производителя, а не выписаны второй раз.
//
// Второе написание чужого словаря расходится молча и расходится там, где это не
// видно: поток продолжает отвечать, но спрашивает модель о неверном действии.
func TestKindsCarryTheProducersOwnAuthzWords(t *testing.T) {
	kinds := Journal().Mapping.Kinds
	if len(kinds) != 1 {
		t.Fatalf("видов в словаре %d, ожидался один — блочное хранение ушло из compute "+
			"миграцией 0021, и его виды больше не принадлежат этому журналу", len(kinds))
	}
	got, ok := kinds[JournalWordInstance]
	if !ok {
		t.Fatalf("вида %q в словаре нет", JournalWordInstance)
	}
	if got.ObjectType != authzfilter.ResourceTypeInstance {
		t.Fatalf("тип объекта %q расходится с производителем %q",
			got.ObjectType, authzfilter.ResourceTypeInstance)
	}
	if got.Action != authzfilter.ActionInstanceRead {
		t.Fatalf("действие %q расходится с тем, которым сужается список машин (%q): "+
			"видимость в потоке обязана равняться видимости в списке",
			got.Action, authzfilter.ActionInstanceRead)
	}
}

// TestStateRoundTripsAFullInstance — нагрузка события правки восстанавливается в
// ПОЛНОЕ состояние, и это ПРОВЕРЕНО, а не предположено.
//
// Симметрия записи и чтения здесь не очевидна: нагрузка пишется обходом
// `encoding/json` по доменной структуре, у которой большинство полей без
// объявленных имён, а часть типов — целочисленные перечисления. Проба подаёт то
// же кодирование, которым пишет `emitCompute`, и требует, чтобы несущие поля
// пережили круг.
func TestStateRoundTripsAFullInstance(t *testing.T) {
	in := &domain.Instance{
		ID:        "epd-1234567890abcdefg",
		ProjectID: "prj-1234567890abcdefg",
		Name:      "web-1",
		ZoneID:    "ru-central1-a",
		Labels:    map[string]string{"env": "prod"},
		Status:    domain.InstanceStatusRunning,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	// То же кодирование, которым пишет репозиторий: сперва в набор, затем в
	// байты. Подать сюда структуру напрямую значило бы проверить другой путь.
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("подготовка нагрузки: %v", err)
	}
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err != nil {
		t.Fatalf("подготовка нагрузки: %v", err)
	}
	payload, err := json.Marshal(asMap)
	if err != nil {
		t.Fatalf("подготовка нагрузки: %v", err)
	}

	got, absence, err := state(subscription.Row{Change: "UPDATED", Payload: payload})
	if err != nil {
		t.Fatalf("состояние не собралось: %v", err)
	}
	if got == nil {
		t.Fatal("состояние отсутствует у события правки — подписчик остался бы без предмета")
	}
	// Причина отсутствия при СОБРАННОМ состоянии — противоречие в объявлении
	// владельца: сервер пишет на него жалобу, и заводить его здесь незачем.
	if absence != subscription.StateAbsenceUnnamed {
		t.Fatalf("вместе с состоянием названа причина его отсутствия (%v) — объявление "+
			"противоречит себе", absence)
	}

	var out computev1.Instance
	if err := got.UnmarshalTo(&out); err != nil {
		t.Fatalf("нагрузка не разбирается как объявленный тип: %v", err)
	}
	if out.Id != in.ID || out.ProjectId != in.ProjectID || out.Name != in.Name || out.ZoneId != in.ZoneID {
		t.Fatalf("несущие поля не пережили круг: id=%q project=%q name=%q zone=%q",
			out.Id, out.ProjectId, out.Name, out.ZoneId)
	}
	if out.Labels["env"] != "prod" {
		t.Fatalf("метки не пережили круг: %v — клиентский отбор по меткам остался бы без источника", out.Labels)
	}
}

// TestStateIsAbsentForRemoval — снятие отдаётся БЕЗ состояния.
//
// Отрицание в паре с положительным контролем выше: без него проба зеленела бы на
// отображении, которое не отдаёт состояния НИКОГДА.
func TestStateIsAbsentForRemoval(t *testing.T) {
	got, absence, err := state(subscription.Row{
		Change:  changeDeleted,
		Payload: []byte(`{"id":"epd-1234567890abcdefg"}`),
	})
	if err != nil {
		t.Fatalf("снятие объявлено ОШИБКОЙ сборки состояния (%v), а это не сбой: "+
			"предмета больше нет, и событие обязано доехать", err)
	}
	if got != nil {
		t.Fatal("у снятия отдано состояние: подписчик вправе читать непустую нагрузку " +
			"как ПОЛНОЕ состояние предмета и записал бы пустые поля как факт — " +
			"имя исчезло, зона исчезла, метки исчезли")
	}
	if absence != subscription.StateNotProduced {
		t.Fatalf("причина отсутствия у снятия %v, ожидалась StateNotProduced: собирать "+
			"было нечего, попытки не было — «не удалось сериализовать» звало бы "+
			"подписчика перечитать снятую машину", absence)
	}
}

// TestAnUnreadablePayloadStaysAFailure — ВТОРАЯ ПОЛОСА той же развилки.
//
// Без неё утверждение выше зеленело бы на отображении, которое называет
// StateNotProduced ВСЕГДА, — то есть на подмене одной неразличимости другой.
// Здесь состояние ЕСТЬ и собрать его не удалось: причину такому исходу даёт
// сервер, и владелец обязан оставить её неназванной, а не подшить к свойству
// журнала.
func TestAnUnreadablePayloadStaysAFailure(t *testing.T) {
	got, absence, err := state(subscription.Row{
		Change:  "UPDATED",
		Payload: []byte(`"не объект"`),
	})
	if err == nil {
		t.Fatal("негодная нагрузка прошла как успех — отказ сборки перестал быть отличим " +
			"от свойства журнала")
	}
	if got != nil {
		t.Fatal("при отказе сборки отдано состояние")
	}
	if absence != subscription.StateAbsenceUnnamed {
		t.Fatalf("отказ сборки назван причиной %v — сбой объявлен свойством журнала, и "+
			"подписчик перестал бы перечитывать там, где перечитать и надо", absence)
	}
}

// TestChangeWordsCoverExactlyWhatTheJournalWrites — словарь родов изменения
// сходится со словами, которыми пишет репозиторий.
//
// Слово вне словаря делает строку НЕДОСТАВЛЯЕМОЙ, и потеря эта тихая: ни отказа,
// ни пропуска в нумерации у клиента.
func TestChangeWordsCoverExactlyWhatTheJournalWrites(t *testing.T) {
	changes := Journal().Mapping.Changes
	for _, word := range []string{"CREATED", "UPDATED", "DELETED"} {
		if changes[word] == subscriptionv1.SubscriptionEvent_CHANGE_UNSPECIFIED {
			t.Errorf("слово %q журнала не названо словарём — строки с ним не доставляются", word)
		}
	}
	if len(changes) != 3 {
		t.Errorf("родов изменения объявлено %d, а репозиторий пишет три: лишняя запись "+
			"переживёт свой предмет и будет читаться как способность журнала", len(changes))
	}
}

// TestProjectGateTakesItsRefusalFromTheProducer — форма отказа стража не сочинена
// здесь, а взята у производителя форм скрытия.
//
// Различимый текст превращает подписку в способ узнать существование чужого
// проекта — то есть ровно в то, что скрытие закрывает.
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
	// Судится ТЕМ ЖЕ судьёй, что и на подъёме: своя копия правил разошлась бы молча.
	cfgJournal := Journal()
	cfgJournal.Storage.Project = subscription.ProjectInColumn
	if err := cfgJournal.Validate(); err != nil {
		t.Fatalf("объявление отвергнуто: %v", err)
	}
}

// TestBackfillKeyMatchesWhatTheJournalActuallyWrites — ключ, по которому миграция
// заполняет якорь у ИСТОРИЧЕСКИХ строк, совпадает с тем, что пишет репозиторий.
//
// Обратное заполнение читает `payload->>'ProjectID'`, и это имя ПОЛЯ Go, а не
// принятое в контракте `projectId`: нагрузка пишется обходом `encoding/json` по
// доменной структуре, у которой поле проекта объявлено без своего имени. Ошибись
// здесь — миграция прошла бы успешно и не заполнила НИ ОДНОЙ строки, а заметить
// это было бы нечем: у исторических строк пустой якорь законен и сам по себе.
//
// Проба пиннит именно ключ, потому что он живёт в ДВУХ местах — в SQL миграции и
// в структуре Go, — и второе меняется обычным переименованием поля.
func TestBackfillKeyMatchesWhatTheJournalActuallyWrites(t *testing.T) {
	const backfillKey = "ProjectID" // дословно из `..._compute_outbox_project_anchor.sql`

	raw, err := json.Marshal(&domain.Instance{
		ID:        "epd-1234567890abcdefg",
		ProjectID: "prj-1234567890abcdefg",
	})
	if err != nil {
		t.Fatalf("подготовка нагрузки: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("подготовка нагрузки: %v", err)
	}
	got, ok := m[backfillKey].(string)
	if !ok {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t.Fatalf("ключа %q в нагрузке НЕТ — обратное заполнение миграции прошло бы "+
			"успехом и не заполнило ни одной строки, а заметить это нечем: у "+
			"исторических строк пустой якорь законен сам по себе.\nКлючи нагрузки: %v",
			backfillKey, keys)
	}
	if got != "prj-1234567890abcdefg" {
		t.Fatalf("по ключу %q лежит %q", backfillKey, got)
	}
}
