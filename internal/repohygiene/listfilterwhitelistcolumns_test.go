// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Поле публичного фильтра списка обязано быть НАСТОЯЩЕЙ колонкой и быть пригодным
// к сравнению по КОНТРАКТНОМУ значению.
//
// # Почему это гейт, а не соглашение
//
// Общий разборщик фильтра подставляет имя поля в SQL **дословно** (значение
// параметризуется, имя — нет; безопасность имени держится ровно на этом белом
// списке). Значит белый список — единственное место, где решается, что арендатор
// вправе написать в `filter`, и ошибка в нём даёт один из трёх исходов, и все три
// молчаливые:
//
//  1. **имя не колонка** (опечатка, путь внутрь JSONB) — запрос падает ошибкой
//     базы на КАЖДОМ вызове с этим полем, и увидит это только арендатор;
//  2. **колонка есть, но хранит перечисление ЧИСЛОМ** (`smallint`) — арендатор
//     пишет контрактное значение (`IPV4`), в колонке лежит `1`. Фильтр либо не
//     находит ничего, либо падает приведением типа. Хуже всего то, что «не
//     нашлось ничего» — законный ответ списка, поэтому дефект неотличим от
//     пустого результата;
//  3. **колонка есть, но полем контракта не является** (`gateway_type`,
//     `addr_type`) — на публичную поверхность выносится внутреннее имя, и снять
//     его потом уже нельзя: арендатор на нём построил.
//
// Гейт закрывает (1) и (2) машинно. Пункт (3) машинно не закрывается — соответствие
// «имя колонки ↔ имя поля контракта» требует чтения контракта и разрешения
// расхождений вида `type`↔`addr_type`, поэтому он адъюдицируется перечислением: у
// каждого ресурса, чей список УЖЕ сужен, причина записана в коде рядом с вызовом.
//
// # Названное предусловие
//
// У таблиц маршрутизации `network_id` в белом списке обязателен: без него снятие
// `NetworkService.ListRouteTables` отнимает возможность «таблицы этой сети», не
// давая замены. Это не стиль — это порядок посадки, и он проверяется здесь, а не
// хранится в чьей-то памяти.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	pgDir        = "../../services/vpc/internal/repo/kacho/pg"
	migrDir      = "../../services/vpc/internal/migrations"
	filterGateRT = "route_tables"
)

// filterParseRe — вызов разборщика вместе со списком. Список может быть перенесён
// на следующую строку, поэтому шаблон допускает перевод строки: привязка к одной
// строке давала бы ложное «ноль находок» ровно там, где список вырос и его
// перенесли.
var filterParseRe = regexp.MustCompile(`filter\.Parse\(\s*f\.Filter\s*,\s*(?:\n\s*)?\[\]string\{([^}]*)\}`)

// fromTableRe — имя таблицы берётся из SQL того же файла, а не выводится
// склонением имени файла: склонение — догадка, а `FROM` — факт.
var fromTableRe = regexp.MustCompile(`FROM\s+([a-z_]+)`)

type filterSite struct {
	file   string
	table  string
	fields []string
}

// readFilterSites собирает места разбора фильтра из слоя хранилища.
func readFilterSites(t *testing.T) []filterSite {
	t.Helper()
	entries, err := os.ReadDir(pgDir)
	if err != nil {
		t.Fatalf("каталог слоя хранилища не прочитан (%s): %v", pgDir, err)
	}
	var out []filterSite
	var filesRead int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(pgDir, e.Name()))
		if rerr != nil {
			t.Fatalf("файл не прочитан (%s): %v", e.Name(), rerr)
		}
		filesRead++
		src := string(raw)
		m := filterParseRe.FindStringSubmatch(src)
		if m == nil {
			continue
		}
		var fields []string
		for _, f := range strings.Split(m[1], ",") {
			f = strings.TrimSpace(strings.Trim(strings.TrimSpace(f), `"`))
			if f != "" {
				fields = append(fields, f)
			}
		}
		table := ""
		if tm := fromTableRe.FindStringSubmatch(src); tm != nil {
			table = tm[1]
		}
		out = append(out, filterSite{file: e.Name(), table: table, fields: fields})
	}
	t.Logf("осмотрено: файлов слоя хранилища %d, из них разбирают фильтр %d", filesRead, len(out))
	if len(out) == 0 {
		t.Fatal("ни одного места разбора фильтра не найдено — гейт читал не то, и «ноль находок» здесь означало бы «ноль прочитанного»")
	}
	return out
}

