// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// subscriptionfloorafterpage_injection_test.go — ДОКАЗАТЕЛЬСТВО СПОСОБНОСТИ ГЕЙТА
// УПАСТЬ, и упасть ровно там, где написано.
//
// # Инъекция роняет ТОЛЬКО проверяемое
//
// Утверждение у анализатора одно — ПОРЯДОК, — и инъекция под него переставляет
// РОВНО ДВА оператора одного тела, ничего больше не трогая: ни состав файлов, ни
// вызовы, ни запрос. Инъекция вида «завести ещё один элемент» доказательством не
// была бы: новый элемент нарушает всё, что требуется от элементов вообще, и
// красное пришло бы от соседа.
//
// Прогонов поэтому три вида, и они дают РАЗНЫЕ исходы, которые нельзя перепутать:
//
//	контроль            — молчат и находки, и предпосылки;
//	инъекция порядка    — ровно одна находка, ошибки обхода нет;
//	инъекция предпосылки — ошибка обхода, находок нет.
//
// Третий обязателен: без него молчание предпосылочных проверок было бы
// неотличимо от их отсутствия.
//
// # Законные близнецы — обязательны, иначе гейт ловит форму
//
// В синтетике лежат три функции, которых гейт трогать НЕ вправе:
//
//	`seat`   — спрашивает пол и страницы не читает вовсе (объявление
//	           возобновимой позиции при открытии потока);
//	`sweep`  — называет журнал и читает его БЕЗ окна (снятие строк);
//	`sample` — вызывает `Floor` ЧУЖОГО пакета (`math.Floor`) перед выборкой окна,
//	           находясь вне пакета подписки.
//
// Третий проверяет ровно предпосылку 3: распознаватель судит по последнему
// идентификатору выражения и одноимённый чужой вызов иначе не отличил бы.
type floorOrderTree struct {
	// floorBeforePage — дофиксовый порядок: пол взят раньше страницы.
	floorBeforePage bool
	// inlineWindow — слитная форма: окно литералом в теле спрашивающего пол.
	inlineWindow bool
	// noWindow — выборки окном нет вовсе (предпосылка 1).
	noWindow bool
	// noFloor — пол не спрашивает никто (предпосылка «утверждение о порядке
	// потеряло предмет»).
	noFloor bool
	// noOwned — файлов подписки нет вовсе (предпосылка 3).
	noOwned bool
}

const floorOrderWindowSQL = `SELECT pos, kind FROM ` + "`" + `journal` + "`" +
	` WHERE pos > $1 AND pos <= $2 ORDER BY pos ASC LIMIT 100`

