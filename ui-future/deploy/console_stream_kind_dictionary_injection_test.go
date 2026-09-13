// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_stream_kind_dictionary_injection_test.go — доказательство, что гейт
// написания вида СПОСОБЕН упасть, СПОСОБЕН смолчать и роняет ТОЛЬКО своё.
//
// Инъекция настоящая: формы взяты с живых объявлений владельцев — у одного вид
// назван местной константой, у другого константой соседнего пакета, у третьего
// ключ карты вовсе не совпадает с написанием провода. Законные близнецы — тоже
// живые: карта предметов объясняет разницу написаний прозой, и гейт, судящий
// сырой текст, краснел бы на собственном объяснении.
//
// Вход подаётся строкой: доказательство, трогающее дерево, испортило бы чужую
// рабочую копию, а доказательство на копии разбора говорило бы о копии.
package deploy_test

import (
	"strings"
	"testing"
)

const (
	// srcSubjectsConverged — карта предметов в сведённой форме, с прозой,
	// называющей и владельца, и вид: ровно та ловушка, из-за которой сверка
	// идёт по коду, а не по тексту.
	srcSubjectsConverged = `
// Написание вида — тип объекта модели прав: volumes у владельца storage это
// kind: "storage_volume", а НЕ слово хранилища. Пример чужого владельца в прозе:
// { owner: "registry", kind: "storage_volume" } — объявлением он не является.
export interface StreamSubject { owner: JournalOwner; kind: string; }
export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
  subnets: { owner: "vpc", kind: "vpc_subnet" },
  volumes: { owner: "storage", kind: "storage_volume" },
  registries: { owner: "registry", kind: "registry_registry" },
};
`

	// srcSubjectsMisspelledKind — главный предмет гейта: опечатка в написании.
	// Владелец назван верно, поэтому соседний гейт молчит.
	srcSubjectsMisspelledKind = `
export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
  subnets: { owner: "vpc", kind: "vpc_subnet" },
  volumes: { owner: "storage", kind: "storage_volumes" },
  registries: { owner: "registry", kind: "registry_registry" },
};
`

	// srcSubjectsMovedKind — вид ПЕРЕЕХАЛ к чужому владельцу. Виды владельца
	// `registry` расходятся по двум журналам — это ловят свойства 2 и 3.
	// Множество имён владельцев не меняется, поэтому соседний гейт молчит.
	srcSubjectsMovedKind = `
export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
  subnets: { owner: "storage", kind: "vpc_subnet" },
  volumes: { owner: "storage", kind: "storage_volume" },
  registries: { owner: "registry", kind: "registry_registry" },
};
`

	// srcSubjectsSwappedOwners — ЧИСТАЯ ПЕРЕСТАНОВКА двух владельцев, у каждого
	// по одному виду. Свойства 2 и 3 на ней ВЫПОЛНЕНЫ: у каждого владельца ровно
	// один журнал, у каждого журнала ровно один владелец. Ради этой дыры и
	// заведено свойство 4 — она найдена инъекцией, а не придумана вперёд.
	srcSubjectsSwappedOwners = `
export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
  subnets: { owner: "vpc", kind: "vpc_subnet" },
  volumes: { owner: "registry", kind: "storage_volume" },
  registries: { owner: "storage", kind: "registry_registry" },
};
`

	// srcSubjectsOwnerWithoutTreeDir — ЗАКОННЫЙ БЛИЗНЕЦ свойства 4: владелец,
	// у которого одноимённого каталога в дереве НЕТ (домен контракта не совпал
	// с именем каталога). Свойство 4 к нему неприменимо, и гейт обязан молчать.
	srcSubjectsOwnerWithoutTreeDir = `
export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
  subnets: { owner: "vpc", kind: "vpc_subnet" },
  volumes: { owner: "storage", kind: "storage_volume" },
  registries: { owner: "registry", kind: "registry_registry" },
  listeners: { owner: "loadbalancer", kind: "nlb_listener" },
};
`

	// srcSubjectsOwnerDropped — дефект СОСЕДНЕГО гейта: владелец, объявленный
	// краю, картой не назван. Нужен, чтобы показать: моя проверка на нём молчит,
	// а его — краснеет.
	srcSubjectsOwnerDropped = `
export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
  subnets: { owner: "vpc", kind: "vpc_subnet" },
  volumes: { owner: "storage", kind: "storage_volume" },
};
`

	// srcJournalStorage — объявление владельца в форме блочного хранения: виды
	// названы константами соседнего пакета, а КЛЮЧИ карты — словами хранилища.
	// Ключи здесь намеренно непохожи на написания провода: гейт, читающий ключ,
	// собрал бы словарь из `JournalWordVolume` и был бы красен на верном дереве.
	srcJournalStorage = `package subscriptionjournal

import (
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/storage/internal/authzfilter"
)

const JournalWordVolume = "Volume"

func Journal() subscription.Journal {
	return subscription.Journal{
		Mapping: subscription.Mapping{
			Kinds: map[string]subscription.Kind{
				JournalWordVolume: {
					ObjectType: authzfilter.ResourceTypeVolume,
					Action:     authzfilter.ActionVolumeList,
				},
				"Snapshot": {
					ObjectType: authzfilter.ResourceTypeSnapshot,
					Action:     authzfilter.ActionSnapshotList,
				},
			},
		},
	}
}`

	// srcJournalRenamedImport — то же объявление с ПЕРЕИМЕНОВАННЫМ импортом.
	// Законная запись Go; разбор, предполагающий квалификатор равным последнему
	// сегменту пути, ослеп бы на ней молча.
	srcJournalRenamedImport = `package subscriptionjournal

import (
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	af "github.com/PRO-Robotech/kacho/services/storage/internal/authzfilter"
)

func Journal() subscription.Journal {
	return subscription.Journal{
		Mapping: subscription.Mapping{
			Kinds: map[string]subscription.Kind{
				"Volume": {ObjectType: af.ResourceTypeVolume},
			},
		},
	}
}`

	// srcJournalLocalAndLiteral — два оставшихся законных написания ссылки:
	// местная константа и строка прямо в объявлении.
	srcJournalLocalAndLiteral = `package subscriptionjournal

import "github.com/PRO-Robotech/kacho/pkg/subscription"

const KindNetwork = "vpc_network"

func Journal() subscription.Journal {
	return subscription.Journal{
		Mapping: subscription.Mapping{
			Kinds: map[string]subscription.Kind{
				"Network": {ObjectType: KindNetwork},
				"Subnet":  {ObjectType: "vpc_subnet"},
			},
		},
	}
}`

	// srcJournalUnresolvable — ссылка, которой разбор не знает. Обязана быть
	// ОТКАЗОМ: молча укоротившийся словарь объявил бы нарушителями законные виды.
	srcJournalUnresolvable = `package subscriptionjournal

import "github.com/PRO-Robotech/kacho/pkg/subscription"

func Journal() subscription.Journal {
	return subscription.Journal{
		Mapping: subscription.Mapping{
			Kinds: map[string]subscription.Kind{
				"Volume": {ObjectType: objectTypeOf("volume")},
			},
		},
	}
}`
)

