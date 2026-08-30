// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «пробу консоли читает разбор типов» СПОСОБЕН
// упасть — и что падает он на существе, а не на форме.
//
// Инъекция идёт в обе стороны, потому что одного «краснеет» мало (гейт,
// краснеющий на всём, ничего не измеряет), и одного «молчит» тоже мало (молчание
// бывает от того, что читать не стали):
//
//	каталог вне include            → находка, называющая координату;
//	тот же каталог в include       → молчание, и перепись файл ЗАСЧИТЫВАЕТ;
//	точное имя файла               → молчание (форма без подстановок);
//	голое имя каталога             → молчание (TypeScript читает его рекурсивно);
//	`*` через границу каталога     → находка: звёздочка не глотает `/`, и файл
//	                                 во вложенном каталоге читателя не имеет;
//	комментарии и `//` в строке    → разобрано верно: путь внутри строки за
//	                                 комментарий не принят;
//	include пуст                   → перепись объявляет НОЛЬ шаблонов, а не
//	                                 чистоту: гейт по дереву на этом Fatal'ит;
//	объявление негодно             → ошибка возвращается, а не проглатывается.
//
// Все случаи гоняют ТУ ЖЕ функцию (`auditConsoleProbeTypeCoverage`), что и прогон
// по дереву: проба, повторяющая логику гейта своей копией, доказывала бы свойство
// копии.
package repohygiene

import (
	"strings"
	"testing"
)

// synthTsconfigWithoutAwaiting — состояние ДО фикса: каталог проб, ждущих
// условия, вне разбора типов. Каркас взят у `ui-future/e2e/tsconfig.json`, вместе
// с его комментариями: они и есть то, обо что наивный разбор JSON спотыкается.
const synthTsconfigWithoutAwaiting = `{
  // Проект TypeScript для сквозных проб консоли.
  /* Собирать здесь нечего: noEmit. */
  "compilerOptions": { "strict": true, "noEmit": true },
  "include": [
    "specs/**/*.ts",
    "scripts/**/*.ts",
    "playwright.config.ts",
    "remote-browser-policy.ts"
  ]
}`

// synthTsconfigWithAwaiting — состояние ПОСЛЕ фикса: тот же проект, каталог
// внесён. Отличие от предыдущего ровно одно — ради этого пара и стоит.
const synthTsconfigWithAwaiting = `{
  "compilerOptions": { "strict": true, "noEmit": true },
  "include": [
    "specs/**/*.ts",
    "specs-awaiting-journal-owner/**/*.ts",
    "scripts/**/*.ts",
    "playwright.config.ts",
    "remote-browser-policy.ts"
  ]
}`

// synthTsconfigBareDir — голое имя каталога: TypeScript читает такой каталог
// рекурсивно, и распознаватель обязан это знать. Не знай он — объявил бы
// непокрытым файл, у которого читатель есть, то есть дал бы находку на исправном.
const synthTsconfigBareDir = `{
  "include": ["specs", "scripts"]
}`

// synthTsconfigSingleStar — `*` НЕ пересекает границу каталога. Форма
// намеренно узкая: вложенный файл читателя не имеет, и это находка, а не
// придирка.
const synthTsconfigSingleStar = `{
  "include": ["specs/*.ts"]
}`

// synthTsconfigStringWithSlashes — путь со слэшами ВНУТРИ строки. Наивное снятие
// комментариев съело бы объявление целиком, перечень шаблонов стал бы пустым, и
// гейт назвал бы непокрытым ВСЁ — находки были бы свойством разбора, а не дерева.
const synthTsconfigStringWithSlashes = `{
  // ссылка на разбор: https://example.invalid/why — не комментарий, а часть строки ниже
  "compilerOptions": { "paths": { "@x/*": ["https://example.invalid/*"] } },
  "include": ["specs/**/*.ts"]
}`

const synthTsconfigEmptyInclude = `{ "compilerOptions": { "noEmit": true } }`

const synthTsconfigBroken = `{ "include": [ "specs/**/*.ts" `

// TestConsoleProbeTypeCoverageSeparatesUnreadFromUnwatched — сердце инъекции:
// один и тот же состав пакета, два объявления проекта, разные вердикты.
func TestConsoleProbeTypeCoverageSeparatesUnreadFromUnwatched(t *testing.T) {
	files := []string{
		"specs/mutate.spec.ts",
		"specs/fixtures.ts",
		"scripts/verdict.ts",
		"playwright.config.ts",
		"remote-browser-policy.ts",
		"specs-awaiting-journal-owner/subscription-stream.spec.ts",
	}

	// ── ДЕФЕКТ: каталог ожидания читателя не имеет ───────────────────────────
	census, findings, err := auditConsoleProbeTypeCoverage(synthTsconfigWithoutAwaiting, files)
	if err != nil {
		t.Fatalf("объявление с комментариями не разобралось: %v — тогда всякий вердикт "+
			"ниже был бы свойством разбора, а не дерева", err)
	}
	if len(findings) != 1 {
		t.Fatalf("гейт не увидел непрочитанной пробы: находок %d, ожидалась 1 (%v). "+
			"Гейт, молчащий на своём предмете, хуже отсутствующего", len(findings), findings)
	}
	if got := findings[0].File; got != "specs-awaiting-journal-owner/subscription-stream.spec.ts" {
		t.Errorf("находка не называет координату: %q. Находка без координаты посылает "+
			"читателя искать не там", got)
	}
	if census.Files != len(files) || census.Covered != len(files)-1 {
		t.Errorf("перепись не сходится: осмотрено %d, покрыто %d при %d файлах — "+
			"«ноль находок» перестало бы быть отличимым от «ноль прочитанного»",
			census.Files, census.Covered, len(files))
	}

	// ── ЗАКОННЫЙ БЛИЗНЕЦ: тот же состав, каталог внесён ──────────────────────
	census2, findings2, err := auditConsoleProbeTypeCoverage(synthTsconfigWithAwaiting, files)
	if err != nil {
		t.Fatalf("объявление после фикса не разобралось: %v", err)
	}
	if len(findings2) != 0 {
		t.Errorf("гейт краснеет на исправном: %v. Гейт, краснеющий на верном коде, "+
			"отключают первым — и вместе с ним перестают читать настоящие находки", findings2)
	}
	if census2.Covered != len(files) {
		t.Errorf("перепись не засчитала внесённый каталог: покрыто %d из %d — "+
			"молчание гейта было бы получено даром", census2.Covered, len(files))
	}
}

