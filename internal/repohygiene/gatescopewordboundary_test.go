// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// gatescopewordboundary_test.go — ГРАНИЦА СЛОВА СЧИТАЕТСЯ ПО РУНЕ, А НЕ ПО БАЙТУ.
//
// # Дефект, ради которого заведена эта проба
//
// Предикат границы проверял БАЙТ и считал словесным всякий байт со старшим
// битом. В тексте на русском такими байтами записаны не только буквы, но и
// кавычки-ёлочки, тире, неразрывные пробелы. А граница требуется с ОБЕИХ
// сторон — значит слово, стоящее вплотную за ёлочкой, не опознавалось, хотя
// разделитель после него стоял.
//
// ЦЕНА ЖИВЁТ В ОДНОМ МЕСТЕ — здесь, и ниже на неё ссылаются, а не повторяют её.
//
// Между байтовой границей и рунной опознание меняется у ВОСЬМИ проверочных
// файлов модуля из 2339. Измерено на `9ae1ca86`, 2026-09-22.
//
// ПОЧЕМУ ПРИ ЧИСЛЕ СТОИТ РЕВИЗИЯ, А НЕ СТОРОЖ.
//
// Сторожа быть не должно: байтовой редакции в дереве нет, и сверка потребовала
// бы держать снятый предикат живым ради одного замера — мёртвый код, чьё
// единственное применение подтверждать фразу о себе.
//
// Но «сторожа нет» полным ответом НЕ БЫЛО, и прежняя редакция этой шапки
// ошибалась дважды. Число не историческое: обе величины ЖИВЫЕ и дрейфуют —
// знаменатель от нового проверочного файла, числитель от нового слова вплотную
// к ёлочке, — и перемерить его можно, восстановив байтовую редакцию из
// истории. Раз можно перемерить, молчание о ревизии оставляло утверждение о
// СЕГОДНЯШНЕМ дереве без даты.
//
// Третий исход: ревизия и дата при числе. Так живое утверждение становится
// записью о прошлом, которая протухнуть не может и снятого предиката не
// требует. Дом у числа по-прежнему один, копий нет.
//
// СТРОКИ И ФАЙЛЫ — РАЗНЫЕ СЧЁТЫ, и здесь они не складываются. Темнели прежде
// всего СТРОКИ: из пяти строк шапки механизма, процитированных ниже, байтовая
// граница молчала на всех пяти. Сам файл механизма при этом оставался опознан
// — через свою строку `:12`, где нужное слово стоит между обычными пробелами,
// — и в восьмёрку не входил. Коммит, задачей которого было сделать обещание
// равным измеренному, сам ввёл дефект в предикат.
//
// Девятки здесь нет намеренно: она считалась от ДРУГОГО референта — редакции
// до снятия хвостового пробела, — и девятым файлом был ложный положительный
// того предиката (слово находилось подстрокой внутри другого слова). Число и
// его референт называются вместе, иначе они не сравнимы.
//
// # Почему чинится КОРЕНЬ, а не называется шестая форма
//
// Назвать «слово за ёлочкой» шестой невидимой формой было бы законно по букве
// — и неверно по существу: это зафиксировало бы дефект как норму и оставило
// неопознанными те файлы, чья цена названа выше. Граница по руне снимает
// регрессию, возвращает их и делает правдой комментарий у предиката. Механизм
// чинится в механизме, а не объявляется в его границах.
//
// Рядом с той ценой живёт ДРУГАЯ величина: у ВОСЕМНАДЦАТИ файлов меняется
// НАЗВАННОЕ СЛОВО при том же исходе (измерено там же и тогда же — `9ae1ca86`,
// 2026-09-22). Складывать их нельзя: это разные счёты.
package repohygiene

