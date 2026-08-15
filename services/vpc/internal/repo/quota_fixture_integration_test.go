// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build integration || !short

package repo_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

// Фикстура учёта числа ресурсов для проб ЭТОГО пакета.
//
// ЗАЧЕМ. Списание квоты живёт в триггере и срабатывает на КАЖДОЙ вставке строки
// ресурса. Проба, сеющая проект литералом и вставляющая адрес, получает отказ
// «потолок не назван» — и это не дефект продукта, а фикстура, отставшая от него:
// проекты здесь не создаются через iam, они просто называются строкой.
//
// ПОЧЕМУ ПЕРЕЧЕНЬ ВЫВОДИТСЯ, А НЕ ВЫПИСЫВАЕТСЯ. Соседний пакет (`repo/kacho/pg`)
// держит перечень проектов фикстуры списком из полусотни литералов, и этот
// список стареет молча: новая проба с новым именем проекта падает не на своём
// предмете, а на отсутствии строки учёта, и автор ищет дефект в продукте. Здесь
// перечень собирается ЧТЕНИЕМ проб пакета — имя, названное пробой, попадает в
// фикстуру by construction.
//
// Сборщик читает исходники, поэтому кроме литералов он знает ЕЩЁ ОДНУ форму —
// шаблон форматирования с числовым полем (`fmt.Sprintf("b1gtestproject%05d", i)`).
// Она нужна не для полноты: именно так проба конкуренции называет проекты своих
// параллельных писателей, и без неё фикстура собиралась бы тихо неполной — с
// первыми двумя именами, которые в пробах есть литералами, и без остальных.
// Такой шаблон разворачивается в диапазон `fixtureFormatSpan` значений.
//
// Цена названа: имя, собранное иначе — конкатенацией (`"prj-" + suffix`) или
// подстановкой нечислового поля, — сборщик не увидит, и проба с таким именем
// упадёт с отказом учёта. Это видно сразу и по тексту («потолок не назван»), а
// не тихо, и лечится тем, что имя проекта в пробе пишется литералом либо
// шаблоном названной выше формы.

// projectCandidate — КАНДИДАТ в имя проекта: любой строковый литерал, который
// на имя проекта похож.
//
// ПОЧЕМУ ШИРОКО, А НЕ ТОЧНО. Точный предикат здесь невозможен, и это измерено, а
// не предположено. Первая редакция ловила формы имени (`prj-…`, `project-…`,
// `b1g…`) — пропустила `f-dsg`, `f-adr`, `f-assoc-a`: 36 проб упали. Вторая
// ловила ПОЗИЦИЮ (`ProjectID: "…"`) — пропустила `prj-gwref`, `prj-namerace`,
// `listaddrprojaaaa000`, `f1`: имя приезжает то параметром SQL, то через
// переменную, то из константы пакета. Обе редакции мерили то, КАК автор пробы
// пишет имя, а это его свободный выбор.
//
// Поэтому предикат намеренно широк, и цена этого названа: фикстура заведёт
// строки учёта и для литералов, которые проектами не являются (имя сети,
// значение метки). Лишняя строка не стоит ничего — её никто не прочитает; а
// пропущенная роняет пробу на чужом предмете и уводит автора искать дефект в
// продукте. Размен выбран в эту сторону осознанно.
//
// Регистр и односимвольные имена включены НЕ для полноты: пробы называют проекты
// `P`, `prj-A`, `f1` — заглавной буквой и одной буквой. Предикат, требовавший
// строчную и минимум два символа, пропускал их, и три пробы падали на чужом
// предмете. Отсекается очевидно-не-имя: пути (`/`), адреса (`.` и `:`), CIDR,
// всё длиннее сорока символов.
var projectCandidate = regexp.MustCompile(`"([A-Za-z][A-Za-z0-9_-]{0,39})"`)

// projectCandidateSQL — то же, но в ОДИНАРНЫХ кавычках: имя, стоящее прямо в
// тексте SQL пробы (`VALUES ($1, 'f1', …)`).
//
// Отдельным выражением, а не альтернативой в одном: Go-строка и SQL-литерал
// живут в разных кавычках, и предикат, знающий только двойные, пропустил `f1` —
// один проект из тысячи, и этого хватило, чтобы четыре пробы падали на чужом
// предмете. Форма, о которой предикат не знает, стоит ровно столько же, сколько
// форма, которой не существует.
var projectCandidateSQL = regexp.MustCompile(`'([A-Za-z][A-Za-z0-9_-]{0,39})'`)

// projectFormatCandidate — имя, собранное форматированием с числовым полем:
// `fmt.Sprintf("b1gtestproject%05d", i)`.
//
// Литералом такое имя в исходниках отсутствует, поэтому широкий предикат выше
// его не видит: там строка обрывается на `%`. Именно так проба конкуренции
// называет проекты своих параллельных писателей.
var projectFormatCandidate = regexp.MustCompile(`"([A-Za-z][A-Za-z0-9_-]*)%0?(\d*)d"`)

