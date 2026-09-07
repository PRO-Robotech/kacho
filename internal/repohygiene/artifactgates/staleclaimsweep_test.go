// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// staleclaimsweep_test.go — гейт: утверждение о механизме, чей предмет опровергнут
// деревом, — находка; и радиус такого утверждения берётся ПО ИМЕНИ механизма, а не
// по диффу.
//
// # Предмет
//
// Изменение, которое УДАЛЯЕТ (список, вычитание, флаг, поле, ветку классификации),
// имеет дифф систематически УЖЕ своего радиуса: дифф показывает место удаления и
// НИ ОДНОГО места, которое на удаляемое ссылалось. Поэтому радиус берётся именем.
//
// Оба предмета этого гейта — реальные, и у каждого своя история, снятая командой:
//
//  1. Классификация отказа в правах от владельца модели. Проза шести файлов nlb
//     описывала её как временную и приписывала блокировке партиции конечный срок.
//     Первая половина была ВЕРНА в день написания (f9574de6, 2026-07-26) и
//     опровергнута НАЗАВТРА (feab3f34, 2026-07-27: отказ стал терминальным).
//     Вторая половина была ЛОЖНА С РОЖДЕНИЯ: удержание временной строки ниже
//     порога отравления (558ed5e3, 2026-07-15) старше прозы на одиннадцать дней,
//     а значит временная строка до конечного срока не доходит вовсе — прежний
//     исход был НЕОГРАНИЧЕННЫМ заклиниванием. Рассказ был неверен в обе стороны.
//
//  2. Вычитание «известного красного» из общего вердикта. Снято 06b8fc8d
//     (2026-07-30); follow-up 19973fa7 вычистил ДВУХ потребителей и пропустил
//     третьего — `deploy/scripts/newman-e2e.sh`, лежащий в ТОМ ЖЕ каталоге, что и
//     вычищенный близнец. Проверяется командой:
//     `git show --name-only 19973fa7 | grep -c newman-e2e.sh` → 0.
//
// # Почему свойство, а не две правки
//
// Правка шести файлов закрывает экземпляр. Следующее удаление оставит такой же
// хвост, и заметить его будет снова некому: устаревшее объявление ничем не
// отличается от верного, пока не спросишь дерево ИМЕНЕМ механизма.
//
// # Что здесь считается доказательством
//
// Реестр `staleClaimMarkers` — данные: имя, предикат, регистр, ОБЛАСТЬ, число
// вхождений на момент посадки, зачем запись и условие её снятия. Расхождение
// счёта с `atLanding` — НАХОДКА, а не повод обновить число: «починил N из N»
// проверяемо только сверкой двух чисел.
//
// # Два слоя: узкий реестр и широкая калибровка
//
// Реестр ищет ФОРМУ УТВЕРЖДЕНИЯ. Одного его мало: утверждение, сформулированное
// иначе, мимо реестра пройдёт, и молчание гейта прочтётся как чистота. Поэтому
// рядом стоит НАДпредикат (`calibrationHit`: обе сущности — отказ и временность —
// в одной строке), а всё, что он ловит законно, перечислено В ЯВНУЮ
// (`legitimateCalibrationHits`). Совпадение калибровки, не покрытое ни реестром,
// ни перечнем законных, роняет гейт НА ПРЕДПОСЫЛКЕ — «реестр не покрывает
// известное вхождение», — а не оставляет его молчать.
//
// # Известный предел, объявленный, а не скрытый
//
// Гейт видит утверждение, уместившееся в ОДНОЙ строке. Разнесённое переносом
// («…treats\n// PermissionDenied as transient») он не увидит — ни реестром, ни
// калибровкой. Это floor, а не ceiling: молчание гейта чистоты не доказывает.
// Второй предел — `censusSelfReference`: сам реестр и артефакт переписи цитируют
// маркеры по своему назначению и из счёта исключены; у исключения есть проверка
// предмета (см. TestStaleClaimSelfReferenceStillHasSubject).
package artifactgates

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"regexp/syntax"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

