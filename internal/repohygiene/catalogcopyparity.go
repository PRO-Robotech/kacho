// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// catalogcopyparity.go — механизм гейта «сверка, обёрнутая условием
// существования файла, не сверяет НИЧЕГО».
//
// # Предмет
//
// Всё, что сверяет два закоммиченных артефакта, — цель Makefile ЛИБО скрипт
// шелла, — обязано исполнять сверку БЕЗУСЛОВНО. Обёртка вида
// `if [ -f X ]; then diff ...; fi` не пропускает ничего ровно до того дня, когда
// X перестанет существовать: с этого дня условие ложно НАВСЕГДА, сверка не
// исполняется ни разу, а цель остаётся зелёной и неотличимой от исправной.
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
// # Популяции ДВЕ, распознаватели ОДНИ, единица разбора РАЗНАЯ (#2086)
//
// Судятся и рецепты make, и скрипты шелла. Расширить охват одним отбором файлов
// было НЕЛЬЗЯ, и это измерено: единица разбора у популяций разная.
//
//	однострочная форма (рецепт make): `if [ -f X ]; then diff …; fi`   — одна логическая строка
//	блочная форма (скрипт):           `if [ -f X ]; then` · `diff …` · `fi` — ТРИ строки
//
// Причина структурная: make отдаёт логическую строку рецепта шеллу целиком,
// поэтому обёртка помещается в единицу разбора; в скрипте она разнесена. Гейт,
// которому скормили бы скрипты без блочного разбора, читал бы сто семьдесят
// восемь файлов и не видел бы того, что объявляет, — ровно «форма без
// содержания», против которой он и написан.
//
// Поэтому расширен РАЗБОР, а не отбор: между строками переносятся стек ветвлений
// `if`/`elif`/`else`/`fi`, состояние цитирования (включая подстановку `$( … )`) и
// тела heredoc. Распознаватели — comparisonCallRe, fileTestExpr и гашение
// литералов — остались ОДНИ на обе популяции; вторая их копия разошлась бы молча
// и разошлась бы там, где обе зелены.
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
// Не судит и форму, где ветка «файла нет» НАЗВАНА (`else`/`elif`): предмет гейта
// — ТИХИЙ пропуск, а названный путь тихим не бывает. Форма живая
// (`gateway/scripts/check-domain-generation.sh`: сверка двух прогонов под
// `if [[ -s A && -s B ]]; then … else finding … fi`), и вменять ей обёртку
// значило бы краснеть на коде, который никто не ломал.
//
// # Куда уводит НЕРАСПОЗНАННОЕ — в молчание, а не в находку
//
// Условие `if`, разнесённое на строки до `then`; `case`/`esac` и циклы; обратные
// кавычки; незакрытая на конце файла кавычка — всё это оставляет разбор без
// следа, и он роняет своих кандидатов, а не выдумывает рамку. Объём такого
// молчания печатается переписью (логических строк, погашено телом heredoc,
// файлов с незакрытой кавычкой) — иначе «находок ноль» было бы неотличимо от
// «прочитано ноль».
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
	"sort"
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

// ─── РАСПОЗНАВАТЕЛИ ─────────────────────────────────────────────────────────
//
// Они ОДНИ на обе популяции — и рецепты make, и скрипты шелла судятся ими же.
// Две копии предиката разошлись бы молча, и разошлись бы там, где обе зелены.

// comparisonCallRe — сверка как КОМАНДА, а не слово в тексте.
//
// Позиция команды опознаётся началом строки либо разделителем перед ней (`;`,
// `&&`, `||`, `(`, `then`, `else`, `do`). Без этого совпадали бы `--- diff …` в
// строке echo и слово «diff» в прозе — то есть гейт краснел бы на собственном
// объяснении (testing.md §«Гейт на класс», п. 4).
var comparisonCallRe = regexp.MustCompile(`(?:^|[;&|(}!]|\bif\b|\belif\b|\bwhile\b|\buntil\b|\bthen\b|\belse\b|\bdo\b)[[:space:]]*(diff|cmp)\b`)

// fileTestExpr — ВЫРАЖЕНИЕ проверки существования файла, из которого собраны обе
// формы её узнавания: условие ветвления (fileTestConditionRe) и однострочная
// обёртка (openingFileTestRe).
//
// Выражение одно намеренно. Две записи одного предиката — та самая пара копий,
// которая расходится молча: обе зелены на исправном дереве, и заметить
// расхождение можно только на том входе, ради которого гейт написан.
//
// `\[` покрывает и `[`, и `[[`: во второй форме совпадение начинается со второй
// скобки, и это верно — предмет здесь оператор проверки, а не скобка.
const fileTestExpr = `(?:\[|test)[[:space:]]+-[efsrdx][[:space:]]`

