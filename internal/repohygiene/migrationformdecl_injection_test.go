// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migrationformdecl_injection_test.go — доказательство в ОБЕ стороны.
//
// (а) верни расхождение — разбор краснеет и НАЗЫВАЕТ координату (файл и строку);
// (б) поставь рядом законный близнец — разбор молчит.
//
// Без (б) проверка ловила бы форму, а не существо: «в файле написано про номер
// миграции» — нормальное состояние и README, и гейта, и разбора решения. Первое
// же ложное срабатывание такую проверку отключило бы.
//
// Близнецы здесь РАЗНЫЕ по существу, а не переписанные: законным делает не
// формулировка, а РОЛЬ места (канонический документ · гейт · все остальные) и
// присутствие действующей формы рядом с отвергнутой.
package repohygiene

import (
	"strings"
	"testing"
)

// synthForm — канон синтетического разбора. Отдельный от боевого намеренно:
// проба обязана проверять РАЗБОР, а не заучивать канон настоящего дерева.
var synthForm = canonicalMigrationForm{Token: "YYYYMMDDHHMMSS", Digits: 14}

const (
	synthLegacyLine = "Имя новой миграции — `<задача><порядковый:3>_<что_делает>.sql`."
	synthCanonLine  = "Имя новой миграции — метка времени `YYYYMMDDHHMMSS_<что_делает>.sql`."
	synthPlainLine  = "Применённую миграцию не редактируем — только новая."
)

func formFindingsFor(t *testing.T, rel, body string) []string {
	t.Helper()
	out, census := migrationFormFindings([]migrationFormDoc{{Rel: rel, Body: body}}, synthForm)
	if census.FilesRead != 1 {
		t.Fatalf("перепись синтетики: прочитано %d документов из 1 — разбор читает не то, "+
			"что положили", census.FilesRead)
	}
	return out
}

func TestMigrationFormDecl_ProvenByInjection(t *testing.T) {
	t.Run("обычное место называет отвергнутую форму — краснеет с координатой", func(t *testing.T) {
		rel := "services/widget/internal/migrations/README.md"
		out := formFindingsFor(t, rel, synthPlainLine+"\n"+synthLegacyLine+"\n")
		if len(out) != 1 {
			t.Fatalf("ждали ровно одну находку, получили %d: %v", len(out), out)
		}
		if !strings.Contains(out[0], rel+":2") {
			t.Errorf("находка не называет координату (файл:строка): %s", out[0])
		}
		if !strings.Contains(out[0], synthForm.Token) {
			t.Errorf("находка не называет действующую форму — читателю некуда идти: %s", out[0])
		}
		if !strings.Contains(out[0], canonicalMigrationFormDoc) {
			t.Errorf("находка не называет место, где форма объявлена: %s", out[0])
		}
	})

	t.Run("законный близнец: то же место повторяет ДЕЙСТВУЮЩУЮ форму — молчит", func(t *testing.T) {
		// Повтор действующей формы там, где читатель смотрит, — законен и полезен.
		// Если бы разбор краснел и на нём, он требовал бы вычистить из README
		// ответ на единственный вопрос, ради которого туда заглядывают.
		out := formFindingsFor(t, "services/widget/internal/migrations/README.md",
			synthPlainLine+"\n"+synthCanonLine+"\n")
		if len(out) != 0 {
			t.Errorf("законный повтор действующей формы объявлен находкой: %v", out)
		}
	})

	t.Run("законный близнец: место вовсе не называет формы — молчит", func(t *testing.T) {
		out := formFindingsFor(t, "services/widget/docs/content/install/deploy.mdx",
			synthPlainLine+"\n")
		if len(out) != 0 {
			t.Errorf("документ без объявления формы объявлен находкой: %v", out)
		}
	})

	t.Run("гейт разбирает отвергнутую форму РЯДОМ с действующей — молчит", func(t *testing.T) {
		// Гейту разбор отвергнутого позволен: он обязан объяснить, что именно
		// запрещает. Условие — действующая форма названа в том же файле.
		rel := gateSourceDir + "migrationwidget_test.go"
		out := formFindingsFor(t, rel, "// Прежде было так: "+synthLegacyLine+
			"\n// Теперь так: "+synthCanonLine+"\n")
		if len(out) != 0 {
			t.Errorf("разбор отвергнутой формы рядом с действующей объявлен находкой: %v", out)
		}
	})

	t.Run("гейт называет ТОЛЬКО отвергнутую форму — краснеет", func(t *testing.T) {
		rel := gateSourceDir + "migrationwidget_test.go"
		out := formFindingsFor(t, rel, "// Отказ: "+synthLegacyLine+"\n")
		if len(out) != 1 {
			t.Fatalf("ждали одну находку, получили %d: %v", len(out), out)
		}
		if !strings.Contains(out[0], rel+":1") {
			t.Errorf("находка не называет координату: %s", out[0])
		}
	})

	t.Run("канонический документ разбирает отвергнутую форму — молчит", func(t *testing.T) {
		out := formFindingsFor(t, canonicalMigrationFormDoc,
			synthCanonLine+"\nОтвергнуто: "+synthLegacyLine+"\n")
		if len(out) != 0 {
			t.Errorf("разбор отвергнутого в месте объявления назван находкой: %v", out)
		}
	})

	t.Run("канонический документ не называет действующей формы — краснеет", func(t *testing.T) {
		out := formFindingsFor(t, canonicalMigrationFormDoc, synthLegacyLine+"\n")
		if len(out) != 1 {
			t.Fatalf("ждали одну находку, получили %d: %v", len(out), out)
		}
		if !strings.Contains(out[0], "не называет действующую") {
			t.Errorf("находка не называет предмет: %s", out[0])
		}
	})

	t.Run("перепись считает ОБЕ формы, а не только находки", func(t *testing.T) {
		_, census := migrationFormFindings([]migrationFormDoc{
			{Rel: "a/README.md", Body: synthCanonLine + "\n"},
			{Rel: "b/README.md", Body: synthLegacyLine + "\n"},
			{Rel: "c/README.md", Body: synthPlainLine + "\n"},
		}, synthForm)
		if census.FilesRead != 3 || census.FilesWithForm != 2 {
			t.Errorf("перепись: прочитано %d (ждали 3), называют форму %d (ждали 2)",
				census.FilesRead, census.FilesWithForm)
		}
		if census.Canonical != 1 || census.Legacy != 1 {
			t.Errorf("перепись форм: действующей %d (ждали 1), отвергнутой %d (ждали 1)",
				census.Canonical, census.Legacy)
		}
	})

	t.Run("пустой корпус даёт нулевую перепись — предпосылка гейта её и ловит", func(t *testing.T) {
		out, census := migrationFormFindings(nil, synthForm)
		if len(out) != 0 || census.FilesRead != 0 {
			t.Errorf("пустой корпус: находок %d, прочитано %d — ждали 0 и 0", len(out), census.FilesRead)
		}
	})
}