// staleClaimMarker — маркер как ДАННЫЕ.
//
// `atLanding` снимается СВОЕЙ командой против названной ревизии и лежит рядом с
// предикатом, а не в чужом отчёте. `retireWhen` привязывается к ВНЕШНЕМУ факту —
// не к состоянию тикета и не к тому, что видно рядом с самой правкой: предикат
// снятия, который делает истинным собственное изменение, самоистечения не даёт.
type staleClaimMarker struct {
	name       string
	pattern    string   // исходник регулярного выражения; регистр задаётся caseFold
	caseFold   bool     // регистронезависимо: «KNOWN-RED» не должен пройти мимо «known-RED»
	scope      []string // префиксы путей; nil — всё дерево
	atLanding  int      // число СТРОК на момент посадки
	why        string
	retireWhen string
}

// staleClaimMarkers — реестр. Числа сняты против
// bb26d90580a9cf92807a8656d94904a0b7a91c0a (вершина redesign/integration на момент
// ветвления agent/xc3-s1-f3); единица счёта — СТРОКИ.
var staleClaimMarkers = []staleClaimMarker{
	{
		name:      "refuted-refusal-is-transient",
		pattern:   `(treats|classifies|considers|marks|deems|regards)\s+PermissionDenied\s+as\s+(a\s+)?transient`,
		caseFold:  true,
		atLanding: 0,
		why: "Утверждение опровергнуто деревом на двух уровнях: применитель nlb относит " +
			"отказ в правах к терминальному классу, и то же делает общая библиотека дренажа " +
			"для КАЖДОГО применителя. До правки — 7 строк в 6 файлах.",
		retireWhen: "TestStaleClaimPremiseHolds перестанет держаться, то есть отказ в правах " +
			"снова начнёт классифицироваться как повторяемый. Тогда запрет станет вредным, " +
			"и снимать его нужно вместе с ним, а не молча.",
	},
	{
		name:      "refuted-partition-unblock-deadline",
		pattern:   `≈ ?150 ?s`,
		caseFold:  true,
		atLanding: 0,
		why: "Конечный срок разблокировки партиции приписан состоянию, которое до него не " +
			"доходит: строка, помеченная повторяемой, удерживается ниже порога отравления и " +
			"блокирует свою партицию НЕОГРАНИЧЕННО, а строка, помеченная терминальной, " +
			"исключается из блокирующего набора сразу. До правки — 6 строк в 6 файлах.",
		retireWhen: "TestStaleClaimPremiseHolds перестанет держаться: удержание ниже порога " +
			"исчезнет из библиотеки дренажа, и конечный срок снова станет описанием исхода.",
	},
	{
		name:      "removed-subtraction-claimed-live",
		pattern:   `whitelist`,
		caseFold:  true,
		scope:     []string{"deploy/scripts/newman-e2e.sh"},
		atLanding: 0,
		why: "Третий потребитель снятого вычитания. Файл утверждал, что вердикт CI считается " +
			"с поправкой на список исключений, и что сырые отказы этим списком покрыты. " +
			"Списка нет с 06b8fc8d. Совет «читайте сырые числа» верен и сохранён — заменена " +
			"ПРЕДПОСЫЛКА, а не рекомендация. До правки — 4 строки.",
		retireWhen: "вычитание вернётся в общий гейт как исполняемый код — это проверяет " +
			"TestStaleClaimPremiseHolds; тогда утверждения снова станут верными.",
	},
	{
		name:      "removed-subtraction-mentioned-historically",
		pattern:   `known-RED`,
		caseFold:  true,
		scope:     []string{"deploy/scripts/newman-parallel.sh"},
		atLanding: 1,
		why: "Перепись, а не находка: вычищенный близнец третьего потребителя несёт ОДНО " +
			"упоминание, и оно историческое («вычитание было снято 2026-07-30»). Число " +
			"пиновано, чтобы отрастание упоминаний в этом файле стало заметным.",
		retireWhen: "историческая врезка будет удалена целиком вместе с памятью о механизме — " +
			"тогда число меняется на 0 осознанно, а не потому, что счёт разъехался.",
	},
	{
		name:      "pre-whitelist",
		pattern:   `pre-whitelist`,
		caseFold:  true,
		atLanding: 0,
		why: "ОБЪЯВЛЕННЫЙ ОТРИЦАТЕЛЬНЫЙ РЕЗУЛЬТАТ. Маркер входил в предикат прежней редакции " +
			"нормативного текста и не имеет в дереве ни одного вхождения. Запись остаётся " +
			"вместе с нулём: «ноль найденного» обязано быть отличимо от «не искали».",
		retireWhen: "не снимать по факту нуля — ноль и есть содержание записи. Снятие " +
			"допустимо только вместе с решением, что механизм больше не разыскивается.",
	},
}