const (
	relJournalStorage = "services/storage/internal/subscriptionjournal/journal.go"
	pkgJournalStorage = "github.com/PRO-Robotech/kacho/services/storage/internal/subscriptionjournal"
	pkgAuthzStorage   = "github.com/PRO-Robotech/kacho/services/storage/internal/authzfilter"
)

// fakeConstLookup — объявления соседних пакетов, поданные строкой.
func fakeConstLookup(consts map[string]map[string]string) constLookup {
	return func(importPath, name string) (string, bool) {
		v, ok := consts[importPath][name]
		return v, ok
	}
}

// probeDictionary — словарь дерева, собранный ТОЙ ЖЕ парой функций, которую
// зовёт гейт.
func probeDictionary(t *testing.T) map[string][]string {
	t.Helper()
	lookup := fakeConstLookup(map[string]map[string]string{
		pkgAuthzStorage: {
			"ResourceTypeVolume":   "storage_volume",
			"ResourceTypeSnapshot": "storage_snapshot",
		},
	})
	decl, err := journalKindRefsOf(relJournalStorage, srcJournalStorage)
	if err != nil {
		t.Fatalf("разбор синтетики не удался: %v", err)
	}
	storage, err := resolveJournalKinds(relJournalStorage, decl, pkgJournalStorage, lookup)
	if err != nil {
		t.Fatalf("резолв синтетики не удался: %v", err)
	}
	return map[string][]string{
		"services/storage/internal/subscriptionjournal":  storage,
		"services/vpc/internal/subscriptionjournal":      {"vpc_network", "vpc_subnet"},
		"services/registry/internal/subscriptionjournal": {"registry_registry"},
		// Каталога `services/loadbalancer` в дереве нет — этот журнал держится
		// взаимной однозначностью, как `nlb` у края.
		"services/nlb/internal/subscriptionjournal": {"nlb_listener"},
	}
}

