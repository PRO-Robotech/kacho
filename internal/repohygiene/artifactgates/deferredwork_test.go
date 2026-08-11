// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package artifactgates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoDeferredWorkInTheTree — в дереве нет маркеров отложенной работы.
//
// Что делать, если гейт сработал, — три исхода, четвёртого нет:
//
//  1. работа нужна → сделать её СЕЙЧАС, в том же изменении. Правило продукта
//     не знает состояния «сделаю в следующем PR»;
//  2. работа не нужна → снять маркер вместе с кодом, который он подпирал;
//  3. работа принадлежит другому предмету и требует решения → завести
//     ПРЕДМЕТ (issue/приёмку) с причиной и предикатом снятия, а в коде не
//     оставлять обещания. Маркер отличается от предмета тем, что за ним никто
//     не отвечает.
//
// Заводить запись в перечень объяснений — НЕ исход: он закрыт и предназначен
// только для прозы О САМИХ маркерах.
func TestNoDeferredWorkInTheTree(t *testing.T) {
	root := repoRoot(t)
	findings, files, err := auditDeferredWork(root)
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}

	t.Logf("перепись: не-тестовых файлов прочитано %d; находок %d "+
		"(перечня исключений у этого гейта нет — предикат ловит форму обращения, а не слово)",
		files, len(findings))

	if files == 0 {
		t.Fatal("не прочитано ни одного файла — «маркеров нет» здесь означало бы «ничего не " +
			"читал», а не чистое дерево")
	}

	for _, f := range findings {
		t.Errorf("отложенная работа: %s — %q\n"+
			"Правила продукта не знают состояния «сделаю позже» (ban #11, ban #14): работа делается "+
			"сразу в production-форме. Исходы: сделать сейчас / снять вместе с кодом / завести "+
			"ПРЕДМЕТ с причиной и предикатом снятия. Маркер отличается от предмета тем, что за "+
			"ним никто не отвечает — и он переживает своё основание: ровно так хук восстановления "+
			"пароля простоял нацеленным в никуда, пока его отсрочка ссылалась на препятствие, "+
			"которого давно не было.", f.Where, f.Line)
	}
}

// Пробы самоистечения перечня исключений здесь НЕТ, потому что нет и перечня:
// уточнение предиката до формы обращения обнулило предмет всех четырёх записей,
// и механизм снят целиком. Если он вернётся — вернётся и проба: список прощённых
// без проверки на предмет есть место, куда отсрочку вносят незамеченной.

// --- инъекция: обе стороны гоняют ТУ ЖЕ функцию, что и гейт по дереву ---

func synthDeferralTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for _, sub := range deferralScanRoots {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
		// Каждое поддерево обязано быть непустым: обход требует состав у индекса
		// и отказывает на пустом — «смотреть не на что» есть отказ, а не успех.
		seed := filepath.Join(root, sub, "seed.go")
		if err := os.WriteFile(seed, []byte("package seed\n"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", sub, err)
		}
	}
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	synthTrack(t, root)
	return root
}

// Сторона дефекта: маркер в прод-коде роняет гейт и называет координату.
func TestDeferralGateCatchesAMarkerInProductionCode(t *testing.T) {
	root := synthDeferralTree(t, map[string]string{
		"services/x/internal/thing.go": "package thing\n\n// TODO: дочинить после релиза\nfunc F() {}\n",
	})
	findings, files, err := auditDeferredWork(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if files == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	if len(findings) != 1 || !strings.Contains(findings[0].Where, "thing.go") {
		t.Fatalf("маркер в прод-коде не пойман: %+v", findings)
	}
}

// Законная сторона: тест и шаблон БЕЗ маркера гейт не трогает.
//
// Без этой половины запрет ловил бы форму, а не существо: первая же фикстура
// соседнего гейта, обязанная написать форму дефекта, красила бы прогон.
func TestDeferralGateStaysSilentOnLawfulTree(t *testing.T) {
	root := synthDeferralTree(t, map[string]string{
		"services/x/internal/thing_test.go": "package thing\n\n// TODO: фикстура гейта пишет форму дефекта\n",
		"deploy/chart.yaml":                 "kind: ConfigMap\n# объяснение без отсрочки\n",
	})
	findings, files, err := auditDeferredWork(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if files == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	if len(findings) != 0 {
		t.Fatalf("гейт нашёл дефект в законном дереве: %+v", findings)
	}
}

// TestEveryDeferralFormIsCaughtThroughTheSieve — КАЖДАЯ объявленная форма
// доходит до решения, пройдя дешёвый отсев.
//
// Отсев по затравке существует ради времени: без него гейт стоил треть бюджета
// пакета и ронял конвейер по таймауту. Но отсев — это место, где форма может
// исчезнуть молча: образец её ловит, а до образца дело не доходит. Поэтому
// каждая форма проверяется НА СКВОЗНОМ пути — тем же auditDeferredWork, что
// работает по дереву, а не образцом в отрыве от сита.
func TestEveryDeferralFormIsCaughtThroughTheSieve(t *testing.T) {
	if len(deferralForms) == 0 {
		t.Fatal("осмотрено: форм 0 — «все формы ловятся» здесь означало бы «форм нет»")
	}
	for _, f := range deferralForms {
		t.Run(f.seed, func(t *testing.T) {
			if !hasDeferralSeed(f.example) {
				t.Fatalf("затравка %q не встречается в примере %q — отсев отсечёт эту форму "+
					"ДО образца, и она перестанет ловиться, оставаясь на вид объявленной",
					f.seed, f.example)
			}
			root := synthDeferralTree(t, map[string]string{
				"services/x/internal/thing.go": "package thing\n\n" + f.example + "\nfunc F() {}\n",
			})
			findings, files, err := auditDeferredWork(root)
			if err != nil {
				t.Fatalf("обход синтетического дерева: %v", err)
			}
			if files == 0 {
				t.Fatal("синтетическое дерево не прочитано")
			}
			if len(findings) != 1 {
				t.Fatalf("форма %q на примере %q не поймана сквозным путём: %+v",
					f.seed, f.example, findings)
			}
		})
	}
	t.Logf("осмотрено: форм %d, затравок %d", len(deferralForms), len(deferralSeeds))
}

// Русскоязычная форма ловится наравне с англоязычной.
//
// Корпус двуязычен, и запрет, знающий только TODO, обходится словом «потом» без
// единой уловки — то есть был бы запретом написания, а не отсрочки.
func TestDeferralGateCatchesTheRussianForm(t *testing.T) {
	root := synthDeferralTree(t, map[string]string{
		"services/x/internal/thing.go": "package thing\n\n// пока заглушка, потом доделаем\nfunc F() {}\n",
	})
	findings, _, err := auditDeferredWork(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("русскоязычная отсрочка прошла мимо гейта — запрет ловит написание, а не предмет")
	}
}
