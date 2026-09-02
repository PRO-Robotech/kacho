// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_verification_mirrors_the_requirement_injection_test.go —
// доказательство того, что MAIL-16 СПОСОБЕН упасть, падает ТОЛЬКО на своём
// предмете (MAIL-17) и судит СУММУ источников, а не файл.
//
// Прогонов девять, и они трёх родов:
//
//	контроль          сведённое дерево — молчание;
//	инъекции (4)      по одной на КАЖДУЮ форму записи и на каждую сторону
//	                  набора: блочная в профиле, поточная в профиле, поточная
//	                  многоключевая, выключение в САМОМ единственном объявлении;
//	близнецы (3)      MAIL-17: поток, требованию не зеркальный; согласное второе
//	                  мнение; проза, называющая объявление словами.
//
// Плюс подпроба ЕДИНИЦЫ СЧЁТА: на одном и том же дефекте пофайловый взгляд
// молчит, а взгляд по эффективному набору находит. Без неё «гейт судит сумму»
// осталось бы заявлением, а не свойством.
//
// Инъекция роняет ТОЛЬКО проверяемое (`testing.md` §«Гейт на класс», п. 2в):
// каждая правка идёт по копии дерева в `t.TempDir()`, и на этой копии зовётся
// только функция MAIL-16 — соседние гейты своих вердиктов отсюда не получают и
// получить не могут.
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// verificationMirrorFixture — копия зонтичного чарта под инъекцию.
type verificationMirrorFixture struct{ root string }

func newVerificationMirrorFixture(t *testing.T) verificationMirrorFixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), "umbrella")
	copyTree(t, umbrellaDir, root)
	return verificationMirrorFixture{root: root}
}

func (f verificationMirrorFixture) run(t *testing.T) []string {
	t.Helper()
	return verificationMirrorFindings(t, f.root)
}

// replace — правка по ЕДИНСТВЕННОМУ якорю. Неоднозначный якорь сядет в первое
// вхождение, условия инъекции может не создать, и зелёный прогон означал бы
// «дефект не воспроизведён», а не «гейт исправен».
func (f verificationMirrorFixture) replace(t *testing.T, file, anchor, with string) {
	t.Helper()
	path := filepath.Join(f.root, file)
	raw := readFileForTest(t, path)
	if n := strings.Count(raw, anchor); n != 1 {
		t.Fatalf("якорь инъекции встречается в %s %d раз, а нужен ровно один:\n%q",
			file, n, anchor)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(raw, anchor, with, 1)), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// Якоря — единственные в своих файлах; единственность проверяется replace выше.
const (
	devFlowsAnchor = "        flows:\n          registration:\n"
	oryFlowsAnchor = "          settings: { ui_url: \"/settings\" }\n"
	// Единственное объявление потока подтверждения — то самое, к которому
	// сведены все стенды.
	singleDeclarationAnchor = "    verification:\n      enabled: true\n"
)

func mustName(t *testing.T, found []string, want ...string) {
	t.Helper()
	if len(found) == 0 {
		t.Fatalf("возвращённый дефект гейт НЕ нашёл — то есть #1234 воспроизводится, " +
			"а гейт остаётся зелёным")
	}
	joined := strings.Join(found, "\n")
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Errorf("находка не называет %q — диагностика есть часть свойства, а не "+
				"украшение: находка без координаты посылает читателя искать не там, на "+
				"неё тратят прогон, а потом снимают гейт как непонятный:\n%s", w, joined)
		}
	}
}