// judgeProbe — одна инъекция карты предметов через ТЕ ЖЕ функции гейта.
func judgeProbe(t *testing.T, subjectsSrc string) consoleKindVerdict {
	t.Helper()
	return judgeConsoleKinds(consoleStreamSubjectsOf(subjectsSrc), probeDictionary(t))
}

// TestKindDictionaryInjectionRunOne_Control — ПРОГОН 1 из трёх: всё цело, молчат
// ОБА гейта. Без него молчание существующего контроля в прогоне 2 неотличимо от
// молчания мёртвого (testing.md §«Гейт на класс», п. 2в).
func TestKindDictionaryInjectionRunOne_Control(t *testing.T) {
	if v := judgeProbe(t, srcSubjectsConverged); !v.empty() {
		t.Errorf("новый гейт краснеет на сведённой карте: %+v", v)
	}
	// Соседний гейт судит ДРУГОЕ множество — владельцев, объявленных краю,
	// поэтому синтетике подаётся тот перечень, который она называет.
	declared := splitOwnerList("registry,storage,vpc")
	if missing := ownersMissingFromConsole(declared, mappedStreamOwners(srcSubjectsConverged)); len(missing) != 0 {
		t.Errorf("существующий гейт краснеет на законной карте — контроль недействителен: %v", missing)
	}
}

// TestKindDictionaryInjectionRunTwo_NewPropertyOnly — ПРОГОН 2: снято НОВОЕ
// свойство, старое цело. Краснеет только новый гейт.
func TestKindDictionaryInjectionRunTwo_NewPropertyOnly(t *testing.T) {
	t.Run("опечатка в написании вида", func(t *testing.T) {
		v := judgeProbe(t, srcSubjectsMisspelledKind)
		if len(v.Undeclared) != 1 {
			t.Fatalf("находок по несуществующему виду %d, ожидалась одна: %v", len(v.Undeclared), v.Undeclared)
		}
		for _, want := range []string{`"volumes"`, `"storage"`, `"storage_volumes"`, "опросе"} {
			if !strings.Contains(v.Undeclared[0], want) {
				t.Errorf("находка не называет %q: %s", want, v.Undeclared[0])
			}
		}
		if len(v.OwnerSpansJournals) != 0 || len(v.JournalSharedByOwners) != 0 {
			t.Errorf("опечатка засчитана ещё и как перепутанная привязка: %+v", v)
		}
	})

	t.Run("вид переехал к чужому владельцу", func(t *testing.T) {
		v := judgeProbe(t, srcSubjectsMovedKind)
		if len(v.Undeclared) != 0 {
			t.Errorf("существующий вид объявлен несуществующим: %v", v.Undeclared)
		}
		if len(v.OwnerSpansJournals) == 0 {
			t.Fatal("владелец, названный при видах двух журналов, не найден — " +
				"переезд вида прошёл бы незамеченным")
		}
		if len(v.JournalSharedByOwners) == 0 {
			t.Fatal("журнал, названный двумя владельцами, не найден — обратная сторона " +
				"привязки не проверяется")
		}
	})

	t.Run("чистая перестановка двух владельцев", func(t *testing.T) {
		v := judgeProbe(t, srcSubjectsSwappedOwners)
		// Ровно та дыра, ради которой свойство 4 и заведено: первые три молчат.
		if len(v.Undeclared) != 0 || len(v.OwnerSpansJournals) != 0 ||
			len(v.JournalSharedByOwners) != 0 {
			t.Fatalf("свойства 1-3 на перестановке сработали — тогда премиса свойства 4 "+
				"описана неверно, и его довод надо перемерить: %+v", v)
		}
		if len(v.NamedJournalMismatch) != 2 {
			t.Fatalf("находок по одноимённому каталогу %d, ожидалось две: %v",
				len(v.NamedJournalMismatch), v.NamedJournalMismatch)
		}
		joined := strings.Join(v.NamedJournalMismatch, "\n")
		for _, want := range []string{
			`"registry"`, `"storage"`,
			"services/registry/internal/subscriptionjournal",
			"services/storage/internal/subscriptionjournal",
		} {
			if !strings.Contains(joined, want) {
				t.Errorf("находки не называют %q: %s", want, joined)
			}
		}
	})

	// Существующий гейт на ОБЕИХ инъекциях выше молчать обязан: множество ИМЁН
	// владельцев они не меняют. Утверждается именно это — моя инъекция не
	// трогает его предмет, значит прогон 3 говорит о нём, а не о ней.
	declared := splitOwnerList("registry,storage,vpc")
	for _, src := range []string{srcSubjectsMisspelledKind, srcSubjectsMovedKind, srcSubjectsSwappedOwners} {
		if missing := ownersMissingFromConsole(declared, mappedStreamOwners(src)); len(missing) != 0 {
			t.Errorf("существующий гейт покраснел от чужой инъекции — доказательство "+
				"недействительно: %v", missing)
		}
	}
}

