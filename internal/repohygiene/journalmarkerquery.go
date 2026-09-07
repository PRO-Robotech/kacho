// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// journalmarkerquery.go — разбор ИСПОЛНЯЕМЫХ запросов к журналу, у которого нет
// признака доставки.
//
// # Предмет
//
// Соседний `deliverymarker.go` отвечает на вопрос «объявляет ли схема признак
// доставки у этой таблицы». Он читает МИГРАЦИИ и ничего не знает о том, кто эту
// таблицу спрашивает. Поэтому после снятия колонки в дереве остаются запросы,
// которые её называют, — и они не краснеют ни у одной проверки: код собирается,
// шаблон рендерится, скрипт запускается.
//
// Отказ приходит только от базы (`42703: column … does not exist`) и приходит В
// РАНТАЙМЕ. Если такой запрос стоит под `|| true` — а именно так зовут фикстуры
// прогонов, — он МОЛЧА не делает ничего, и это неотличимо от исправной работы.
// Наблюдалось: гейт оседания очереди прав опрашивал `kaname.fga_outbox` по
// `sent_at IS NULL` после того, как колонку сняли вместе с дренажем; он
// отказывал на каждом прогоне, отказ проглатывался вызывающим, и волна проб шла
// без оседания, ради которого гейт написан (kacho#1049).
//
// Вторая половина цены — руководство ОПЕРАТОРА: команду оттуда человек
// исполняет в момент разбора аварии и получает отказ вместо ответа.
//
// # ЧТО СЧИТАЕТСЯ ИСПОЛНЯЕМЫМ — и почему проза сюда не входит
//
// Отличать запрос от РАССКАЗА о запросе несущее: правильная правка этого класса
// оставляет в дереве объяснение в прошедшем времени («прежняя редакция
// предлагала считать отставание запросом `WHERE sent_at IS NULL`»), и проверка,
// судящая по слову, покраснела бы на собственном объяснении. Поэтому носитель
// режется на фрагменты ПО СВОЕЙ ФОРМЕ, а прозаическая часть отбрасывается и
// печатается своим числом:
//
//	.go                    — строковые литералы (обратные кавычки и двойные);
//	                         комментарии не читаются вовсе;
//	.sh .bash .py .yaml    — строки, кроме целиком-комментариев;
//	.sql                   — операторы между `;`, без `--`-комментариев;
//	.md .mdx               — ТОЛЬКО содержимое огороженных блоков команд, и в
//	                         них тоже без `#`-комментариев оболочки.
//
// # ЧЕГО РАЗБОР НЕ ВИДИТ — названо, а не спрятано
//
//   - имя таблицы, собираемое в рантайме (`fmt.Sprintf("… FROM %s …")`): его
//     текста в дереве нет by construction;
//   - `#` внутри строкового литерала оболочки: комментарием считается только
//     строка, начинающаяся с `#`. Ошибка здесь идёт в сторону НАХОДКИ, а не
//     тишины, и это выбрано осознанно;
//   - словарь колонок ЗАКРЫТ признаком доставки ([DeliveryMarkerColumn]) — той
//     же колонкой, по которой сосед судит семью таблицы. Это не сужение по
//     недосмотру: `sent_at IS NULL` есть определение клейма, и запрос к журналу
//     без клейма именно им и отказывает. Вторая форма пометки обязана прийти
//     сюда своим изменением, а не быть принятой молча.
package repohygiene

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// JournalTable — таблица, у которой признака доставки НЕТ: спрашивать её по
// этому признаку нельзя ни одним запросом.
type JournalTable struct {
	// Owner — «services/iam» и т. п., как его называет перепись миграций.
	Owner string
	// Schema — схема объявления («kaname»); пустая, если объявлена без неё.
	Schema string
	// Name — имя таблицы в нижнем регистре.
	Name string
}

// Qualified — то написание, которым таблицу называют в запросе.
func (j JournalTable) Qualified() string {
	if j.Schema == "" {
		return j.Name
	}
	return j.Schema + "." + j.Name
}

// SQLChunk — фрагмент носителя, который ИСПОЛНЯЕТСЯ.
type SQLChunk struct {
	// Line — номер первой строки фрагмента в исходном файле.
	Line int
	// Text — сам фрагмент.
	Text string
}

// ChunkCensus — объём осмотренного у одного носителя.
//
// Полосы печатаются порознь: одно суммарное число не отличает «проза
// отброшена» от «файл не носитель», а различие несущее — во втором случае
// проверка не читала ничего.
type ChunkCensus struct {
	// Carrier — расширение файла разбору известно.
	Carrier bool
	// Chunks — исполняемых фрагментов вырезано.
	Chunks int
	// ProseLines — строк прозы отброшено (комментарии, текст вне блоков команд).
	ProseLines int
}

// Add складывает переписи.
func (c *ChunkCensus) Add(o ChunkCensus) {
	if o.Carrier {
		c.Carrier = true
	}
	c.Chunks += o.Chunks
	c.ProseLines += o.ProseLines
}