// censusSelfReference — ПРЕФИКСЫ путей, цитирующих маркеры ПО СВОЕМУ НАЗНАЧЕНИЮ.
//
// Реестр обязан называть то, что ищет, а артефакт переписи — приводить найденное
// ДОСЛОВНО, включая сохранённый вывод красного прогона. Оба поэтому содержат
// маркеры, и оба из счёта исключены.
//
// Что это стоило измерить, а не предположить: сохранённый вывод красного прогона,
// закоммиченный рядом с гейтом, УДВОИЛ каждое число (7→14, 6→12) и породил
// вхождение маркера, у которого их не было (0→1) — то есть артефакт переписи
// начал сам себя считать. Померено на ce34fe20, не выведено рассуждением.
//
// Исключение не бесплатное: у каждого префикса проверяется предмет
// (TestStaleClaimSelfReferenceStillHasSubject) — префикс, под которым больше
// никто не цитирует маркеры, обязан уйти, иначе он останется слепой зоной.
// Предел объявлен: всё, что положат под эти префиксы, гейт не читает.
var censusSelfReference = []string{
	"internal/repohygiene/artifactgates/staleclaimsweep_test.go",
	"docs/plans/xc-3/",
}

// selfReferenced — путь попадает под один из префиксов самоцитирования.
func selfReferenced(rel string) bool {
	for _, p := range censusSelfReference {
		if rel == p || strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

// legitimateCalibrationHits — совпадения НАДпредиката, которые предметом не
// являются.
//
// Ключуется файлом и отличительной подстрокой строки, а НЕ номером строки: номер
// сдвигает любая правка выше по файлу, и перечень начал бы «истекать» от
// косметики. Все три записи — утверждения ПРОТИВОПОЛОЖНОГО: временный сбой НЕ
// должен становиться отказом в правах. Они и есть отрицательный контроль
// предиката, снятый с реального дерева, а не синтетический.
var legitimateCalibrationHits = []struct{ file, contains string }{
	{
		file:     "services/iam/internal/apps/kaname/api/access_binding/get_error_mapping_test.go",
		contains: "must map to a retriable code, not PermissionDenied",
	},
	{
		file:     "services/iam/internal/apps/kaname/api/access_binding/get_error_mapping_test.go",
		contains: "must map to a retriable/internal code, not PermissionDenied",
	},
	{
		file:     "services/iam/internal/authzguard/scope.go",
		contains: "must not become a terminal PermissionDenied",
	},
}

// calibrationAnchor — обязательная половина НАДпредиката. Вынесена в выражение,
// чтобы поиск шёл по ФАЙЛУ целиком (одно обращение к движку вместо одного на
// строку), а вторая половина проверялась уже на найденной строке.
var (
	calibrationAnchor        = regexp.MustCompile(`(?i)permissiondenied`)
	calibrationAnchorLiteral = requiredLiteral(`(?i)permissiondenied`)
)

// calibrationHit — НАДпредикат реестра: отказ и временность названы в одной
// строке, в любой форме. Он заведомо шире реестра и ловит в том числе законное —
// именно поэтому законное перечислено явно, а не отфильтровано выражением.
func calibrationHit(line string) bool {
	low := strings.ToLower(line)
	return strings.Contains(low, "permissiondenied") && strings.Contains(low, "transient")
}

// textFile — прочитанный файл индекса, пригодный для текстового осмотра.
//
// Хранится ТЕЛО целиком плюс смещения начал строк. Предикаты применяются к телу
// одним обращением к движку регулярных выражений, а не по строке на файл на
// маркер: замер под детектором гонок дал 143 с на построчном обходе против
// секунд на пофайловом, при 5806 файлах и пяти маркерах. Семантика при этом
// остаётся СТРОЧНОЙ — совпадение, внутри которого есть перевод строки,
// отбрасывается (matchesByLine), поэтому объявленный предел «утверждение,
// разнесённое переносом, не ловится» сохраняется в точности.
type textFile struct {
	rel        string
	body       string
	lowerBody  string // то же тело в нижнем регистре — только для отсева файлов
	lineStarts []int  // смещение начала каждой строки, отсортировано
}

// line — текст строки по её индексу (с нуля), без завершающего перевода.
func (f textFile) line(i int) string {
	start := f.lineStarts[i]
	end := len(f.body)
	if i+1 < len(f.lineStarts) {
		end = f.lineStarts[i+1] - 1
	}
	return f.body[start:end]
}

// matchLine — совпавшая СТРОКА: номер (с единицы) и её текст.
type matchLine struct {
	num  int
	text string
}

// matchesByLine — СТРОКИ, в которых выражение совпало, по одной записи на строку.
//
// Семантика СТРОЧНАЯ: выражение применяется к строке, поэтому утверждение,
// разнесённое переносом, не ловится — это объявленный предел гейта, а не
// побочный эффект реализации.
//
// `anchor` — литерал в нижнем регистре, БЕЗ которого выражение совпасть не может.
// Он не задаётся рукой, а ВЫВОДИТСЯ из самого выражения (requiredLiteral), поэтому
// отсев не может незаметно сузить предикат: если вывести нечего, anchor пуст и
// отсева нет. Замер, ради которого это сделано: построчное применение пяти
// выражений ко всему дереву под детектором гонок — 143 с, пофайловое через
// FindAllStringIndex — 211 с, отсев по выведенному литералу — секунды. Числа
// вердикта при всех трёх реализациях совпали.
func (f textFile) matchesByLine(re *regexp.Regexp, anchor string) []matchLine {
	if anchor != "" && !strings.Contains(f.lowerBody, anchor) {
		return nil
	}
	var out []matchLine
	for i := range f.lineStarts {
		text := f.line(i)
		if re.MatchString(text) {
			out = append(out, matchLine{num: i + 1, text: text})
		}
	}
	return out
}

// requiredLiteral — литерал, который ОБЯЗАН присутствовать, чтобы выражение
// совпало. Выводится разбором самого выражения: спуск идёт только через
// конкатенацию и группировку, то есть через узлы, каждый потомок которых
// обязателен. В альтернативу, повторение и «ноль или один» разбор НЕ заходит —
// их содержимое обязательным не является.
//
// Пусто — законный ответ: у выражения без обязательного литерала отсева нет, и
// оно применяется ко всем строкам.
func requiredLiteral(pattern string) string {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return ""
	}
	re = re.Simplify()
	best := ""
	var walk func(r *syntax.Regexp)
	walk = func(r *syntax.Regexp) {
		switch r.Op {
		case syntax.OpLiteral:
			if s := string(r.Rune); len(s) > len(best) {
				best = s
			}
		case syntax.OpConcat, syntax.OpCapture:
			for _, sub := range r.Sub {
				walk(sub)
			}
		}
	}
	walk(re)
	return strings.ToLower(best)
}

// censusResult — итог одного обхода дерева.
type censusResult struct {
	files                      []textFile
	skippedBinary, skippedSelf int
	err                        error
}

var (
	censusOnce sync.Once
	censusMemo censusResult
)

// staleClaimCensus читает ДЕРЕВО (индекс git, не диск: рабочие копии агентов под
// .claude/worktrees в репозиторий не входят) и возвращает текстовые файлы вместе
// с числом прочитанного. Двоичное и самоцитирующее исключается здесь, один раз.
//
// Обход ЗАПОМИНАЕТСЯ на пакет: четыре здешние проверки спрашивают одно и то же
// неизменное дерево, и три лишних обхода стоили ровно втрое (замер под
// детектором гонок: 143 с против ~50 с). Память не делает вывод менее
// проверяемым — дерево во время прогона не меняется, а инъекции ставятся ДО
// запуска процесса, не внутри него.
func staleClaimCensus(t *testing.T, root string) (files []textFile, skippedBinary, skippedSelf int) {
	t.Helper()
	censusOnce.Do(func() { censusMemo = buildStaleClaimCensus(t, root) })
	if censusMemo.err != nil {
		t.Fatalf("перепись: %v — обход обязан прочитать всё, что назвал", censusMemo.err)
	}
	return censusMemo.files, censusMemo.skippedBinary, censusMemo.skippedSelf
}

func buildStaleClaimCensus(t *testing.T, root string) censusResult {
	t.Helper()
	var res censusResult
	tt := newTrackedTree(t, root)
	rels := make([]string, 0, tt.count())
	for rel := range tt.files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		if selfReferenced(rel) {
			res.skippedSelf++
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			// Файл индекса, которого нет на диске, — не «пропуск»: перепись
			// перестала бы относиться к названному дереву.
			res.err = fmt.Errorf("чтение %s: %w", rel, err)
			return res
		}
		if !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 {
			res.skippedBinary++
			continue
		}
		text := string(body)
		starts := []int{0}
		for i := 0; i < len(text); i++ {
			if text[i] == '\n' {
				starts = append(starts, i+1)
			}
		}
		res.files = append(res.files, textFile{
			rel: rel, body: text, lowerBody: strings.ToLower(text), lineStarts: starts,
		})
	}
	return res
}

