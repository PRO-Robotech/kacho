// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

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
	findings, census, err := auditDeferredWork(root)
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}

	t.Logf("%s; находок %d", census, len(findings))

	if census.Read == 0 {
		t.Fatal("не прочитано ни одного файла — «маркеров нет» здесь означало бы «ничего не " +
			"читал», а не чистое дерево")
	}
	if len(census.Roots) == 0 {
		t.Fatal("верхний уровень индекса пуст — область обхода выведена из ничего, и её покрытие " +
			"ничего не утверждает")
	}
	// Вид вычитания, которому больше нечего вычитать, — находка: под его именем
	// можно положить что угодно, и счёт не сдвинется.
	for _, s := range deferralSkips {
		if census.Skipped[s.name] == 0 {
			t.Errorf("вид вычитания %q не вычел НИ ОДНОГО файла — у него больше нет предмета "+
				"(заведён потому, что %s). Снимите вид вместе с предметом: оставленный, он "+
				"остаётся слепой зоной, в которую можно внести отсрочку незамеченной", s.name, s.why)
		}
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

// Перепись цитат (deferralCensus.Mentions) печатается, но НЕ роняет прогон на
// нуле — и это решение, а не упущение. Способность фильтра сработать доказывается
// ИНЪЕКЦИЕЙ (TestDeferralGateStaysSilentOnAQuotedMentionOfTheMarker), а не тем,
// что в дереве сегодня есть о чём поговорить. Требование «отсечено хотя бы одно»
// было бы требованием держать в дереве прозу о маркерах ради зелёного — то же
// самое, что запись в перечне прощённых, только наизнанку.

// Пробы самоистечения перечня исключений здесь НЕТ, потому что нет и перечня:
// уточнение предиката до формы обращения обнулило предмет всех четырёх записей,
// и механизм снят целиком. Если он вернётся — вернётся и проба: список прощённых
// без проверки на предмет есть место, куда отсрочку вносят незамеченной.

// --- инъекция: обе стороны гоняют ТУ ЖЕ функцию, что и гейт по дереву ---

func synthDeferralTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	// Дерево обязано быть непустым: обход берёт состав у индекса и отказывает на
	// пустом — «смотреть не на что» есть отказ, а не успех. Подкаталоги при этом
	// НЕ засеиваются: область выводится из индекса, поэтому корни синтетического
	// дерева — ровно те, которые написала сама проба. Прежняя редакция засевала
	// семь выписанных корней, и проба про восьмой была бы неотличима от пробы про
	// первый.
	if err := os.WriteFile(filepath.Join(root, "README.md"),
		[]byte("# синтетическое дерево пробы\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
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
	findings, census, err := auditDeferredWork(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if census.Read == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	if len(findings) != 1 || !strings.Contains(findings[0].Where, "thing.go") {
		t.Fatalf("маркер в прод-коде не пойман: %+v", findings)
	}
}

// Сторона дефекта, которую ВЫПИСАННАЯ область не ловила: маркер в корне,
// которого в перечне не было.
//
// Область обхода задавалась семью именами, выписанными руками, а верхний уровень
// индекса несёт больше. Вне перечня оставалась почти четверть отслеживаемого
// дерева, и «находок ноль» там означало «не читал»: запрет держался не свойством
// дерева, а тем, что нарушение легло в один из семи названных каталогов.
func TestDeferralGateCatchesAMarkerOutsideTheHandwrittenRoots(t *testing.T) {
	root := synthDeferralTree(t, map[string]string{
		"ui-future/console/src/app.ts": "export const x = 1;\n" +
			"// " + "TODO" + ": дочинить после релиза\n",
	})
	findings, census, err := auditDeferredWork(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if census.Read == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	if len(findings) != 1 || !strings.Contains(findings[0].Where, "ui-future/console/src/app.ts") {
		t.Fatalf("маркер вне выписанных корней не пойман (%+v): область обхода обязана "+
			"ВЫВОДИТЬСЯ из индекса дерева, иначе каталог, появившийся после написания "+
			"перечня, покрыт не будет и об этом никто не узнает", findings)
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
	findings, census, err := auditDeferredWork(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if census.Read == 0 {
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
			findings, census, err := auditDeferredWork(root)
			if err != nil {
				t.Fatalf("обход синтетического дерева: %v", err)
			}
			if census.Read == 0 {
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

// TestDeferralGateStaysSilentOnAQuotedMentionOfTheMarker — разговор О маркере
// обещанием не является, и порядок слов в нём ничего не решает.
//
// Англоязычная форма отделяет обещание от упоминания ФОРМОЙ ОБРАЩЕНИЯ (`TODO:`
// адресует читателя кода, голое слово внутри предложения — нет). У русских форм
// такого знака препинания в языке не существует, поэтому та же граница проходит
// по употреблению против упоминания: фраза в кавычках — цитата, а не обещание.
//
// Проба несёт ОБЕ стороны в одном дереве: цитата обязана молчать, а настоящее
// обещание рядом с ней — краснеть. Без положительного контроля «ноль находок»
// было бы неотличимо от гейта, разучившегося падать.
func TestDeferralGateStaysSilentOnAQuotedMentionOfTheMarker(t *testing.T) {
	root := synthDeferralTree(t, map[string]string{
		// Упоминание в прямом порядке слов — под отрицанием, в кавычках.
		"services/x/docs/acceptance/a.md": "То, что задача не делает, названо явно\n" +
			"— не «" + "потом " + "доделаем», а другой предмет с другим номером.\n",
		// Упоминание в обратном порядке слов — то же по существу.
		"services/x/docs/acceptance/b.md": "| Р8 | Полнота: нет «" + "доделаем " + "потом» | ДА |\n",
		// Положительный контроль В ТОМ ЖЕ ВИДЕ ФАЙЛА, что и цитаты: проза, та же
		// фраза, отличается ТОЛЬКО отсутствием кавычек. Без него молчание на
		// цитатах было бы неотличимо от «гейт разучился читать разметку».
		"services/x/docs/acceptance/c.md": "Порт закрыт наполовину, " + "потом " + "доделаем.\n",
		// Положительный контроль в прод-коде.
		"services/x/internal/thing.go": "package thing\n\n// " + "пока " + "заглушка\nfunc F() {}\n",
	})
	findings, census, err := auditDeferredWork(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if census.Read == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	seen := map[string]bool{}
	for _, f := range findings {
		seen[f.Where] = true
		if strings.HasPrefix(f.Where, "services/x/docs/acceptance/a.md") ||
			strings.HasPrefix(f.Where, "services/x/docs/acceptance/b.md") {
			t.Errorf("гейт объявил находкой ЦИТАТУ маркера: %s — %q\n"+
				"Разговор о маркере не есть обещание; запрет, ловящий слово, заставляет "+
				"переписывать собственную документацию о запрете — и первым делом снимают "+
				"сам запрет", f.Where, f.Line)
		}
	}
	if census.Mentions != 2 {
		t.Errorf("цитат отсечено %d, а их в дереве пробы две — перепись обязана называть "+
			"объём отсечённого, иначе «находок ноль» неотличимо от «фильтр съел всё»",
			census.Mentions)
	}
	// Обе стороны положительного контроля названы координатой: находка без неё —
	// не действие.
	for _, want := range []string{"services/x/docs/acceptance/c.md:1", "services/x/internal/thing.go:3"} {
		if !seen[want] {
			t.Fatalf("положительный контроль не сработал: обещание %s не поймано (%+v) — "+
				"тогда молчание на цитатах ничего не доказывает, гейт мог просто "+
				"разучиться падать", want, findings)
		}
	}
}

// TestDeferralGateCatchesTheReversedRussianWordOrder — порядок слов не есть
// существо: «доделаем потом» откладывает ровно то же, что «потом доделаем».
//
// Порядок слов в русском свободен, поэтому распознаватель, знающий одну
// перестановку из двух, оставляет вторую вне наблюдения — не находкой и не
// чистотой, а невидимостью (норма §«Гейт на класс», п.7).
func TestDeferralGateCatchesTheReversedRussianWordOrder(t *testing.T) {
	root := synthDeferralTree(t, map[string]string{
		"services/x/internal/thing.go": "package thing\n\n// " + "доделаем " + "потом\nfunc F() {}\n",
	})
	findings, census, err := auditDeferredWork(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if census.Read == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	if len(findings) != 1 || !strings.Contains(findings[0].Where, "thing.go") {
		t.Fatalf("обратный порядок слов прошёл мимо гейта (%+v): распознаватель различает "+
			"ПЕРЕСТАНОВКУ, а не предмет — всё написанное во второй перестановке остаётся "+
			"вне наблюдения", findings)
	}
}
