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

## Разбор

Правило корпуса ` + "`data-integrity.md`" + ` названо здесь ПРОЗОЙ и поверхностью
решения не является; замер сделан по ` + "`services/x/internal/delete.go`" + `.
`

// ─── ОСЬ 1: объявление преемника ─────────────────────────────────────────────

func TestSuccessorIsReadFromTheDeclarationAndNotFromProse(t *testing.T) {
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