func writeFloorOrderTree(t *testing.T, tree floorOrderTree) string {
	t.Helper()
	root := t.TempDir()

	write := func(rel, body string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("каталог %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("файл %s: %v", rel, err)
		}
	}

	// ── Законный близнец ВНЕ пакета подписки: `Floor` чужого пакета ─────────
	write("tools/bench/measure.go", ""+
		"package bench\n\n"+
		"import \"math\"\n\n"+
		"func sample(x float64) float64 {\n"+
		"\tlo := math.Floor(x)\n"+
		"\tq := \""+floorOrderWindowSQL+"\"\n"+
		"\t_ = q\n"+
		"\treturn lo\n"+
		"}\n")

	if tree.noOwned {
		return root
	}

	// ── Помощник, читающий страницу ОКНОМ ──────────────────────────────────
	reader := "" +
		"package subscription\n\n" +
		"func (s *Server) read(cursor, settled int64) ([]int64, error) {\n"
	if tree.noWindow {
		// Предмета нет: выборки окном не осталось.
		reader += "\treturn nil, nil\n}\n"
	} else {
		reader += "\tq := \"" + floorOrderWindowSQL + "\"\n\t_ = q\n\treturn nil, nil\n}\n"
	}
	// Законный близнец: журнал назван, окна нет — снятие строк пропуска не
	// создаёт, и гейт обязан о нём молчать.
	reader += "\nfunc (s *Server) sweep(upTo int64) error {\n" +
		"\tq := \"DELETE FROM journal WHERE pos <= $1\"\n\t_ = q\n\treturn nil\n}\n"
	write("pkg/subscription/read.go", reader)

	// ── Тело, где решается порядок ─────────────────────────────────────────
	page := "\trows, err := s.read(cursor, settled)\n\tif err != nil {\n\t\treturn err\n\t}\n"
	if tree.inlineWindow {
		page = "\tq := \"" + floorOrderWindowSQL + "\"\n\trows, err := s.query(q)\n" +
			"\tif err != nil {\n\t\treturn err\n\t}\n"
	}
	floor := "\tif floor := h.Floor(1); floor > cursor {\n\t\treturn errLost\n\t}\n"
	if tree.noFloor {
		floor = ""
	}
	body := page + floor
	if tree.floorBeforePage && !tree.noFloor {
		body = floor + page
	}
	write("pkg/subscription/drain.go", ""+
		"package subscription\n\n"+
		"func (s *Server) drain(h *Watermark, cursor, settled int64) error {\n"+
		body+
		"\t_ = rows\n"+
		"\treturn nil\n"+
		"}\n\n"+
		// Законный близнец: пол при открытии потока, страницы нет вовсе.
		"func (s *Server) seat(h *Watermark) int64 {\n"+
		"\treturn h.Floor(1)\n"+
		"}\n")

	// ── Потребитель ИЗ ДРУГОГО пакета: импорт делает его файлом подписки ────
	write("services/iam/internal/repo/pg/journal.go", ""+
		"package pg\n\n"+
		"import \"github.com/PRO-Robotech/kacho/pkg/subscription\"\n\n"+
		"var _ = subscription.RetainsFromEarliestRow\n")

	return root
}

func runFloorOrderAudit(t *testing.T, tree floorOrderTree) (
	[]SubscriptionFloorAfterPageFinding, SubscriptionFloorAfterPageCensus, error,
) {
	t.Helper()
	root := writeFloorOrderTree(t, tree)
	return AuditSubscriptionFloorAfterPage(SubscriptionFloorAfterPageOptions{Root: root}, nil)
}

// TestSubscriptionFloorOrderGateIsSilentOnTheHealthyTree — КОНТРОЛЬ.
//
// Без него всякая находка ниже означала бы лишь то, что гейт краснеет всегда.
// Здесь же утверждается и вторая половина: три законных близнеца молчат, а
// объём осмотренного НЕ нулевой.
func TestSubscriptionFloorOrderGateIsSilentOnTheHealthyTree(t *testing.T) {
	findings, census, err := runFloorOrderAudit(t, floorOrderTree{})
	if err != nil {
		t.Fatalf("обход синтетики: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на здоровом дереве: %v", findings)
	}
	if len(census.PageReaders) != 1 {
		t.Fatalf("выборок окном найдено %d, ожидалась одна (`read`): %v",
			len(census.PageReaders), census.PageReaders)
	}
	if len(census.Judged) != 1 || len(census.OrderOK) != 1 {
		t.Fatalf("рассмотрено %d функций, порядок верен у %d — ожидалась ровно одна (`drain`): %v",
			len(census.Judged), len(census.OrderOK), census.Judged)
	}
	// Близнецы: `seat` спрашивает пол, но страницы не читает; `sample` зовёт
	// одноимённый метод чужого пакета вне подписки.
	if len(census.FloorAsks) != 2 {
		t.Fatalf("вопросов о поле найдено %d, ожидалось два (`drain` и `seat`): %v",
			len(census.FloorAsks), census.FloorAsks)
	}
	if joined := strings.Join(census.Judged, " "); strings.Contains(joined, "sample") ||
		strings.Contains(joined, "seat") || strings.Contains(joined, "sweep") {
		t.Fatalf("законный близнец попал под суд: %s", joined)
	}
}

