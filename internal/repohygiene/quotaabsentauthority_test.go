// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// quotaabsentauthority_test.go — репо-широкий гейт: списывающий триггер учёта
// ЧИТАЕТ объявление домена величин и не отвергает, когда домен объявлен
// отсутствующим.
//
// # Зачем он существует
//
// Ручка домена величин принимает два законных значения — адрес соседа либо слово
// `not-deployed`. Второе означает «потолки в этой установке не назначаются
// НИКЕМ», и приёмка (решение `Д2` п. 1) требует от него ровно одного
// наблюдаемого следствия: мутация проходит. До задачи
// `PRO-Robotech/kacho#2216` объявление доезжало только до фонового тянущего, а
// путь запроса не менялся: списывающий триггер по-прежнему требовал строки
// учёта и отвергал КАЖДУЮ вставку считаемого вида `KQ002`.
//
// # Почему свойство держит гейт, а не пять проб по одной на владельца
//
// «Списывающий оператор читает объявление» — свойство ДЕРЕВА: владельцев учёта
// пятеро, и шестой заведётся копией чужого тела. Проба одного владельца об
// остальных не утверждает ничего и остаётся зелёной ровно на том дефекте, ради
// которого написана. Поведение при этом закреплено отдельно и на живой базе —
// интеграционной парой у vpc (`quota_absent_authority_integration_test.go`),
// потому что гейт судит текст, а не исход.
//
// # Что считается находкой
//
// У владельца, чей списывающий триггер зовёт производителя отказа, ПОСЛЕДНЕЕ во
// времени определение этого триггера обязано: читать состояние из курсора
// синхронизации; нести объявленное отсутствие в предикате КАЖДОГО списания; и
// прикрывать им КАЖДЫЙ вызов производителя отказа. Отсутствие любого из трёх —
// находка с координатой файла.
//
// «Последнее во времени» несущее: применённую миграцию править нельзя (ban #5),
// поэтому тело одной функции определено в дереве несколько раз, и верно только
// самое старшее. Гейт, судящий все определения разом, краснел бы на прежних —
// то есть на верной работе.
//
// # Почему комментарии вырезаются перед разбором
//
// Слова `v_absent` и `quota_sync_cursor` стоят в прозе самих миграций — в
// разборе решения и в объяснении отката. Предикат по сырому тексту нашёл бы их
// в СОБСТВЕННОМ объяснении и остался бы зелёным на снятой защите
// (`testing.md` §«Гейт на класс», п. 4). Комментарии SQL снимаются, и судится
// исполняемая часть.
//
// # Чего гейт не ловит, и это сказано честно
//
// Он не проверяет, что послабление наступает ТОЛЬКО на объявленном отсутствии:
// «неизвестно не есть отсутствует» — свойство исхода, и держит его
// интеграционная проба. И он не судит блок отката: там стоит прежнее тело,
// не знающее объявления, — это и есть смысл отката.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// quotaChargeTriggerName — имя списывающей триггерной функции. Одно на всех
// владельцев: таблица у них одна и та же с точностью до схемы, и функция тоже.
const quotaChargeTriggerName = "kacho_quota_count"

// quotaAuthorityAwareExempt — владельцы, от которых объявление НЕ требуется, и
// причина по каждому.
//
// Запись самоистекает: у освобождённого владельца не должно быть таблицы
// курсора синхронизации — появится, и запись станет находкой. Освобождение,
// которому нечего освобождать, унаследовала бы следующая слепая зона.
var quotaAuthorityAwareExempt = map[string]string{
	// Служба доступа САМА является авторитетом величин, поэтому объявлять ей
	// нечего и спрашивать не у кого: таблицы курсора синхронизации у неё нет и
	// быть не должно. Её цепь миграций вдобавок сведена в применённую первичную,
	// править которую нельзя (ban #5).
	"iam": "сама является авторитетом величин: курсора синхронизации у неё нет",
}

// quotaChargeDefRe — определение списывающей триггерной функции.
var quotaChargeDefRe = regexp.MustCompile(
	`(?i)CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+[a-z_.]*` + quotaChargeTriggerName + `\s*\(`)

// quotaMigrationVersionRe — числовой префикс имени файла миграции.
var quotaMigrationVersionRe = regexp.MustCompile(`^(\d+)_`)

