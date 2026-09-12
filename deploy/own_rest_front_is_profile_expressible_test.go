// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// own_rest_front_is_profile_expressible_test.go — «ФРОНТ НЕ ПОДНЯТ» ОБЯЗАНО
// БЫТЬ ВЫРАЗИМО ПРОФИЛЕМ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Умолчание адреса собственного REST-фронта снято у ПРОЦЕССА осознанно, и
// комментарий рядом с рендером это объявляет: «адрес, приходящий умолчанием
// процесса, непуст всегда — профиль, о ребре умолчавший, поднимал бы его молча».
//
// У ЧАРТА умолчание при этом осталось — номером порта. Свойство, которое
// комментарий обещает, не выполнялось: шаблон собирал адрес из порта, у которого
// есть умолчание, поэтому ВСЯКАЯ посадка поднимала фронт, ничего о нём не
// сказав. Состояние «фронта на этом стенде нет» профилем не выражалось вовсе.
//
// Тот же класс во втором чарте службы, и там он виден особенно ясно: комментарий
// над ключом дословно говорит «Здесь умолчания нет», а следующая строка его
// задаёт. Два места об одном предмете, из которых верно то, у которого есть
// читатель.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ СУДЯТСЯ ТРИ МЕСТА, А НЕ ОДНО
//
// Фронт приезжает в кластер ТРЕМЯ объявлениями, и снятие умолчания у одного из
// них состояния не производит:
//
//	адрес в настройках   — по нему процесс решает, поднимать ли слушателя;
//	порт контейнера      — по нему под объявляет, что у него есть эта дверь;
//	порт Service         — по нему до двери вообще можно дозвониться.
//
// Профиль, снявший адрес и оставивший порт контейнера, даёт под, объявляющий
// дверь, за которой никто не слушает; снявший порт Service и оставивший адрес —
// слушателя, до которого не дозвониться. Оба состояния выглядят исправными в
// своей половине, поэтому проверка требует ОДНОГО условия на все три места.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СЧИТАЕТСЯ УМОЛЧАНИЕМ — И ПОЧЕМУ ВСТРОЕННОЕ В ШАБЛОН ТОЖЕ
//
// Умолчанием является не только ключ в файле значений, но и запасное значение
// внутри шаблона (`| default 9098`). Второе опаснее первого: его не видно в
// файле значений, поэтому оператор, снявший ключ, получает фронт всё равно — и
// ищет причину не там.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТА ПРОВЕРКА НЕ ДЕЛАЕТ — сказано прямо
//
//   - она НЕ требует, чтобы фронт был выключен: сегодня его поднимают ВСЕ
//     стенды, и это их решение. Требуется, чтобы решение было ОБЪЯВЛЕНО, а не
//     получено умолчанием;
//   - она НЕ судит транспорт фронта и не сверяет его с полистенными ручками —
//     это соседний предмет со своими проверками;
//   - она читает ОБЪЯВЛЕНИЯ, а не рендер: рендер требует собранных зависимостей
//     и умеет ПРОПУСТИТЬСЯ, а пропустившаяся проверка неотличима от прошедшей.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// restFront — один собственный REST-фронт службы.
type restFront struct {
	name      string   // как о нём говорить в находке
	portName  string   // имя порта в описании пода и Service
	configKey string   // ключ адреса в настройках процесса
	declKeys  []string // ключи значений, ОБЪЯВЛЯЮЩИЕ этот фронт
}

var ownRestFronts = []restFront{
	{
		name: "публичный", portName: "http-rest", configKey: "rest-endpoint",
		declKeys: []string{"ports.rest", "service.public.restPort", "apiServer.restEndpoint"},
	},
	{
		name: "внутренний", portName: "http-rest-int", configKey: "internal-rest-endpoint",
		declKeys: []string{"ports.internalRest", "service.internal.internalRestPort", "apiServer.internalRestEndpoint"},
	},
}

// restFrontRender — одно место, где фронт материализуется.
type restFrontRender struct {
	path    string
	line    int
	text    string
	front   string   // имя фронта
	guards  []string // условия, охватывающие эту строку
	inlined string   // встроенное умолчание, если есть
}

// restFrontDefault — умолчание фронта, объявленное файлом значений чарта.
type restFrontDefault struct {
	chart string
	key   string
	front string
}

type restFrontCensus struct {
	templatesRead int // шаблонов прочитано
	rendersJudged int // мест материализации рассмотрено
	guarded       int // из них обусловленных объявлением
	valuesRead    int // файлов значений чартов прочитано
	stacks        int // стеков рассмотрено
	stacksPublic  int // из них объявивших публичный фронт
	stacksInt     int // из них объявивших внутренний фронт
}