func TestVerificationMirrorGateFailsOnAReturnedDefect(t *testing.T) {
	t.Run("контроль: сведённое дерево — молчание", func(t *testing.T) {
		f := newVerificationMirrorFixture(t)
		if found := f.run(t); len(found) > 0 {
			t.Errorf("гейт краснеет на НЕТРОНУТОЙ копии — его находки не про инъекцию, "+
				"и прогоны ниже недействительны:\n%s", strings.Join(found, "\n"))
		}
	})

	t.Run("инъекция: БЛОЧНАЯ форма возвращает выключение в профиль стенда", func(t *testing.T) {
		f := newVerificationMirrorFixture(t)
		f.replace(t, "values.dev.yaml", devFlowsAnchor,
			"        flows:\n          verification:\n            enabled: false\n          registration:\n")
		// Это дословное состояние дерева ДО сведения потоков: тот же файл, тот
		// же ключ, та же величина.
		mustName(t, f.run(t), "dev", "values.dev.yaml", verifiedAddressHook)
	})

	t.Run("инъекция: ПОТОЧНАЯ форма возвращает выключение в профиль боевой посадки", func(t *testing.T) {
		f := newVerificationMirrorFixture(t)
		f.replace(t, "values.fe3455-ory-posture.yaml", oryFlowsAnchor,
			oryFlowsAnchor+"          verification: { enabled: false }\n")
		mustName(t, f.run(t), "fe3455", "values.fe3455-ory-posture.yaml")
	})

	t.Run("инъекция: поточная форма С НЕСКОЛЬКИМИ ключами", func(t *testing.T) {
		// Форма, о которой распознаватель не знает, — не край: всё записанное в
		// ней оказывается ВНЕ наблюдения, то есть ни находкой, ни молчанием.
		f := newVerificationMirrorFixture(t)
		f.replace(t, "values.fe3455-ory-posture.yaml", oryFlowsAnchor,
			oryFlowsAnchor+"          verification: { enabled: false, ui_url: \"/verification\" }\n")
		mustName(t, f.run(t), "fe3455")
	})

	t.Run("инъекция: поток выключен в САМОМ единственном объявлении", func(t *testing.T) {
		// Состояние, которого MAIL-18 не видит BY CONSTRUCTION: второго мнения
		// нет, есть одно — и оно неверное. Ради этого случая гейт и существует
		// после того, как потоки сведены к одному месту.
		f := newVerificationMirrorFixture(t)
		f.replace(t, identityConfigInUmbrella, singleDeclarationAnchor,
			"    verification:\n      enabled: false\n")
		found := f.run(t)
		mustName(t, found, "prod", "dev")
		if n := len(found); n < 6 {
			t.Errorf("выключение в единственном объявлении задевает КАЖДЫЙ стенд, "+
				"а найдено %d — гейт судит не тот источник", n)
		}
	})

	t.Run("MAIL-17 близнец: поток, требованию НЕ зеркальный, — молчание", func(t *testing.T) {
		// `recovery` доставляется письмом и потому судится MAIL-15 и MAIL-18, но
		// зеркалом требования подтверждённого адреса не является. Покраснев на
		// нём, гейт стал бы красным там, где решения о выключении восстановления
		// приняты осознанно, — а такой снимают первым.
		f := newVerificationMirrorFixture(t)
		f.replace(t, "values.fe3455-ory-posture.yaml", oryFlowsAnchor,
			oryFlowsAnchor+"          recovery: { enabled: false }\n")
		if found := f.run(t); len(found) > 0 {
			t.Errorf("гейт покраснел на потоке, зеркалом требования НЕ являющемся, — он "+
				"ловит форму («ключ потока»), а не существо («поток, которым "+
				"подтверждается адрес»):\n%s", strings.Join(found, "\n"))
		}
	})

	t.Run("MAIL-17 близнец: СОГЛАСНОЕ второе мнение — молчание", func(t *testing.T) {
		// Второе мнение, СОВПАДАЮЩЕЕ с единственным объявлением, — предмет
		// MAIL-18 (форма), а не этого гейта (сумма значений). Покраснев здесь,
		// он стал бы вторым местом об одном предмете — ровно тем классом,
		// который обе проверки ловят.
		f := newVerificationMirrorFixture(t)
		f.replace(t, "values.dev.yaml", devFlowsAnchor,
			"        flows:\n          verification:\n            enabled: true\n          registration:\n")
		if found := f.run(t); len(found) > 0 {
			t.Errorf("гейт покраснел на СОГЛАСНОМ втором мнении — он повторяет предмет "+
				"MAIL-18 вместо своего:\n%s", strings.Join(found, "\n"))
		}
	})

	t.Run("MAIL-17 близнец: ПРОЗА, называющая объявление словами, — молчание", func(t *testing.T) {
		// Комментарии, которыми сведение потоков заменило снятые блоки, содержат
		// и `verification`, и `enabled`. Гейт, читающий сырой текст, находит своё
		// имя в объяснении собственного предмета и остаётся зелёным при снятом
		// объявлении.
		f := newVerificationMirrorFixture(t)
		f.replace(t, "values.dev.yaml", devFlowsAnchor,
			"        # verification:\n        #   enabled: false\n"+devFlowsAnchor)
		if found := f.run(t); len(found) > 0 {
			t.Errorf("гейт прочитал КОММЕНТАРИЙ как объявление — он судит текст, а не "+
				"то, что доезжает до процесса:\n%s", strings.Join(found, "\n"))
		}
	})
}

// TestVerificationMirrorGateJudgesTheSumNotTheFile — единица счёта доказана на
// ОДНОМ И ТОМ ЖЕ дефекте, а не заявлена в комментарии.
//
// Это тот самый замер, который §11 шаг 2 приёмки требует провести ДО написания
// гейта: пофайловый предикат зелён на обеих сторонах, и потому не измеряет
// ничего. Здесь он закреплён пробой, чтобы число не устарело молча.
func TestVerificationMirrorGateJudgesTheSumNotTheFile(t *testing.T) {
	f := newVerificationMirrorFixture(t)
	f.replace(t, "values.dev.yaml", devFlowsAnchor,
		"        flows:\n          verification:\n            enabled: false\n          registration:\n")

	perFile := verificationMirrorPerFileFindings(t, f.root)
	if len(perFile) > 0 {
		t.Fatalf("ПОФАЙЛОВЫЙ взгляд нашёл %v — значит дефект воспроизведён не тот: "+
			"расхождение обязано складываться только из СУММЫ источников, и каждый "+
			"файл обязан остаться внутренне согласованным", perFile)
	}
	if found := verificationMirrorFindings(t, f.root); len(found) == 0 {
		t.Fatalf("взгляд по ЭФФЕКТИВНОМУ НАБОРУ на том же дефекте промолчал — гейт " +
			"судит файл, а не сумму, и его единица счёта ничего не измеряет")
	}
}
