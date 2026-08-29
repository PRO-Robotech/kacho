// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// docsgodependencydirectory_test.go — каталог, названный источником Go-импорта,
// обязан содержать Go-код.
//
// # Предмет
//
// Страницы архитектуры перечисляют, что слою `domain/` разрешено импортировать:
// «только stdlib + <координата>». Такое перечисление — утверждение о ЗАВИСИМОСТИ
// Go-кода, и оно проверяемо: из каталога без единого `.go` импортировать нельзя
// by construction.
//
// `proto/` — ровно такой каталог: в нём контракты (`.proto`), конфигурация buf
// и модель прав, и НИ ОДНОГО Go-файла. Go-представление контракта — это
// сгенерённые стабы `pkg/api/...`, и один документ дерева говорит это верно:
//
//	Никаких импортов кроме stdlib и сгенерённых стабов `pkg/api/...`
//
// # Почему класс, а не опечатка
//
// Утверждение попало сюда правкой, снимавшей имена прежних полирепозиториев:
// `kacho-proto` заменялось на `proto/`. Замена вылечила мёртвую координату и
// тем же движением завела ЖИВУЮ, но ложную — а живая координата опаснее мёртвой:
// мёртвая себя выдаёт (её не найти), живая приземляет читателя в существующий
// каталог и молчит. Ни сборка сайта, ни проверка ссылок этого не видят: каталог
// есть, ссылка цела, неверно ЛИШЬ УТВЕРЖДЕНИЕ о нём.
//
// # Предикат
//
// Судится соседство `stdlib` и координаты каталога — не время глагола и не
// словарь связок («импортирует» / «зависит» / «imports»): корпус двуязычный, и
// словарный предикат мерил бы язык, а не предмет. `stdlib` — термин Go, он
// пишется одинаково на обоих языках, и строка, перечисляющая разрешённое рядом
// с ним, есть перечисление разрешённых ИМПОРТОВ.
//
// Координата резолвится ЦЕЛИКОМ. Резолв по самому длинному существующему
// префиксу отвергнут замером: он зачитывал `internal/` за `internal/domain`,
// которого в корне нет, — то есть молчал по неверной причине. Не резолвящийся
// токен пропускается: относительная координата внутри сервиса — законная форма
// и не предмет этого гейта.
//
// # Что считается находкой
//
// Перечисление, в окне которого НИ ОДНА резолвящаяся координата не содержит
// Go-кода. Названа хотя бы одна с Go-кодом — гейт молчит: читатель приземляется
// верно, а соседство каталога контрактов законно, `.proto` действительно лежат
// там. Правило выведено замером, а не выбрано: предикат «любая координата окна
// без Go-кода» краснел на верном тексте — на строке, которая ОДНОВРЕМЕННО
// называет стабы и говорит, где лежат сами схемы. Гейт, краснеющий на верном
// тексте, отключают первым.
//
// Обратная сторона названа и закрыта пробой: соседство каталога без Go-кода не
// спасает, если верного источника не названо ни одного, — иначе правило
// выродилось бы во всеразрешение.
//
// # Окно
//
// Строка вхождения плюс соседние сверху и снизу: проза жёстко переносится по
// ширине. Замер это подтвердил числом — окно ловит на одну находку больше, и
// это ровно перенесённое по ширине предписание в известных расхождениях nlb.
//
// # Охват
//
// Судится ВСЯ отслеживаемая проза, включая приёмки `docs/specs` и отчёты
// прогонов. Соседний гейт прежних имён их исключает намеренно — там предмет
// «указание читателю ИДТИ куда-то», а датированное свидетельство описывает своё
// прошлое. Здесь предмет другой: утверждение о том, что Go-код зависит от
// каталога без Go-кода, ложно на любую дату — из `proto/` нельзя было
// импортировать и тогда, когда приёмка писалась. Замер это подтверждает: на 314
// документах против 307 находок не прибавилось ни одной.
//
// # Перепись
//
// Печатается: документов осмотрено, строк со `stdlib`, из них называющих
// резолвящуюся координату, из них каталог с Go-кодом и без. Ноль строк со
// `stdlib` во всём корпусе — ОТКАЗ: предмета не стало либо сломан обход, и
// молчание тогда означает «не прочитано», а не «верно». Ноль строк, называющих
// координату, отказом НЕ является — перечисление без координаты законно, и
// перепись это показывает отдельным числом.
package repohygiene

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// trimDocLineForFinding укорачивает строку документа для текста находки.
//
// Режется ПО РУНАМ, а не по байтам: корпус двуязычный, кириллическая буква
// занимает два байта, и байтовый срез рвёт её пополам — диагностика тогда
// заканчивается заменяющим символом ровно там, где читателю нужен предмет.
func trimDocLineForFinding(line string) string {
	t := strings.TrimSpace(line)
	r := []rune(t)
	if len(r) > 96 {
		return string(r[:96]) + "…"
	}
	return t
}

// goDependencyMarker — термин, по которому опознаётся перечисление разрешённых
// Go-импортов. Одно слово, одинаковое на обоих языках корпуса.
const goDependencyMarker = "stdlib"