// fileTestConditionRe — та же проверка, взятая как УСЛОВИЕ ветвления: текст
// между `if`/`elif` и `then`. Хвоста `; then`/`&&` у неё нет by construction —
// его роль играет само ключевое слово, найденное разбором блока.
var fileTestConditionRe = regexp.MustCompile(fileTestExpr)

// openingFileTestRe — условие существования файла, ОТКРЫВАЮЩЕЕ ветку В ТОЙ ЖЕ
// строке, в двух формах, какими его тут пишут:
//
//	if [ -f X ]; then …      · if test -f X; then …
//	[ -f X ] && …            · test -f X && …
//
// Форма отказа (`test -f X || { echo …; exit 1; }`) сюда НЕ попадает намеренно:
// она не открывает ветку, а закрывает исполнение, и сверка после неё
// безусловна. Это и есть законный близнец находки — различает их оператор,
// а не наличие проверки файла.
var openingFileTestRe = regexp.MustCompile(
	`(?:if[[:space:]]+)?` + fileTestExpr + `[^;]*?(?:;[[:space:]]*then|&&)`)

// blankShellQuoted — гасит СОДЕРЖИМОЕ строковых литералов шелла и хвостовой
// комментарий В ПРЕДЕЛАХ ОДНОЙ строки, сохраняя её длину.
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
// Строка рецепта make самодостаточна — make отдаёт её шеллу целиком, — поэтому
// здесь состояние кавычек не переносится. Скрипту этого мало: там строковый
// литерал живёт через десятки строк (встроенная программа awk), и перенос
// состояния делает blankShellQuotedFrom.
func blankShellQuoted(s string) string {
	out, _ := blankShellQuotedFrom(s, nil)
	return out
}

// shellQuoteState — состояние цитирования, переносимое между строками.
//
// Последний элемент — кавычка, открытая СЕЙЧАС (0 — вне кавычек); каждый
// элемент ниже — кавычка, внутри которой открыта подстановка `$( … )`. Стек, а
// не один знак, потому что подстановка заводит НОВЫЙ контекст команд: в
// `"$(printf '%s' "$x")"` внутренняя двойная кавычка внешнюю не закрывает, и
// плоский счётчик кавычек ровно здесь и сбивается — а сбившись, читает остаток
// файла как данные и молчит.
type shellQuoteState []byte

func (st shellQuoteState) cur() byte {
	if len(st) == 0 {
		return 0
	}
	return st[len(st)-1]
}

// blankShellQuotedFrom — то же гашение, но с ПЕРЕНОСИМЫМ состоянием цитирования:
// принимает состояние, оставшееся от предыдущей строки, и возвращает то, что
// осталось после этой.
//
// Перенос — не педантизм, а условие того, чтобы разбор блока не читал ДАННЫЕ как
// команды. В этом дереве встроенная программа awk занимает десятки строк внутри
// одинарных кавычек и содержит слова `if`, `esac`, `exit`: без переноса состояния
// они попали бы в стек ветвлений, и вердикт стал бы свойством чужой программы.
//
// Правила взяты шелловские: внутри `'…'` не особого нет ничего до закрывающей
// кавычки; внутри `"…"` одинарная кавычка литеральна, а `\` экранирует
// следующий знак. Незакрытая кавычка гасит остаток строки и переносится на
// следующую — в сторону молчания, а не ложной находки.
func blankShellQuotedFrom(s string, st shellQuoteState) (string, shellQuoteState) {
	if len(st) == 0 {
		st = shellQuoteState{0}
	} else {
		st = append(shellQuoteState(nil), st...)
	}
	out := []byte(s)
	for i := 0; i < len(out); i++ {
		c, quote := out[i], st.cur()
		switch {
		case (quote == 0 || quote == '"') && c == '$' && i+1 < len(out) && out[i+1] == '(':
			// Подстановка команд — НОВЫЙ контекст: внутри неё кавычки свои, а
			// её тело есть команды, а не данные, даже когда сама подстановка
			// стоит внутри двойных кавычек.
			st = append(st, 0)
			i++
		case quote == 0 && c == ')' && len(st) > 1:
			st = st[:len(st)-1]
		case quote == 0 && c == '\\' && i+1 < len(out):
			// Вне кавычек `\` снимает особость СЛЕДУЮЩЕГО знака. Без этого
			// правила `don\'t` открывало бы кавычку, которой в шелле нет, и
			// разбор уезжал бы внутрь литерала до конца файла. Хвостовой `\`
			// (продолжение строки) под условие не подпадает и уцелеет.
			out[i], out[i+1] = ' ', ' '
			i++
		case quote == 0 && (c == '\'' || c == '"'):
			st[len(st)-1] = c
		case quote == 0 && c == '#' && startsWord(out, i):
			// Хвостовой комментарий шелла: остаток строки не исполняется.
			// Комментарий начинается ТОЛЬКО в начале слова — иначе под нож
			// попадала бы подстановка `${x#обр}`, а вместе с ней и открывающая
			// кавычка правее по строке.
			for ; i < len(out); i++ {
				out[i] = ' '
			}
		case quote == '"' && c == '\\' && i+1 < len(out):
			out[i], out[i+1] = ' ', ' '
			i++
		case quote == c:
			st[len(st)-1] = 0
		case quote != 0:
			out[i] = ' '
		}
	}
	return string(out), st
}

