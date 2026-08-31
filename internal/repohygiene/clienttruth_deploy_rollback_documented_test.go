// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_deploy_rollback_documented_test.go — у страницы развёртывания есть
// процедура отката, и она говорит про необратимость.
//
// Процедуры отката не было ни на одной из семи страниц: `helm rollback` не
// встречался ни разу, а единственное, что документация называла откатом, — это
// `migrate-down`, стоящий рядом с рамкой, предупреждающей о ДРУГОМ. Читатель
// оставался с выводом, что миграции обратимы.
//
// Они обратимы наполовину, и эта половина — форма. Down-миграция воссоздаёт
// схему пустой; строки, снесённые `DROP`, не возвращает ничто, кроме резервной
// копии. Продукт знает это и говорит на своей стороне — накат считает живые
// строки перед сносом и отказывает, — но оператору это сказано не было.
//
// Гейт требует ТРЁХ вещей сразу, потому что каждая по отдельности выполняется
// вхолостую: раздел без `helm rollback` — заголовок без процедуры; процедура без
// слов о данных — та же ловушка, из которой раздел и вырос; а необратимость,
// названная где угодно на странице, но не в разделе отката, читателя не догонит.
//
// Судит ТЕКСТ, и это здесь законно: предмет проверки — то, что читает человек, а
// не то, что исполняет машина. Единственная форма без содержания, от которой
// текст надо защищать, — упоминание в чужом месте; поэтому три признака ищутся В
// ПРЕДЕЛАХ раздела, а не по всей странице.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// rollbackHeading — заголовок раздела. Совпадает с тем, что стоит на страницах.
const rollbackHeading = "## Откат выкатки"

var (
	// dataNotRestoredRe — утверждение о том, что откат схемы не возвращает строки.
	// Форм несколько, потому что фраза живёт в прозе, а не в коде.
	dataNotRestoredRe = regexp.MustCompile(`(?i)(не\s+данные|данные[^.]{0,40}не\s+возвращ|восстановима\s+схема)`)
	// nextHeadingRe — конец раздела: следующий заголовок того же уровня.
	nextHeadingRe = regexp.MustCompile(`(?m)^## `)
)

// fencedBlocksContain отвечает, встречается ли подстрока в ИСПОЛНЯЕМОЙ строке
// блока кода.
//
// Отброшено ДВАЖДЫ, и оба раза по одной причине — текст, объясняющий команду, не
// является командой. Сначала проза вне блоков: абзац о том, чего `helm rollback`
// НЕ делает сам, стоит в разделе законно, и гейт, искавший подстроку по всему
// разделу, оставался зелёным, когда команду из него вынули. Затем комментарий
// ВНУТРИ блока: строка `# ДО helm rollback, пока в поде ещё новый образ` — тоже
// объяснение, и она держала гейт зелёным ровно так же, только этажом ниже.
//
// Оператор ночью копирует строку, которую можно исполнить. Её и ищем.
func fencedBlocksContain(body, want string) bool {
	parts := strings.Split(body, "```")
	// Внутри блоков — нечётные части: текст ``` код ``` текст ``` код ``` …
	for i := 1; i < len(parts); i += 2 {
		for _, line := range strings.Split(parts[i], "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if strings.Contains(line, want) {
				return true
			}
		}
	}
	return false
}

// deployPages — страницы развёртывания сервисов. Перечень выводится обходом:
// сервис, заведённый завтра, попадает под гейт сам.
func deployPages(t *testing.T, root string) []string {
	t.Helper()
	servicesDir := filepath.Join(root, "services")
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		t.Fatalf("read %s: %v", servicesDir, err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(servicesDir, e.Name(), "docs", "content", "install", "deploy.mdx")
		if fileExists(p) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		t.Fatalf("обход не нашёл ни одной страницы развёртывания под %s — гейт не утверждает ничего", servicesDir)
	}
	return out
}

// TestEveryDeployPageDocumentsRollbackAndItsIrreversibleHalf — по каждой странице.
//
// Проваливается на: странице без раздела отката; разделе без `helm rollback`;
// разделе, молчащем о том, что данные не возвращаются; и на пустом обходе.
func TestEveryDeployPageDocumentsRollbackAndItsIrreversibleHalf(t *testing.T) {
	root := repoRoot(t)
	pages := deployPages(t, root)

	var withSection int
	for _, path := range pages {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		body := string(raw)
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = path
		}

		idx := strings.Index(body, rollbackHeading)
		if idx < 0 {
			t.Errorf("%s: нет раздела «%s». Оператору, читающему страницу ночью после неудачной "+
				"выкатки, не сказано ни что возвращать первым, ни какие шаги необратимы",
				rel, strings.TrimPrefix(rollbackHeading, "## "))
			continue
		}
		withSection++

		// Раздел — от заголовка до следующего заголовка того же уровня. Признаки
		// ищутся ВНУТРИ него: `helm rollback`, названный в другом разделе, читателя
		// этого раздела не догонит.
		section := body[idx+len(rollbackHeading):]
		if m := nextHeadingRe.FindStringIndex(section); m != nil {
			section = section[:m[0]]
		}

		// Команда ищется В БЛОКЕ КОДА, а не где угодно в разделе. Проза про
		// `helm rollback` — в том числе абзац о том, чего он НЕ делает сам, — стоит
		// в этом разделе законно и процедурой не является: оператор ночью копирует
		// команду, а не пересказ. Первая редакция гейта искала подстроку по всему
		// разделу и оставалась зелёной, когда команду из него вынули.
		if !fencedBlocksContain(section, "helm rollback") {
			t.Errorf("%s: в разделе отката нет блока кода с `helm rollback` — заголовок и "+
				"объяснение есть, исполнимой процедуры нет", rel)
		}
		if !dataNotRestoredRe.MatchString(section) {
			t.Errorf("%s: раздел отката молчит о том, что down-миграция возвращает ФОРМУ, а не "+
				"данные. Ровно из этого умолчания раздел и вырос: читатель выполняет «откат "+
				"последней», схема возвращается, снесённые строки нет, и узнаёт он это после", rel)
		}
	}

	// Перепись: «ноль находок» обязано быть отличимо от «ноль прочитанного».
	if len(pages) == 0 {
		t.Fatal("страниц развёртывания не прочитано — гейт судил бы по пустоте")
	}
	t.Logf("перепись: страниц развёртывания %d · несут раздел отката %d", len(pages), withSection)
}