// MarkerQueryFinding — координата запроса, который спрашивает журнал по
// несуществующему признаку доставки.
type MarkerQueryFinding struct {
	File    string
	Line    int
	Table   string
	Excerpt string
}

// String — текст находки: координата, таблица, колонка и сам фрагмент.
func (f MarkerQueryFinding) String() string {
	return f.File + ":" + strconv.Itoa(f.Line) + "  " + f.Table + " спрашивается по «" +
		DeliveryMarkerColumn + "», которого у него нет: " + f.Excerpt
}

var (
	// sqlVerbRe — признак того, что фрагмент есть ЗАПРОС, а не совпадение слов.
	sqlVerbRe = regexp.MustCompile(`(?is)\b(select|insert\s+into|update|delete\s+from|create\s+index|alter\s+table|copy)\b`)
	// markerWordRe — признак доставки как ОТДЕЛЬНОЕ слово: без границ слова
	// `sent_at` совпал бы с `notified_at`-подобными именами и с чужими суффиксами.
	markerWordRe = regexp.MustCompile(`\b` + regexp.QuoteMeta(DeliveryMarkerColumn) + `\b`)
	// fenceRe — открытие/закрытие огороженного блока в разметке.
	fenceRe = regexp.MustCompile("^\\s*(```|~~~)")
	// branchRe — граница ОПЕРАТОРА внутри фрагмента: `;` и ветвь объединения.
	//
	// Без неё единицей суждения остаётся весь фрагмент, и сводный запрос по
	// нескольким очередям обвинял бы КАЖДУЮ названную в нём таблицу — включая ту,
	// чья ветвь признака не спрашивает. Такая находка верна по форме и ложна по
	// существу, а гейт, у которого часть находок ложные, перестают читать.
	branchRe = regexp.MustCompile(`(?i);|\bunion\b`)
)

// ExecutableSQLChunks режет носитель на фрагменты, которые ИСПОЛНЯЮТСЯ.
//
// Возвращает пустой список и `Carrier:false` для расширения, разбору
// неизвестного: «не носитель» и «носитель без фрагментов» — разные состояния, и
// сводить их в одно значило бы отдать «ноль прочитанного» за «ноль находок».
func ExecutableSQLChunks(path string, src []byte) ([]SQLChunk, ChunkCensus) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return journalGoStringLiterals(src)
	case ".sh", ".bash", ".zsh", ".py", ".yaml", ".yml":
		return hashCommentedLines(src)
	case ".sql":
		return sqlStatements(src)
	case ".md", ".mdx":
		return fencedBlocks(src)
	default:
		return nil, ChunkCensus{}
	}
}

// goStringLiterals вырезает строковые литералы Go. Комментарии не читаются: SQL
// живёт в литерале, а объяснение — в комментарии, и это ровно та граница,
// которую разбор обязан держать.
func journalGoStringLiterals(src []byte) ([]SQLChunk, ChunkCensus) {
	census := ChunkCensus{Carrier: true}
	var out []SQLChunk
	s := string(src)
	line := 1
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\n':
			line++
		case s[i] == '/' && i+1 < len(s) && s[i+1] == '/':
			for i < len(s) && s[i] != '\n' {
				i++
			}
			census.ProseLines++
			line++
		case s[i] == '/' && i+1 < len(s) && s[i+1] == '*':
			start := i
			i += 2
			for i+1 < len(s) && (s[i] != '*' || s[i+1] != '/') {
				i++
			}
			census.ProseLines += 1 + strings.Count(s[start:min(i, len(s))], "\n")
			line += strings.Count(s[start:min(i, len(s))], "\n")
			i++
		case s[i] == '`':
			start, startLine := i+1, line
			i++
			for i < len(s) && s[i] != '`' {
				if s[i] == '\n' {
					line++
				}
				i++
			}
			out = append(out, SQLChunk{Line: startLine, Text: s[start:min(i, len(s))]})
			census.Chunks++
		case s[i] == '"':
			start, startLine := i+1, line
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' {
					i++
				}
				if i < len(s) && s[i] == '\n' {
					break
				}
				i++
			}
			out = append(out, SQLChunk{Line: startLine, Text: s[start:min(i, len(s))]})
			census.Chunks++
		}
	}
	return out, census
}

// hashCommentedLines отдаёт строки носителя, кроме целиком-комментариев.
//
// Фрагментом считается СВЯЗНЫЙ участок непустых строк: запрос оболочки часто
// разложен на несколько строк, и построчный разбор потерял бы `WHERE` под
// именем таблицы — то есть промолчал бы на самом обычном написании.
func hashCommentedLines(src []byte) ([]SQLChunk, ChunkCensus) {
	census := ChunkCensus{Carrier: true}
	var out []SQLChunk
	var cur []string
	curLine := 0
	flush := func() {
		if len(cur) > 0 {
			out = append(out, SQLChunk{Line: curLine, Text: strings.Join(cur, "\n")})
			census.Chunks++
			cur = nil
		}
	}
	for i, ln := range strings.Split(string(src), "\n") {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "#") {
			census.ProseLines++
			flush()
			continue
		}
		if trimmed == "" {
			flush()
			continue
		}
		if len(cur) == 0 {
			curLine = i + 1
		}
		cur = append(cur, ln)
	}
	flush()
	return out, census
}