// startsWord — стоит ли знак в позиции i началом слова: только там шелл
// открывает комментарий.
func startsWord(b []byte, i int) bool {
	if i == 0 {
		return true
	}
	switch b[i-1] {
	case ' ', '\t', ';', '&', '|', '(', '{':
		return true
	}
	return false
}

// shellHeredocRe — открытие heredoc: `<<МЕТКА`, `<<-МЕТКА`, метка допускается в
// кавычках. Знак перед `<<` захвачен, чтобы here-string `<<<` сюда не попал:
// заглядывания назад в этом диалекте регулярных выражений нет, а без него
// `<<<EOF` совпал бы со сдвигом на один знак.
var shellHeredocRe = regexp.MustCompile(
	`(?:^|[^<])<<-?[[:space:]]*(?:\\)?(?:'([^']*)'|"([^"]*)"|([A-Za-z_][A-Za-z0-9_]*))`)

// ─── ЕДИНИЦА РАЗБОРА: ЛОГИЧЕСКАЯ СТРОКА ШЕЛЛА ───────────────────────────────

// guardScanLine — одна логическая строка шелла, готовая к разбору.
//
// Единица общая для обеих популяций, и в этом весь смысл: рецепт make и скрипт
// приходят к обходчику в одной форме, поэтому распознаватели у них одни.
type guardScanLine struct {
	// Line — номер ПЕРВОЙ физической строки: координата находки.
	Line int
	// Target — цель make, чьему рецепту принадлежит строка; у скрипта пусто.
	Target string
	// Executable — исполняемая часть: содержимое кавычек, хвостовые комментарии
	// и тела heredoc погашены, смещения при этом СОХРАНЕНЫ.
	Executable string
	// Raw — как записано: текст находки.
	Raw string
}

// shellSource — логические строки одного скрипта плюс перепись прочитанного:
// «ноль находок» обязано быть отличимо от «ноль прочитанного».
type shellSource struct {
	Lines         []guardScanLine
	PhysicalLines int
	// HeredocLines — сколько физических строк погашено как тело heredoc.
	HeredocLines int
	// UnclosedQuote — файл кончился внутри строкового литерала. Не отказ, а
	// сигнал: с этого места разбор гасил всё подряд, то есть уводил в молчание.
	UnclosedQuote bool
}

