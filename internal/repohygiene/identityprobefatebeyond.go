// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// identityprobefatebeyond.go — две записи ведомости судьбы проб полосы личности
// СВЕРХ её строк (задача #1276, часть 2 — гейты и пробы). Ведомость и её гейт —
// identityprobefate.go; здесь то, что её строки не держат by construction.
//
// ─────────────────────────────────────────────────────────────────────────────
// А. РЕШЕНИЕ ПО КАЖДОМУ ИЗ ГЕЙТОВ ПРИЗНАКА
//
// Признак задачи #1276 назвал гейты `deploy/identity_*_test.go` на ревизии
// `identitySignGateRevision`: их `identitySignGateCount`, и каждый написан против
// отказа, который уже случался. Предикат снятия требует записанного решения по
// КАЖДОМУ. Строки ведомости этого не держат: снятая проба уходит из ведомости
// вместе со своей строкой (так устроена её дисциплина), и о снятом гейте в
// документе оставались только номера строк прежней записи в прозе — номера,
// которые не указывают ни во что. С ними ушла бы и память о классе отказа, а
// первый же вернувшийся класс встретил бы дерево без проверки.
//
// Поэтому у каждого гейта признака — ровно одна строка таблицы
// `identitySignGateHeader`, и она судится против дерева:
//
//  1. СОСТАВ. Строк ровно столько, сколько гейтов назвал признак; имя — одно имя
//     пробы, и ни одно не повторено.
//  2. ИСХОД — из закрытого словаря ведомости (`identityProbeFates`) и согласен с
//     деревом: «снять» — файла в индексе нет и строки ведомости о нём нет;
//     «оставить» и «переписать» — файл в индексе и строка ведомости о нём есть.
//  3. ИЗМЕНЕНИЕ названо задачей (`#N`) у снятого и переписанного; у оставленного
//     его нет (`—`).
//  4. КЛАСС ОТКАЗА назван словами.
//  5. ЧЕМ ДЕРЖИТСЯ КЛАСС — из закрытого словаря `identitySignHolds`: у снятого —
//     «преемник» либо «невоспроизводим», у живого — «проба на месте».
//  6. ДОВОД РЕЗОЛВИТСЯ. У снятого — хотя бы одна ссылка: координата в этом
//     дереве (её разрешение и якорь судит гейт ведомости — она стоит вне строк
//     ведомости) либо проба пиненного модуля службы в форме
//     `<псевдоним>:<путь>#<символ>`, и символ в этом файле ОБЪЯВЛЕН. У живого —
//     координата в сам гейт.
//  7. РАЗБИВКА СХОДИТСЯ со строками, итог — с числом гейтов признака.
//
// Ссылка на пиненный модуль судится по ПИНУ из go.mod, а не по соседнему клону:
// подъём пина, снявший преемника, краснеет здесь, а не проходит молча.
//
// ─────────────────────────────────────────────────────────────────────────────
// Б. СУДЬБА КАЖДОЙ ПРОБЫ ВНЕ ВЕДОМОСТИ, НАЗЫВАЮЩЕЙ ПОСТАВЩИКА
//
// Ведомость держит только `deploy/identity_*_test.go`. Пробы, называющие снятого
// поставщика, в дереве стоят и вне этого образца — их решение по одной не было
// записано нигде. Таблица `identityBeyondHeader` — одна строка на пробу:
//
//  1. ПОПУЛЯЦИЯ ВЫВОДИТСЯ ОБХОДОМ: всякий отслеживаемый файл формы пробы
//     (`identityBeyondForm`), чья строка называет поставщика
//     (`identityVendorNamedIn`), кроме строк самой ведомости. Файл популяции без
//     строки — находка.
//  2. ОДНА СТРОКА — ОДИН ПУТЬ от корня, формы пробы; образец, перечень и проба
//     ведомости — находка.
//  3. ИСХОД из словаря ведомости и ИСПОЛНЕНИЕ согласны с деревом: «снять, да» —
//     файла в индексе нет; «снять, нет» и «переписать» — файл есть; «переписать,
//     нет» — и всё ещё называет поставщика; «оставить» исполнения не имеет (`—`).
//  4. ФРАГМЕНТ ИМЕНИ: у пробы, называющей поставщика, — текст, стоящий на одной
//     из строк, которые его называют; у пробы, его не называющей (либо снятой), —
//     `—`. Фрагмент ушёл из пробы — строка описывает уже другое содержимое.
//  5. ДОВОД у исполненного и ожидающего называет изменение задачей (`#N`).
//  6. РАЗБИВКА по исходу и исполнению сходится со строками.
//
// Строки таблицы Б координатами документа не читаются: фрагмент имени —
// содержимое пробы, а не адрес в ней, и судится подстрокой, а не якорем.
//
// ─────────────────────────────────────────────────────────────────────────────
// РАСПОЗНАВАТЕЛЬ ИМЕНИ — ОДИН ДОМ ПЛЮС БРЕНД
//
// Имена служб поставщика и пространство его образов берутся у единственного дома
// — `vendorMarkBoundedIn` (метки `retiredVendorMarks`, граница справа). Сверх них
// здесь судится БРЕНД отдельным словом (`identityVendorBrand`): им пробы называют
// поставщика там, где имени службы нет, — префикс полосы раздачи, слой учётных
// данных площадки. Граница двусторонняя: слева не латинская буква, справа не
// строчная (`memory`, `story` — не имя). Названное исключение одно — путь
// импорта библиотек под тем же именем (`identityVendorLibraryPrefix`): одна из
// них законно остаётся у собственной чеканки токенов, и потолок привязок
// исключает голый бренд по той же причине. Цена границы та же, что у потолка:
// бренд прописными, продолженный прописной, неотличим от английского слова.
//
// ─────────────────────────────────────────────────────────────────────────────
// ФОРМЫ ПРОБЫ — ПЕРЕЧИСЛЕНЫ, И ПРОЧИХ НЕТ
//
// `identityBeyondForm` знает: `*_test.go` · `*.test.ts(x)`/`*.test.mjs` ·
// `*.spec.ts` · всё под `deploy/tests/` и `tests/` · `…/testdata/…` ·
// `…/src/test/…` · `*-test.sh`/`*_test.sh` · `*.bats` · `*_test.py`/`test_*.py`.
// Проза (`.md`, `.mdx`) пробой не является. Перепись печатает, сколько файлов
// каждой формы прочитано: форма, выпавшая из обхода, видна нулём.

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// identitySignGateCount — число гейтов, названных признаком #1276.
const identitySignGateCount = 17

