// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/storage/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/storage/internal/subscriptionjournal"
)

// TestJournalIsAcceptedByTheCommonServer — объявление владельца проходит суд
// общей формы.
//
// Судится ОБЪЯВЛЕНИЕ, а не дерево: существует ли таблица и та ли у неё форма —
// вопрос подъёма, и на него отвечают интеграционные пробы этого же пакета
// вызовом. Здесь закрывается то, что после подъёма закрыть уже нечем:
// незаявленная ось, разошедшиеся половины одного решения и имя, которое нельзя
// безопасно поставить в запрос.
func TestJournalIsAcceptedByTheCommonServer(t *testing.T) {
	if err := subscriptionjournal.Journal().Validate(); err != nil {
		t.Fatalf("общий сервер не принял объявление журнала storage: %v", err)
	}
}

// TestProjectAxisIsAnsweredByTheColumn — ось проекта отбирается ЗАПРОСОМ.
//
// У всех трёх видов storage якорь лежит колонкой `project_id` самой строки
// ресурса, поэтому он назначается триггером из `NEW.project_id`/`OLD.project_id`
// и у СНЯТИЯ есть так же, как у создания. Разбор нагрузки дал бы у снятий пустой
// якорь — то есть утверждение «предмет уровня аккаунта», ложное для тома, — и
// подписка с осью проекта такие события молча не пропускала бы.
func TestProjectAxisIsAnsweredByTheColumn(t *testing.T) {
	st := subscriptionjournal.Journal().Storage
	if st.Project != subscription.ProjectInColumn {
		t.Fatalf("ось проекта объявлена как %d, ожидалась колонка: иначе якорь у снятия "+
			"брать неоткуда, и потребитель держал бы удалённый том вечно", st.Project)
	}
	if st.ProjectColumn != "project_id" {
		t.Fatalf("колонка якоря названа %q; в схеме storage она `project_id` "+
			"(0003 для volumes/snapshots, 0007 для images)", st.ProjectColumn)
	}
}

// TestKindWordsCarryTheProducersObjectTypes — вид на проводе есть ТИП ОБЪЕКТА
// модели прав, взятый у производителя.
//
// Слово журнала (`Volume`) и тип объекта (`storage_volume`) здесь РАЗНЫЕ, и это
// то самое расхождение, ради которого вид едет типом: второе написание предмета
// клиенту завести некуда, а тип уже есть у `authzfilter` — им же спрашивают о
// видимости строки.
func TestKindWordsCarryTheProducersObjectTypes(t *testing.T) {
	want := map[string]struct{ object, action string }{
		subscriptionjournal.JournalWordVolume: {
			authzfilter.ResourceTypeVolume, authzfilter.ActionVolumeList,
		},
		subscriptionjournal.JournalWordSnapshot: {
			authzfilter.ResourceTypeSnapshot, authzfilter.ActionSnapshotList,
		},
		subscriptionjournal.JournalWordImage: {
			authzfilter.ResourceTypeImage, authzfilter.ActionImageList,
		},
	}

	kinds := subscriptionjournal.Journal().Mapping.Kinds
	if len(kinds) != len(want) {
		t.Fatalf("видов объявлено %d, ожидалось %d: три тенантных предмета storage, "+
			"несущих проектное измерение", len(kinds), len(want))
	}
	for word, exp := range want {
		got, ok := kinds[word]
		if !ok {
			t.Errorf("вид %q не объявлен — строка журнала с ним НЕ доставляется, "+
				"и потеря эта тихая", word)
			continue
		}
		if got.ObjectType != exp.object {
			t.Errorf("вид %q едет клиенту как %q, а тип объекта модели прав — %q: "+
				"вопрос о видимости уходил бы про несуществующий тип",
				word, got.ObjectType, exp.object)
		}
		if got.Action != exp.action {
			t.Errorf("вид %q авторизуется действием %q, а список того же предмета — %q: "+
				"видимость в потоке обязана равняться видимости в списке",
				word, got.Action, exp.action)
		}
	}
}

