// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// quotausedwriter_test.go — репо-широкий гейт: потребление квоты двигает ТОЛЬКО
// её триггер.
//
// ЗАЧЕМ ОН СУЩЕСТВУЕТ. Потолок на число ресурсов держится не ограничением схемы,
// а предикатом `used < limit_value` в условном `UPDATE` триггера учёта. Так
// сделано намеренно: `CHECK (used <= limit_value)` запрещал бы состояние «занято
// больше, чем разрешено» и тем самым делал бы невыразимым САМО ПОНИЖЕНИЕ предела
// — штатное административное действие стало бы заложником того, кого оно
// ограничивает (измерено на прецеденте
// `services/compute/internal/migrations/0031_project_instance_limit.sql`).
//
// У этого выбора есть цена, и она названа в шапке миграции учёта: раз инвариант
// держит предикат, а не схема, то путь, который запишет `used` мимо этого
// предиката, потолок обойдёт — молча и не отказав ни разу. Гейт и есть то, чем
// цена оплачивается: он доказывает, что такого пути нет, вместо того чтобы
// полагаться на внимание следующего контрибьютора.
//
// ЧТО СЧИТАЕТСЯ НАХОДКОЙ. Любой `UPDATE` таблицы учёта, трогающий `used`, вне
// файла, определяющего триггерную функцию. Плюс `INSERT`, задающий `used`
// ненулевым литералом: материализация заводит строку с нулём, и это законно, а
// вставка сразу с потреблением — тот же обход, только с другой стороны.
//
// ЧЕГО ГЕЙТ НЕ ЛОВИТ, И ЭТО СКАЗАНО ЧЕСТНО. Он читает текст запросов в дереве,
// поэтому запрос, собранный из кусков в рантайме, ему не виден. Тот же предел
// есть у всякой проверки этого класса здесь; закрывается он тем, что запросы в
// этом дереве пишутся литералами — и первое же отступление от этого само станет
// находкой для читателя, а не для гейта.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// quotaUsageTables — таблицы учёта потребления. Перечень ВЫПИСАН, а не выведен:
// имя таблицы — координата, и вывести его из дерева можно только тем же поиском,
// который гейт и делает, — то есть предикат совпал бы с предметом.
//
// Запись, которой больше нечего исключать, роняет гейт (см. ниже): перечень
// самоистекающий, поэтому таблица, снятая вместе со своим механизмом, не оставит
// здесь мёртвой строки.
// `project_instance_quotas` (прецедент compute) отсюда СНЯТА вместе со своим
// предметом: таблица дропнута миграцией
// `services/compute/internal/migrations/0036_project_resource_quotas.sql`,
// списание в Go-функции репозитория удалено, механизма под этим именем в дереве
// нет. Имя ещё встречается — в применённой 0031, которую править нельзя (ban #5),
// и в снимающей её 0036, — но ни та, ни другая `used` не пишут. Watch без
// механизма был бы записью, которой нечего исключать, и его унаследовала бы
// следующая слепая зона.
var quotaUsageTables = []string{
	"project_resource_quotas",
}

// quotaTriggerDefiningFiles — файлы, которым писать `used` РАЗРЕШЕНО, потому что
// они этот механизм и определяют. Ключ — таблица, значение — суффикс пути.
//
// У `project_instance_quotas` (прецедент compute) записей БОЛЬШЕ НЕТ, и это не
// упущение: прецедент снят вместе с заведением канонического механизма
// (`services/compute/internal/migrations/0036_project_resource_quotas.sql`), его
// списание в Go-функции репозитория удалено, и писать `used` там стало некому.
// Само имя таблицы остаётся в наблюдаемых ниже — оно ещё встречается в дереве
// (применённая 0031 и снимающая её 0036), и запись сторожит его возвращение.
var quotaTriggerDefiningFiles = map[string][]string{
	"project_resource_quotas": {
		"services/vpc/internal/migrations/0040_project_resource_quotas.sql",
		"services/compute/internal/migrations/0036_project_resource_quotas.sql",
	},
}

var (
	// `UPDATE <что-то> … SET … used` — перевод строки допускается, поэтому (?s).
	quotaUpdateUsedRe = regexp.MustCompile(`(?is)UPDATE\s+[a-z_.]*%s\b[^;]{0,400}?\bSET\b[^;]{0,400}?\bused\s*=`)
	// `INSERT INTO <учёт> (… used …) VALUES (… <ненулевой литерал> …)`.
	quotaInsertUsedRe = regexp.MustCompile(`(?is)INSERT\s+INTO\s+[a-z_.]*%s\b`)
)

// scanRootsForQuotaWriters — где искать. Пустой обход — провал, а не «чисто».
var scanRootsForQuotaWriters = []string{"services", "pkg", "gateway"}

func forEachQuotaScannedFile(t *testing.T, root string, fn func(rel string, body []byte)) {
	t.Helper()
	for _, sub := range scanRootsForQuotaWriters {
		base := filepath.Join(root, sub)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			name := info.Name()
			isGo := strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
			isSQL := strings.HasSuffix(name, ".sql")
			if !isGo && !isSQL {
				return nil
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			fn(filepath.ToSlash(rel), body)
			return nil
		})
		if err != nil {
			t.Fatalf("обход %s: %v", base, err)
		}
	}
}