// sqlStatements режет миграцию на операторы между `;`, сняв `--`-комментарии.
func sqlStatements(src []byte) ([]SQLChunk, ChunkCensus) {
	census := ChunkCensus{Carrier: true}
	var out []SQLChunk
	var cur []string
	curLine := 0
	flushAt := func(line int) {
		if len(cur) > 0 {
			out = append(out, SQLChunk{Line: curLine, Text: strings.Join(cur, "\n")})
			census.Chunks++
			cur = nil
		}
		_ = line
	}
	for i, ln := range strings.Split(string(src), "\n") {
		body := ln
		if idx := strings.Index(body, "--"); idx >= 0 {
			body = body[:idx]
			census.ProseLines++
		}
		if strings.TrimSpace(body) == "" {
			continue
		}
		if len(cur) == 0 {
			curLine = i + 1
		}
		cur = append(cur, body)
		if strings.Contains(body, ";") {
			flushAt(i + 1)
		}
	}
	flushAt(0)
	return out, census
}

// fencedBlocks отдаёт ТОЛЬКО содержимое огороженных блоков разметки: всё
// остальное в документе — проза, и запросом не является.
func fencedBlocks(src []byte) ([]SQLChunk, ChunkCensus) {
	census := ChunkCensus{Carrier: true}
	var out []SQLChunk
	var cur []string
	curLine := 0
	inFence := false
	for i, ln := range strings.Split(string(src), "\n") {
		if fenceRe.MatchString(ln) {
			if inFence {
				if len(cur) > 0 {
					out = append(out, SQLChunk{Line: curLine, Text: strings.Join(cur, "\n")})
					census.Chunks++
					cur = nil
				}
				inFence = false
			} else {
				inFence = true
				curLine = i + 2
			}
			continue
		}
		if inFence {
			// Комментарий оболочки внутри блока команд — тоже проза: он ОПИСЫВАЕТ
			// запрос, а не является им, и оставленный в выдержке делает находку
			// нечитаемой. Разметка `sql`, где `#` комментарием не является, строк с
			// него не начинает.
			if strings.HasPrefix(strings.TrimSpace(ln), "#") {
				census.ProseLines++
				continue
			}
			cur = append(cur, ln)
			continue
		}
		census.ProseLines++
	}
	if len(cur) > 0 {
		out = append(out, SQLChunk{Line: curLine, Text: strings.Join(cur, "\n")})
		census.Chunks++
	}
	return out, census
}

// ScanMarkerQueries ищет в носителе исполняемые запросы, которые называют
// журнал БЕЗ признака доставки вместе с этим признаком.
func ScanMarkerQueries(path string, src []byte, journals []JournalTable) ([]MarkerQueryFinding, ChunkCensus) {
	chunks, census := ExecutableSQLChunks(path, src)
	if !census.Carrier || len(journals) == 0 {
		return nil, census
	}
	var out []MarkerQueryFinding
	for _, ch := range chunks {
		if !markerWordRe.MatchString(ch.Text) || !sqlVerbRe.MatchString(ch.Text) {
			continue
		}
		for _, stmt := range branchRe.Split(ch.Text, -1) {
			if !markerWordRe.MatchString(stmt) || !sqlVerbRe.MatchString(stmt) {
				continue
			}
			lower := strings.ToLower(stmt)
			for _, j := range journals {
				q := strings.ToLower(j.Qualified())
				if q == "" || !strings.Contains(lower, q) {
					continue
				}
				out = append(out, MarkerQueryFinding{
					File:    path,
					Line:    ch.Line,
					Table:   j.Qualified(),
					Excerpt: excerpt(stmt),
				})
			}
		}
	}
	return out, census
}

// excerpt — короткая выдержка фрагмента для текста находки: находка, не
// показывающая запрос, посылает читателя искать самому.
func excerpt(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 180 {
		return s[:180] + "…"
	}
	return s
}

// ScanTableSchemas отвечает, В КАКОЙ СХЕМЕ объявлена каждая таблица миграции.
//
// Сосед по файлу `deliverymarker.go` схему отбрасывает: ему довольно пары
// «владелец + имя». Здесь схема несущая — запрос называет таблицу ИМЕННО тем
// написанием, которым её объявили, и поиск по голому имени совпал бы с
// одноимённой таблицей чужой службы (таблица `operations` объявлена восемью
// владельцами).
func ScanTableSchemas(src []byte) map[string]string {
	up, _ := gooseUpSection(src)
	clean := blankSQLComments(up)
	_, top := splitDollarBodies(clean)
	out := map[string]string{}
	for _, m := range createTableRe.FindAllSubmatchIndex(top, -1) {
		name := unquote(string(top[m[4]:m[5]]))
		if name == "" || isSubstituted(name) {
			continue
		}
		schema := ""
		if m[2] >= 0 {
			schema = unquote(string(top[m[2]:m[3]]))
		}
		if isSubstituted(schema) {
			schema = ""
		}
		out[name] = schema
	}
	return out
}