// identitySignGateRevision — ревизия, на которой признак их назвал.
const identitySignGateRevision = "9f6e5b126a1"

// identitySignGateHeader — заголовок таблицы решений по гейтам признака.
var identitySignGateHeader = []string{"#", "гейт признака", "исход", "изменение", "класс отказа",
	"чем держится класс", "довод (координата)"}

// identitySignGateTotals — заголовок разбивки решений по исходам.
var identitySignGateTotals = []string{"исход признака", "гейтов"}

// Чем держится класс отказа гейта признака. Словарь ЗАКРЫТ.
const (
	// identitySignHeldBySuccessor — свойство судит другая проба, названная ссылкой.
	identitySignHeldBySuccessor = "преемник"
	// identitySignHeldByAbsence — механизма отказа под нашей посадкой нет, и ссылка
	// называет то, что делает его невоспроизводимым либо ловит его возврат.
	identitySignHeldByAbsence = "невоспроизводим"
	// identitySignHeldInPlace — гейт жив и судит свой класс сам.
	identitySignHeldInPlace = "проба на месте"
)

// identitySignHolds — закрытый словарь в порядке печати.
var identitySignHolds = []string{identitySignHeldBySuccessor, identitySignHeldByAbsence, identitySignHeldInPlace}

// identityNoValue — ячейка «значения нет»: изменения у оставленного гейта,
// исполнения у оставленной пробы, фрагмента у пробы, не называющей поставщика.
const identityNoValue = "—"

// identityTaskRef — ссылка на задачу в ячейке изменения и в доводе.
var identityTaskRef = regexp.MustCompile(`#[0-9]+`)

// identityPinnedRef — ссылка на пробу пиненного модуля: псевдоним, путь от
// корня модуля и символ, который файл обязан объявлять.
var identityPinnedRef = regexp.MustCompile("`([a-z]+):([A-Za-z0-9_./-]+)#([A-Za-z_][A-Za-z0-9_]*)`")

// identityPinnedModules — псевдонимы пиненных модулей. Перечень ЗАКРЫТ:
// псевдоним вне него — находка, а не молчание.
var identityPinnedModules = map[string]string{"kaname": "github.com/PRO-Robotech/kaname"}

// identityBeyondHeader — заголовок таблицы проб вне ведомости.
var identityBeyondHeader = []string{"#", "проба", "исход", "исполнено", "фрагмент имени", "довод"}

// identityBeyondTotals — заголовок разбивки проб вне ведомости.
var identityBeyondTotals = []string{"исход вне ведомости", "исполнено", "проб"}

// Исполнение исхода пробы вне ведомости.
const (
	identityBeyondDone    = "да"
	identityBeyondPending = "нет"
)

// identityVendorBrand — бренд снятого поставщика, судимый отдельным словом.
const identityVendorBrand = "ory"

// identityVendorLibraryPrefix — начало пути импорта: бренд, стоящий сразу за
// ним и продолженный `/`, — путь библиотеки, а не имя службы.
const identityVendorLibraryPrefix = "github.com/"

// identityBeyondPathForm — путь от корня, выразимый одной ячейкой: без
// образцов, перечней и пробелов.
var identityBeyondPathForm = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

// identityPinnedRefRow — прочитанная ссылка на пиненный модуль.
type identityPinnedRefRow struct {
	Alias, Path, Symbol, Raw string
}

// Key — ключ файла пиненного модуля в фактах.
func (r identityPinnedRefRow) Key() string { return r.Alias + ":" + r.Path }

// identitySignGateRow — строка таблицы решений по гейтам признака.
type identitySignGateRow struct {
	Line                            int
	File, Fate, Change, Class, Held string
	Coords                          []identityProbeCoordRef
	Pinned                          []identityPinnedRefRow
	Malformed                       bool
}

// identityBeyondRow — строка таблицы проб вне ведомости.
type identityBeyondRow struct {
	Line                  int
	Path, Fate, Done, Why string
	Fragment              string
	HasFragment           bool
	Malformed             bool
}

// identityBeyondKey — клетка разбивки: исход и исполнение.
type identityBeyondKey struct{ Fate, Done string }

// identityProbeFateBeyond — прочитанные записи А и Б.
type identityProbeFateBeyond struct {
	SignTables int
	Sign       []identitySignGateRow
	// SignTotals — письменная разбивка решений; SignTotal — итог (-1 — нет).
	SignTotals       map[string]int
	SignTotalsLine   map[string]int
	SignTotal        int
	SignTotalsTables int
	SignUnreadable   []string

	BeyondTables       int
	Beyond             []identityBeyondRow
	BeyondTotals       map[identityBeyondKey]int
	BeyondTotalsLine   map[identityBeyondKey]int
	BeyondTotal        int
	BeyondTotalsTables int
	BeyondUnreadable   []string
}