// TestQuotaUsedIsWrittenOnlyByItsTrigger — потребление двигает только механизм,
// который его и определяет.
func TestQuotaUsedIsWrittenOnlyByItsTrigger(t *testing.T) {
	root := repoRoot(t)

	scannedFiles := 0
	mentionsByTable := map[string]int{}
	var findings []string

	forEachQuotaScannedFile(t, root, func(rel string, body []byte) {
		scannedFiles++
		text := string(body)
		for _, table := range quotaUsageTables {
			if !strings.Contains(text, table) {
				continue
			}
			mentionsByTable[table]++
			if quotaFileMayWriteUsed(rel, table) {
				continue
			}
			updRe := regexp.MustCompile(strings.Replace(quotaUpdateUsedRe.String(), "%s", regexp.QuoteMeta(table), 1))
			if loc := updRe.FindStringIndex(text); loc != nil {
				findings = append(findings, rel+":"+strconv.Itoa(lineOfOffset(text, loc[0]))+
					" — UPDATE "+table+" … SET used вне механизма, определяющего списание")
			}
			insRe := regexp.MustCompile(strings.Replace(quotaInsertUsedRe.String(), "%s", regexp.QuoteMeta(table), 1))
			for _, loc := range insRe.FindAllStringIndex(text, -1) {
				stmt := quotaStatementAt(text, loc[0])
				if quotaInsertSetsNonZeroUsed(stmt) {
					findings = append(findings, rel+":"+strconv.Itoa(lineOfOffset(text, loc[0]))+
						" — INSERT INTO "+table+" с ненулевым used: обход предиката с другой стороны")
				}
			}
		}
	})

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного»: сломанный обход
	// зеленеет ровно так же, как чистое дерево.
	if scannedFiles == 0 {
		t.Fatalf("гейт не прочитал ни одного файла в %v — предпосылка обхода сломана, "+
			"молчание ничего не доказывает", scanRootsForQuotaWriters)
	}

	// Самоистечение: запись перечня, которой больше нечего исключать, — находка.
	// Иначе снятый прецедент оставил бы здесь мёртвую строку, которую унаследует
	// следующая слепая зона.
	for _, table := range quotaUsageTables {
		if mentionsByTable[table] == 0 {
			t.Errorf("таблица учёта %q не встречается в дереве ни разу: механизм снят, "+
				"а запись о нём в этом гейте осталась — снимите её вместе с предметом", table)
		}
	}
	for table, files := range quotaTriggerDefiningFiles {
		for _, f := range files {
			body, err := os.ReadFile(filepath.Join(root, f))
			if err != nil {
				t.Errorf("послаблению %q (таблица %s) больше нечего исключать: файла нет — "+
					"снимите запись вместе с её предметом", f, table)
				continue
			}
			// Существование файла — ещё НЕ предмет послабления. Предмет —
			// запись `used` в нём. Файл, переставший писать, оставляет
			// послабление, которое ничего не исключает: оно молча разрешит
			// будущему автору этого файла обойти предикат потолка. Проверка
			// существования такое не ловит — ровно этот случай и возник, когда
			// списание compute переехало из Go-функции в триггер, а запись о
			// ней осталась бы висеть.
			updRe := regexp.MustCompile(strings.Replace(
				quotaUpdateUsedRe.String(), "%s", regexp.QuoteMeta(table), 1))
			if !updRe.MatchString(string(body)) {
				t.Errorf("послабление %q (таблица %s) не имеет предмета: файл существует, "+
					"но `used` в нём не пишется — снимите запись вместе с её предметом", f, table)
			}
		}
	}

	t.Logf("осмотрено файлов: %d; упоминаний таблиц учёта: %v", scannedFiles, mentionsByTable)

	if len(findings) > 0 {
		t.Fatalf("потребление квоты пишется мимо своего механизма (%d):\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}

func quotaFileMayWriteUsed(rel, table string) bool {
	for _, allowed := range quotaTriggerDefiningFiles[table] {
		if rel == allowed {
			return true
		}
	}
	return false
}

// quotaStatementAt возвращает текст оператора, начинающегося на offset, до `;`.
func quotaStatementAt(text string, offset int) string {
	end := strings.IndexByte(text[offset:], ';')
	if end < 0 {
		end = len(text) - offset
	}
	return text[offset : offset+end]
}

// quotaInsertSetsNonZeroUsed — вставка называет `used` и даёт ему не ноль.
//
// Ноль законен: материализация заводит строку с нулевым потреблением. Ненулевой
// литерал означает «строка появилась уже потраченной», то есть тот же обход
// предиката, только со стороны вставки.
func quotaInsertSetsNonZeroUsed(stmt string) bool {
	lower := strings.ToLower(stmt)
	if !strings.Contains(lower, "used") {
		return false
	}
	open := strings.IndexByte(lower, '(')
	if open < 0 {
		return false
	}
	closeIdx := strings.IndexByte(lower[open:], ')')
	if closeIdx < 0 {
		return false
	}
	cols := strings.Split(lower[open+1:open+closeIdx], ",")
	usedIdx := -1
	for i, c := range cols {
		if strings.TrimSpace(c) == "used" {
			usedIdx = i
			break
		}
	}
	if usedIdx < 0 {
		return false
	}
	rest := lower[open+closeIdx:]
	vOpen := strings.Index(rest, "values")
	if vOpen < 0 {
		return false
	}
	tupleStart := strings.IndexByte(rest[vOpen:], '(')
	if tupleStart < 0 {
		return false
	}
	tupleEnd := strings.IndexByte(rest[vOpen+tupleStart:], ')')
	if tupleEnd < 0 {
		return false
	}
	vals := strings.Split(rest[vOpen+tupleStart+1:vOpen+tupleStart+tupleEnd], ",")
	if usedIdx >= len(vals) {
		return false
	}
	v := strings.TrimSpace(vals[usedIdx])
	// Параметр ($1) не литерал — судить о нём статически нечем, и объявлять его
	// находкой значило бы ловить форму вместо существа.
	if strings.HasPrefix(v, "$") {
		return false
	}
	return v != "0"
}

func lineOfOffset(text string, off int) int {
	return strings.Count(text[:off], "\n") + 1
}