// parseShellSource — логические строки скрипта.
//
// # Что склеивается в одну логическую строку
//
//	продолжение по `\`            — как и в рецепте make
//	хвостовой `&&`, `||`, `|`     — оператор требует следующей команды
//
// Второе названо в предмете гейта отдельно: `&&`-цепочка есть вторая форма, в
// которой обёртка записывается без `if`, и без склейки её половины попали бы в
// разные единицы разбора.
//
// # Чего разбор НЕ умеет, и куда это уводит
//
// Условие `if`, разнесённое на несколько строк ДО `then`, читается только до
// конца своей строки; `case`/`esac`, циклы и функции стеку ветвлений не
// принадлежат и на него не влияют. Нераспознанное уводит в МОЛЧАНИЕ, а не в
// ложную находку: незакрытая рамка на конце файла свои кандидаты роняет.
//
// Отказ возвращается, а не роняет прогон: этой же функцией пользуется инъекция,
// а она обязана НАБЛЮДАТЬ исход, а не завершать процесс.
func parseShellSource(path string) (shellSource, error) {
	dir, name := filepath.Split(path)
	root, err := os.OpenRoot(filepath.Clean(dir))
	if err != nil {
		return shellSource{}, fmt.Errorf("не открыт корень %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(name)
	if err != nil {
		return shellSource{}, fmt.Errorf("не прочитан %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(f)
	if err != nil {
		return shellSource{}, fmt.Errorf("не прочитан %s: %w", path, err)
	}
	physical := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	out := shellSource{PhysicalLines: len(physical)}

	var (
		quote     shellQuoteState // цитирование, перенесённое с прошлой строки
		heredocs  []string        // очередь ожидаемых меток закрытия heredoc
		pending   *guardScanLine
		pendingOK bool
	)
	flush := func() {
		if pendingOK {
			out.Lines = append(out.Lines, *pending)
			pendingOK = false
		}
	}

	for i, line := range physical {
		// ── ТЕЛО HEREDOC — ДАННЫЕ, А НЕ КОМАНДЫ ────────────────────────────
		//
		// Стоит первым: строка внутри heredoc не исполняется ничем, и читать её
		// как команду значило бы судить чужой текст. Незакрытый heredoc гасит
		// остаток файла — снова в сторону молчания.
		if len(heredocs) > 0 {
			out.HeredocLines++
			if strings.TrimSpace(line) == heredocs[0] {
				heredocs = heredocs[1:]
			}
			continue
		}

		entryQuote := quote.cur()
		exe, next := blankShellQuotedFrom(line, quote)
		quote = next

		// Метки heredoc берутся с СЫРОЙ строки — гашение стёрло бы метку в
		// кавычках (`<<'EOF'`), — но принимаются только там, где гашёная копия
		// сохранила сами знаки `<<`: внутри литерала и комментария это данные.
		if entryQuote == 0 {
			for _, m := range shellHeredocRe.FindAllStringSubmatchIndex(line, -1) {
				op := strings.Index(line[m[0]:m[1]], "<<") + m[0]
				if op < 0 || op+2 > len(exe) || exe[op:op+2] != "<<" {
					continue
				}
				for _, g := range [][2]int{{m[2], m[3]}, {m[4], m[5]}, {m[6], m[7]}} {
					if g[0] >= 0 {
						heredocs = append(heredocs, line[g[0]:g[1]])
						break
					}
				}
			}
		}

		trimmed := strings.TrimRight(exe, " \t")
		continues := strings.HasSuffix(trimmed, `\`) ||
			strings.HasSuffix(trimmed, "&&") ||
			strings.HasSuffix(trimmed, "||") ||
			(strings.HasSuffix(trimmed, "|") && !strings.HasSuffix(trimmed, "||"))
		body := exe
		if strings.HasSuffix(trimmed, `\`) {
			body = strings.TrimSuffix(trimmed, `\`)
		}

		if !pendingOK {
			if strings.TrimSpace(body) == "" && !continues {
				continue
			}
			pending = &guardScanLine{Line: i + 1, Executable: body, Raw: strings.TrimSpace(line)}
			pendingOK = true
		} else {
			pending.Executable += " " + strings.TrimLeft(body, " \t")
			pending.Raw += " " + strings.TrimSpace(line)
		}
		if !continues {
			flush()
		}
	}
	flush()
	out.UnclosedQuote = quote.cur() != 0 || len(quote) > 1
	return out, nil
}

// isShellScript — скрипт ли это шелла.
//
// Признаков ДВА, и второй не для полноты: расширение `.sh` носят не все
// исполняемые файлы дерева — хук отправки и снимки-фикстуры проб названы иначе, а
// шеллом при этом являются. Отбор по одному расширению оставил бы их вне
// наблюдения, и «находок ноль» означало бы «этих файлов не читали».
func isShellScript(path string) (bool, error) {
	if strings.HasSuffix(path, ".sh") {
		return true, nil
	}
	dir, name := filepath.Split(path)
	root, err := os.OpenRoot(filepath.Clean(dir))
	if err != nil {
		return false, fmt.Errorf("не открыт корень %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(name)
	if err != nil {
		return false, fmt.Errorf("не прочитан %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, 256)
	n, err := f.Read(head)
	if err != nil && n == 0 {
		// Пустой файл шеллом не является; отказ чтения — не «нет», а «не
		// прочитано», и он обязан дойти до вызывающего.
		if err == io.EOF {
			return false, nil
		}
		return false, fmt.Errorf("не прочитан %s: %w", path, err)
	}
	first := string(head[:n])
	if i := strings.IndexByte(first, '\n'); i >= 0 {
		first = first[:i]
	}
	if !strings.HasPrefix(first, "#!") {
		return false, nil
	}
	for _, sh := range []string{"sh", "bash", "dash", "zsh", "ksh"} {
		if strings.HasSuffix(first, "/"+sh) || strings.HasSuffix(first, " "+sh) ||
			strings.Contains(first, "/"+sh+" ") || strings.Contains(first, " "+sh+" ") {
			return true, nil
		}
	}
	return false, nil
}

// ─── ОБХОДЧИК: ОДИН НА ОБЕ ПОПУЛЯЦИИ ────────────────────────────────────────

// guardedComparison — находка: сверка, исполняемая условно по наличию файла.
type guardedComparison struct {
	File   string
	Line   int
	Target string
	Text   string
}

// shellControlKeywordRe — ключевое слово ветвления в КОМАНДНОЙ позиции.
//
// Позиция та же, что у сверки: начало строки либо разделитель перед словом. Без
// неё совпадали бы `fi` в имени файла и `if` в слове `notify`.
var shellControlKeywordRe = regexp.MustCompile(
	`(?:^|[;&|(){}]|\bthen\b|\belse\b|\bdo\b)[[:space:]]*(if|elif|else|then|fi)\b`)

// ifFrame — открытая рамка `if … fi`.
type ifFrame struct {
	// fileTest — условие ОТКРЫВАЮЩЕЙ ветки есть проверка существования файла.
	fileTest bool
	// inThen — разбор идёт по ветке `then`, а не по `else`.
	inThen bool
	// hasElse — у оператора есть `else`/`elif`, то есть путь «файла нет» НАЗВАН
	// автором. Такая рамка находкой не становится: предмет гейта — ТИХИЙ
	// пропуск, а названный путь тихим не бывает.
	hasElse bool
	// pending — сверки, встреченные внутри рамки: вердикт по ним выносится на
	// `fi`, потому что `else` стоит ПОСЛЕ них.
	pending []pendingComparison
}

// pendingComparison — сверка внутри ещё не закрытой рамки.
type pendingComparison struct {
	g guardedComparison
	// inThen — записана в ветке `then`. Сверка из ветки `else` исполняется,
	// когда файла НЕТ; это другой класс, и рамка её не присваивает.
	inThen bool
}

// scanGuardedComparisons — сверки под условием существования файла в одной
// последовательности логических строк, плюс перепись: сколько сверок вообще
// осмотрено.
//
// Число сверок возвращается отдельно и является ПОЛОЖИТЕЛЬНЫМ КОНТРОЛЕМ
// распознавателя: ноль сверок по всей популяции означает, что распознавание
// сломалось, а не что дерево чисто.
//
// # Один обходчик, две популяции — различает их ровно ОДИН параметр
//
// resetPerLine=true — каждая логическая строка есть ОТДЕЛЬНЫЙ вызов шелла: так
// make отдаёт рецепт, и рамка `if`, не закрытая в своей строке, следующей строке
// не принадлежит. resetPerLine=false — строки одного процесса, рамка живёт до
// своего `fi`.
//
// Это и есть та структурная разница, из-за которой одним отбором файлов охват не
// расширяется: единица разбора у популяций разная, а распознаватели — одни.
func scanGuardedComparisons(relPath string, lines []guardScanLine, resetPerLine bool) (found []guardedComparison, comparisons int) {
	var stack []*ifFrame

	// record — вердикт по одной встреченной сверке. Сверка вне рамок (либо
	// внутри рамки, которая находкой не станет) находкой не является.
	record := func(g guardedComparison) {
		if len(stack) == 0 {
			return
		}
		top := stack[len(stack)-1]
		top.pending = append(top.pending, pendingComparison{g: g, inThen: top.inThen})
	}

	for _, l := range lines {
		if resetPerLine {
			stack = stack[:0]
		}
		exe := l.Executable

		// События строки — ключевые слова и сверки — упорядочиваются ПО
		// СМЕЩЕНИЮ: однострочная форма `if … then diff … fi` иначе читалась бы
		// как набор признаков без порядка, а подчинение задаёт именно порядок.
		type event struct {
			at   int
			end  int
			kind string // if · elif · else · then · fi · cmp
			// rank — порядок при СОВПАДАЮЩЕМ смещении: ключевое слово раньше
			// сверки. Совпадение не гипотетично — совпадением оно и бывает
			// всегда: в `if cmp -s A B` слово `if` есть и рамка, и командная
			// позиция сверки, и оба совпадения начинаются на нём. Без второго
			// ключа порядок был бы произволом сортировки, то есть вердикт
			// зависел бы от реализации.
			rank int
		}
		var events []event
		for _, m := range shellControlKeywordRe.FindAllStringSubmatchIndex(exe, -1) {
			events = append(events, event{at: m[2], end: m[3], kind: exe[m[2]:m[3]], rank: 0})
		}
		for _, m := range comparisonCallRe.FindAllStringIndex(exe, -1) {
			events = append(events, event{at: m[0], end: m[1], kind: "cmp", rank: 1})
		}
		sort.SliceStable(events, func(i, j int) bool {
			if events[i].at != events[j].at {
				return events[i].at < events[j].at
			}
			return events[i].rank < events[j].rank
		})

		// pendingIf — открытое `if`/`elif`, чьё условие читается до `then`.
		pendingIf := -1
		closeCondition := func(upto int) {
			if pendingIf < 0 {
				return
			}
			cond := exe[pendingIf:upto]
			top := stack[len(stack)-1]
			top.fileTest = fileTestConditionRe.MatchString(cond)
			pendingIf = -1
		}

		for _, ev := range events {
			switch ev.kind {
			case "if":
				closeCondition(ev.at)
				stack = append(stack, &ifFrame{inThen: true})
				pendingIf = ev.end
			case "elif":
				closeCondition(ev.at)
				if len(stack) > 0 {
					// Ветка «файла нет» названа: рамка находкой не станет.
					stack[len(stack)-1].hasElse = true
					stack[len(stack)-1].inThen = false
				}
			case "else":
				closeCondition(ev.at)
				if len(stack) > 0 {
					stack[len(stack)-1].hasElse = true
					stack[len(stack)-1].inThen = false
				}
			case "then":
				closeCondition(ev.at)
			case "fi":
				closeCondition(ev.at)
				if len(stack) == 0 {
					// Непарный `fi`: разбор потерял след (heredoc, конструкция
					// вне поля зрения). Молчим, а не выдумываем рамку.
					continue
				}
				frame := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				qualifies := frame.fileTest && !frame.hasElse
				for _, c := range frame.pending {
					if qualifies && c.inThen {
						found = append(found, c.g)
						continue
					}
					// Сверка, которую ЭТА рамка не присвоила, остаётся под
					// внешней: вложенное `if` по своему условию не отменяет
					// того, что снаружи стоит проверка файла.
					record(c.g)
				}
			case "cmp":
				comparisons++
				g := guardedComparison{File: relPath, Line: l.Line, Target: l.Target, Text: l.Raw}
				// Однострочная форма решается СРАЗУ и в рамку не попадает:
				// иначе `if [ -f X ]; then diff …; fi` дала бы две находки на
				// одну сверку — от условия в тексте и от закрытия рамки.
				if openingFileTestRe.MatchString(exe[:ev.end]) {
					found = append(found, g)
					continue
				}
				record(g)
			}
		}
	}
	// Незакрытые рамки на конце: разбор не дошёл до `fi`, значит о ветке
	// `else` он ничего не знает. Роняем кандидатов — молчание, а не догадка.
	return found, comparisons
}

// findGuardedComparisons — сверки под условием существования файла в одном
// Makefile. Тонкий переходник к общему обходчику: каждая логическая строка
// рецепта — отдельный вызов шелла, поэтому стек ветвлений её не переживает.
func findGuardedComparisons(relPath string, recipes makefileRecipes) (found []guardedComparison, comparisons int) {
	lines := make([]guardScanLine, 0, len(recipes.Lines))
	for _, rl := range recipes.Lines {
		lines = append(lines, guardScanLine{
			Line:       rl.Line,
			Target:     rl.Target,
			Executable: blankShellQuoted(rl.Text),
			Raw:        strings.TrimSpace(rl.Text),
		})
	}
	return scanGuardedComparisons(relPath, lines, true)
}

// findGuardedComparisonsInShell — то же для скрипта: строки принадлежат ОДНОМУ
// процессу, поэтому рамка `if` живёт до своего `fi`.
func findGuardedComparisonsInShell(relPath string, src shellSource) (found []guardedComparison, comparisons int) {
	return scanGuardedComparisons(relPath, src.Lines, false)
}