// TestMigrationFormCanonIsReadFromTheEnforcingGate — канон берётся из гейта,
// который его требует, а не выписан рядом.
func TestMigrationFormCanonIsReadFromTheEnforcingGate(t *testing.T) {
	t.Run("ширина берётся у ПРИНИМАЮЩЕЙ регулярки, а не у первой попавшейся", func(t *testing.T) {
		// У гейта их две: принимающая (14) и отвергающая (6). Текстовый поиск
		// взял бы любую — разбор берёт связанную с именем принимающей.
		src := "package p\n" +
			"// Версия новой миграции — `YYYYMMDDHHMMSS`.\n" +
			"func f() {\n" +
			"\tlegacy := regexp.MustCompile(`^(\\d{6})_`)\n" +
			"\ttimestamped := regexp.MustCompile(`^(\\d{14})_`)\n" +
			"\t_, _ = legacy, timestamped\n" +
			"}\n"
		got, err := readCanonicalMigrationForm(src)
		if err != nil {
			t.Fatalf("канон не извлечён: %v", err)
		}
		if got.Digits != 14 {
			t.Errorf("ширина %d, ждали 14 — взята регулярка отказа, а не приёма", got.Digits)
		}
		if got.Token != "YYYYMMDDHHMMSS" {
			t.Errorf("запись формы %q, ждали YYYYMMDDHHMMSS", got.Token)
		}
	})

	t.Run("гейт, называющий РАЗНЫЕ записи формы, — отказ, а не молчание", func(t *testing.T) {
		src := "package p\n" +
			"// Версия новой миграции — `YYYYMMDDHHMMSS`.\n" +
			"// А в тексте отказа сказано `YYYYMMDD`.\n" +
			"func f() { timestamped := regexp.MustCompile(`^(\\d{14})_`); _ = timestamped }\n"
		if _, err := readCanonicalMigrationForm(src); err == nil {
			t.Error("гейт называет читателю две разные записи формы, а разбор принял это молча")
		}
	})
}