// parseIdentityProbeFateBeyond читает записи А и Б и возвращает строки
// документа (с единицы), которые координатами документа не читаются: таблица Б
// и её разбивка. Таблица А остаётся прозой для суда координат — её доводы
// разрешает и прибивает якорями гейт ведомости.
func parseIdentityProbeFateBeyond(lines []string) (identityProbeFateBeyond, map[int]bool) {
	b := identityProbeFateBeyond{
		SignTotals: map[string]int{}, SignTotalsLine: map[string]int{}, SignTotal: -1,
		BeyondTotals: map[identityBeyondKey]int{}, BeyondTotalsLine: map[identityBeyondKey]int{}, BeyondTotal: -1,
	}
	claimed := map[int]bool{}
	claim := func(at int, rowLines []int) {
		claimed[at+1] = true
		if at+1 < len(lines) && isMarkdownSeparator(splitMarkdownRow(lines[at+1])) {
			claimed[at+2] = true
		}
		for _, n := range rowLines {
			claimed[n] = true
		}
	}
	for i, line := range lines {
		cells := splitMarkdownRow(line)
		switch {
		case sameCells(cells, identitySignGateHeader):
			b.SignTables++
			rows, rowLines := tableAfter(lines, i)
			for k, c := range rows {
				b.Sign = append(b.Sign, signGateRowOf(c, rowLines[k]))
			}
		case sameCells(cells, identitySignGateTotals):
			b.SignTotalsTables++
			rows, rowLines := tableAfter(lines, i)
			for k, c := range rows {
				readSignTotal(&b, c, rowLines[k])
			}
		case sameCells(cells, identityBeyondHeader):
			b.BeyondTables++
			rows, rowLines := tableAfter(lines, i)
			claim(i, rowLines)
			for k, c := range rows {
				b.Beyond = append(b.Beyond, beyondRowOf(c, rowLines[k]))
			}
		case sameCells(cells, identityBeyondTotals):
			b.BeyondTotalsTables++
			rows, rowLines := tableAfter(lines, i)
			claim(i, rowLines)
			for k, c := range rows {
				readBeyondTotal(&b, c, rowLines[k])
			}
		}
	}
	return b, claimed
}

// signGateRowOf — разбор строки таблицы А.
func signGateRowOf(cells []string, line int) identitySignGateRow {
	r := identitySignGateRow{Line: line}
	if len(cells) != len(identitySignGateHeader) {
		r.Malformed = true
		return r
	}
	r.File = strings.Trim(cells[1], "` ")
	r.Fate = plainCell(cells[2])
	r.Change = plainCell(cells[3])
	r.Class = plainCell(cells[4])
	r.Held = plainCell(cells[5])
	r.Coords = coordsOf(cells[6], "", line)
	r.Pinned = pinnedRefsOf(cells[6])
	return r
}

// pinnedRefsOf — ссылки на пиненный модуль во фрагменте.
func pinnedRefsOf(text string) []identityPinnedRefRow {
	var out []identityPinnedRefRow
	for _, m := range identityPinnedRef.FindAllStringSubmatch(text, -1) {
		out = append(out, identityPinnedRefRow{Alias: m[1], Path: m[2], Symbol: m[3], Raw: m[0]})
	}
	return out
}

// beyondRowOf — разбор строки таблицы Б.
func beyondRowOf(cells []string, line int) identityBeyondRow {
	r := identityBeyondRow{Line: line}
	if len(cells) != len(identityBeyondHeader) {
		r.Malformed = true
		return r
	}
	r.Path = strings.Trim(cells[1], "` ")
	r.Fate = plainCell(cells[2])
	r.Done = plainCell(cells[3])
	if frag := strings.TrimSpace(cells[4]); frag != identityNoValue {
		r.Fragment, r.HasFragment = anchorText(frag)
		if !r.HasFragment {
			r.Fragment, r.HasFragment = frag, true
		}
	}
	r.Why = strings.TrimSpace(cells[5])
	return r
}

// readSignTotal — строка разбивки А.
func readSignTotal(b *identityProbeFateBeyond, cells []string, line int) {
	if len(cells) != len(identitySignGateTotals) {
		b.SignUnreadable = append(b.SignUnreadable, fmt.Sprintf("строка %d", line))
		return
	}
	key := plainCell(cells[0])
	n, err := strconv.Atoi(plainCell(cells[1]))
	switch {
	case err != nil:
		b.SignUnreadable = append(b.SignUnreadable, fmt.Sprintf("строка %d (%q)", line, cells[1]))
	case key == identityProbeFateTotalRow:
		b.SignTotal = n
	default:
		if _, dup := b.SignTotals[key]; dup {
			b.SignUnreadable = append(b.SignUnreadable, fmt.Sprintf("строка %d: исход %q второй раз", line, key))
			return
		}
		b.SignTotals[key] = n
		b.SignTotalsLine[key] = line
	}
}