// TestSubscriptionFloorOrderGateFindsTheFloorTakenFirst — ИНЪЕКЦИЯ ПОРЯДКА.
//
// Переставлены РОВНО ДВА оператора одного тела. Состав файлов, вызовы и запрос
// те же самые, поэтому красное не может прийти ни от какого соседа.
func TestSubscriptionFloorOrderGateFindsTheFloorTakenFirst(t *testing.T) {
	findings, census, err := runFloorOrderAudit(t, floorOrderTree{floorBeforePage: true})
	if err != nil {
		t.Fatalf("обход синтетики: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась ровно одна: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0].What, "drain") {
		t.Errorf("находка не называет виновника поимённо: %s", findings[0].What)
	}
	if !strings.Contains(findings[0].What, "пол берётся РАНЬШЕ страницы") {
		t.Errorf("находка не называет предмет: %s", findings[0].What)
	}
	if len(census.Judged) != 1 || len(census.OrderOK) != 0 {
		t.Errorf("перепись не сошлась: рассмотрено %d, порядок верен у %d",
			len(census.Judged), len(census.OrderOK))
	}
}

// TestSubscriptionFloorOrderGateFindsItInTheInlineForm — ИНЪЕКЦИЯ ПОРЯДКА во
// ВТОРОЙ законной форме записи предмета.
//
// Выборка перенесена внутрь тела (окно литералом, помощника нет). Распознаватель,
// знающий только расщеплённую форму, объявил бы её ОТСУТСТВУЮЩЕЙ — не
// нарушением, а невидимостью, — и перенос выборки внутрь функции молча снимал бы
// гейт, не меняя поведения.
func TestSubscriptionFloorOrderGateFindsItInTheInlineForm(t *testing.T) {
	ok, _, err := runFloorOrderAudit(t, floorOrderTree{inlineWindow: true})
	if err != nil {
		t.Fatalf("обход синтетики (порядок верен): %v", err)
	}
	if len(ok) != 0 {
		t.Fatalf("слитная форма с ВЕРНЫМ порядком объявлена нарушением: %v", ok)
	}

	findings, _, err := runFloorOrderAudit(t,
		floorOrderTree{inlineWindow: true, floorBeforePage: true})
	if err != nil {
		t.Fatalf("обход синтетики: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась ровно одна: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0].What, "drain") {
		t.Errorf("находка не называет виновника поимённо: %s", findings[0].What)
	}
}

// TestSubscriptionFloorOrderGateRefusesAPremiselessTree — ИНЪЕКЦИЯ ПРЕДПОСЫЛКИ.
//
// Исход здесь ДРУГОЙ и перепутать его с находкой нельзя: обход отказывает
// ошибкой, а находок не производит вовсе. Без этого прогона молчание
// предпосылочных проверок было бы неотличимо от их отсутствия — то есть «ноль
// находок» от «ноль прочитанного».
func TestSubscriptionFloorOrderGateRefusesAPremiselessTree(t *testing.T) {
	cases := []struct {
		name string
		tree floorOrderTree
		says string
	}{
		{"выборки окном нет", floorOrderTree{noWindow: true}, "ни одной выборки журнала окном"},
		{"пол не спрашивает никто", floorOrderTree{noFloor: true}, "потеряло предмет"},
		{"файлов подписки нет", floorOrderTree{noOwned: true}, "ни одного файла пакета"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			findings, _, err := runFloorOrderAudit(t, c.tree)
			if err == nil {
				t.Fatalf("обход прошёл молча при снятой предпосылке — «ноль находок» стало "+
					"неотличимо от «ноль прочитанного» (находок %d)", len(findings))
			}
			if len(findings) != 0 {
				t.Errorf("при отказе обхода произведено %d находок — исходы обязаны быть разными",
					len(findings))
			}
			if !strings.Contains(err.Error(), c.says) {
				t.Errorf("отказ не называет снятую предпосылку: %v", err)
			}
		})
	}

	// Пустое дерево: обход не прочитал НИ ОДНОГО файла.
	_, _, err := AuditSubscriptionFloorAfterPage(
		SubscriptionFloorAfterPageOptions{Root: t.TempDir()}, nil)
	if err == nil || !strings.Contains(err.Error(), "ни одного файла Go") {
		t.Fatalf("пустой обход не назван беспредметным: %v", err)
	}
}