var (
	inlineDefault = regexp.MustCompile(`\|\s*default\s+(\d{4})`)
	ifOpen        = regexp.MustCompile(`{{-?\s*if\s`)
	blockEnd      = regexp.MustCompile(`{{-?\s*end\s*-?}}`)
	elseBranch    = regexp.MustCompile(`{{-?\s*else`)
)

// collectRestFrontRenders разбирает ТЕКСТ шаблона и отдаёт места, где фронт
// материализуется, вместе с охватывающими их условиями.
//
// Функция чистая: тем же входом её кормит инъекция.
func collectRestFrontRenders(path, text string) []restFrontRender {
	var (
		out      []restFrontRender
		guards   []string
		pending  string // имя фронта, чей блок портов только что открылся
		out2     []restFrontRender
		lastPort string
	)
	_ = out2
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)

		// Стек условий: он нужен, чтобы ответить «обусловлена ли эта строка».
		if ifOpen.MatchString(line) {
			guards = append(guards, trimmed)
		} else if blockEnd.MatchString(line) && len(guards) > 0 {
			guards = guards[:len(guards)-1]
		} else if elseBranch.MatchString(line) && len(guards) > 0 {
			guards[len(guards)-1] = guards[len(guards)-1] + " (иначе)"
		}

		if strings.HasPrefix(trimmed, "#") {
			continue // проза о предмете предметом не является
		}

		// Порт: `- name: <имя>` открывает запись, следующие строки её несут.
		if m := strings.TrimPrefix(trimmed, "- name: "); m != trimmed {
			lastPort = strings.TrimSpace(m)
			pending = ""
			for _, f := range ownRestFronts {
				if lastPort == f.portName {
					pending = f.name
				}
			}
			continue
		}
		if pending != "" && (strings.HasPrefix(trimmed, "port:") || strings.HasPrefix(trimmed, "containerPort:")) {
			r := restFrontRender{path: path, line: i + 1, text: trimmed, front: pending,
				guards: append([]string{}, guards...)}
			if m := inlineDefault.FindStringSubmatch(trimmed); m != nil {
				r.inlined = m[1]
			}
			out = append(out, r)
			continue
		}

		// Адрес в настройках процесса.
		for _, f := range ownRestFronts {
			if !strings.HasPrefix(trimmed, f.configKey+":") {
				continue
			}
			r := restFrontRender{path: path, line: i + 1, text: trimmed, front: f.name,
				guards: append([]string{}, guards...)}
			if m := inlineDefault.FindStringSubmatch(trimmed); m != nil {
				r.inlined = m[1]
			}
			out = append(out, r)
		}
	}
	return out
}

// frontOf возвращает описание фронта по его имени.
func frontOf(name string) restFront {
	for _, f := range ownRestFronts {
		if f.name == name {
			return f
		}
	}
	return restFront{}
}

// auditOwnRestFront судит места материализации и умолчания значений.
func auditOwnRestFront(renders []restFrontRender, defaults []restFrontDefault, census restFrontCensus) ([]string, restFrontCensus) {
	var findings []string

	for _, r := range renders {
		census.rendersJudged++
		f := frontOf(r.front)

		guarded := false
		for _, g := range r.guards {
			for _, k := range f.declKeys {
				if strings.Contains(g, k) {
					guarded = true
				}
			}
		}
		if guarded {
			census.guarded++
		} else {
			findings = append(findings, fmt.Sprintf(
				"%s:%d — %s REST-фронт материализуется БЕЗУСЛОВНО (%s). Профиль, о нём "+
					"умолчавший, поднимает его молча: состояние «фронта на этом стенде нет» "+
					"не выражается вовсе",
				r.path, r.line, f.name, r.text))
		}
		if r.inlined != "" {
			findings = append(findings, fmt.Sprintf(
				"%s:%d — %s REST-фронт несёт ВСТРОЕННОЕ в шаблон умолчание порта %s. "+
					"Оператор, снявший ключ в файле значений, получит фронт всё равно и "+
					"будет искать причину не там",
				r.path, r.line, f.name, r.inlined))
		}
	}

	for _, d := range defaults {
		findings = append(findings, fmt.Sprintf(
			"%s — умолчание чарта %s объявляет %s REST-фронт за профиль. Умолчание снято у "+
				"ПРОЦЕССА именно затем, чтобы решение принимал профиль; умолчание чарта "+
				"возвращает то же самое одним слоем выше",
			d.chart, d.key, d.front))
	}

	sort.Strings(findings)
	return findings, census
}