// inScope — область маркера. nil означает всё дерево.
func (m staleClaimMarker) inScope(rel string) bool {
	if len(m.scope) == 0 {
		return true
	}
	for _, p := range m.scope {
		if strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

func (m staleClaimMarker) compile(t *testing.T) (*regexp.Regexp, string) {
	t.Helper()
	src := m.pattern
	if m.caseFold {
		src = "(?i)" + src
	}
	re, err := regexp.Compile(src)
	if err != nil {
		t.Fatalf("маркер %q: предикат не компилируется (%v) — реестр не ищет ничего", m.name, err)
	}
	return re, requiredLiteral(src)
}

// TestStaleClaimSweepIsTakenByName — перепись по имени механизма: числа сверяются
// с объявленными, «ноль находок» отличается от «ноль прочитанного».
func TestStaleClaimSweepIsTakenByName(t *testing.T) {
	root := repoRoot(t)

	if len(staleClaimMarkers) == 0 {
		t.Fatalf("реестр маркеров пуст — гейт не утверждает ничего, и его молчание " +
			"неотличимо от чистоты дерева")
	}

	files, skippedBinary, skippedSelf := staleClaimCensus(t, root)
	if len(files) == 0 {
		t.Fatalf("обход не прочитал НИ ОДНОГО текстового файла под %s — молчание гейта "+
			"в этом состоянии ничего не доказывает", root)
	}

	total := 0
	for _, m := range staleClaimMarkers {
		re, anchor := m.compile(t)
		var hits []string
		inScope := 0
		for _, f := range files {
			if !m.inScope(f.rel) {
				continue
			}
			inScope++
			for _, hit := range f.matchesByLine(re, anchor) {
				hits = append(hits,
					f.rel+":"+strconv.Itoa(hit.num)+": "+strings.TrimSpace(hit.text))
			}
		}
		total += len(hits)

		if inScope == 0 {
			t.Errorf("маркер %q: в его области не оказалось НИ ОДНОГО файла — область "+
				"устарела, и ноль вхождений здесь означает «не искали».\nОбласть: %v",
				m.name, m.scope)
			continue
		}
		if len(hits) != m.atLanding {
			t.Errorf("маркер %q: вхождений %d, на момент посадки было %d.\n"+
				"Расхождение — НАХОДКА, а не повод обновить число: перепись ведётся по имени "+
				"механизма, и «починил N из N» проверяемо только сверкой двух чисел.\n"+
				"Зачем эта запись: %s\nУсловие снятия: %s\nКоординаты (%d):\n  %s",
				m.name, len(hits), m.atLanding, m.why, m.retireWhen,
				len(hits), strings.Join(hits, "\n  "))
			continue
		}
		if m.atLanding == 0 {
			t.Logf("отрицательный результат объявлен: маркер %q — вхождений ноль (осмотрено "+
				"файлов в области: %d)", m.name, inScope)
		}
	}

	t.Logf("перепись: прочитано текстовых файлов %d (двоичных пропущено %d, "+
		"самоцитирующих %d); маркеров в реестре %d; вхождений найдено %d",
		len(files), skippedBinary, skippedSelf, len(staleClaimMarkers), total)
}

// TestStaleClaimPrefilterIsDerivedNotAssumed — отсев обязан следовать ИЗ предиката,
// а не стоять рядом с ним.
//
// Отсев по литералу — единственное место, где гейт может незаметно сузиться:
// литерал, выбранный рукой и НЕ обязательный для совпадения, тихо выбросил бы
// файлы, в которых предикат сработал бы. Поэтому литерал выводится разбором самого
// выражения, а здесь проверяется, что вывод (а) даёт подстроку самого выражения и
// (б) НЕ выводит литерал, обязательным не являющийся.
//
// Контроль двусторонний и на выражениях с известным ответом:
//   - обязательный литерал в конкатенации — выводится;
//   - тот же литерал внутри альтернативы / необязательной группы / повторения —
//     НЕ выводится (иначе отсев выбросил бы законные совпадения второй ветви).
func TestStaleClaimPrefilterIsDerivedNotAssumed(t *testing.T) {
	for _, tc := range []struct {
		name, pattern, want string
	}{
		{"обязательный литерал в конкатенации", `(?i)foo\s+BARBAZ\s+qux`, "barbaz"},
		{"литерал внутри альтернативы не обязателен", `(?i)(barbaz|zzz)`, ""},
		{"литерал под «ноль или один» не обязателен", `(?i)(barbaz)?x`, "x"},
		{"литерал под повторением не обязателен", `(?i)(barbaz)+`, ""},
	} {
		if got := requiredLiteral(tc.pattern); got != tc.want {
			t.Errorf("%s: из %q выведено %q, ожидалось %q — вывод обязательного литерала "+
				"сломан, и отсев по нему начнёт выбрасывать законные совпадения",
				tc.name, tc.pattern, got, tc.want)
		}
	}

	for _, m := range staleClaimMarkers {
		_, anchor := m.compile(t)
		if anchor == "" {
			t.Logf("маркер %q: обязательного литерала нет — отсева нет, предикат "+
				"применяется ко всем строкам", m.name)
			continue
		}
		if !strings.Contains(strings.ToLower(m.pattern), anchor) {
			t.Errorf("маркер %q: выведенный литерал %q не встречается в самом предикате %q — "+
				"вывод рассогласован с выражением", m.name, anchor, m.pattern)
			continue
		}
		t.Logf("маркер %q: отсев по выведенному литералу %q", m.name, anchor)
	}
}

// TestStaleClaimRosterCoversKnownOccurrences — предпосылка реестра.
//
// Без этой половины реестр становится местом, куда можно НЕ дописать: убери
// маркер — и вхождение в дереве останется, а гейт промолчит. Здесь стоит
// НАДпредикат, законные совпадения которого перечислены явно; всё, что он ловит
// сверх перечня и сверх реестра, роняет гейт на предпосылке.
func TestStaleClaimRosterCoversKnownOccurrences(t *testing.T) {
	root := repoRoot(t)
	files, _, _ := staleClaimCensus(t, root)
	if len(files) == 0 {
		t.Fatalf("обход не прочитал ни одного файла — предпосылку проверять не на чем")
	}

	res := make([]*regexp.Regexp, len(staleClaimMarkers))
	for i, m := range staleClaimMarkers {
		res[i], _ = m.compile(t)
	}

	var uncovered []string
	scanned := 0
	for _, f := range files {
		for _, anchor := range f.matchesByLine(calibrationAnchor, calibrationAnchorLiteral) {
			if !calibrationHit(anchor.text) {
				continue // якорь есть, второй половины нет — не совпадение
			}
			scanned++
			if legitimateCalibration(f.rel, anchor.text) {
				continue
			}
			covered := false
			for j, m := range staleClaimMarkers {
				if m.inScope(f.rel) && res[j].MatchString(anchor.text) {
					covered = true
					break
				}
			}
			if !covered {
				uncovered = append(uncovered,
					f.rel+":"+strconv.Itoa(anchor.num)+": "+strings.TrimSpace(anchor.text))
			}
		}
	}

	if len(uncovered) > 0 {
		t.Errorf("реестр не покрывает %d известных вхождений НАДпредиката (отказ и "+
			"временность в одной строке):\n  %s\n\n"+
			"Это отказ НА ПРЕДПОСЫЛКЕ, а не находка в дереве: либо строка законна и её "+
			"место в legitimateCalibrationHits (с отличительной подстрокой, не с номером "+
			"строки), либо она — предмет, и в реестре обязан быть маркер, который её ловит. "+
			"Молчание при снятом маркере — ровно тот исход, ради которого эта проверка "+
			"стоит рядом.", len(uncovered), strings.Join(uncovered, "\n  "))
	}
	t.Logf("предпосылка: совпадений НАДпредиката %d, из них законных перечислено %d, "+
		"непокрытых %d", scanned, len(legitimateCalibrationHits), len(uncovered))
}

func legitimateCalibration(rel, line string) bool {
	for _, l := range legitimateCalibrationHits {
		if l.file == rel && strings.Contains(line, l.contains) {
			return true
		}
	}
	return false
}

// TestStaleClaimLegitimateHitsStillHaveSubject — перечень законного обязан
// истекать сам.
//
// Запись, которой больше нечего исключать, — находка: её унаследует следующий как
// слепую зону надпредиката. Предикат снятия привязан к ВНЕШНЕМУ факту (строка
// живёт в чужом файле, который эта работа не трогает), а не к тому, что видно
// рядом с правкой.
func TestStaleClaimLegitimateHitsStillHaveSubject(t *testing.T) {
	root := repoRoot(t)
	files, _, _ := staleClaimCensus(t, root)

	byPath := map[string]textFile{}
	for _, f := range files {
		byPath[f.rel] = f
	}
	for _, l := range legitimateCalibrationHits {
		f, ok := byPath[l.file]
		if !ok {
			t.Errorf("перечень законного: файла %s в дереве нет — запись пережила свой "+
				"предмет, удали её", l.file)
			continue
		}
		found := false
		for _, anchor := range f.matchesByLine(calibrationAnchor, calibrationAnchorLiteral) {
			if calibrationHit(anchor.text) && strings.Contains(anchor.text, l.contains) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("перечень законного: в %s больше нет строки с подстрокой %q, "+
				"попадающей под НАДпредикат — запись исключает пустоту и обязана уйти, "+
				"иначе она станет слепой зоной для следующего вхождения", l.file, l.contains)
		}
	}

	// Перепись: сколько записей перечня рассмотрено. «Все с предметом» на пустом
	// перечне — утверждение ни о чём.
	t.Logf("перепись: записей перечня законного рассмотрено %d, файлов осмотрено %d", len(legitimateCalibrationHits), len(files))
}

// TestStaleClaimSelfReferenceStillHasSubject — исключение самоцитирования обязано
// иметь предмет.
//
// Файл, переставший цитировать маркеры, из списка обязан уйти: иначе он остаётся
// вне переписи, и туда можно внести утверждение о механизме незамеченным.
func TestStaleClaimSelfReferenceStillHasSubject(t *testing.T) {
	root := repoRoot(t)
	res := make([]*regexp.Regexp, len(staleClaimMarkers))
	for i, m := range staleClaimMarkers {
		res[i], _ = m.compile(t)
	}
	tt := newTrackedTree(t, root)
	rels := make([]string, 0, tt.count())
	for rel := range tt.files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	for _, prefix := range censusSelfReference {
		covered, quoting := 0, 0
		for _, rel := range rels {
			if rel != prefix && !strings.HasPrefix(rel, prefix) {
				continue
			}
			covered++
			body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				continue
			}
			for _, re := range res {
				if re.Match(body) {
					quoting++
					break
				}
			}
		}
		if covered == 0 {
			t.Errorf("самоцитирование %q: под этим префиксом в индексе нет НИ ОДНОГО файла — "+
				"запись пережила свой предмет, удали её из censusSelfReference", prefix)
			continue
		}
		if quoting == 0 {
			t.Errorf("самоцитирование %q: ни один из %d файлов под ним не цитирует маркеры "+
				"реестра — исключать нечего. Удали запись, иначе поддерево останется вне "+
				"переписи и туда можно будет внести утверждение о механизме незамеченным.",
				prefix, covered)
			continue
		}
		t.Logf("самоцитирование %q: файлов под префиксом %d, из них цитирующих маркеры %d",
			prefix, covered, quoting)
	}
}

// TestStaleClaimPremiseHolds — запрет опирается на факты о дереве, и при их смене
// обязан падать САМ, а не продолжать требовать своё.
//
// Проза объявлена устаревшей ровно потому, что (а) отказ в правах от владельца
// модели относится к терминальному классу и у применителя nlb, и в общей
// библиотеке дренажа; (б) строка, помеченная повторяемой, удерживается НИЖЕ порога
// отравления, поэтому конечного срока разблокировки у неё нет вовсе; (в) вычитание
// из общего вердикта не исполняется. Перестанет держаться любое из трёх — запрет
// начнёт запрещать верное описание, и это обязано быть видно здесь.
//
// Разбор идёт по ДЕРЕВУ РАЗБОРА, а не поиском слова: те же имена стоят в прозе
// рядом с каждым из этих мест (комментарии объясняют выбор класса), и текстовый
// поиск зачёл бы объяснение за реализацию.
func TestStaleClaimPremiseHolds(t *testing.T) {
	root := repoRoot(t)

	t.Run("отказ_в_правах_терминален_у_применителя_nlb", func(t *testing.T) {
		fd := mustFunc(t, root, "services/nlb/internal/clients/iam/register_applier.go",
			"classifyRegisterErr")
		requireBodyMentions(t, fd, "codes", "PermissionDenied")
		requireBodyMentions(t, fd, "drainer", "ErrPermanent")
	})

	t.Run("отказ_в_правах_терминален_в_общей_библиотеке", func(t *testing.T) {
		fd := mustFunc(t, root, "pkg/outbox/drainer/classify.go", "isPermanentGRPC")
		requireBodyMentions(t, fd, "codes", "PermissionDenied")
	})

	t.Run("повторяемая_строка_удерживается_ниже_порога", func(t *testing.T) {
		fd := mustFunc(t, root, "pkg/outbox/drainer/internal.go", "markTransientFailure")
		if !bodyCapsBelowMaxAttempts(fd) {
			t.Fatalf("markTransientFailure больше не удерживает попытку ниже порога " +
				"(выражения вида MaxAttempts - 1 в теле нет). Тогда строка, помеченная " +
				"повторяемой, доходит до отравления, и КОНЕЧНЫЙ срок разблокировки партиции " +
				"снова становится верным описанием — а маркер " +
				"refuted-partition-unblock-deadline начинает запрещать правду.")
		}
	})

	t.Run("вычитание_из_вердикта_не_исполняется", func(t *testing.T) {
		const gate = "services/iam/tests/newman/scripts/assert-suites-green.sh"
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(gate)))
		if err != nil {
			t.Fatalf("чтение %s: %v — предпосылку проверять не на чем", gate, err)
		}
		var live []string
		for i, line := range strings.Split(string(body), "\n") {
			code := strings.TrimSpace(line)
			if code == "" || strings.HasPrefix(code, "#") {
				continue // цельная строка комментария — это проза, а не исполнение
			}
			low := strings.ToLower(code)
			if strings.Contains(low, "known-red") || strings.Contains(low, "whitelist") {
				live = append(live, gate+":"+strconv.Itoa(i+1)+": "+code)
			}
		}
		if len(live) > 0 {
			t.Fatalf("вычитание вернулось в ИСПОЛНЯЕМУЮ часть общего гейта:\n  %s\n\n"+
				"Тогда утверждения прогонщика о списке исключений снова верны, и маркер "+
				"removed-subtraction-claimed-live запрещает правду. Сначала решается, что "+
				"делает гейт, потом — что о нём написано.", strings.Join(live, "\n  "))
		}
		t.Logf("предпосылка: исполняемых упоминаний вычитания в общем гейте — 0 "+
			"(прочитано строк: %d)", len(strings.Split(string(body), "\n")))
	})
}

