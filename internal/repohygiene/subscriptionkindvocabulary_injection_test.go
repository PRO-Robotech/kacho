// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionkindvocabulary_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что
// анализатор способен упасть, называет координату и молчит на законном близнеце.
//
// Стенд синтетический: настоящее дерево нельзя ни сломать, ни вернуть, а вердикт
// о нём (`subscriptionkindvocabulary_test.go`) ничего не говорит о способности
// проверки падать — зелёный получает и та, что не смотрит никуда.
//
// Инъекции ЧЕТЫРЕ, по числу видов находки, и каждая внесена ОТДЕЛЬНО: инъекция,
// нарушающая два свойства разом, доказывает лишь то, что покраснело хоть что-то.
// К каждой приложен законный близнец той же формы, обязанный молчать.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// kindStand — синтетическое дерево: контракт с аннотациями типов объекта,
// пакет-производитель имён и владелец журнала, берущий имена у него.
//
// Это ЗАКОННОЕ состояние, и на нём анализатор обязан молчать.
type kindStand struct {
	root string
}

func newKindStand(t *testing.T) *kindStand {
	t.Helper()
	s := &kindStand{root: t.TempDir()}

	s.write(t, "proto/kacho/cloud/probe/v1/probe_service.proto", `
syntax = "proto3";
package kacho.cloud.probe.v1;

// В комментарии стоит object_type: "probe_ghost" — и это НЕ объявление.
// Анализатор, считающий сырой текст, объявил бы призрак известным платформе.
service ProbeService {
  rpc Get(GetRequest) returns (Probe) {
    option (kacho.iam.authz.v1.scope_extractor) = {
      object_type:        "probe_machine"
      from_request_field: "machine_id"
    };
  }
  rpc List(ListRequest) returns (ListResponse) {
    option (kacho.iam.authz.v1.scope_extractor) = {
      object_type:        "probe_balancer"
      from_request_field: "project_id"
    };
  }
}
`)
	// Производитель имён: единственный источник написания.
	s.write(t, "services/probe/internal/authzfilter/actions.go", `package authzfilter

const (
	ResourceTypeMachine  = "probe_machine"
	ResourceTypeBalancer = "probe_balancer"
	ResourceTypeGhost    = "probe_ghost"
	ResourceTypeShouting = "Probe_Machine"
)

const (
	ActionMachineRead  = "probe.machines.list"
	ActionBalancerRead = "probe.balancers.list"
)
`)
	s.writeJournal(t, `
			Kinds: map[string]subscription.Kind{
				"Machine":       {ObjectType: authzfilter.ResourceTypeMachine, Action: authzfilter.ActionMachineRead},
				"probe_balancr": {ObjectType: authzfilter.ResourceTypeBalancer, Action: authzfilter.ActionBalancerRead},
			},`)

	// ЗАКОННЫЙ БЛИЗНЕЦ ФАЙЛА: чужая структура с полем того же имени и литералом
	// в нём. Она не про подписку — файл не импортирует общую форму, — и
	// анализатор обязан её не замечать.
	s.write(t, "services/probe/internal/catalog/catalog.go", `package catalog

type Entry struct {
	ObjectType string
	Action     string
}

var Catalogue = map[string]Entry{
	"Machine": {ObjectType: "probe_machine", Action: "probe.machines.list"},
}
`)
	return s
}

// writeJournal кладёт объявление владельца с заданным телом словаря видов.
func (s *kindStand) writeJournal(t *testing.T, kinds string) {
	t.Helper()
	s.write(t, "services/probe/internal/subscriptionjournal/journal.go", `package subscriptionjournal

import (
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/probe/internal/authzfilter"
)

var _ = authzfilter.ResourceTypeMachine

func Journal() subscription.Journal {
	return subscription.Journal{
		Channel: "probe_outbox",
		Mapping: subscription.Mapping{`+kinds+`
		},
	}
}
`)
}

