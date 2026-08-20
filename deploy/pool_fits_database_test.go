// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// pool_fits_database_test.go — объявленная посадка не вправе обещать базе больше
// соединений, чем та принимает.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЭТО ПРОВЕРЯЕТСЯ ПО ОБЪЯВЛЕНИЮ, А НЕ ТОЛЬКО ЗАГРУЗОЧНЫМ СТРАЖЕМ
//
// Величины назначены в РАЗНЫХ файлах и ни одна не знает о другой: пул объявлен на
// ОДНУ реплику в значениях службы, предел — общий на ВСЕ, в свободном тексте
// настроек базы. Их произведение не записано нигде, поэтому расхождение не видно
// ни в одном файле по отдельности, и обзор изменения его не показывает.
//
// Загрузочный страж ловит то же расхождение, но ПОЗЖЕ — когда посадка уже
// раскатана и под отказывается стартовать. Здесь оно ловится ДО развёртывания и
// сразу на всех стеках, включая те, куда рука не доходит.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТА ПРОВЕРКА НЕ ЛОВИТ — НАЗВАНО, А НЕ УМОЛЧАНО
//
// Она судит ОБЪЯВЛЕННУЮ посадку. Реплика, поднятая помимо объявления (правкой
// живого развёртывания), в объявлении не отражается, и здесь не видна by
// construction — именно так и была получена посадка на пяти репликах, с которой
// начался разбор. Такую видит только загрузочный страж, и то если объявленный
// бюджет реплик приведён в соответствие.

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// pgDefaultMaxConnections / pgDefaultSuperuserReserved — умолчания СБОРКИ
// PostgreSQL.
//
// Стоят здесь потому, что предел бывает не объявлен ни одним профилем: чарт базы
// этой настройки не выставляет вовсе, и тогда действует то, с чем собран образ.
// Считать неназванный предел бесконечным значило бы считать самый опасный случай
// самым безопасным — «не объявлено» превратилось бы в «не ограничено».
const (
	pgDefaultMaxConnections    = 100
	pgDefaultSuperuserReserved = 3
)

var maxConnectionsLine = regexp.MustCompile(`(?m)^\s*max_connections\s*=\s*(\d+)`)

// knownUnfitting — связки «стек → база», чьё несоответствие ЭТОТ гейт нашёл и
// которые заведены СВОИМ предметом.
//
// Перечень существует не затем, чтобы отвернуться от находки, а затем, чтобы
// чужой предмет чинился своим изменением: смешение линий делает вердикт
// непрослеживаемым, а перепись — недоказуемой.
//
// Запись САМОИСТЕКАЕТ: как только связка начинает помещаться, ей больше нечего
// исключать — и гейт падает на самой записи. Иначе исключение пережило бы свой
// предмет, а следующий читатель принял бы его за действующее ограничение.
// Ведомость ПУСТА — и это её нормальное состояние, а не признак поломки. Обе
// записи, стоявшие здесь с заведения гейта, сняты вместе со своим предметом
// (#762): предел pg-vpc объявлен профилем на обоих боевых стеках — 210 в
// values.prod.yaml, столько же в values.fe3455.yaml, — и пул службы (200 на
// реплику) в него помещается с учётом трёх соединений, зарезервированных
// суперпользователю.
var knownUnfitting = map[string]string{}

// poolPaths — известные формы пути к пределу пула в значениях службы.
//
// Форм несколько потому, что службы раскладывались в разное время; перечень
// нужен затем, чтобы «предел не найден» означало ИМЕННО отсутствие объявления, а
// не незнание проверки о четвёртой форме. Служба, ни одной формы не объявившая,
// попадает в перепись пропущенных — молча она не исчезает.
var poolPaths = [][]string{
	{"config", "repository", "postgres", "maxConns"},
	{"repository", "postgres", "maxConns"},
	{"db", "maxConns"},
}

func asInt(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		return n, err == nil
	}
	return 0, false
}

// valuesWithSubchartDefaults — значения, которые helm получит для этого стека:
// умолчания КАЖДОГО ПОДЧАРТА под умолчаниями умбреллы под профилями цепочки.
//
// Отличается от соседнего `effectiveValues` (managed_cluster_profile_test.go)
// ровно первым слоем, и слой этот здесь обязателен: боевые профили службу не
// переобъявляют вовсе, её предел пула и число реплик живут только в её
// собственном values.yaml. Проверка, читающая одни файлы умбреллы, увидела бы
// там пустоту и объявила бы боевой стек чистым — «ноль находок» вместо «ноль
// прочитанного». Соседняя функция не расширена намеренно: её читатель
// спрашивает про другой предмет, и менять его основание ради этой проверки
// значило бы двигать чужой вердикт.
func valuesWithSubchartDefaults(t *testing.T, chain []string) map[string]any {
	t.Helper()
	out := map[string]any{}
	for alias, dir := range subchartDirs(t) {
		vals := readYAML(t, filepath.Join(dir, "values.yaml"))
		out[alias] = mergeValues(map[string]any{}, vals)
	}
	out = mergeValues(out, readYAML(t, filepath.Join(umbrellaDir, "values.yaml")))
	for _, p := range chain {
		out = mergeValues(out, readYAML(t, filepath.Join(umbrellaDir, p)))
	}
	return out
}