// mustFunc — разбирает файл и достаёт объявление функции по имени.
func mustFunc(t *testing.T, root, rel, name string) *ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("разбор %s: %v — предпосылка не проверена, а не «пройдена»", rel, err)
	}
	var out *ast.FuncDecl
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if ok && fd.Name.Name == name {
			out = fd
			break
		}
	}
	if out == nil {
		t.Fatalf("в %s нет функции %s — предпосылка запрета переехала или исчезла; "+
			"запрет обязан пересматриваться вместе с ней, а не молча продолжать действовать",
			rel, name)
	}
	return out
}

// requireBodyMentions — в теле функции есть селектор `pkg.Name`.
func requireBodyMentions(t *testing.T, fd *ast.FuncDecl, pkg, name string) {
	t.Helper()
	found := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != name {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == pkg {
			found = true
			return false
		}
		return true
	})
	if !found {
		t.Fatalf("в теле %s больше нет %s.%s — классификация отказа изменилась, и запрет "+
			"на прежнюю прозу обязан пересматриваться вместе с ней", fd.Name.Name, pkg, name)
	}
}

// bodyCapsBelowMaxAttempts — в теле есть выражение вида `….MaxAttempts - 1`.
func bodyCapsBelowMaxAttempts(fd *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		be, ok := n.(*ast.BinaryExpr)
		if !ok || be.Op != token.SUB {
			return true
		}
		sel, ok := be.X.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "MaxAttempts" {
			return true
		}
		if lit, ok := be.Y.(*ast.BasicLit); ok && lit.Kind == token.INT && lit.Value == "1" {
			found = true
			return false
		}
		return true
	})
	return found
}
