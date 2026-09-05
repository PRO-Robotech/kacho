// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// Владелец считаемого вида ОБЯЗАН быть читателем пределов.
//
// ПРЕДМЕТ. Списание квоты живёт у владельца типа, а величина — у iam. Значит на
// первой мутации владелец идёт к соседу за потолком, и если права звать резолв у
// него нет, отказ приходит fail-closed: НИ ОДНА его мутация не проходит.
//
// ЧТО ЭТО СТОИЛО. Волна дала списание четырём доменам и не выдала им членства в
// группе читателей: сквозной прогон одного из них дал 748 упавших утверждений из
// 1806 — каскад от ОДНОГО отказа в правах. Собственные пробы каждого домена были
// зелёными: у них резолв подставной и прав не спрашивает, а право живёт в третьем
// месте — в посеве модели прав iam.
//
// ПОЧЕМУ ГЕЙТ, А НЕ ВНИМАНИЕ. Разрыв невидим с обеих сторон: сборка цела, пробы
// владельца зелёные, пробы iam о чужом домене не знают. Единственное место, где
// два факта встречаются, — это дерево, и спрашивать его должен предикат.
//
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ: множество доменов, у которых есть списание (триггер
// `kacho_quota_count` в их миграциях), совпадает с множеством доменов, чья
// служебная учётка названа членом группы читателей пределов в миграциях iam.
func TestEveryQuotaChargingOwnerIsAQuotaReader(t *testing.T) {
	t.Parallel()

	root := repoRootFor(t)
	files, err := treecorpus.UnderWithSuffix(filepath.Join(root, "services"), ".sql")
	require.NoError(t, err, "перечень миграций берётся у индекса дерева, а не обходом диска")

	// Кто списывает: домен, чья миграция ставит триггер счётчика.
	charging := map[string]string{} // домен → файл, где найдено
	// Кто назван читателем: домен, чья служебная учётка попала в группу.
	readers := map[string]string{}

	chargeRe := regexp.MustCompile(`kacho_quota_count\(`)
	// Членство ищется по ИМЕНИ СЛУЖБЫ в файле, который упоминает группу читателей,
	// а НЕ по форме выражения вокруг него.
	//
	// Первая редакция искала `md5('kacho-<домен>')` — и не увидела собственную
	// миграцию этой правки, где имена приходят переменной из перечня, а `md5`
	// применяется к ней. Предикат мерил бы форму записи (как автор выразил вывод
	// идентичности), а не предмет (назван ли домен читателем). Форма — свободный
	// выбор автора и меняется; предмет — нет.
	readerRe := regexp.MustCompile(`'kacho-([a-z]+)'`)

	migrationsSeen := 0
	for _, path := range files {
		if !strings.Contains(path, "/internal/migrations/") {
			continue
		}
		raw, err := os.ReadFile(path)
		require.NoError(t, err, "чтение %s", path)
		migrationsSeen++
		body := string(raw)

		// Путь абсолютный; домен читается из части ПОСЛЕ `services/`.
		rel := path
		if i := strings.Index(path, "/services/"); i >= 0 {
			rel = path[i+1:]
		}

		if strings.HasPrefix(rel, "services/iam/") {
			// ФОРМА ВТОРАЯ — сведённая первичная миграция.
			//
			// В ней имени группы нет вовсе, а членство записано ИДЕНТИФИКАТОРАМИ:
			// `pg_dump` печатает значения столбцов, а не выражения, которыми их
			// когда-то вывели. Предикат по имени группы («файл упоминает
			// module.quota_readers») на таком файле не срабатывает НИ РАЗУ — и это
			// молчание, а не находка: гейт объявил бы, что читателей ноль, при
			// пяти живых членах группы. Так он и объявил на первом же сведённом
			// сервисе.
			//
			// Разрешение идёт ВНУТРИ одного файла и потому не требует базы: свод
			// несёт и группу с её именем, и членов, и служебные учётки с их
			// именами. Домен читается из имени учётки `kacho-<домен>` — того же
			// предмета, что и в первой форме.
			for domain := range quotaReaderDomainsByID(body) {
				readers[domain] = rel
			}

			// Владелец ВЕЛИЧИН исключён из множества списывающих НАМЕРЕННО, а не
			// по устройству цикла: с задачи #484 он тоже списывает (число
			// аккаунтов на личность), но членства в группе читателей ему не
			// требуется — резолв к самому себе по сети не идёт, и права звать
			// себя не существует как предмета.
			//
			// Сказано это здесь потому, что прежде исключение получалось само:
			// файлы iam уходили в ветку читателей и до предиката списания не
			// доходили. Совпадение было верным и стало бы ложным молча, если бы
			// ветка когда-нибудь перестала поглощать их целиком.
			if !strings.Contains(body, "module.quota_readers") {
				continue
			}
			for _, m := range readerRe.FindAllStringSubmatch(body, -1) {
				if m[1] == "system" {
					continue // владелец группы, не читатель
				}
				readers[m[1]] = rel
			}
			continue
		}

		if chargeRe.MatchString(body) {
			// services/<домен>/internal/migrations/…
			parts := strings.Split(rel, "/")
			if len(parts) > 1 {
				charging[parts[1]] = rel
			}
		}
	}

	require.NotZero(t, migrationsSeen,
		"гейт не прочитал НИ ОДНОЙ миграции — он объявил бы «ноль находок», ничего не осмотрев")
	require.NotEmpty(t, charging,
		"гейт не нашёл ни одного домена со списанием: либо имя триггера сменилось, "+
			"либо предикат перестал его ловить. Осмотрено миграций: %d", migrationsSeen)

	var missing []string
	for domain, where := range charging {
		if _, ok := readers[domain]; !ok {
			missing = append(missing, domain+" — списывает квоту ("+where+"), но не назван читателем пределов")
		}
	}
	sort.Strings(missing)

	t.Logf("перепись: миграций осмотрено %d; доменов со списанием %d (%s); читателей пределов %d (%s); "+
		"владелец величин из счёта списывающих исключён намеренно — резолва к самому себе нет",
		migrationsSeen, len(charging), joinKeys(charging), len(readers), joinKeys(readers))

	require.Empty(t, missing,
		"владелец считаемого вида обязан быть читателем пределов — иначе КАЖДАЯ его мутация "+
			"отвергается fail-closed на пути материализации, и это не видно ни одной его собственной пробе:\n%s",
		strings.Join(missing, "\n"))
}

// repoRootFor — корень репозитория; пути перечня относительны ему.
func repoRootFor(t testing.TB) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	return root
}