// readColumnTypes — колонка → объявленный тип, по ВСЕМ миграциям.
//
// Первая редакция читала только базовую миграцию, и это была негодная предпосылка:
// гейт объявил находкой `placement_type` у подсети, тогда как колонка приходит
// миграцией 0012. Живого дефекта не было — был дефект гейта, и он показателен:
// проверка, читающая ЧАСТЬ источника, отчитывается находкой о том, чего не
// прочитала. Поэтому читаются все файлы миграций, и добавления колонок
// (`ALTER TABLE … ADD COLUMN`) — тоже: схема собирается ПРИМЕНЕНИЕМ, а не одним
// файлом.
func readColumnTypes(t *testing.T) map[string]map[string]string {
	t.Helper()
	entries, err := os.ReadDir(migrDir)
	if err != nil {
		t.Fatalf("каталог миграций не прочитан: %v", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	out := map[string]map[string]string{}
	createRe := regexp.MustCompile(`CREATE TABLE (?:IF NOT EXISTS )?(?:kacho_vpc\.)?([a-z_]+)`)
	colRe := regexp.MustCompile(`^\s{4}([a-z_]+)\s+([a-z0-9 ()\[\]]+)`)
	alterRe := regexp.MustCompile(`ALTER TABLE\s+(?:kacho_vpc\.)?([a-z_]+)`)
	addColRe := regexp.MustCompile(`ADD COLUMN (?:IF NOT EXISTS )?([a-z_]+)\s+([a-z0-9 ()\[\]]+)`)
	dropColRe := regexp.MustCompile(`DROP COLUMN (?:IF EXISTS )?([a-z_]+)`)

	var filesRead, addedCols, droppedCols int
	for _, n := range names {
		raw, rerr := os.ReadFile(filepath.Join(migrDir, n))
		if rerr != nil {
			t.Fatalf("миграция не прочитана (%s): %v", n, rerr)
		}
		filesRead++
		// Читаем только восходящую часть: нисходящая описывает откат и объявляет
		// обратные операции, которые к действующей схеме отношения не имеют.
		body := string(raw)
		if i := strings.Index(body, "-- +goose Down"); i >= 0 {
			body = body[:i]
		}
		var curCreate, curAlter string
		for _, line := range strings.Split(body, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "--") {
				continue
			}
			if m := createRe.FindStringSubmatch(line); m != nil {
				curCreate, curAlter = m[1], ""
				if out[curCreate] == nil {
					out[curCreate] = map[string]string{}
				}
				continue
			}
			if m := alterRe.FindStringSubmatch(line); m != nil {
				curAlter, curCreate = m[1], ""
				if out[curAlter] == nil {
					out[curAlter] = map[string]string{}
				}
			}
			if curAlter != "" {
				if m := addColRe.FindStringSubmatch(line); m != nil {
					out[curAlter][m[1]] = strings.TrimSpace(m[2])
					addedCols++
				}
				if m := dropColRe.FindStringSubmatch(line); m != nil {
					delete(out[curAlter], m[1])
					droppedCols++
				}
			}
			if curCreate != "" {
				if strings.HasPrefix(trimmed, ");") {
					curCreate = ""
					continue
				}
				if m := colRe.FindStringSubmatch(line); m != nil {
					name := m[1]
					if name == "constraint" || strings.HasPrefix(name, "primary") ||
						strings.HasPrefix(name, "unique") || strings.HasPrefix(name, "check") ||
						strings.HasPrefix(name, "exclude") || strings.HasPrefix(name, "foreign") {
						continue
					}
					out[curCreate][name] = strings.TrimSpace(m[2])
				}
			}
		}
	}
	if len(out) < 5 {
		t.Fatalf("из миграций прочитано таблиц %d — слишком мало, гейт читал не то", len(out))
	}
	t.Logf("осмотрено: файлов миграций %d, таблиц %d, добавлений колонок %d, снятий колонок %d",
		filesRead, len(out), addedCols, droppedCols)
	return out
}

func TestListFilterWhitelistFieldsAreUsableColumns(t *testing.T) {
	sites := readFilterSites(t)
	types := readColumnTypes(t)

	var checked int
	for _, s := range sites {
		if s.table == "" {
			t.Errorf("%s: имя таблицы из SQL файла не извлечено — проверить поля не с чем", s.file)
			continue
		}
		cols, ok := types[s.table]
		if !ok {
			// Таблица может быть заведена не базовой миграцией — это законно, но
			// тогда гейт о её полях не утверждает НИЧЕГО, и молчать об этом нельзя.
			t.Logf("%s: таблица %q не объявлена базовой миграцией — поля этого места не проверены", s.file, s.table)
			continue
		}
		for _, f := range s.fields {
			checked++
			typ, exists := cols[f]
			if !exists {
				t.Errorf("%s: поле фильтра %q не является колонкой таблицы %s. Имя подставляется в SQL дословно, поэтому запрос упадёт ошибкой базы на каждом вызове с этим полем",
					s.file, f, s.table)
				continue
			}
			if strings.HasPrefix(typ, "smallint") || strings.HasPrefix(typ, "integer") {
				t.Errorf("%s: поле фильтра %q — колонка типа %s: перечисление хранится ЧИСЛОМ, а арендатор пишет контрактное значение. Фильтр либо не найдёт ничего (и это неотличимо от законного пустого ответа), либо упадёт приведением типа",
					s.file, f, typ)
			}
		}
	}
	t.Logf("осмотрено: полей фильтра сверено с колонками %d", checked)
}

// TestRouteTableFilterCarriesNetworkID — НАЗВАННОЕ ПРЕДУСЛОВИЕ снятия метода
// под-перечисления таблиц маршрутизации.
//
// Порядок посадки не хранится в памяти: снятие `ListRouteTables` без этого поля
// отнимает у арендатора возможность «таблицы этой сети» и не даёт замены. У
// подсети и группы правил такое сужение есть, поэтому их снятия замену имеют.
func TestRouteTableFilterCarriesNetworkID(t *testing.T) {
	sites := readFilterSites(t)
	var found bool
	for _, s := range sites {
		if s.table != filterGateRT {
			continue
		}
		found = true
		var has bool
		for _, f := range s.fields {
			if f == "network_id" {
				has = true
			}
		}
		if !has {
			t.Errorf("%s: белый список фильтра таблиц маршрутизации не несёт network_id (%v). Снятие ListRouteTables до этого поля отнимает возможность, не давая замены",
				s.file, s.fields)
		}
		sort.Strings(s.fields)
		t.Logf("белый список таблиц маршрутизации: %v", s.fields)
	}
	if !found {
		t.Fatalf("места разбора фильтра для таблицы %q не найдено — предусловие проверить нечем", filterGateRT)
	}
}

// TestFilterWhitelistGateCanFail — положительный контроль САМОГО гейта: без него
// «ноль находок» неотличимо от «предикат ничего не ищет».
func TestFilterWhitelistGateCanFail(t *testing.T) {
	cols := map[string]map[string]string{
		"addresses": {"name": "text", "ip_version": "smallint NOT NULL DEFAULT 0", "reserved": "boolean"},
	}
	cases := []struct {
		имя     string
		поле    string
		находка bool
	}{
		{"настоящая текстовая колонка проходит", "name", false},
		{"логическая колонка проходит", "reserved", false},
		{"перечисление числом — находка", "ip_version", true},
		{"не колонка вовсе — находка", "internal_ipv4->>'address'", true},
		{"опечатка — находка", "nmae", true},
	}
	for _, c := range cases {
		t.Run(c.имя, func(t *testing.T) {
			typ, exists := cols["addresses"][c.поле]
			bad := !exists || strings.HasPrefix(typ, "smallint") || strings.HasPrefix(typ, "integer")
			if bad != c.находка {
				t.Fatalf("предикат гейта негоден: поле %q → находка=%v, ожидалось %v", c.поле, bad, c.находка)
			}
		})
	}
}