// restFrontCharts — чарты этого дерева, поставляющие службу.
//
// Здесь стояли ДВА чарта — подчарт умбреллы и собственный чарт службы, — и
// перечень нёс оговорку «оба, а не тот, где дефект заметили: класс чинится по
// свойству, а не по месту находки». Оговорка остаётся нормой, а второго чарта
// в этом дереве больше НЕТ: служба вынесена отдельным продуктом (задача #1111),
// её собственный чарт уехал вместе с каталогом `services/iam`.
//
// Свойство при этом не сузилось до «того места, где заметили»: подчарт умбреллы
// — ЕДИНСТВЕННАЯ поставка службы, оставшаяся здесь, то есть перечень
// по-прежнему полон по своему предмету. Второй чарт судит теперь его
// собственное дерево, и обеих сторон равенства здесь взяться неоткуда.
//
// Обход отказывает на пустом перечне (`census.templatesRead == 0` ниже):
// «ноль находок» обязано быть отличимо от «ноль прочитанного».
var restFrontCharts = []string{
	filepath.Join(umbrellaDir, "charts", "kaname"),
}

func readOwnRestFront(t *testing.T) ([]restFrontRender, []restFrontDefault, restFrontCensus) {
	t.Helper()
	var (
		renders  []restFrontRender
		defaults []restFrontDefault
		census   restFrontCensus
	)
	for _, chart := range restFrontCharts {
		tpl := filepath.Join(chart, "templates")
		entries, err := os.ReadDir(tpl)
		require.NoError(t, err)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(tpl, e.Name()))
			require.NoError(t, err)
			census.templatesRead++
			renders = append(renders, collectRestFrontRenders(filepath.Join(tpl, e.Name()), string(raw))...)
		}

		values := readYAML(t, filepath.Join(chart, "values.yaml"))
		census.valuesRead++
		for _, f := range ownRestFronts {
			for _, k := range f.declKeys {
				if _, ok := leafString(values, strings.Split(k, ".")); ok {
					defaults = append(defaults, restFrontDefault{chart: chart, key: k, front: f.name})
				}
			}
		}
	}
	return renders, defaults, census
}

// TestOwnRestFrontIsExpressibleByTheProfile — гейт класса.
func TestOwnRestFrontIsExpressibleByTheProfile(t *testing.T) {
	renders, defaults, census := readOwnRestFront(t)

	// Перепись по стекам: обе величины, а не одна. «Стенды объявляют фронт» и
	// «стендов столько-то» вместе отвечают на вопрос, который одна не закрывает.
	stacks := deployStacks(t)
	subchart := readYAML(t, filepath.Join(umbrellaDir, "charts", "kaname", "values.yaml"))
	for name := range stacks {
		census.stacks++
		tree := effectiveValues(t, stacks[name])
		merged := mergeValues(mergeValues(map[string]any{}, subchart), asMapOrEmpty(tree["kaname"]))
		if _, ok := leafString(merged, []string{"ports", "rest"}); ok {
			census.stacksPublic++
		}
		if _, ok := leafString(merged, []string{"ports", "internalRest"}); ok {
			census.stacksInt++
		}
	}

	findings, census := auditOwnRestFront(renders, defaults, census)

	t.Logf("перепись: шаблонов %d · файлов значений чартов %d · мест материализации %d "+
		"(обусловленных объявлением %d) · стеков %d (объявили публичный фронт %d, "+
		"внутренний %d)",
		census.templatesRead, census.valuesRead, census.rendersJudged, census.guarded,
		census.stacks, census.stacksPublic, census.stacksInt)

	require.NotZero(t, census.templatesRead, "шаблонов не прочитано — вердикт беспредметен")
	require.NotZero(t, census.rendersJudged,
		"мест материализации фронта не найдено ни одного — распознаватель перестал их "+
			"узнавать, и это НЕ «фронта нет»")
	require.NotZero(t, census.stacks, "стеков не прочитано — вердикт беспредметен")

	if len(findings) > 0 {
		t.Fatalf("состояние «собственный REST-фронт не поднят» не производится профилем — "+
			"%d находок:\n  %s", len(findings), strings.Join(findings, "\n  "))
	}

	require.NotZero(t, census.stacksPublic,
		"положительный контроль пуст: публичный фронт не объявил НИ ОДИН стек — "+
			"условие выше выполнилось бы и на дереве, где фронта нет вовсе")
}

// asMapOrEmpty — узел как отображение либо пустое отображение. Отсутствие ключа
// и ключ с не-отображением здесь означают одно: накладывать нечего.
//
// Жил в гейте имени базы службы доступа; тот снят вместе со своим предметом
// (чарт-тёзка уехал в репозиторий службы), а помощник переехал к единственному
// оставшемуся вызывающему.
func asMapOrEmpty(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