// quotaRefusalCallRe — вызов единственного производителя отказа.
var quotaRefusalCallRe = regexp.MustCompile(`(?i)PERFORM\s+[a-z_.]*kacho_quota_refuse\s*\(`)

// quotaChargeUpdateRe — списывающий оператор: прибавление места.
var quotaChargeUpdateRe = regexp.MustCompile(`(?is)UPDATE\s+[a-z_.]*project_resource_quotas\s+SET\s+used\s*=\s*used\s*\+\s*1[^;]{0,600};`)

// quotaAbsentGuard — исполняемая форма чтения объявления в предикате.
const quotaAbsentGuard = "COALESCE(v_absent, false)"

// quotaAbsentCensus — объём осмотренного. Печатается ВСЕГДА и НЕСКОЛЬКИМИ
// величинами: одно число («находок 0») скрывает ровно тот случай, ради которого
// гейт заведён, — обход не нашёл ни одного предмета.
type quotaAbsentCensus struct {
	Services  int
	WithCharg int
	Aware     int
	Exempt    int
	Files     int
}

func (c quotaAbsentCensus) String() string {
	return fmt.Sprintf(
		"каталогов сервисов осмотрено %d · файлов миграций прочитано %d · "+
			"со списывающим триггером %d · читают объявление домена величин %d · освобождено %d",
		c.Services, c.Files, c.WithCharg, c.Aware, c.Exempt)
}

// Снятие комментариев берётся у соседа (`readpathconcat_test.go`,
// `stripSQLComments`): второй экземпляр разошёлся бы с первым молча, и разошёлся
// бы именно там, где расхождение не видно, — на форме комментария.

// upBlock отдаёт исполняемую часть блока `+goose Up`.
//
// Блок отката судить нельзя by construction: в нём стоит ПРЕЖНЕЕ тело, не знающее
// объявления, — и это его смысл, а не нарушение.
func upBlock(body string) string {
	up := strings.Index(body, "-- +goose Up")
	if up < 0 {
		return ""
	}
	rest := body[up:]
	if down := strings.Index(rest, "-- +goose Down"); down >= 0 {
		rest = rest[:down]
	}
	// Комментарии снимаются, пустые строки убираются: и то и другое НЕ является
	// исполняемой частью, а расстояние до охраны, посчитанное по физическим
	// строкам, зависело бы от длины соседнего объяснения — то есть гейт мерил бы
	// вёрстку.
	var kept []string
	for _, line := range strings.Split(stripSQLComments(rest), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// TestQuotaChargeTriggerReadsTheAuthorityDeclaration — у каждого владельца учёта
// СТАРШЕЕ определение списывающего триггера читает объявление домена величин и
// не отвергает, когда домен объявлен отсутствующим.
func TestQuotaChargeTriggerReadsTheAuthorityDeclaration(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	census, findings := auditQuotaAbsentAuthority(t, root)

	t.Log(census.String())
	if census.Services == 0 || census.Files == 0 {
		t.Fatalf("обход пуст — вердикт беспредметен: %s", census)
	}
	if census.WithCharg == 0 {
		t.Fatalf("ни одного списывающего триггера не найдено — предмет гейта исчез "+
			"либо распознаватель ослеп: %s", census)
	}
	for _, f := range findings {
		t.Errorf("%s", f)
	}
}

// auditQuotaAbsentAuthority — предмет гейта, вынесенный отдельно, чтобы инъекция
// звала ТУ ЖЕ функцию, а не свою копию разбора.
func auditQuotaAbsentAuthority(t *testing.T, root string) (quotaAbsentCensus, []string) {
	t.Helper()

	base := filepath.Join(root, "services")
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("чтение каталога сервисов: %v", err)
	}

	var (
		census   quotaAbsentCensus
		findings []string
	)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		svc := e.Name()
		migDir := filepath.Join(base, svc, "internal", "migrations")
		if _, statErr := os.Stat(migDir); statErr != nil {
			continue
		}
		census.Services++

		files, listErr := treecorpus.UnderWithSuffix(migDir, ".sql")
		if listErr != nil {
			t.Fatalf("состав миграций %s: %v", svc, listErr)
		}

		type def struct {
			version int64
			rel     string
			up      string
		}
		var defs []def
		for _, path := range files {
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("чтение %s: %v", path, readErr)
			}
			census.Files++
			up := upBlock(string(body))
			if !quotaChargeDefRe.MatchString(up) {
				continue
			}
			m := quotaMigrationVersionRe.FindStringSubmatch(filepath.Base(path))
			if m == nil {
				t.Fatalf("имя миграции %s не несёт числового префикса", path)
			}
			v, convErr := strconv.ParseInt(m[1], 10, 64)
			if convErr != nil {
				t.Fatalf("номер миграции %s: %v", path, convErr)
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				t.Fatalf("относительный путь %s: %v", path, relErr)
			}
			defs = append(defs, def{version: v, rel: rel, up: up})
		}
		if len(defs) == 0 {
			continue
		}
		census.WithCharg++

		if why, ok := quotaAuthorityAwareExempt[svc]; ok {
			census.Exempt++
			// Освобождение самоистекает: предмет у него — ОТСУТСТВИЕ курсора
			// синхронизации. Появится — освобождать станет нечего, и запись
			// обязана стать находкой, а не пережить свой предмет.
			hasCursor := false
			for _, path := range files {
				body, readErr := os.ReadFile(path)
				if readErr != nil {
					t.Fatalf("чтение %s: %v", path, readErr)
				}
				if strings.Contains(stripSQLComments(string(body)), "quota_sync_cursor") {
					hasCursor = true
					break
				}
			}
			if hasCursor {
				findings = append(findings, fmt.Sprintf(
					"services/%s: освобождение объявлено причиной %q, но курсор "+
						"синхронизации у него ЕСТЬ — предмета у освобождения больше нет, "+
						"снимите запись из quotaAuthorityAwareExempt", svc, why))
			}
			continue
		}

		sort.Slice(defs, func(i, j int) bool { return defs[i].version < defs[j].version })
		latest := defs[len(defs)-1]

		problems := quotaAbsentAuthorityProblems(latest.up)
		if len(problems) == 0 {
			census.Aware++
			continue
		}
		findings = append(findings, fmt.Sprintf(
			"%s (старшее определение %s у services/%s): %s",
			latest.rel, quotaChargeTriggerName, svc, strings.Join(problems, "; ")))
	}
	return census, findings
}