// TestKindDictionaryInjectionRunThree_ExistingPropertyOnly — ПРОГОН 3: снято
// СУЩЕСТВУЮЩЕЕ свойство (владелец края не назван картой). Краснеет только
// соседний гейт, новый молчит.
func TestKindDictionaryInjectionRunThree_ExistingPropertyOnly(t *testing.T) {
	declared := splitOwnerList("registry,storage,vpc")
	missing := ownersMissingFromConsole(declared, mappedStreamOwners(srcSubjectsOwnerDropped))
	if len(missing) == 0 {
		t.Fatal("существующий гейт не увидел неназванного владельца — он мёртв, " +
			"и его молчание в прогонах 1 и 2 ничего не доказывало")
	}
	if v := judgeProbe(t, srcSubjectsOwnerDropped); !v.empty() {
		t.Errorf("новый гейт краснеет на чужом предмете: %+v", v)
	}
}

// TestKindDictionaryGateIsSilentOnLegalTwins — гейт СПОСОБЕН смолчать, и словарь
// он собирает из ТОГО, что уходит по проводу.
func TestKindDictionaryGateIsSilentOnLegalTwins(t *testing.T) {
	t.Run("проза с чужой парой владелец+вид не является объявлением", func(t *testing.T) {
		got := consoleStreamSubjectsOf(srcSubjectsConverged)
		if len(got) != 4 {
			t.Fatalf("записей разобрано %d, ожидалось четыре: %+v", len(got), got)
		}
		for _, s := range got {
			if s.Owner == "registry" && s.Kind == "storage_volume" {
				t.Errorf("пара из комментария прочитана как объявление: %+v", s)
			}
			if s.Spec == "StreamSubject" {
				t.Errorf("объявление типа прочитано как запись карты: %+v", s)
			}
		}
	})

	t.Run("словарь берётся у ObjectType, а не у ключа карты", func(t *testing.T) {
		dict := probeDictionary(t)["services/storage/internal/subscriptionjournal"]
		want := []string{"storage_snapshot", "storage_volume"}
		if strings.Join(dict, ",") != strings.Join(want, ",") {
			t.Fatalf("словарь владельца: получено %v, ожидалось %v — гейт сверяет не то, "+
				"что уходит клиенту", dict, want)
		}
		for _, key := range []string{"Volume", "JournalWordVolume", "Snapshot"} {
			for _, got := range dict {
				if got == key {
					t.Errorf("в словарь попало слово ХРАНИЛИЩА %q — по проводу уходит "+
						"тип объекта модели прав", key)
				}
			}
		}
	})

	t.Run("переименованный импорт объявляющего пакета", func(t *testing.T) {
		decl, err := journalKindRefsOf(relJournalStorage, srcJournalRenamedImport)
		if err != nil {
			t.Fatalf("разбор синтетики не удался: %v", err)
		}
		got, rerr := resolveJournalKinds(relJournalStorage, decl, pkgJournalStorage,
			fakeConstLookup(map[string]map[string]string{
				pkgAuthzStorage: {"ResourceTypeVolume": "storage_volume"},
			}))
		if rerr != nil {
			t.Fatalf("переименованный импорт не разрешился: %v", rerr)
		}
		if strings.Join(got, ",") != "storage_volume" {
			t.Errorf("словарь при переименованном импорте: %v", got)
		}
	})

	t.Run("местная константа и строка прямо в объявлении", func(t *testing.T) {
		decl, err := journalKindRefsOf("services/vpc/internal/subscriptionjournal/journal.go",
			srcJournalLocalAndLiteral)
		if err != nil {
			t.Fatalf("разбор синтетики не удался: %v", err)
		}
		got, rerr := resolveJournalKinds("services/vpc/internal/subscriptionjournal/journal.go", decl,
			"github.com/PRO-Robotech/kacho/services/vpc/internal/subscriptionjournal",
			fakeConstLookup(map[string]map[string]string{
				"github.com/PRO-Robotech/kacho/services/vpc/internal/subscriptionjournal": {
					"KindNetwork": "vpc_network",
				},
			}))
		if rerr != nil {
			t.Fatalf("местная константа или литерал не разрешились: %v", rerr)
		}
		if strings.Join(got, ",") != "vpc_network,vpc_subnet" {
			t.Errorf("словарь: получено %v", got)
		}
	})

	t.Run("владелец без одноимённого каталога держится однозначностью", func(t *testing.T) {
		v := judgeProbe(t, srcSubjectsOwnerWithoutTreeDir)
		if !v.empty() {
			t.Fatalf("гейт краснеет на владельце, чьё имя есть домен контракта, а не "+
				"каталог сервиса: %+v", v)
		}
		if v.PinnedByName != 3 || v.PinnedByBijection != 1 {
			t.Errorf("перепись привязки: именем %d, однозначностью %d — ожидалось 3 и 1",
				v.PinnedByName, v.PinnedByBijection)
		}
	})

	t.Run("второй владелец без одноимённого каталога СЧИТАЕТСЯ", func(t *testing.T) {
		// Премиса свойства 4 с верхней стороны: между ДВУМЯ владельцами, которых
		// держит только взаимная однозначность, перестановка снова невидима.
		// Счётчик обязан её увидеть — иначе слепая зона расширилась бы молча.
		const src = `
export const STREAM_SUBJECTS = {
  listeners: { owner: "loadbalancer", kind: "nlb_listener" },
  machines: { owner: "workload", kind: "compute_instance" },
};
`
		v := judgeConsoleKinds(consoleStreamSubjectsOf(src), map[string][]string{
			"services/nlb/internal/subscriptionjournal":     {"nlb_listener"},
			"services/compute/internal/subscriptionjournal": {"compute_instance"},
		})
		if !v.empty() {
			t.Fatalf("гейт краснеет на законной паре — тогда премиса читалась бы как "+
				"находка свойства: %+v", v)
		}
		if v.PinnedByName != 0 || v.PinnedByBijection != 2 {
			t.Fatalf("перепись привязки: именем %d, однозначностью %d — ожидалось 0 и 2; "+
				"счётчик слеп ровно там, где премиса и обязана сработать",
				v.PinnedByName, v.PinnedByBijection)
		}
	})

	t.Run("запись формой, которой разбор не знает, СЧИТАЕТСЯ", func(t *testing.T) {
		// Перенос полей на отдельные строки и обратный порядок `kind`/`owner` —
		// законные записи TypeScript. Разбор их не знает НАМЕРЕННО: закрывать
		// каждую форму значило бы гнаться за средством форматирования. Но
		// невидимой такая запись быть не вправе, иначе «ноль находок» в ней
		// означало бы «ноль прочитанного».
		const src = `
export const STREAM_SUBJECTS = {
  networks: { owner: "vpc", kind: "vpc_network" },
  volumes: { kind: "storage_volume", owner: "storage" },
};
`
		parsed, declared := consoleSubjectCounts(src)
		if parsed != 1 {
			t.Fatalf("разобрано записей %d, ожидалась одна — синтетика не воспроизводит "+
				"слепую зону, и утверждение ниже вакуумно", parsed)
		}
		if declared != 2 {
			t.Fatalf("объявлений владельца найдено %d, ожидалось два — счётчик слеп "+
				"ровно там, где слеп разбор, и расхождение никогда не всплывёт", declared)
		}

		// Обратная сторона: на форме, которую разбор ЗНАЕТ, счётчики сходятся —
		// иначе гейт роняли бы прогон на верной карте, и его сняли бы первым.
		if p2, d2 := consoleSubjectCounts(srcSubjectsConverged); p2 != d2 || p2 != 4 {
			t.Errorf("на законной карте счётчики разошлись: разобрано %d, объявлено %d", p2, d2)
		}
	})

	t.Run("нерезолвившаяся ссылка — отказ, а не короткий словарь", func(t *testing.T) {
		decl, err := journalKindRefsOf(relJournalStorage, srcJournalUnresolvable)
		if err != nil {
			t.Fatalf("разбор синтетики не удался: %v", err)
		}
		_, rerr := resolveJournalKinds(relJournalStorage, decl, pkgJournalStorage,
			fakeConstLookup(nil))
		if rerr == nil {
			t.Fatal("выражение, которого разбор не знает, прошло молча — словарь стал бы " +
				"короче объявленного, и законные виды консоли объявились бы нарушителями")
		}
		if !strings.Contains(rerr.Error(), relJournalStorage) {
			t.Errorf("отказ не называет координату: %v", rerr)
		}
	})
}
