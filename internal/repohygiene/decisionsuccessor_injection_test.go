// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// decisionsuccessor_injection_test.go — ДОКАЗАТЕЛЬСТВО, что разбор
// задачи-преемника СПОСОБЕН найти расхождение и способен СМОЛЧАТЬ.
//
// Близнец каждый раз отличается РОВНО ОДНИМ фактом. Вход синтетический: на
// настоящем документе ни падения, ни молчания не показать, не сломав его.
package repohygiene

import (
	"strings"
	"testing"
)

const decisionDocFixture = `# Решение

**Статус:** принято (задача #1594, закрыта)

` + SuccessorMarker + ` #1231

` + SurfacesMarker + " `proto/x/y.proto` ·\n`services/x/docs/page.mdx`\n" + `

` + RefusalMarker + " `Project <id> is not empty (` · `REFERENCE_IN_USE`\n" + `

## Разбор

Правило корпуса ` + "`data-integrity.md`" + ` названо здесь ПРОЗОЙ и поверхностью
решения не является; замер сделан по ` + "`services/x/internal/delete.go`" + `.
`

// ─── ОСЬ 1: объявление преемника ─────────────────────────────────────────────

func TestSuccessorIsReadFromTheDeclarationAndNotFromProse(t *testing.T) {
	t.Parallel()
	if got := DeclaredSuccessor([]byte(decisionDocFixture)); got != 1231 {
		t.Fatalf("объявленный преемник прочитан как #%d, ожидалось #1231", got)
	}
	// Близнец: РОВНО ОДИН изменённый факт — строки объявления нет, а прозаическое
	// упоминание задачи осталось. Без этой пары разбор брал бы первый попавшийся
	// номер и объявлял преемником задачу, при которой решение принималось.
	without := strings.Replace(decisionDocFixture, SuccessorMarker+" #1231", "Преемника пока нет", 1)
	if got := DeclaredSuccessor([]byte(without)); got != 0 {
		t.Fatalf("без строки объявления преемник обязан быть НЕ ОПРЕДЕЛЁН, прочитано #%d", got)
	}
}

// ─── ОСЬ 2: поверхности берутся из объявления, а не со всего документа ───────

func TestSurfacesComeFromTheDeclaredParagraphOnly(t *testing.T) {
	t.Parallel()
	got := DeclaredCoordinates([]byte(decisionDocFixture))
	want := map[string]bool{"proto/x/y.proto": true, "services/x/docs/page.mdx": true}
	if len(got) != 2 {
		t.Fatalf("координат прочитано %d, ожидалось 2 (перенос строки — законная форма объявления): %v", len(got), got)
	}
	for _, p := range got {
		if !want[p] {
			t.Errorf("координата %q поверхностью решения не объявлена — правило корпуса и "+
				"файл-свидетельство обязанности называть преемника не несут", p)
		}
	}
}

// ─── ОСЬ 3: находка против молчания ──────────────────────────────────────────

func TestCitesSuccessorRedsOnAStaleReferenceAndIsSilentOnAHistoricalOne(t *testing.T) {
	t.Parallel()
	stale := []byte("// см. PRO-Robotech/kacho#1594.")
	cites, found := CitesSuccessor(stale, 1231)
	if cites {
		t.Fatalf("поверхность, называющая ТОЛЬКО прежнюю задачу, преемника не называет")
	}
	if len(found) != 1 || found[0] != 1594 {
		t.Fatalf("встреченные номера прочитаны неверно: %v", found)
	}
	// ЗАКОННЫЙ БЛИЗНЕЦ: изменён РОВНО ОДИН факт — рядом с исторической ссылкой
	// появился преемник. Историческая ссылка законна и запрещаться не должна.
	both := []byte("// преемник — kacho#1231; решение принято под #1594.")
	if ok, _ := CitesSuccessor(both, 1231); !ok {
		t.Fatalf("поверхность, называющая преемника РЯДОМ с исторической ссылкой, законна")
	}
}

// ─── ОСЬ 4: литера разметки — не задача ──────────────────────────────────────

func TestHtmlEntityIsNotReadAsAnIssueNumber(t *testing.T) {
	t.Parallel()
	// Дефект найден этим гейтом на его ПЕРВОМ прогоне: `&#91;` читалось задачей
	// №91, и страница арендатора объявлялась называющей четыре несуществующие.
	entity := []byte("В коде: &#91;array&#93; и &#123;obj&#125;.")
	_, found := CitesSuccessor(entity, 1231)
	if len(found) != 0 {
		t.Fatalf("числовые литеры разметки задачами не являются, прочитано: %v", found)
	}
	// Близнец: РОВНО ОДИН изменённый факт — тот же номер написан ссылкой.
	real := []byte("см. #91.")
	_, found2 := CitesSuccessor(real, 1231)
	if len(found2) != 1 || found2[0] != 91 {
		t.Fatalf("настоящая ссылка обязана читаться, прочитано: %v", found2)
	}
}