func joinKeys(m map[string]string) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// quotaReaderDomainsByID — домены, чьи служебные учётки состоят в группе
// читателей пределов, когда членство записано ИДЕНТИФИКАТОРАМИ.
//
// Это вторая законная форма записи того же предмета, и появилась она вместе со
// сведением цепочки миграций в одну первичную: `pg_dump` печатает значения, а не
// выражения. Первая форма (имя группы и имена служб литералами) остаётся и
// разбирается там, где стояла, — обе живут рядом, потому что «предмета нет» и
// «предмет записан иначе» обязаны различаться.
//
// Разрешение замкнуто на один файл: свод несёт группу, её членов и служебные
// учётки разом. Базы для этого не нужно, и гейт остаётся проверкой ДЕРЕВА.
func quotaReaderDomainsByID(body string) map[string]string {
	out := map[string]string{}

	var groupID string
	for _, m := range reSquashGroupRow.FindAllStringSubmatch(body, -1) {
		if m[2] == quotaReaderGroupName {
			groupID = m[1]
			break
		}
	}
	if groupID == "" {
		return out
	}

	accounts := map[string]string{} // id учётки → её имя
	for _, m := range reSquashServiceAccountRow.FindAllStringSubmatch(body, -1) {
		accounts[m[1]] = m[2]
	}

	for _, m := range reSquashGroupMemberRow.FindAllStringSubmatch(body, -1) {
		if m[1] != groupID || m[2] != "service_account" {
			continue
		}
		name, ok := accounts[m[3]]
		if !ok {
			continue
		}
		d := strings.TrimPrefix(name, "kacho-")
		if d == name || d == "system" {
			continue
		}
		out[d] = name
	}
	return out
}

// quotaReaderGroupName — имя группы читателей пределов в СТРОКЕ, а не в
// выражении. Вывод идентификатора из имени (`substr(md5(…))`) остался в первой
// форме; здесь имя читается прямо со строки, которую напечатал дамп.
const quotaReaderGroupName = "module-quota-readers"

var (
	reSquashGroupRow = regexp.MustCompile(
		`INSERT INTO kaname\.groups \([^)]*\) VALUES \('([^']+)', '[^']*', '([^']+)'`)
	reSquashServiceAccountRow = regexp.MustCompile(
		`INSERT INTO kaname\.service_accounts \([^)]*\) VALUES \('([^']+)', '[^']*', '([^']+)'`)
	reSquashGroupMemberRow = regexp.MustCompile(
		`INSERT INTO kaname\.group_members \([^)]*\) VALUES \('([^']+)', '([^']+)', '([^']+)'`)
)