func TestDeclaredPoolFitsTheDatabaseItConnectsTo(t *testing.T) {
	stacks := deployStacks(t)

	var examined, skipped int
	var skippedWhy []string

	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		vals := valuesWithSubchartDefaults(t, stacks[name])

		// Пределы каждой базы стека.
		ceilings := map[string]int{}
		declared := map[string]bool{}
		for alias, node := range vals {
			if !strings.HasPrefix(alias, "pg-") {
				continue
			}
			sub, _ := node.(map[string]any)
			if sub == nil {
				continue
			}
			ceilings[alias] = pgDefaultMaxConnections
			raw, ok := lookup(sub, "primary", "extendedConfiguration")
			if !ok {
				continue
			}
			if m := maxConnectionsLine.FindStringSubmatch(fmt.Sprint(raw)); m != nil {
				n, _ := strconv.Atoi(m[1])
				ceilings[alias] = n
				declared[alias] = true
			}
		}
		if len(ceilings) == 0 {
			t.Fatalf("стек %q: не нашлось ни одной базы (ключи pg-*) — предикат перестал их "+
				"узнавать, а не базы исчезли", name)
		}

		// Что каждая служба обещает своей базе.
		promised := map[string]int{}
		for alias, node := range vals {
			sub, _ := node.(map[string]any)
			if sub == nil || strings.HasPrefix(alias, "pg-") {
				continue
			}
			host, ok := lookup(sub, "db", "host")
			if !ok {
				continue
			}
			var pgAlias string
			for pg := range ceilings {
				if strings.HasSuffix(fmt.Sprint(host), pg) {
					pgAlias = pg
					break
				}
			}
			if pgAlias == "" {
				continue
			}
			var pool int
			found := false
			for _, p := range poolPaths {
				if raw, ok := lookup(sub, p...); ok {
					if n, ok := asInt(raw); ok {
						pool, found = n, true
						break
					}
				}
			}
			if !found || pool <= 0 {
				// Предел не объявлен либо объявлен нулём — тогда действует
				// умолчание драйвера, зависящее от числа ядер УЗЛА, которого
				// объявление не знает. Судить об этом отсюда нечем; молча
				// пропустить — значит объявить стек чистым, не прочитав службу.
				skipped++
				skippedWhy = append(skippedWhy, fmt.Sprintf(
					"%s/%s: предел пула не объявлен (действует умолчание драйвера)", name, alias))
				continue
			}
			reps := 1
			for _, k := range []string{"replicas", "replicaCount"} {
				if raw, ok := lookup(sub, k); ok {
					if n, ok := asInt(raw); ok {
						reps = n
					}
				}
			}
			examined++
			promised[pgAlias] += pool * reps
			t.Logf("стек %s: %s → %s, пул %d × %d реплик = %d", name, alias, pgAlias, pool, reps, pool*reps)
		}

		for pg, total := range promised {
			available := ceilings[pg] - pgDefaultSuperuserReserved
			key := name + "/" + pg
			if why, known := knownUnfitting[key]; known {
				if total <= available {
					t.Errorf("записи %q в перечне известных несоответствий больше НЕЧЕГО "+
						"исключать: %s обещает %d при доступных %d. Снимите запись — "+
						"исключение, пережившее свой предмет, читается как действующее "+
						"ограничение (%s)", key, pg, total, available, why)
					continue
				}
				t.Logf("известное несоответствие %s: обещано %d, принимается %d — %s",
					key, total, available, why)
				continue
			}
			if total > available {
				note := "объявлен профилем"
				if !declared[pg] {
					note = "НЕ ОБЪЯВЛЕН ни одним профилем — действует умолчание сборки образа, " +
						"то есть предел никто не выбирал"
				}
				t.Errorf("стек %q: службы обещают базе %s %d соединений, а она принимает %d "+
					"(max_connections %d, %s; запас суперпользователя %d). "+
					"Обещанное сверх принимаемого не «иногда медленнее» — это отказ в "+
					"подключении ровно тогда, когда соединения понадобились",
					name, pg, total, available, ceilings[pg], note, pgDefaultSuperuserReserved)
			}
		}
	}

	// ПЕРЕПИСЬ: «ноль находок» обязано быть отличимо от «ноль прочитанного».
	t.Logf("осмотрено: стеков %d, связок служба→база с объявленным пулом %d, пропущено %d",
		len(stacks), examined, skipped)
	for _, w := range skippedWhy {
		t.Logf("  пропущено — %s", w)
	}
	if examined == 0 {
		t.Fatal("ни одной связки служба→база не осмотрено — проверка ничего не утверждает, " +
			"хотя выглядит зелёной")
	}
}