// readBeyondTotal — строка разбивки Б.
func readBeyondTotal(b *identityProbeFateBeyond, cells []string, line int) {
	if len(cells) != len(identityBeyondTotals) {
		b.BeyondUnreadable = append(b.BeyondUnreadable, fmt.Sprintf("строка %d", line))
		return
	}
	key := identityBeyondKey{Fate: plainCell(cells[0]), Done: plainCell(cells[1])}
	n, err := strconv.Atoi(plainCell(cells[2]))
	switch {
	case err != nil:
		b.BeyondUnreadable = append(b.BeyondUnreadable, fmt.Sprintf("строка %d (%q)", line, cells[2]))
	case key.Fate == identityProbeFateTotalRow:
		b.BeyondTotal = n
	default:
		if _, dup := b.BeyondTotals[key]; dup {
			b.BeyondUnreadable = append(b.BeyondUnreadable,
				fmt.Sprintf("строка %d: клетка «%s · %s» второй раз", line, key.Fate, key.Done))
			return
		}
		b.BeyondTotals[key] = n
		b.BeyondTotalsLine[key] = line
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// А. Суд над решениями по гейтам признака.

// identitySignFacts — дерево, против которого судятся решения.
type identitySignFacts struct {
	// Tracked — пути индекса git.
	Tracked map[string]bool
	// Pinned — строки файлов пиненных модулей по ключу `<псевдоним>:<путь>`;
	// ключа нет — файла в пиненном дереве нет.
	Pinned map[string][]string
}

// identitySignCensus — объём осмотренного.
type identitySignCensus struct {
	Rows, Absent, Present int
	ByFate                map[string]int
	ByHeld                map[string]int
	Coords, Pinned        int
}

// String — перепись одной строкой.
func (c identitySignCensus) String() string {
	parts := []string{fmt.Sprintf("гейтов признака %d (@%s) · записано %d · в индексе %d · снято %d",
		identitySignGateCount, identitySignGateRevision, c.Rows, c.Present, c.Absent)}
	for _, f := range identityProbeFates {
		parts = append(parts, fmt.Sprintf("%s %d", f, c.ByFate[f]))
	}
	for _, h := range identitySignHolds {
		parts = append(parts, fmt.Sprintf("%s %d", h, c.ByHeld[h]))
	}
	parts = append(parts, fmt.Sprintf("координат в доводах %d · проб пиненного модуля %d", c.Coords, c.Pinned))
	return strings.Join(parts, " · ")
}

// knownHeld — ответ из закрытого словаря «чем держится класс».
func knownHeld(h string) bool {
	for _, k := range identitySignHolds {
		if h == k {
			return true
		}
	}
	return false
}

// pinnedDeclares — файл пиненного модуля объявляет символ. Форма объявления
// выводится из вида файла; вида, которого разбор не знает, — нет ответа.
func pinnedDeclares(path string, text []string, symbol string) (bool, error) {
	var head string
	switch {
	case strings.HasSuffix(path, ".go"):
		head = "func " + symbol + "("
	case strings.HasSuffix(path, ".py"):
		head = "def " + symbol + "("
	default:
		return false, fmt.Errorf("объявление символа в файле вида %q разбор не знает", path)
	}
	for _, l := range text {
		if strings.HasPrefix(strings.TrimSpace(l), head) {
			return true, nil
		}
	}
	return false, nil
}

// judgeIdentitySignGates — семь требований записи А. want — число гейтов
// признака (в дереве — identitySignGateCount).
func judgeIdentitySignGates(l identityProbeFateLedger, f identitySignFacts, probeDir, ledgerFile string,
	want int) ([]string, identitySignCensus) {
	c := identitySignCensus{ByFate: map[string]int{}, ByHeld: map[string]int{}}
	b := l.Beyond
	switch {
	case b.SignTables == 0:
		return []string{fmt.Sprintf("%s: решения по %d гейтам признака #1276 (`%s/identity_*_test.go` @ %s) "+
			"не записаны — таблицы «| %s |» в документе нет. Снятая проба уходит из ведомости вместе со "+
			"своей строкой, и память о классе отказа, против которого гейт написан, не держит тогда ничто",
			ledgerFile, want, probeDir, identitySignGateRevision, strings.Join(identitySignGateHeader, " | "))}, c
	case b.SignTables > 1:
		return []string{fmt.Sprintf("%s: таблица решений по гейтам признака стоит %d раз — два перечня об "+
			"одном предмете, и действующим оказался бы прочитанный первым", ledgerFile, b.SignTables)}, c
	}

	var found []string
	at := func(r identitySignGateRow) string { return fmt.Sprintf("%s:%d", ledgerFile, r.Line) }
	inLedger := map[string]bool{}
	for _, r := range l.Rows {
		inLedger[r.File] = true
	}
	named := map[string]int{}
	for _, r := range b.Sign {
		if r.Malformed {
			found = append(found, fmt.Sprintf("%s: строка решения несёт не %d ячеек — разбор не знает, где в "+
				"ней гейт, исход и довод", at(r), len(identitySignGateHeader)))
			continue
		}
		if !identityProbeName.MatchString(r.File) {
			found = append(found, fmt.Sprintf("%s: ячейка гейта %q — не одно имя пробы `identity_<имя>_test.go`. "+
				"Групповая запись прячет за признаком гейты, решения о которых поимённо не записал никто",
				at(r), r.File))
			continue
		}
		if prev, dup := named[r.File]; dup {
			found = append(found, fmt.Sprintf("%s: %s назван второй раз (первый — строка %d) — два решения "+
				"об одном гейте", at(r), r.File, prev))
			continue
		}
		named[r.File] = r.Line
		c.Rows++
		c.Coords += len(r.Coords)
		c.Pinned += len(r.Pinned)

		path := probeDir + "/" + r.File
		tracked := f.Tracked[path]
		if tracked {
			c.Present++
		} else {
			c.Absent++
		}
		if knownFate(r.Fate) {
			c.ByFate[r.Fate]++
		} else {
			found = append(found, fmt.Sprintf("%s: %s — исход %q вне закрытого словаря %v",
				at(r), r.File, r.Fate, identityProbeFates))
		}
		if knownHeld(r.Held) {
			c.ByHeld[r.Held]++
		} else {
			found = append(found, fmt.Sprintf("%s: %s — «чем держится класс» %q вне закрытого словаря %v",
				at(r), r.File, r.Held, identitySignHolds))
		}
		if strings.TrimSpace(r.Class) == "" || r.Class == identityNoValue {
			found = append(found, fmt.Sprintf("%s: %s — класс отказа не назван: гейт написан против "+
				"случавшегося отказа, и без его имени решение нечем проверить", at(r), r.File))
		}

		switch r.Fate {
		case identityFateRemove:
			if tracked {
				found = append(found, fmt.Sprintf("%s: %s записан снятым, а %s в индексе есть — снимите его "+
					"вместе с предметом либо запишите исход, который исполнен", at(r), r.File, path))
			}
			if inLedger[r.File] {
				found = append(found, fmt.Sprintf("%s: %s записан снятым, а строка о нём в ведомости стоит — "+
					"два исхода об одной пробе", at(r), r.File))
			}
			if !identityTaskRef.MatchString(r.Change) {
				found = append(found, fmt.Sprintf("%s: %s — не названа задача, которая его сняла (ячейка "+
					"изменения %q без `#<номер>`)", at(r), r.File, r.Change))
			}
			if r.Held == identitySignHeldInPlace {
				found = append(found, fmt.Sprintf("%s: %s снят, а класс «держится пробой на месте» — у снятого "+
					"гейта класс держит %s либо довод «%s»", at(r), r.File,
					identitySignHeldBySuccessor, identitySignHeldByAbsence))
			}
			if len(r.Coords)+len(r.Pinned) == 0 {
				found = append(found, fmt.Sprintf("%s: %s снят, а довод без ссылки — назовите координату "+
					"преемника (или того, что делает класс невоспроизводимым) в этом дереве либо пробу "+
					"пиненного модуля `<псевдоним>:<путь>#<символ>`", at(r), r.File))
			}
		case identityFateKeep, identityFateRewrite:
			if !tracked {
				found = append(found, fmt.Sprintf("%s: %s записан живым (%s), а %s в индексе нет — гейт снят; "+
					"запишите исход «%s», изменение и то, чем держится класс", at(r), r.File, r.Fate, path,
					identityFateRemove))
			}
			if !inLedger[r.File] {
				found = append(found, fmt.Sprintf("%s: %s записан живым, а строки о нём в ведомости нет — "+
					"судьба живой пробы пишется её строкой", at(r), r.File))
			}
			if r.Held != identitySignHeldInPlace {
				found = append(found, fmt.Sprintf("%s: %s жив, а класс держится «%s» — живой гейт судит свой "+
					"класс сам («%s»)", at(r), r.File, r.Held, identitySignHeldInPlace))
			}
			own := false
			for _, co := range r.Coords {
				own = own || co.Path == path
			}
			if !own {
				found = append(found, fmt.Sprintf("%s: %s — довод живого гейта обязан указывать в сам гейт "+
					"(`%s:<строка>`)", at(r), r.File, path))
			}
			if r.Fate == identityFateKeep && r.Change != identityNoValue {
				found = append(found, fmt.Sprintf("%s: %s оставлен, а изменение названо (%q) — оставленный "+
					"гейт не переписывался, пишите «%s»", at(r), r.File, r.Change, identityNoValue))
			}
			if r.Fate == identityFateRewrite && !identityTaskRef.MatchString(r.Change) {
				found = append(found, fmt.Sprintf("%s: %s — не названа задача, которая его переписала",
					at(r), r.File))
			}
		}

		for _, p := range r.Pinned {
			if _, ok := identityPinnedModules[p.Alias]; !ok {
				found = append(found, fmt.Sprintf("%s: %s — псевдоним модуля %q в %s вне перечня %v",
					at(r), r.File, p.Alias, p.Raw, pinnedAliases()))
				continue
			}
			text, ok := f.Pinned[p.Key()]
			if !ok {
				found = append(found, fmt.Sprintf("%s: %s — %s: файла %s в пиненном дереве %s нет — преемник "+
					"снят или перенесён подъёмом пина, и класс отказа держит теперь неизвестно что",
					at(r), r.File, p.Raw, p.Path, p.Alias))
				continue
			}
			declared, err := pinnedDeclares(p.Path, text, p.Symbol)
			switch {
			case err != nil:
				found = append(found, fmt.Sprintf("%s: %s — %s: %v", at(r), r.File, p.Raw, err))
			case !declared:
				found = append(found, fmt.Sprintf("%s: %s — %s: в %s пиненного дерева объявления %s нет — "+
					"преемник снят или переименован", at(r), r.File, p.Raw, p.Path, p.Symbol))
			}
		}
	}
	if c.Rows != want {
		found = append(found, fmt.Sprintf("%s: решений записано %d, а признак #1276 назвал гейтов %d (@%s) — "+
			"решение пишется по КАЖДОМУ", ledgerFile, c.Rows, want, identitySignGateRevision))
	}

	switch {
	case b.SignTotalsTables == 0:
		found = append(found, fmt.Sprintf("%s: разбивки решений по исходам («| %s |») нет", ledgerFile,
			strings.Join(identitySignGateTotals, " | ")))
	case b.SignTotalsTables > 1:
		found = append(found, fmt.Sprintf("%s: разбивка решений стоит %d раз", ledgerFile, b.SignTotalsTables))
	default:
		for _, u := range b.SignUnreadable {
			found = append(found, fmt.Sprintf("%s: разбивка решений не читается — %s", ledgerFile, u))
		}
		for key, line := range b.SignTotalsLine {
			if !knownFate(key) {
				found = append(found, fmt.Sprintf("%s:%d: в разбивке решений исход %q вне словаря %v",
					ledgerFile, line, key, identityProbeFates))
			}
		}
		for _, fate := range identityProbeFates {
			written, ok := b.SignTotals[fate]
			switch {
			case !ok:
				found = append(found, fmt.Sprintf("%s: в разбивке решений нет строки исхода %q — ноль тоже "+
					"пишется числом", ledgerFile, fate))
			case written != c.ByFate[fate]:
				found = append(found, fmt.Sprintf("%s:%d: разбивка решений пишет «%s %d», а строк с этим "+
					"исходом %d", ledgerFile, b.SignTotalsLine[fate], fate, written, c.ByFate[fate]))
			}
		}
		switch {
		case b.SignTotal < 0:
			found = append(found, fmt.Sprintf("%s: в разбивке решений нет итоговой строки %q", ledgerFile,
				identityProbeFateTotalRow))
		case b.SignTotal != want:
			found = append(found, fmt.Sprintf("%s: итог разбивки решений %d, а гейтов признака %d",
				ledgerFile, b.SignTotal, want))
		}
	}
	sort.Strings(found)
	return found, c
}

// pinnedAliases — псевдонимы пиненных модулей в порядке печати.
func pinnedAliases() []string {
	m := identityPinnedModules
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Б. Распознаватель, формы пробы и суд над пробами вне ведомости.

// vendorASCIILetter — буква латиницы любого регистра.
func vendorASCIILetter(b byte) bool { return vendorLowerLetter(b) || vendorUpperLetter(b) }

// identityBrandBoundedIn — текст несёт бренд поставщика отдельным словом, и это
// не путь импорта библиотеки под тем же именем.
func identityBrandBoundedIn(text string) bool {
	lower := vendorASCIILower(text)
	for idx := 0; idx < len(lower); {
		k := strings.Index(lower[idx:], identityVendorBrand)
		if k < 0 {
			return false
		}
		at := idx + k
		end := at + len(identityVendorBrand)
		idx = at + 1
		if at > 0 && vendorASCIILetter(text[at-1]) {
			continue
		}
		if end < len(text) && vendorLowerLetter(text[end]) {
			continue
		}
		if strings.HasSuffix(lower[:at], identityVendorLibraryPrefix) && end < len(text) && text[end] == '/' {
			continue
		}
		return true
	}
	return false
}

// identityVendorNamedIn — строка называет снятого поставщика.
func identityVendorNamedIn(line string) bool {
	return vendorMarkBoundedIn(line) || identityBrandBoundedIn(line)
}

// Формы файла пробы — в порядке, в котором перепись их называет.
const (
	identityFormGoTest   = "*_test.go"
	identityFormTSTest   = "*.test.ts(x)/*.test.mjs"
	identityFormSpec     = "*.spec.ts"
	identityFormDeploy   = "deploy/tests/"
	identityFormTests    = "tests/"
	identityFormTestdata = "…/testdata/"
	identityFormSrcTest  = "…/src/test/"
	identityFormScript   = "*-test.sh/*_test.sh/*.bats/*_test.py/test_*.py"
)

// identityBeyondForms — формы в порядке печати.
var identityBeyondForms = []string{identityFormGoTest, identityFormTSTest, identityFormSpec, identityFormDeploy,
	identityFormTests, identityFormTestdata, identityFormSrcTest, identityFormScript}

// identityBeyondForm — форма пробы пути от корня; пусто — не проба.
func identityBeyondForm(rel string) string {
	lower := vendorASCIILower(rel)
	if strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".mdx") {
		return ""
	}
	base := rel[strings.LastIndex(rel, "/")+1:]
	switch {
	case strings.HasSuffix(rel, "_test.go"):
		return identityFormGoTest
	case strings.HasSuffix(rel, ".test.ts"), strings.HasSuffix(rel, ".test.tsx"), strings.HasSuffix(rel, ".test.mjs"):
		return identityFormTSTest
	case strings.HasSuffix(rel, ".spec.ts"):
		return identityFormSpec
	case strings.HasPrefix(rel, "deploy/tests/"):
		return identityFormDeploy
	case strings.HasPrefix(rel, "tests/"):
		return identityFormTests
	case strings.Contains(rel, "/testdata/"):
		return identityFormTestdata
	case strings.Contains(rel, "/src/test/"):
		return identityFormSrcTest
	case strings.HasSuffix(rel, "-test.sh"), strings.HasSuffix(rel, "_test.sh"), strings.HasSuffix(rel, ".bats"),
		strings.HasSuffix(rel, "_test.py"), strings.HasPrefix(base, "test_") && strings.HasSuffix(rel, ".py"):
		return identityFormScript
	}
	return ""
}

// identityLedgerProbe — путь — проба самой ведомости (`<каталог>/identity_*_test.go`).
func identityLedgerProbe(rel, probeDir string) bool {
	name := strings.TrimPrefix(rel, probeDir+"/")
	return name != rel && !strings.Contains(name, "/") && identityProbeName.MatchString(name)
}

// identityBeyondFacts — дерево, против которого судится запись Б.
type identityBeyondFacts struct {
	// Tracked — пути индекса git.
	Tracked map[string]bool
	// Named — популяция: проба вне ведомости → номера строк, называющих поставщика.
	Named map[string][]int
	// Text — строки файлов популяции.
	Text map[string][]string
	// ByForm — прочитано файлов каждой формы; Read — всего.
	ByForm map[string]int
	Read   int
}

// identityBeyondFactsOf — факты из текстов файлов формы пробы. Отделено от
// чтения диска ради инъекции: вход — путь → текст, и популяция выводится тем же
// распознавателем, что в дереве.
func identityBeyondFactsOf(tracked map[string]bool, texts map[string]string, probeDir string) identityBeyondFacts {
	f := identityBeyondFacts{Tracked: tracked, Named: map[string][]int{}, Text: map[string][]string{},
		ByForm: map[string]int{}}
	paths := make([]string, 0, len(texts))
	for p := range texts {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		form := identityBeyondForm(p)
		if form == "" || identityLedgerProbe(p, probeDir) {
			continue
		}
		f.ByForm[form]++
		f.Read++
		lines := identityProbeLinesOf(texts[p])
		for i, l := range lines {
			if identityVendorNamedIn(l) {
				f.Named[p] = append(f.Named[p], i+1)
			}
		}
		if len(f.Named[p]) > 0 {
			f.Text[p] = lines
		}
	}
	return f
}

// identityProbeLinesOf — строки текста, как их нумерует редактор: завершающий
// перевод строки новой строки не открывает.
func identityProbeLinesOf(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// identityBeyondCensus — объём осмотренного.
type identityBeyondCensus struct {
	Rows, Population, Read, Fragments int
	ByForm                            map[string]int
	ByCell                            map[identityBeyondKey]int
}

// identityBeyondCells — законные клетки «исход · исполнение» в порядке печати.
var identityBeyondCells = []identityBeyondKey{
	{identityFateRemove, identityBeyondDone}, {identityFateRemove, identityBeyondPending},
	{identityFateRewrite, identityBeyondDone}, {identityFateRewrite, identityBeyondPending},
	{identityFateKeep, identityNoValue},
}

// String — перепись одной строкой.
func (c identityBeyondCensus) String() string {
	parts := []string{fmt.Sprintf("файлов формы пробы прочитано %d", c.Read)}
	for _, form := range identityBeyondForms {
		parts = append(parts, fmt.Sprintf("%s %d", form, c.ByForm[form]))
	}
	parts = append(parts, fmt.Sprintf("называют поставщика %d · строк записи %d · фрагментов сверено %d",
		c.Population, c.Rows, c.Fragments))
	for _, k := range identityBeyondCells {
		parts = append(parts, fmt.Sprintf("%s/%s %d", k.Fate, k.Done, c.ByCell[k]))
	}
	return strings.Join(parts, " · ")
}

// legalBeyondCell — клетка «исход · исполнение» из законных.
func legalBeyondCell(k identityBeyondKey) bool {
	for _, c := range identityBeyondCells {
		if c == k {
			return true
		}
	}
	return false
}

// judgeIdentityBeyond — шесть требований записи Б.
func judgeIdentityBeyond(l identityProbeFateLedger, f identityBeyondFacts, probeDir, ledgerFile string) ([]string, identityBeyondCensus) {
	c := identityBeyondCensus{Population: len(f.Named), Read: f.Read, ByForm: f.ByForm,
		ByCell: map[identityBeyondKey]int{}}
	b := l.Beyond
	var found []string
	switch {
	case b.BeyondTables == 0:
		found = append(found, fmt.Sprintf("%s: судьба проб вне ведомости, называющих поставщика, не "+
			"записана — таблицы «| %s |» в документе нет, а таких проб в индексе %d", ledgerFile,
			strings.Join(identityBeyondHeader, " | "), len(f.Named)))
	case b.BeyondTables > 1:
		return []string{fmt.Sprintf("%s: таблица проб вне ведомости стоит %d раз", ledgerFile, b.BeyondTables)}, c
	}
	at := func(r identityBeyondRow) string { return fmt.Sprintf("%s:%d", ledgerFile, r.Line) }
	named := map[string]int{}
	for _, r := range b.Beyond {
		if r.Malformed {
			found = append(found, fmt.Sprintf("%s: строка пробы вне ведомости несёт не %d ячеек", at(r),
				len(identityBeyondHeader)))
			continue
		}
		switch {
		case !identityBeyondPathForm.MatchString(r.Path):
			found = append(found, fmt.Sprintf("%s: ячейка пробы %q — не один путь от корня. Групповая "+
				"запись прячет за признаком пробы, судьбу которых поимённо не назвал никто", at(r), r.Path))
			continue
		case identityBeyondForm(r.Path) == "":
			found = append(found, fmt.Sprintf("%s: %s — не проба ни одной из форм %v", at(r), r.Path,
				identityBeyondForms))
			continue
		case identityLedgerProbe(r.Path, probeDir):
			found = append(found, fmt.Sprintf("%s: %s — проба самой ведомости: её судьба пишется строкой "+
				"ведомости, второй записи о ней не заводится", at(r), r.Path))
			continue
		}
		if prev, dup := named[r.Path]; dup {
			found = append(found, fmt.Sprintf("%s: %s назван второй раз (первый — строка %d)", at(r), r.Path, prev))
			continue
		}
		named[r.Path] = r.Line
		c.Rows++

		key := identityBeyondKey{Fate: r.Fate, Done: r.Done}
		if !knownFate(r.Fate) {
			found = append(found, fmt.Sprintf("%s: %s — исход %q вне закрытого словаря %v", at(r), r.Path,
				r.Fate, identityProbeFates))
			continue
		}
		if !legalBeyondCell(key) {
			found = append(found, fmt.Sprintf("%s: %s — исполнение %q при исходе %q: у «%s» исполнения нет "+
				"(«%s»), у «%s» и «%s» оно «%s» либо «%s»", at(r), r.Path, r.Done, r.Fate, identityFateKeep,
				identityNoValue, identityFateRemove, identityFateRewrite, identityBeyondDone, identityBeyondPending))
			continue
		}
		c.ByCell[key]++

		tracked := f.Tracked[r.Path]
		lines, isNamed := f.Named[r.Path]
		switch {
		case key.Fate == identityFateRemove && key.Done == identityBeyondDone && tracked:
			found = append(found, fmt.Sprintf("%s: %s записана снятой, а в индексе есть — снимите её вместе с "+
				"предметом либо запишите «%s»", at(r), r.Path, identityBeyondPending))
		case (key.Fate != identityFateRemove || key.Done != identityBeyondDone) && !tracked:
			found = append(found, fmt.Sprintf("%s: %s записана живой (%s/%s), а в индексе её нет — снята? "+
				"запишите «%s/%s» и изменение", at(r), r.Path, r.Fate, r.Done, identityFateRemove, identityBeyondDone))
		case key.Fate == identityFateRewrite && key.Done == identityBeyondPending && !isNamed:
			found = append(found, fmt.Sprintf("%s: %s ждёт переписи, а поставщика больше не называет — "+
				"переписана? запишите «%s»", at(r), r.Path, identityBeyondDone))
		}

		switch {
		case isNamed && !r.HasFragment:
			found = append(found, fmt.Sprintf("%s: %s называет поставщика (строка %d), а фрагмент имени не "+
				"записан — строка обязана назвать, ЧЕМ проба его называет", at(r), r.Path, lines[0]))
		case isNamed:
			hit := false
			for _, n := range lines {
				hit = hit || strings.Contains(f.Text[r.Path][n-1], r.Fragment)
			}
			if hit {
				c.Fragments++
			} else {
				found = append(found, fmt.Sprintf("%s: %s — фрагмента «%s» нет ни на одной строке, называющей "+
					"поставщика (%s): строка описывает уже другое содержимое, перечитайте пробу",
					at(r), r.Path, r.Fragment, lineList(lines)))
			}
		case r.HasFragment:
			found = append(found, fmt.Sprintf("%s: %s поставщика не называет (или снята), а фрагмент «%s» "+
				"записан — пишите «%s»", at(r), r.Path, r.Fragment, identityNoValue))
		}

		if key.Fate != identityFateKeep && !identityTaskRef.MatchString(r.Why) {
			found = append(found, fmt.Sprintf("%s: %s — довод не называет задачу (`#<номер>`), которая исход "+
				"исполнила или исполнит", at(r), r.Path))
		}
		if key.Fate == identityFateKeep && r.Why == "" {
			found = append(found, fmt.Sprintf("%s: %s — довод пуст", at(r), r.Path))
		}
	}

	var missing []string
	for p := range f.Named {
		if _, ok := named[p]; !ok {
			missing = append(missing, p)
		}
	}
	sort.Strings(missing)
	for _, p := range missing {
		found = append(found, fmt.Sprintf("%s называет поставщика (%s), а строки о нём в %s нет — судьба "+
			"пробы не названа: прочитайте её и назовите исход с доводом", p, lineList(f.Named[p]), ledgerFile))
	}

	if b.BeyondTables == 1 {
		switch {
		case b.BeyondTotalsTables == 0:
			found = append(found, fmt.Sprintf("%s: разбивки проб вне ведомости («| %s |») нет", ledgerFile,
				strings.Join(identityBeyondTotals, " | ")))
		case b.BeyondTotalsTables > 1:
			found = append(found, fmt.Sprintf("%s: разбивка проб вне ведомости стоит %d раз", ledgerFile,
				b.BeyondTotalsTables))
		default:
			for _, u := range b.BeyondUnreadable {
				found = append(found, fmt.Sprintf("%s: разбивка проб вне ведомости не читается — %s", ledgerFile, u))
			}
			for key, line := range b.BeyondTotalsLine {
				if !legalBeyondCell(key) {
					found = append(found, fmt.Sprintf("%s:%d: в разбивке клетка «%s · %s» вне законных",
						ledgerFile, line, key.Fate, key.Done))
				}
			}
			for _, key := range identityBeyondCells {
				written, ok := b.BeyondTotals[key]
				switch {
				case !ok:
					found = append(found, fmt.Sprintf("%s: в разбивке проб вне ведомости нет клетки «%s · %s» "+
						"— ноль тоже пишется числом", ledgerFile, key.Fate, key.Done))
				case written != c.ByCell[key]:
					found = append(found, fmt.Sprintf("%s:%d: разбивка пишет «%s · %s — %d», а строк с этим "+
						"исходом %d", ledgerFile, b.BeyondTotalsLine[key], key.Fate, key.Done, written, c.ByCell[key]))
				}
			}
			switch {
			case b.BeyondTotal < 0:
				found = append(found, fmt.Sprintf("%s: в разбивке проб вне ведомости нет итоговой строки", ledgerFile))
			case b.BeyondTotal != c.Rows:
				found = append(found, fmt.Sprintf("%s: итог разбивки проб вне ведомости %d, а строк %d",
					ledgerFile, b.BeyondTotal, c.Rows))
			}
		}
	}
	sort.Strings(found)
	return found, c
}

// lineList — номера строк словами находки.
func lineList(lines []int) string {
	parts := make([]string, len(lines))
	for i, n := range lines {
		parts[i] = ":" + strconv.Itoa(n)
	}
	if len(parts) > identityProbeAnchorWhereCap {
		return "строки " + strings.Join(parts[:identityProbeAnchorWhereCap], ", ") +
			fmt.Sprintf(" и ещё %d", len(parts)-identityProbeAnchorWhereCap)
	}
	return "строки " + strings.Join(parts, ", ")
}