// treeCoordinateRe — токен, похожий на путь: хотя бы один слэш внутри.
var treeCoordinateRe = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_.-]*(?:/[A-Za-z0-9_.*-]*)+`)

type goDepFinding struct {
	doc        string
	line       int
	coordinate string
	dir        string
	text       string
}

func (f goDepFinding) String() string {
	t := trimDocLineForFinding(f.text)
	return fmt.Sprintf("%s:%d — `%s` (каталог %s, Go-файлов нет): %s", f.doc, f.line, f.coordinate, f.dir, t)
}

type goDepCensus struct {
	docs        int
	markerLines int
	withCoord   int
	withGoCode  int
	withoutGo   int
}

func (c goDepCensus) String() string {
	return fmt.Sprintf("документов %d; строк со `%s` %d, из них называют резолвящуюся "+
		"координату %d (каталог с Go-кодом %d, без Go-кода %d)",
		c.docs, goDependencyMarker, c.markerLines, c.withCoord, c.withGoCode, c.withoutGo)
}

// treeDirectories — каталоги дерева и признак «в каталоге есть Go-код».
type treeDirectories struct {
	dirs   map[string]bool
	withGo map[string]bool
}

func directoriesOf(files map[string]bool) treeDirectories {
	t := treeDirectories{dirs: map[string]bool{}, withGo: map[string]bool{}}
	for p := range files {
		parts := strings.Split(p, "/")
		isGo := strings.HasSuffix(p, ".go")
		for i := 1; i < len(parts); i++ {
			d := strings.Join(parts[:i], "/")
			t.dirs[d] = true
			if isGo {
				t.withGo[d] = true
			}
		}
	}
	return t
}

// normalizeCoordinate снимает хвостовой слэш и подстановки (`/...`, `/*`),
// которыми проза обозначает «и всё, что внутри».
func normalizeCoordinate(tok string) string {
	t := strings.TrimRight(tok, "/")
	for {
		switch {
		case strings.HasSuffix(t, "/..."):
			t = strings.TrimSuffix(t, "/...")
		case strings.HasSuffix(t, "/*"):
			t = strings.TrimSuffix(t, "/*")
		default:
			return t
		}
	}
}

// resolvedCoordinates — координаты строки, резолвящиеся к каталогу дерева ЦЕЛИКОМ.
func resolvedCoordinates(line string, tree treeDirectories) [][2]string {
	var out [][2]string
	for _, tok := range treeCoordinateRe.FindAllString(line, -1) {
		d := normalizeCoordinate(tok)
		if tree.dirs[d] {
			out = append(out, [2]string{tok, d})
		}
	}
	return out
}

func scanGoDependencyClaims(docs []string, read func(rel string) ([]byte, error), tree treeDirectories) ([]goDepFinding, goDepCensus, error) {
	census := goDepCensus{docs: len(docs)}
	var findings []goDepFinding
	for _, rel := range docs {
		body, err := read(rel)
		if err != nil {
			return nil, census, fmt.Errorf("%s: %w — документ не прочитан, а непрочитанный "+
				"документ обязан быть отказом, а не молчаливым нулём", rel, err)
		}
		lines := strings.Split(string(body), "\n")
	lines:
		for i, line := range lines {
			if !strings.Contains(strings.ToLower(line), goDependencyMarker) {
				continue
			}
			census.markerLines++
			var window [][2]string
			for _, j := range []int{i - 1, i, i + 1} {
				if j >= 0 && j < len(lines) {
					window = append(window, resolvedCoordinates(lines[j], tree)...)
				}
			}
			if len(window) == 0 {
				continue
			}
			census.withCoord++
			// Находка — перечисление, где НИ ОДНА названная координата не
			// содержит Go-кода. Названа хотя бы одна с Go-кодом — читатель
			// приземляется верно, и соседство каталога контрактов законно:
			// `.proto` действительно лежат там.
			for _, c := range window {
				if tree.withGo[c[1]] {
					census.withGoCode++
					continue lines
				}
			}
			census.withoutGo++
			for _, c := range window {
				findings = append(findings, goDepFinding{rel, i + 1, c[0], c[1], line})
			}
		}
	}
	return findings, census, nil
}

// ── гейт на дереве ───────────────────────────────────────────────────────────

func TestGoDependencyClaimsNameADirectoryThatHasGoCode(t *testing.T) {
	root := repoRoot(t)
	docs, files := trackedDocsAndFiles(t, root)
	tree := directoriesOf(files)

	osRoot, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("открыть корень %s: %v", root, err)
	}
	defer func() { _ = osRoot.Close() }()

	findings, census, err := scanGoDependencyClaims(docs, osRoot.ReadFile, tree)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	t.Logf("перепись: %s", census)

	if census.markerLines == 0 {
		t.Fatalf("предпосылка не выполняется: в %d документах не найдено ни одной строки "+
			"со словом `%s`. Либо перечислений разрешённых импортов не осталось вовсе "+
			"(тогда гейт снимается вместе с предметом), либо сломан обход — и тогда корпус "+
			"молча не читается, ровно тот дефект, ради которого гейт заведён",
			census.docs, goDependencyMarker)
	}

	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("\n  " + f.String())
		}
		t.Fatalf("%d утверждений о зависимости Go-кода называют каталог без единого "+
			"Go-файла:%s\n\nИз такого каталога импортировать нельзя by construction. "+
			"Go-представление контракта — сгенерённые стабы `pkg/api/...`; сам `proto/` "+
			"несёт `.proto`, конфигурацию buf и модель прав, но не Go-код.\nПерепись: %s",
			len(findings), b.String(), census)
	}
}
