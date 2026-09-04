// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// catalogcopyparity.go — механизм гейта «сверка, обёрнутая условием
// существования файла, не сверяет НИЧЕГО».
//
// # Предмет
//
// Цель Makefile, которая сверяет два закоммиченных артефакта, обязана исполнять
// сверку БЕЗУСЛОВНО. Обёртка вида `if [ -f X ]; then diff ...; fi` не пропускает
// ничего ровно до того дня, когда X перестанет существовать: с этого дня условие
// ложно НАВСЕГДА, сверка не исполняется ни разу, а цель остаётся зелёной и
// неотличимой от исправной.
//
// Это класс «послабление, которое не истечёт само» (testing.md §«Гейт на класс»,
// п. 5) в самой тихой его форме: у обёртки нет ни маркера отсрочки, ни записи в
// ведомости, ни номера задачи — снаружи она выглядит аккуратной защитой от
// неполного чекаута.
//
// # Где наблюдалось и чем это стоило бы
//
// gateway/Makefile, цель `permission-catalog-check`: сверка двух вшитых копий
// каталога прав (край и посев iam) стояла под `if [ -f $(IAM_CATALOG) ]`. Вынос
// iam отдельным продуктом уносит копию iam из этого дерева — то есть день, когда
// условие станет ложным, был ЗАПЛАНИРОВАН. Цена расхождения копий не
// косметическая: RPC без записи в каталоге даёт `catalog: no entry for method`
// → AUTHZ_DENIED на каждый вызов, независимо от выданных прав.
//
// Зеркальная цель у iam (`sync-permission-catalog`, services/iam/Makefile)
// отказывала явно с самого начала — `test -f … || { echo …; exit 1; }`. То есть
// один и тот же вопрос две стороны одного шва решали по-разному, и никто этого
// не решал: перекос и есть признак, по которому класс виден.
//
// # Что гейт судит, а что НЕТ
//
// Судит ОДНУ ось: исполняется ли сверка условно по наличию файла. Не судит
// `|| true` на сверке — там исход зависит от того, регенерирующая цель или
// сверяющая, а это суждение, а не предикат: у регенерирующей цели diff
// информационен и `|| true` законен (gateway/Makefile, цели
// `permission-catalog` и `rest-route-table`). Смешение двух осей дало бы гейт,
// краснеющий на верном коде, — а такой отключают первым.
//
// # Ведомости исключений НЕТ, и это не упущение
//
// Обёрнутых сверок в дереве ноль, и перечень прощённых заводить нельзя: каждая
// запись в нём — место, куда отсрочку вносят незамеченной (core, ban #11).
// Появится законная обёртка — её законность решается автором, а не ведомостью.
package repohygiene

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// makeRecipeLine — одна ЛОГИЧЕСКАЯ строка рецепта: продолжения по `\` склеены,
// префиксы рецепта (`@`, `-`, `+`) сняты.
//
// Логическая строка, а не физическая, — потому что именно она есть единица
// исполнения: make отдаёт её шеллу целиком, и обёртка `if … then … fi` в этом
// дереве написана как раз через продолжение. Гейт, читающий физические строки,
// увидел бы `if [ -f X ]; then diff …` и `|| { … }` порознь и не смог бы
// сказать, что чему подчинено.
type makeRecipeLine struct {
	// Target — цель, чьему рецепту принадлежит строка.
	Target string
	// Line — номер ПЕРВОЙ физической строки: координата находки.
	Line int
	// Text — исполняемое тело без префиксов рецепта.
	Text string
}

// makefileRecipes — рецепты одного Makefile, а также число прочитанных
// физических строк: «ноль находок» обязано быть отличимо от «ноль
// прочитанного».
type makefileRecipes struct {
	Lines          []makeRecipeLine
	PhysicalLines  int
	RecipeLinesRaw int
}

// ruleHeadRe — ЗАГОЛОВОК правила: `цель:` либо `цель: зависимости`. Присваивания
// (`X := …`, `X ?= …`, `X += …`, `X = …`) правилом не являются и рецепт
// предыдущей цели закрывают.
var ruleHeadRe = regexp.MustCompile(`^([^\t#:=][^:=]*):(?:[^=]|$)`)