// TestConsoleProbeTypeCoverageKnowsEveryLegalPatternForm — распознаватель обязан
// знать ВСЕ законные формы записи предмета: форма, о которой он не знает, даёт
// находку там, где читатель есть.
func TestConsoleProbeTypeCoverageKnowsEveryLegalPatternForm(t *testing.T) {
	// Голое имя каталога — рекурсивное чтение.
	_, findings, err := auditConsoleProbeTypeCoverage(synthTsconfigBareDir,
		[]string{"specs/mutate.spec.ts", "specs/nested/deep.spec.ts", "scripts/verdict.ts"})
	if err != nil {
		t.Fatalf("голое имя каталога не разобралось: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("каталог, объявленный голым именем, читается TypeScript рекурсивно, "+
			"а гейт назвал его файлы непрочитанными: %v", findings)
	}

	// Звёздочка НЕ пересекает границу каталога — и это не придирка: файл во
	// вложенном каталоге такой шаблон не читает.
	_, findings, err = auditConsoleProbeTypeCoverage(synthTsconfigSingleStar,
		[]string{"specs/mutate.spec.ts", "specs/nested/deep.spec.ts"})
	if err != nil {
		t.Fatalf("узкий шаблон не разобрался: %v", err)
	}
	if len(findings) != 1 || findings[0].File != "specs/nested/deep.spec.ts" {
		t.Errorf("`*` не должна глотать `/`: ожидалась ровно одна находка о вложенном "+
			"файле, получено %v", findings)
	}

	// Путь со слэшами внутри строки за комментарий не принимается.
	census, findings, err := auditConsoleProbeTypeCoverage(synthTsconfigStringWithSlashes,
		[]string{"specs/mutate.spec.ts"})
	if err != nil {
		t.Fatalf("объявление со строкой, содержащей `//`, не разобралось: %v — "+
			"наивное снятие комментариев съело бы предмет", err)
	}
	if len(census.Patterns) != 1 || len(findings) != 0 {
		t.Errorf("строка с `//` прочитана как комментарий: шаблонов %v, находок %v",
			census.Patterns, findings)
	}
}

// TestConsoleProbeTypeCoverageRefusesToJudgeWithoutADeclaration — предпосылка
// гейта заявляется, а не подразумевается.
func TestConsoleProbeTypeCoverageRefusesToJudgeWithoutADeclaration(t *testing.T) {
	// Пустой include: гейт по дереву на этом Fatal'ит, а чистая функция обязана
	// показать ноль шаблонов — иначе «непокрыто всё» читалось бы как находка о
	// дереве, хотя это поломка объявления.
	census, findings, err := auditConsoleProbeTypeCoverage(synthTsconfigEmptyInclude,
		[]string{"specs/mutate.spec.ts"})
	if err != nil {
		t.Fatalf("проект без include не разобрался: %v", err)
	}
	if len(census.Patterns) != 0 {
		t.Errorf("проект не объявляет include, а перепись насчитала шаблоны: %v", census.Patterns)
	}
	if len(findings) != 1 {
		t.Errorf("без единого шаблона читателя нет ни у кого — ожидалась находка, получено %v", findings)
	}

	// Негодное объявление — ошибка, а не молчание: гейт, проглотивший поломку
	// своего входа, объявил бы дерево чистым по причине, к дереву отношения не
	// имеющей.
	if _, _, err := auditConsoleProbeTypeCoverage(synthTsconfigBroken, []string{"specs/a.ts"}); err == nil {
		t.Error("негодное объявление разобрано без ошибки: поломка входа обязана быть " +
			"названа, а не превращена в зелёный вердикт")
	}
}

// TestConsoleProbeTypeCoverageFindingNamesTheSubject — находка обязана называть
// предмет прямо: по её тексту читатель понимает, что чинить.
func TestConsoleProbeTypeCoverageFindingNamesTheSubject(t *testing.T) {
	_, findings, err := auditConsoleProbeTypeCoverage(synthTsconfigWithoutAwaiting,
		[]string{"specs-awaiting-journal-owner/subscription-stream.spec.ts"})
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("ожидалась одна находка, получено %v", findings)
	}
	if !strings.Contains(findings[0].Why, "include") {
		t.Errorf("находка не называет, ЧЕМ файл непокрыт: %q — читатель пойдёт "+
			"искать причину не там", findings[0].Why)
	}
}