import (
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

// Слово, стоящее ВПЛОТНУЮ к небуквенной руне, опознаётся.
//
// # ПЕРЕБОР ВЫВЕДЕН ИЗ ДЕРЕВА, А НЕ ВЫПИСАН
//
// Прежние редакции этой пробы выписывали формы окружения и числа при них от
// руки, и это стоило трёх кругов подряд: подменённая единица (строка исполняет
// ПАРУ, а число считало ЗНАК), протухшее число в одной строке из двадцати
// пяти, и форма, которую прибор не мог выдать ПО ПОСТРОЕНИЮ — перенос строки,
// потому что перепись резала вход на строки ПРЕЖДЕ счёта.
//
// Выписанный перебор неполон всегда: он перечисляет то, о чём автор вспомнил.
// Поэтому состав форм и числа при них здесь СЧИТАЮТСЯ ИЗ ДЕРЕВА тем же
// предикатом, которым работает механизм, а таблица ниже лишь задаёт ИСХОД для
// каждой формы. Форма, найденная в дереве и не названная таблицей, — находка.
//
// # ПРЕДИКАТ ПЕРЕПИСИ — ТОТ ЖЕ, ЧТО У МЕХАНИЗМА: УЗЛОВОЙ
//
// Боевой вызывающий подаёт `c.Text` УЗЛА комментария: у `//` это строка, у
// блочного — весь блок вместе с переводами строк. Прежняя перепись была
// СТРОЧНОЙ и потому расходилась с механизмом, а заодно не могла увидеть
// перевод строки соседом.
//
// # ДВЕ СТОРОНЫ — ДВА РАЗБИЕНИЯ ОДНОГО МНОЖЕСТВА
//
// У каждого вхождения ровно один сосед слева и один справа, поэтому суммы по
// сторонам обязаны СОВПАДАТЬ. Это проверяется, и именно это поймало бы
// протухшее число: расхождение в единицу означало, что одну колонку
// перемерили, а вторую нет.
//
// ОБЛАСТЬ ОБХОДА: проверочные файлы Go, взятые у ИНДЕКСА репозитория.
// ОСТАТОК: файлы вне индекса и неразбираемые — о них перепись не высказывается.

// neighbourSide — сторона соседа.
type neighbourSide int

const (
	sideLeft neighbourSide = iota
	sideRight
)

// neighbourForm — форма окружения: сторона и руна соседа.
type neighbourForm struct {
	side neighbourSide
	r    rune
	edge bool // сосед — КРАЙ УЗЛА
}

func (nf neighbourForm) String() string {
	where := "слева"
	if nf.side == sideRight {
		where = "справа"
	}
	if nf.edge {
		return where + " край узла"
	}
	switch nf.r {
	case ' ':
		return where + " пробел"
	case '\t':
		return where + " табуляция"
	case '\n':
		return where + " перевод строки"
	}
	return where + " " + string(nf.r)
}

// isWordNeighbour — словесный ли сосед (буква либо цифра).
func (nf neighbourForm) isWordNeighbour() bool {
	return !nf.edge && isWordRune(nf.r)
}

// TestScopeWordBoundary_AWordAgainstANonASCIISeparatorIsFound — перебор форм
// окружения, ВЫВЕДЕННЫЙ из дерева, с исходом предиката по каждой.
func TestScopeWordBoundary_AWordAgainstANonASCIISeparatorIsFound(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	counts, occurrences := neighbourCensus(t, root)
	if occurrences == 0 {
		t.Fatal("вхождений слов перечня в дереве не найдено — перебор судил бы о непрочитанном")
	}

	// ДВЕ СТОРОНЫ — ОДНА СУММА. Расхождение означает, что счёт вёлся разными
	// предикатами либо одна колонка осталась от прежней ревизии.
	left, right := 0, 0
	for form, n := range counts {
		if form.side == sideLeft {
			left += n
		} else {
			right += n
		}
	}
	if left != right {
		t.Errorf("суммы по сторонам разошлись: слева %d, справа %d. Стороны суть два разбиения "+
			"ОДНОГО множества вхождений, и одна сумма — их предпосылка", left, right)
	}

	// ИСХОД ПРЕДИКАТА ПО КАЖДОЙ НАЙДЕННОЙ ФОРМЕ. Ожидание выводится из самой
	// формы: словесный сосед — границей не является, всякий иной — является.
	const word = "каждое"
	judged, disagreed := 0, 0
	forms := make([]neighbourForm, 0, len(counts))
	for form := range counts {
		forms = append(forms, form)
	}
	sort.Slice(forms, func(i, j int) bool { return forms[i].String() < forms[j].String() })
	for _, form := range forms {
		text, wantFound := probeTextFor(form, word)
		if containsWord(strings.ToLower(text), word) != wantFound {
			disagreed++
			t.Errorf("%s (вхождений %d): предикат ответил %v, ожидалось %v (вход %q)",
				form, counts[form], !wantFound, wantFound, text)
			continue
		}
		judged++
	}

	// ФОРМЫ, В ДЕРЕВЕ НЕ ВСТРЕЧАЮЩИЕСЯ, проверяются отдельно и помечены: ноль
	// вхождений — не повод не знать исхода, но и не повод считать форму живой.
	future := futureNeighbourForms()
	for _, form := range future {
		if counts[form] != 0 {
			t.Errorf("%s объявлена не встречающейся, а вхождений %d — помета пережила свой предмет",
				form, counts[form])
			continue
		}
		text, wantFound := probeTextFor(form, word)
		if containsWord(strings.ToLower(text), word) != wantFound {
			t.Errorf("%s (на будущее): предикат ответил %v, ожидалось %v",
				form, !wantFound, wantFound)
		}
	}

	t.Logf("перепись: ЕДИНИЦА — форма окружения (сторона + руна соседа), ВЫВЕДЕНА из дерева "+
		"узловым предикатом. Вхождений слов перечня %d · форм найдено %d · исход предиката "+
		"сошёлся у %d · разошёлся у %d · сумма слева %d = справа %d · форм, в дереве не "+
		"встречающихся, проверено на будущее %d",
		occurrences, len(counts), judged, disagreed, left, right, len(future))
}

// futureNeighbourForms — формы, которых в дереве нет ни разу. Перечень
// объявлен, и это сказано: вывести «чего в дереве НЕТ» из дерева нельзя.
// Среди них ПЕРЕВОД СТРОКИ — сосед, возможный только у блочного узла; прежняя
// строчная перепись не могла выдать его по построению.
func futureNeighbourForms() []neighbourForm {
	return []neighbourForm{
		{side: sideLeft, r: '\n'},
		{side: sideRight, r: '\n'},
		{side: sideLeft, r: '—'},
		{side: sideRight, r: '—'},
		{side: sideLeft, r: '\u00a0'},
		{side: sideLeft, r: '„'},
		{side: sideRight, r: '“'},
	}
}

// probeTextFor — вход предиката для названной формы и ожидаемый исход.
func probeTextFor(form neighbourForm, word string) (string, bool) {
	const filler = "обещает"
	switch {
	case form.edge && form.side == sideLeft:
		return word + " " + filler, true
	case form.edge && form.side == sideRight:
		return filler + " " + word, true
	case form.side == sideLeft:
		return filler + string(form.r) + word + " " + filler, !form.isWordNeighbour()
	default:
		return filler + " " + word + string(form.r) + filler, !form.isWordNeighbour()
	}
}

// neighbourCensus — формы окружения и их числа, считанные ПО УЗЛАМ
// комментариев проверочных файлов индекса.
func neighbourCensus(t *testing.T, root string) (map[neighbourForm]int, int) {
	t.Helper()
	counts := map[neighbourForm]int{}
	total := 0
	for _, path := range goTestFilesUnder(t, root) {
		src, err := os.ReadFile(path) // #nosec G304 -- путь пришёл из индекса репозитория
		if err != nil {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, src, parser.ParseComments)
		if err != nil {
			continue
		}
		for _, group := range f.Comments {
			for _, c := range group.List {
				text := strings.ToLower(c.Text)
				for _, w := range completenessWords {
					for i := 0; ; {
						j := strings.Index(text[i:], w)
						if j < 0 {
							break
						}
						start, end := i+j, i+j+len(w)
						counts[neighbourAt(text, start, sideLeft)]++
						counts[neighbourAt(text, end, sideRight)]++
						total++
						i = start + 1
						if i >= len(text) {
							break
						}
					}
				}
			}
		}
	}
	return counts, total
}

// neighbourAt — форма соседа с названной стороны.
func neighbourAt(text string, at int, side neighbourSide) neighbourForm {
	if side == sideLeft {
		if at <= 0 {
			return neighbourForm{side: side, edge: true}
		}
		r, _ := utf8.DecodeLastRuneInString(text[:at])
		return neighbourForm{side: side, r: r}
	}
	if at >= len(text) {
		return neighbourForm{side: side, edge: true}
	}
	r, _ := utf8.DecodeRuneInString(text[at:])
	return neighbourForm{side: side, r: r}
}

// Вторая половина того же, и она о ДЕРЕВЕ, а не о синтетике: строки шапки
// самого механизма. Они и есть тот текст, ради которого предикат написан.
//
// До починки байтовая граница молчала на ВСЕХ ПЯТИ. После починки опознаются
// ТРИ: две остальные молчат по устройству, и причина у каждой названа ниже.
// «Молчала на всех пяти» — о прежнем состоянии; сколько возвращает нынешнее,
// сказано отдельно, чтобы одно не читалось как второе.
//
// Строки взяты ДОСЛОВНО, а не прочитаны из файла: предмет здесь ФОРМА ЗАПИСИ,
// а не текущее содержимое соседа.
//
// «Изменится там — пробу пересмотрит то же изменение» БЫЛО ОБЕЩАНИЕМ БЕЗ
// МЕХАНИЗМА, и оно нарушилось в том же коммите, который его дал: одна из пяти
// строк была переписана, и в дереве её не стало, а проба продолжала цитировать
// несуществующее. Механизм теперь есть — ниже проверяется, что каждая цитата
// ЕЩЁ ЛЕЖИТ в соседнем файле. Цитата, пережившая свой оригинал, есть
// утверждение о дереве, которого дерево не исполняет.
func TestScopeWordBoundary_TheMechanismsOwnHeaderLinesAreRecognised(t *testing.T) {
	t.Parallel()

	// Строки, несущие слово ВИДИМОЙ формы вплотную за ёлочкой. До починки не
	// опознавалась ни одна.
	carrying := headerLinesCarryingAVisibleForm()
	recognised := 0
	for _, line := range carrying {
		if wordOfPerchenIn(line) == "" {
			t.Errorf("строка шапки механизма не опознана ни одним словом перечня: %q — "+
				"предикат молчит на том самом тексте, которым механизм иллюстрирует обещание "+
				"полноты", line)
			continue
		}
		recognised++
	}

	// ОТРИЦАТЕЛЬНАЯ ПОЛОВИНА, и она не послабление: эти две строки шапки
	// опознаваться НЕ ДОЛЖНЫ, и причина у каждой своя — она названа, а не
	// списана на границу слова.
	notCarrying := headerLinesCarryingNone()
	for _, tc := range notCarrying {
		if w := wordOfPerchenIn(tc.line); w != "" {
			t.Errorf("строка опознана словом %q, а опознаваться не должна: %s (%q)",
				w, tc.why, tc.line)
		}
	}

	t.Logf("перепись: строк шапки проверено %d · несущих слово видимой формы %d, опознано %d · "+
		"не несущих %d (причины названы: объявленно невидимые формы · оборот, разорванный "+
		"переносом строки)",
		len(carrying)+len(notCarrying), len(carrying), recognised, len(notCarrying))
}

// ЦИТАТА, ПЕРЕЖИВШАЯ СВОЙ ОРИГИНАЛ, — НАХОДКА.
//
// Проба выше цитирует строки соседнего файла дословно и утверждает о них. Без
// проверки такая цитата живёт дольше своего предмета: строку перепишут, проба
// останется зелёной и будет судить текст, которого в дереве нет. Ровно это и
// случилось в коммите, который цитаты завёл.
//
// Механизм простой и потому надёжный: каждая цитата обязана НАЙТИСЬ в соседе.
// Обход — один названный файл, и это область, а не умолчание.
func TestScopeWordBoundary_EveryQuotedHeaderLineStillExistsInTheNeighbour(t *testing.T) {
	t.Parallel()

	const neighbour = "gatescopedeclared_test.go"
	raw, err := os.ReadFile(neighbour)
	if err != nil {
		t.Fatalf("сосед %s не прочитан: %v — проверять цитаты не у чего", neighbour, err)
	}
	body := string(raw)

	quoted := quotedHeaderLines()
	if len(quoted) == 0 {
		t.Fatal("цитат не объявлено — проба судила бы о непрочитанном")
	}
	stale := 0
	for _, line := range quoted {
		if !strings.Contains(body, strings.TrimSpace(line)) {
			stale++
			t.Errorf("цитата пережила свой оригинал: в %s строки нет — %q. Проба утверждает о "+
				"тексте, которого в дереве больше не существует", neighbour, line)
		}
	}
	t.Logf("перепись: ОБЛАСТЬ — один файл (%s); цитат объявлено %d · найдено в соседе %d",
		neighbour, len(quoted), len(quoted)-stale)
}

// quotedHeaderLines — ВСЕ строки соседа, которые цитируют пробы этого файла.
// Один перечень на обе пробы: два разошлись бы молча.
func quotedHeaderLines() []string {
	var out []string
	out = append(out, headerLinesCarryingAVisibleForm()...)
	for _, tc := range headerLinesCarryingNone() {
		out = append(out, tc.line)
	}
	return out
}

// wordOfPerchenIn — слово перечня, опознанное в строке; "" если ни одного.
func wordOfPerchenIn(line string) string {
	low := strings.ToLower(line)
	for _, w := range completenessWords {
		if containsWord(low, w) {
			return w
		}
	}
	return ""
}

// headerLinesCarryingAVisibleForm — цитаты шапки соседа, несущие слово ВИДИМОЙ
// формы вплотную за небуквенной руной.
func headerLinesCarryingAVisibleForm() []string {
	return []string{
		"// ИСПОЛНЯЕМОГО. Гейт обещает шапкой «всякое», «каждое», «ни одного вне», а",
		"// сюда. Дефект — когда шапка обещает полноту («всякое», «каждое», «ни",
		"// Ищется в КОММЕНТАРИЯХ, а не в строках: текст отказа, называющий «каждый»,",
	}
}

// headerLineWithoutAVisibleForm — цитата, которая опознаваться НЕ должна, и
// причина этого.
type headerLineWithoutAVisibleForm struct {
	line string
	why  string
}

// headerLinesCarryingNone — цитаты шапки соседа, не несущие ни одного слова
// видимой формы. Причина у каждой своя и названа.
func headerLinesCarryingNone() []headerLineWithoutAVisibleForm {
	return []headerLineWithoutAVisibleForm{
		{
			line: "// НЕ ВИДНЫ шесть: «ни один» · «все» · «исчерпывающий» · «полон» ·",
			why: "несёт только слова ОБЪЯВЛЕННО НЕВИДИМЫХ форм («ни один», «все», " +
				"«исчерпывающий», «полон») — молчание здесь и есть объявленное поведение",
		},
		{
			line: "// одного вне»), а корень выписан: тогда «вне области 0» читается как",
			why: "оборот «ни одного» разорван переносом строки, а предикат судит ОДИН УЗЕЛ " +
				"комментария — у `//` это строка, у блочного весь блок, — и слово ищет сплошным " +
				"текстом: через перенос оборот сплошным не является ни там, ни там. Это шестая " +
				"невидимая форма, и она названа в шапке соседа",
		},
	}
}