// TestAdministrativeKindsAreExcludedByName — типы дисков, бэкенды хранения и
// привязки типов в словарь НЕ входят, и это требование, а не пробел.
//
// У них нет проектного измерения: колонки `project_id` в их таблицах не
// существует (`0003_storage_domain.sql` для disk_types, `0015_storage_backends_
// and_bindings.sql` для остальных двух). Значит якорь брать неоткуда, а без
// якоря вопрос «вправе ли вызывающий это видеть» задать НЕЧЕМ — сервер такую
// строку не доставил бы, оставаясь зелёным.
//
// Проба утверждает ОТСУТСТВИЕ, поэтому рядом стоит положительный контроль: три
// тенантных вида обязаны быть на месте. Без него утверждение зеленело бы на
// пустом словаре, а пустой словарь общая форма и так отвергает — то есть проба
// не различала бы ничего.
func TestAdministrativeKindsAreExcludedByName(t *testing.T) {
	kinds := subscriptionjournal.Journal().Mapping.Kinds

	tenantFacing := 0
	for _, word := range []string{
		subscriptionjournal.JournalWordVolume,
		subscriptionjournal.JournalWordSnapshot,
		subscriptionjournal.JournalWordImage,
	} {
		if _, ok := kinds[word]; ok {
			tenantFacing++
		}
	}
	if tenantFacing != 3 {
		t.Fatalf("положительный контроль не выполнен: тенантных видов в словаре %d из 3 — "+
			"утверждение об отсутствии административных зеленело бы на пустоте", tenantFacing)
	}

	for word, binding := range kinds {
		lower := strings.ToLower(word + " " + binding.ObjectType)
		for _, forbidden := range []string{"disktype", "disk_type", "backend", "binding"} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("в словаре объявлен административный предмет (%q → %q): его таблица "+
					"не несёт project_id, якорь брать неоткуда, и строка не доставилась бы "+
					"вовсе", word, binding.ObjectType)
			}
		}
	}
}

// TestKindDictionaryIsSortedAndUnique — перечень видов, который сервер называет
// на открытии, упорядочен и без повторов.
func TestKindDictionaryIsSortedAndUnique(t *testing.T) {
	got := subscriptionjournal.Journal().KindDictionary()
	want := []string{
		authzfilter.ResourceTypeImage,
		authzfilter.ResourceTypeSnapshot,
		authzfilter.ResourceTypeVolume,
	}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("словарь видов на проводе %v, ожидался %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("словарь видов на проводе %v, ожидался %v", got, want)
		}
	}
}

// TestProjectGateBorrowsTheOwnersNotFoundForm — форма отказа стража берётся у
// ПРОИЗВОДИТЕЛЯ форм скрытия.
//
// Отличимый текст здесь есть оракул существования чужого проекта: по нему
// отличают «нет доступа» от «не существует», то есть ровно то, что скрытие и
// должно закрывать.
func TestProjectGateBorrowsTheOwnersNotFoundForm(t *testing.T) {
	gate, err := subscriptionjournal.ProjectGate()
	if err != nil {
		t.Fatalf("страж оси project_id не собрался: %v", err)
	}
	form, ok := authz.OwnerNotFoundFormat("project")
	if !ok {
		t.Fatal("у типа `project` нет формы отсутствия у производителя — предпосылка пробы неверна")
	}
	if gate.NotFoundFormat != form {
		t.Fatalf("страж отвечает формой %q, владелец проектов — %q: различимый текст "+
			"выдаёт существование чужого проекта", gate.NotFoundFormat, form)
	}
	if gate.ObjectType != "project" || gate.Action != "iam.projects.get" {
		t.Fatalf("страж спрашивает про %q действием %q — вопрос обязан быть ТЕМ ЖЕ, "+
			"каким владелец проектов гейтит своё чтение", gate.ObjectType, gate.Action)
	}
}