// ─── ОСЬ 5: текст находки называет координату и оба номера ───────────────────

func TestSuccessorFindingNamesThePathAndWhatWasCitedInstead(t *testing.T) {
	t.Parallel()
	msg := SuccessorFinding(DecisionSurface{Path: "proto/x/y.proto", Found: []int{1594}}, 1231)
	for _, want := range []string{"proto/x/y.proto", "#1231", "#1594"} {
		if !strings.Contains(msg, want) {
			t.Errorf("текст находки не называет %q: %s", want, msg)
		}
	}
	// Поверхность БЕЗ единой ссылки — отдельный случай: «названы: » с пустым
	// перечнем читалось бы как обрыв текста.
	none := SuccessorFinding(DecisionSurface{Path: "proto/x/y.proto"}, 1231)
	if !strings.Contains(none, "НИ ОДНОЙ") {
		t.Errorf("поверхность без ссылок обязана называться отдельно: %s", none)
	}
}

// ─── ОСЬ 6: объявленный отказ — на каждой поверхности ДОСЛОВНО ──────────────

func TestRefusalLiteralsAreReadFromTheDeclarationAndNotFromProse(t *testing.T) {
	t.Parallel()
	got := DeclaredRefusalLiterals([]byte(decisionDocFixture))
	want := []string{"Project <id> is not empty (", "REFERENCE_IN_USE"}
	if len(got) != len(want) {
		t.Fatalf("литералов отказа прочитано %d, ожидалось %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("литерал %d прочитан как %q, ожидалось %q", i, got[i], want[i])
		}
	}
	// Близнец: РОВНО ОДИН изменённый факт — строки объявления нет, а тот же тон
	// назван в прозе документа (в обратных кавычках, как его пишет корпус). Без
	// этой пары разбор собирал бы литералы со всего документа и требовал бы от
	// контракта дословного присутствия каждого примера из разбора.
	without := strings.Replace(decisionDocFixture,
		RefusalMarker+" `Project <id> is not empty (` · `REFERENCE_IN_USE`",
		"Тон отказа — форма сети: `Project <id> is not empty (`, признак `REFERENCE_IN_USE`.", 1)
	if got := DeclaredRefusalLiterals([]byte(without)); len(got) != 0 {
		t.Fatalf("без строки объявления литералов отказа быть НЕ ДОЛЖНО, прочитано: %q", got)
	}
}

func TestMissingRefusalLiteralsRedOnTheOldContractAndAreSilentOnTheNewOne(t *testing.T) {
	t.Parallel()
	lits := []string{"Project <id> is not empty (", "REFERENCE_IN_USE"}
	// Контракт ДО посадки механизма (kaname@40bd49a3): называет преемника, но
	// объявляет обратное поведение. Именно эту поверхность гейт видел до
	// подъёма пина — и обязан был на ней краснеть.
	old := []byte("// NOT BLOCKED BY LIVE RESOURCES. See PRO-Robotech/kacho#1231.")
	missing := MissingRefusalLiterals(old, lits)
	if len(missing) != 2 {
		t.Fatalf("на прежнем контракте недостающих литералов обязано быть 2, найдено %d: %q", len(missing), missing)
	}
	// Законный близнец: РОВНО ОДИН изменённый факт — те же литералы стоят в
	// комментарии контракта дословно, среди чужого текста.
	fresh := []byte("//   message \"Project <id> is not empty (vpc.network: 3)\"\n" +
		"//   details ErrorInfo{reason: \"REFERENCE_IN_USE\"}\n// See PRO-Robotech/kacho#1231.")
	if got := MissingRefusalLiterals(fresh, lits); len(got) != 0 {
		t.Fatalf("на контракте, несущем оба литерала, недостающих быть не должно: %q", got)
	}
	// Половина — тоже находка, и находка называет ИМЕННО отсутствующее: пропажа
	// признака при живом тексте есть тот самый случай, когда клиент, ключующийся
	// на токене, перестаёт различать полосу.
	half := []byte("//   message \"Project <id> is not empty (vpc.network: 3)\"")
	got := MissingRefusalLiterals(half, lits)
	if len(got) != 1 || got[0] != "REFERENCE_IN_USE" {
		t.Fatalf("при живом тексте и пропавшем признаке находка обязана назвать признак, получено: %q", got)
	}
}

func TestRefusalFindingNamesThePathAndEveryMissingLiteral(t *testing.T) {
	t.Parallel()
	msg := RefusalFinding("proto/x/y.proto", []string{"Project <id> is not empty (", "REFERENCE_IN_USE"})
	for _, want := range []string{"proto/x/y.proto", "Project <id> is not empty (", "REFERENCE_IN_USE"} {
		if !strings.Contains(msg, want) {
			t.Errorf("текст находки не называет %q: %s", want, msg)
		}
	}
}