// quotaAbsentAuthorityProblems — ВЕСЬ вердикт по одному телу, вынесенный
// отдельной чистой функцией.
//
// Отдельной затем, чтобы доказательство способности упасть звало ТУ ЖЕ функцию,
// что и гейт, а не свою копию разбора: копия разошлась бы с оригиналом молча и
// доказывала бы себя саму.
func quotaAbsentAuthorityProblems(up string) []string {
	var problems []string
	if !strings.Contains(up, "quota_sync_cursor") || !strings.Contains(up, "authority_state") {
		problems = append(problems,
			"не читает объявление домена величин из курсора синхронизации")
	}
	for _, charge := range quotaChargeUpdateRe.FindAllString(up, -1) {
		if !strings.Contains(charge, quotaAbsentGuard) {
			problems = append(problems,
				"списывающий оператор не несёт объявленного отсутствия в предикате: "+
					"при снятом домене величин он не списал бы, и потребление отстало бы "+
					"от числа строк, которые оно считает")
			break
		}
	}
	for _, loc := range quotaRefusalCallRe.FindAllStringIndex(up, -1) {
		if !guardedByAbsent(up, loc[0]) {
			problems = append(problems,
				"вызов производителя отказа не прикрыт объявленным отсутствием: "+
					"мутация отвергалась бы там, где потолок не назначаем никем")
			break
		}
	}
	return problems
}

// guardedByAbsent говорит, прикрыт ли вызов производителя отказа объявленным
// отсутствием домена величин.
//
// Смотрится БЛИЖАЙШЕЕ предшествующее условие, а не окно из N строк. Окно мерило
// бы расстояние — величину, зависящую от того, сколько строк занял соседний
// оператор, — и «охрана есть» зеленело бы от охраны соседней оси. Ближайшее
// условие отвечает на тот вопрос, который задан: под каким `IF` стоит вызов.
//
// Предел назван честно: условие, разложенное на несколько строк, будет прочитано
// по последней из них, и охрана в первой не зачтётся. Это даёт ГРОМКУЮ ложную
// находку с координатой, а не тихий пропуск, — сторона ошибки выбрана осознанно.
func guardedByAbsent(up string, at int) bool {
	lines := strings.Split(up[:at], "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(strings.ToUpper(trimmed), "IF ") {
			continue
		}
		return strings.Contains(trimmed, quotaAbsentGuard)
	}
	return false
}