// TestRemovalCarriesNoState — снятие отдаётся БЕЗ состояния.
//
// У снятия полного состояния нет и быть не может: предмета больше нет. Род
// изменения спрашивается ЯВНО, а не выводится из бедности нагрузки: вывод из
// бедности сработал бы и на настоящем сбое разбора, и два разных исхода стали бы
// неразличимы.
func TestRemovalCarriesNoState(t *testing.T) {
	state := subscriptionjournal.Journal().Mapping.State
	if state == nil {
		t.Fatal("отображение состояния не назначено — общая форма его требует")
	}
	got, absence, err := state(subscription.Row{
		Kind:    subscriptionjournal.JournalWordVolume,
		ID:      "vol-0000000000000001",
		Change:  "DELETED",
		Payload: []byte(`{"id":"vol-0000000000000001","project_id":"prj-1"}`),
	})
	if err != nil {
		t.Fatalf("снятие дало ОШИБКУ разбора (%v): отсутствие состояния у снятия — "+
			"свойство предмета, а не сбой, и ошибка звала бы чинить несуществующую поломку", err)
	}
	if got != nil {
		t.Fatal("у снятия отдано состояние: подписчик вправе читать непустую нагрузку " +
			"как ПОЛНОЕ состояние и записал бы пустые поля как факт")
	}
	if absence != subscription.StateNotProduced {
		t.Fatalf("причина отсутствия %v, ожидалась StateNotProduced", absence)
	}
}

// TestStateAbsenceIsNamedForEveryKindAndChange — состояния нет НИ У ОДНОГО вида и
// НИ У ОДНОГО рода, и это НАЗВАННОЕ свойство журнала, а не сбой.
//
// # Почему по всем видам и родам, а не на одном примере
//
// Отсутствие состояния у storage — свойство ЖУРНАЛА (публичная проекция всех трёх
// видов выводится ЧЕРЕЗ таблицы, и собрать её в триггере значило бы завести вторую
// реализацию `protoconv` на SQL). Свойство журнала обязано держаться на всём
// журнале: заведись у одного вида ветка со своим ответом — эта проба покраснеет и
// потребует назвать причину ЕЙ, а не унаследовать соседнюю.
//
// # Почему причина, а не только пустота
//
// Пустота без названной причины доезжает клиенту как `REASON_UNSPECIFIED` —
// «владелец забыл назвать». Действия у клиента разные: на «не производится» он
// идёт за предметом чтением по идентификатору, на «не удалось собрать» —
// перечитывает событие. Утверждать одну пустоту значит не утверждать ничего о том,
// ради чего причина введена.
func TestStateAbsenceIsNamedForEveryKindAndChange(t *testing.T) {
	journal := subscriptionjournal.Journal()
	kinds := journal.Mapping.Kinds
	changes := journal.Mapping.Changes
	if len(kinds) == 0 || len(changes) == 0 {
		t.Fatal("словарь видов или родов пуст — судить не о чем, и зелёное было бы пустым обходом")
	}
	state := journal.Mapping.State
	seen := 0
	for kind := range kinds {
		for change := range changes {
			got, absence, err := state(subscription.Row{
				Kind:    kind,
				ID:      "probe",
				Change:  change,
				Payload: []byte(`{"id":"probe","project_id":"prj-1"}`),
			})
			seen++
			if err != nil {
				t.Errorf("вид %q род %q: отсутствие объявлено ОШИБКОЙ (%v); это свойство "+
					"журнала, а не сбой сборки — следующий читатель чинил бы несуществующую поломку",
					kind, change, err)
			}
			if got != nil {
				t.Errorf("вид %q род %q: отдано состояние, собранное из строки таблицы. "+
					"Публичная проекция выводится ЧЕРЕЗ таблицы, и частичная уехала бы "+
					"подписчику как ПОЛНАЯ", kind, change)
			}
			if absence != subscription.StateNotProduced {
				t.Errorf("вид %q род %q: причина отсутствия %v, ожидалась StateNotProduced — "+
					"неназванная доезжает клиенту как «владелец забыл»", kind, change, absence)
			}
		}
	}
	t.Logf("перепись: видов %d · родов %d · пар осмотрено %d", len(kinds), len(changes), seen)
}