func (s *kindStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (s *kindStand) audit(t *testing.T) ([]SubscriptionKindFinding, SubscriptionKindCensus) {
	t.Helper()
	var log strings.Builder
	findings, census, err := AuditSubscriptionKindVocabulary(SubscriptionKindOptions{
		Root:      s.root,
		ProtoRoot: "proto",
		GoRoots:   []string{"pkg", "services"},
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v\n%s", err, log.String())
	}
	t.Log(strings.TrimSpace(log.String()))
	return findings, census
}

func kindFindingsOf(findings []SubscriptionKindFinding, kind string) []SubscriptionKindFinding {
	var out []SubscriptionKindFinding
	for _, f := range findings {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

// TestKindVocabularyGateIsSilentOnTheLawfulTree — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ, и он
// первый.
//
// Без него всякое покраснение ниже доказывало бы лишь то, что анализатор
// краснеет всегда. Заодно утверждается, что ОБЕ половины вердикта вынесены: ноль
// разрешённых типов означал бы, что вторая не спрашивалась ни разу.
func TestKindVocabularyGateIsSilentOnTheLawfulTree(t *testing.T) {
	findings, census := newKindStand(t).audit(t)
	if len(findings) != 0 {
		t.Fatalf("на законном дереве найдено %d: %v", len(findings), findings)
	}
	if census.JournalMappings != 1 || census.KindEntries != 2 {
		t.Fatalf("объявлений журнала %d, записей вида %d — стенд прочитан не весь",
			census.JournalMappings, census.KindEntries)
	}
	if census.ObjectTypesUsed != 2 {
		t.Fatalf("разрешённых типов объекта %d при двух записях: имена не добылись, "+
			"и вторая половина вердикта не вынесена", census.ObjectTypesUsed)
	}
	if census.DeclaredTypes != 2 {
		t.Fatalf("объявленных аннотациями типов %d, ожидалось два: призрак из комментария "+
			"засчитан за объявление", census.DeclaredTypes)
	}
}

// TestKindVocabularyGateCatchesALiteral — вид, выписанный литералом.
//
// Это главный вид находки: литерал есть второе написание чужого словаря, и
// расходится оно молча.
func TestKindVocabularyGateCatchesALiteral(t *testing.T) {
	s := newKindStand(t)
	s.writeJournal(t, `
			Kinds: map[string]subscription.Kind{
				"Machine":       {ObjectType: "probe_machine", Action: authzfilter.ActionMachineRead},
				"probe_balancr": {ObjectType: authzfilter.ResourceTypeBalancer, Action: authzfilter.ActionBalancerRead},
			},`)

	findings, _ := s.audit(t)
	got := kindFindingsOf(findings, KindVocabularyLiteral)
	if len(got) != 1 {
		t.Fatalf("находок о литерале %d, ожидалась одна: %v", len(got), findings)
	}
	if !strings.Contains(got[0].Where, "subscriptionjournal/journal.go") {
		t.Errorf("находка не называет координату: %q", got[0].Where)
	}
	if !strings.Contains(got[0].What, "probe_machine") {
		t.Errorf("находка не называет выписанное слово: %q", got[0].What)
	}
	// Законный близнец не задет: соседняя запись взяла имя у производителя.
	if n := len(kindFindingsOf(findings, KindVocabularyUndeclared)); n != 0 {
		t.Errorf("литерал, совпадающий с объявленным типом, дал ещё и находку о неизвестности: %d", n)
	}
}

// TestKindVocabularyGateCatchesALocalName — вид взят у СВОЕГО пакета.
//
// Голое имя есть та же копия чужого словаря, только шагом дальше: константа
// рядом расходится с производителем ровно так же молча.
func TestKindVocabularyGateCatchesALocalName(t *testing.T) {
	s := newKindStand(t)
	s.write(t, "services/probe/internal/subscriptionjournal/journal.go", `package subscriptionjournal

import (
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/probe/internal/authzfilter"
)

const localMachine = "probe_machine"

func Journal() subscription.Journal {
	return subscription.Journal{
		Channel: "probe_outbox",
		Mapping: subscription.Mapping{
			Kinds: map[string]subscription.Kind{
				"Machine":       {ObjectType: localMachine, Action: authzfilter.ActionMachineRead},
				"probe_balancr": {ObjectType: authzfilter.ResourceTypeBalancer, Action: authzfilter.ActionBalancerRead},
			},
		},
	}
}
`)
	findings, _ := s.audit(t)
	got := kindFindingsOf(findings, KindVocabularyLocal)
	if len(got) != 1 {
		t.Fatalf("находок о своём имени %d, ожидалась одна: %v", len(got), findings)
	}
	if !strings.Contains(got[0].What, "localMachine") {
		t.Errorf("находка не называет имя: %q", got[0].What)
	}
	// Инъекция обязана ронять ТОЛЬКО проверяемое: соседняя запись цела, значит
	// прочие виды находок молчат, и красное пришло не от них.
	for _, other := range []string{KindVocabularyLiteral, KindVocabularyUndeclared, KindVocabularyShape} {
		if n := len(kindFindingsOf(findings, other)); n != 0 {
			t.Errorf("инъекция задела соседнее свойство %s (%d находок): %v", other, n, findings)
		}
	}
}

// TestKindVocabularyGateCatchesATypeThePlatformDoesNotDeclare — тип объекта,
// которого не знает ни одна аннотация контракта.
//
// Инъекция берёт имя У ПРОИЗВОДИТЕЛЯ, то есть первую половину вердикта НЕ
// нарушает: без этого красное пришло бы от соседа, и о второй половине не было
// бы сказано ничего.
func TestKindVocabularyGateCatchesATypeThePlatformDoesNotDeclare(t *testing.T) {
	s := newKindStand(t)
	s.writeJournal(t, `
			Kinds: map[string]subscription.Kind{
				"Machine": {ObjectType: authzfilter.ResourceTypeGhost, Action: authzfilter.ActionMachineRead},
			},`)

	findings, census := s.audit(t)
	if n := len(kindFindingsOf(findings, KindVocabularyLiteral)); n != 0 {
		t.Fatalf("инъекция задела ПЕРВУЮ половину вердикта (%d находок о литерале) — "+
			"красное пришло бы от соседа: %v", n, findings)
	}
	got := kindFindingsOf(findings, KindVocabularyUndeclared)
	if len(got) != 1 {
		t.Fatalf("находок о неизвестном платформе типе %d, ожидалась одна: %v", len(got), findings)
	}
	if !strings.Contains(got[0].What, "probe_ghost") {
		t.Errorf("находка не называет тип: %q", got[0].What)
	}
	if census.ObjectTypesUsed != 1 {
		t.Errorf("разрешённых типов %d — имя не добылось, и находка получена не тем путём",
			census.ObjectTypesUsed)
	}
}

// TestKindVocabularyGateCatchesAWordWrittenTheOtherWay — слово, написанное не как
// имя типа модели прав.
//
// Это дословно тот дефект, ради которого заведён весь предмет: слово хранилища
// (`Instance`, с заглавной и без домена) уехало клиенту. Имя взято у
// производителя, поэтому первая половина вердикта не задета.
func TestKindVocabularyGateCatchesAWordWrittenTheOtherWay(t *testing.T) {
	s := newKindStand(t)
	s.writeJournal(t, `
			Kinds: map[string]subscription.Kind{
				"Machine": {ObjectType: authzfilter.ResourceTypeShouting, Action: authzfilter.ActionMachineRead},
			},`)

	findings, _ := s.audit(t)
	if n := len(kindFindingsOf(findings, KindVocabularyLiteral)); n != 0 {
		t.Fatalf("инъекция задела первую половину вердикта: %v", findings)
	}
	got := kindFindingsOf(findings, KindVocabularyShape)
	if len(got) != 1 {
		t.Fatalf("находок о написании %d, ожидалась одна: %v", len(got), findings)
	}
	if !strings.Contains(got[0].What, "Probe_Machine") {
		t.Errorf("находка не называет слово: %q", got[0].What)
	}
}

// TestKindVocabularyGateSaysWhenItCouldNotRead — имя взято у производителя, но
// значение разбором не добылось.
//
// Это ТРЕТИЙ исход, и он обязан быть отличим от двух других: «прочитал и чисто»
// против «прочитать не смог». Молчание здесь означало бы, что вторая половина
// вердикта не вынесена, а выглядело бы как её прохождение — ровно тот класс, из-за
// которого анализатор и заведён.
//
// Форма реалистичная: константа, собранная из другой константы, а не литералом.
// Такую пишут ради общего префикса, и она законна — незаконно молчать о том, что
// её значение не прочитано.
func TestKindVocabularyGateSaysWhenItCouldNotRead(t *testing.T) {
	s := newKindStand(t)
	s.write(t, "services/probe/internal/authzfilter/derived.go", `package authzfilter

const prefix = "probe_"

const ResourceTypeDerived = prefix + "machine"
`)
	s.writeJournal(t, `
			Kinds: map[string]subscription.Kind{
				"Machine": {ObjectType: authzfilter.ResourceTypeDerived, Action: authzfilter.ActionMachineRead},
			},`)

	findings, census := s.audit(t)
	got := kindFindingsOf(findings, KindVocabularyUnresolved)
	if len(got) != 1 {
		t.Fatalf("находок о непрочитанном значении %d, ожидалась одна: %v", len(got), findings)
	}
	if !strings.Contains(got[0].What, "authzfilter.ResourceTypeDerived") {
		t.Errorf("находка не называет, что именно не прочиталось: %q", got[0].What)
	}
	if census.ObjectTypesUsed != 0 {
		t.Errorf("разрешённых типов %d — значение всё-таки добылось, и проба измеряет не то",
			census.ObjectTypesUsed)
	}
	// Инъекция обязана ронять ТОЛЬКО проверяемое.
	for _, other := range []string{KindVocabularyLiteral, KindVocabularyLocal, KindVocabularyUndeclared, KindVocabularyShape} {
		if n := len(kindFindingsOf(findings, other)); n != 0 {
			t.Errorf("инъекция задела соседнее свойство %s (%d находок)", other, n)
		}
	}
}

// TestKindVocabularyGateFailsOnAnEmptyWalk — «ноль находок» обязано быть отличимо
// от «ноль прочитанного».
//
// Три предпосылки, и каждая проверяется отдельно: без файлов Go вердикта нет
// вовсе; без объявленных типов не выносится вторая половина; без объявлений
// журнала разбор сломан либо форма сменилась.
func TestKindVocabularyGateFailsOnAnEmptyWalk(t *testing.T) {
	for name, prepare := range map[string]func(t *testing.T, s *kindStand){
		"нет файлов Go": func(t *testing.T, s *kindStand) {
			t.Helper()
			if err := os.RemoveAll(filepath.Join(s.root, "services")); err != nil {
				t.Fatal(err)
			}
		},
		"нет объявлений типа в контрактах": func(t *testing.T, s *kindStand) {
			t.Helper()
			if err := os.RemoveAll(filepath.Join(s.root, "proto")); err != nil {
				t.Fatal(err)
			}
		},
		"нет объявлений журнала": func(t *testing.T, s *kindStand) {
			t.Helper()
			if err := os.Remove(filepath.Join(s.root,
				"services", "probe", "internal", "subscriptionjournal", "journal.go")); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := newKindStand(t)
			prepare(t, s)
			var log strings.Builder
			_, _, err := AuditSubscriptionKindVocabulary(SubscriptionKindOptions{
				Root:      s.root,
				ProtoRoot: "proto",
				GoRoots:   []string{"pkg", "services"},
			}, &log)
			if err == nil {
				t.Fatalf("пустой обход (%s) дал вердикт вместо отказа: %s", name, log.String())
			}
		})
	}
}