// recipePrefixRe — префиксы рецепта make: `@` (не печатать), `-` (игнорировать
// код возврата), `+` (исполнять и под `-n`). Снимаются, потому что предмет гейта
// — исполняемое тело, а не то, печатается ли оно.
var recipePrefixRe = regexp.MustCompile(`^[-@+]+`)

// parseMakefileRecipes — рецепты Makefile по целям.
//
// Отказ возвращается, а не роняет прогон: этой же функцией пользуется инъекция,
// а она обязана НАБЛЮДАТЬ исход, а не завершать процесс.
func parseMakefileRecipes(path string) (makefileRecipes, error) {
	// Чтение — через корень дерева, а не по склейке пути: `os.Root` не выпускает
	// за корень ни по «..», ни по символической ссылке. Прежде здесь стояло
	// подавление в диалекте, которого в этом репозитории не читает никто, —
	// то есть предмет не снимался, а объявлялся снятым.
	dir, name := filepath.Split(path)
	root, err := os.OpenRoot(filepath.Clean(dir))
	if err != nil {
		return makefileRecipes{}, fmt.Errorf("не открыт корень %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(name)
	if err != nil {
		return makefileRecipes{}, fmt.Errorf("не прочитан %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(f)
	if err != nil {
		return makefileRecipes{}, fmt.Errorf("не прочитан %s: %w", path, err)
	}
	physical := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	out := makefileRecipes{PhysicalLines: len(physical)}

	target := ""
	for i := 0; i < len(physical); i++ {
		line := physical[i]

		if !strings.HasPrefix(line, "\t") {
			trimmed := strings.TrimSpace(line)
			// Пустая строка и строка-комментарий рецепт НЕ закрывают — так
			// устроен make, и рецепт цели `permission-catalog-check` в этом
			// дереве действительно разбит комментариями.
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if m := ruleHeadRe.FindStringSubmatch(line); m != nil {
				// У правила бывает несколько целей через пробел; рецепт
				// принадлежит им всем, и для координаты достаточно первой.
				target = strings.Fields(strings.TrimSpace(m[1]))[0]
			} else {
				target = ""
			}
			continue
		}

		out.RecipeLinesRaw++
		if target == "" {
			continue
		}
		first := i + 1 // номера строк 1-based, как их печатает всякий инструмент
		body := strings.TrimPrefix(line, "\t")
		for strings.HasSuffix(strings.TrimRight(body, " \t"), `\`) && i+1 < len(physical) {
			body = strings.TrimSuffix(strings.TrimRight(body, " \t"), `\`) + " " +
				strings.TrimLeft(physical[i+1], " \t")
			i++
			out.RecipeLinesRaw++
		}
		body = recipePrefixRe.ReplaceAllString(strings.TrimLeft(body, " "), "")
		if strings.TrimSpace(body) == "" || strings.HasPrefix(strings.TrimSpace(body), "#") {
			continue
		}
		out.Lines = append(out.Lines, makeRecipeLine{Target: target, Line: first, Text: body})
	}
	return out, nil
}

// comparisonCallRe — сверка как КОМАНДА, а не слово в тексте.
//
// Позиция команды опознаётся началом строки либо разделителем перед ней (`;`,
// `&&`, `||`, `(`, `then`, `else`, `do`). Без этого совпадали бы `--- diff …` в
// строке echo и слово «diff» в прозе — то есть гейт краснел бы на собственном
// объяснении (testing.md §«Гейт на класс», п. 4).
var comparisonCallRe = regexp.MustCompile(`(?:^|[;&|(}]|\bthen\b|\belse\b|\bdo\b)[[:space:]]*(diff|cmp)\b`)

// blankShellQuoted — гасит СОДЕРЖИМОЕ строковых литералов шелла и хвостовой
// комментарий, сохраняя длину строки.
//
// # Зачем, и почему одних разделителей мало
//
// Разделитель перед командой отделяет команду от прозы, но НЕ от текста внутри
// кавычек: `echo "… ; then diff …"` несёт и разделитель, и имя команды, командой
// при этом не являясь. Это тот же класс, что гейт, ищущий защиту словом в сыром
// тексте и находящий её в комментарии, который эту защиту объясняет.
//
// Найдено не рассуждением, а ИНЪЕКЦИЕЙ: законный близнец «проза и echo»
// (catalogcopyparity_injection_test.go) на первой редакции распознавателя дал
// ложную находку — гейт покраснел на строке echo, объясняющей снятую обёртку.
//
// Длина сохраняется, потому что вызывающий ищет условие в тексте ПЕРЕД сверкой
// по индексу совпадения: смещения гашёной копии обязаны совпадать с исходной.
//
// Правила взяты шелловские: внутри `'…'` не особого нет ничего до закрывающей
// кавычки; внутри `"…"` одинарная кавычка литеральна, а `\` экранирует
// следующий знак. Незакрытая кавычка гасит остаток строки — в сторону молчания,
// а не ложной находки.
func blankShellQuoted(s string) string {
	out := []byte(s)
	var quote byte // 0 — вне кавычек, иначе открывающая кавычка
	for i := 0; i < len(out); i++ {
		c := out[i]
		switch {
		case quote == 0 && (c == '\'' || c == '"'):
			quote = c
		case quote == 0 && c == '#':
			// Хвостовой комментарий шелла: остаток строки не исполняется.
			for ; i < len(out); i++ {
				out[i] = ' '
			}
		case quote == '"' && c == '\\' && i+1 < len(out):
			out[i], out[i+1] = ' ', ' '
			i++
		case quote == c:
			quote = 0
		case quote != 0:
			out[i] = ' '
		}
	}
	return string(out)
}

// openingFileTestRe — условие существования файла, ОТКРЫВАЮЩЕЕ ветку, в двух
// формах, какими его тут пишут:
//
//	if [ -f X ]; then …      · if test -f X; then …
//	[ -f X ] && …            · test -f X && …
//
// Форма отказа (`test -f X || { echo …; exit 1; }`) сюда НЕ попадает намеренно:
// она не открывает ветку, а закрывает исполнение, и сверка после неё
// безусловна. Это и есть законный близнец находки — различает их оператор,
// а не наличие проверки файла.
var openingFileTestRe = regexp.MustCompile(
	`(?:if[[:space:]]+)?(?:\[|test)[[:space:]]+-[efsrdx][[:space:]][^;]*?(?:;[[:space:]]*then|&&)`)

// guardedComparison — находка: сверка, исполняемая условно по наличию файла.
type guardedComparison struct {
	File   string
	Line   int
	Target string
	Text   string
}

// findGuardedComparisons — сверки под условием существования файла в одном
// Makefile, плюс перепись: сколько сверок вообще осмотрено.
//
// Число сверок возвращается отдельно и является ПОЛОЖИТЕЛЬНЫМ КОНТРОЛЕМ
// распознавателя: ноль сверок по всему дереву означает, что распознавание
// сломалось, а не что дерево чисто.
func findGuardedComparisons(relPath string, recipes makefileRecipes) (found []guardedComparison, comparisons int) {
	for _, rl := range recipes.Lines {
		// Судится ИСПОЛНЯЕМАЯ часть: содержимое кавычек и хвостовой комментарий
		// погашены, смещения при этом сохранены.
		executable := blankShellQuoted(rl.Text)
		loc := comparisonCallRe.FindStringIndex(executable)
		if loc == nil {
			continue
		}
		comparisons++
		// Условие ищется в тексте ПЕРЕД сверкой: подчинение читается порядком,
		// а не наличием обоих признаков где угодно в строке.
		if openingFileTestRe.MatchString(executable[:loc[1]]) {
			found = append(found, guardedComparison{
				File:   relPath,
				Line:   rl.Line,
				Target: rl.Target,
				Text:   strings.TrimSpace(rl.Text),
			})
		}
	}
	return found, comparisons
}