// fixtureFormatSpan — сколько значений разворачивать у шаблона.
//
// Число с запасом и названо здесь, а не подобрано: параллельных писателей в
// пробах пакета единицы, а строка учёта стоит одну вставку.
const fixtureFormatSpan = 64

// fixtureQuotaKinds — виды, которые считает vpc. Перечень принадлежит платформе;
// здесь он нужен целиком, потому что проба может вставить строку любого вида.
var fixtureQuotaKinds = []string{
	"vpc.network", "vpc.subnet", "vpc.address", "vpc.networkInterface",
	"vpc.securityGroup", "vpc.routeTable", "vpc.gateway", "vpc.cidrGroup",
}

// fixtureQuotaLimit — потолок фикстуры. Заведомо больше, чем создаёт любая проба
// пакета: предмет здешних проб — не исчерпание, а работа механизма под ним.
// Исчерпание проверяется отдельно и намеренно, в пробах учёта.
const fixtureQuotaLimit = 10000

// collectFixtureProjects читает исходники проб пакета и собирает имена проектов.
func collectFixtureProjects(t testing.TB) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	require.NoError(t, err, "фикстура учёта: чтение каталога пакета")

	seen := make(map[string]struct{}, 64)
	filesRead := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Clean(e.Name()))
		require.NoError(t, err, "фикстура учёта: чтение %s", e.Name())
		filesRead++
		text := string(body)
		for _, m := range projectCandidate.FindAllStringSubmatch(text, -1) {
			seen[m[1]] = struct{}{}
		}
		for _, m := range projectCandidateSQL.FindAllStringSubmatch(text, -1) {
			seen[m[1]] = struct{}{}
		}
		for _, m := range projectFormatCandidate.FindAllStringSubmatch(text, -1) {
			head, width := m[1], 0
			if m[2] != "" {
				width, _ = strconv.Atoi(m[2])
			}
			for i := range fixtureFormatSpan {
				seen[head+padNumber(i, width)] = struct{}{}
			}
		}
	}

	require.NotZero(t, filesRead,
		"фикстура учёта прочитала НОЛЬ файлов проб — она собралась бы пустой и молча, "+
			"и все пробы падали бы на отказе учёта вместо своего предмета")

	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)

	require.NotEmpty(t, out,
		"фикстура учёта не нашла ни одного имени проекта в %d файле(ах) проб: "+
			"либо форма имени сменилась, либо предикат перестал её ловить", filesRead)
	return out
}

// padNumber повторяет то, что делает `%0Nd`: число, дополненное нулями слева до
// ширины N. Ширина 0 означает формат без дополнения (`%d`).
func padNumber(v, width int) string {
	s := strconv.Itoa(v)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// seedFixtureQuotas приводит базу пробы в состояние «проекты материализованы».
//
// Идёт через `kachopg.MaterializeQuotas` — тот же и единственный оператор, каким
// пользуется живой путь. Своего INSERT здесь нет намеренно: копия оператора
// разошлась бы с настоящим молча, и разошлась бы на составе столбцов.
func seedFixtureQuotas(t testing.TB, dsn string) {
	t.Helper()
	ctx := context.Background()

	projects := collectFixtureProjects(t)

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err, "фикстура учёта: подключение к базе пробы")
	defer func() { _ = conn.Close(ctx) }()

	rows := make([]kacho.QuotaRow, 0, len(projects)*len(fixtureQuotaKinds))
	for _, p := range projects {
		for _, k := range fixtureQuotaKinds {
			rows = append(rows, kacho.QuotaRow{
				CarrierType:   "project",
				CarrierID:     p,
				Kind:          k,
				Limit:         fixtureQuotaLimit,
				SourceScope:   "DEFAULT",
				SourceScopeID: "",
				LimitRevision: 0,
				// Зеркало аккаунта непусто: схема отвергает пустое, и отвергает
				// правильно — строка без зеркала невидима аккаунтной дельте.
				AccountID: "acc-fixture",
			})
		}
	}

	n, err := kachopg.MaterializeQuotas(ctx, conn, rows)
	require.NoError(t, err, "фикстура учёта: заведение строк")
	require.Equal(t, int64(len(rows)), n,
		"перепись: объявлено строк %d, заведено %d. Расхождение означает, что часть "+
			"идентичностей уже существовала, то есть фикстура работает не на свежей базе",
		len(rows), n)

	t.Logf("фикстура учёта: проектов %d, видов %d, строк заведено %d",
		len(projects), len(fixtureQuotaKinds), n)
}
