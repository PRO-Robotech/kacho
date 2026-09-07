// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// r7_3_32_engine_landing_retired_test.go — R7-3-32: у ВНЕШНЕГО ДВИЖКА ОТНОШЕНИЙ
// снята не только связка вызовов, но и ПОСАДКА.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Движок прав жил в дереве развёртывания шире, чем строкой зависимости: свой
// подчарт с шаблонами (задание начальной настройки, права для него, секрет с
// идентификатором модели, карта-заготовка модели, бюджет недоступности, задание
// подготовки его базы), выделенная база отдельной зависимостью с псевдонимом,
// ручки в каждом профиле и рубильник источника вердикта у службы прав.
//
// ПОДЧАРТ НАМЕРЕННО НЕ ОБЪЯВЛЯЛСЯ ЗАВИСИМОСТЬЮ (см. шапку
// helm/umbrella/Chart.yaml): каталог сабчартов helm загружает ЦЕЛИКОМ. Значит
// «его нет в списке зависимостей» не значит ничего, и проверять надо КАТАЛОГ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПО ОБЪЯВЛЕНИЮ, А НЕ ПО РЕНДЕРУ
//
// Та же причина, что у соседних проверок этого каталога: рендер требует `helm`
// и скачанных зависимостей, поэтому проба на рендере умеет ПРОПУСТИТЬСЯ — и
// пропустится ровно тогда, когда нужна. Здесь читается то, что лежит в дереве.
//
// ─────────────────────────────────────────────────────────────────────────────
// КАК ЧИТАЕТСЯ ПРОФИЛЬ — РАЗБОРОМ, А НЕ ПОИСКОМ ПОДСТРОКИ
//
// Профиль разбирается как YAML, поэтому под проверку попадают ИМЕНА КЛЮЧЕЙ и
// ЗНАЧЕНИЯ, а комментарии отбрасываются разбором by construction. Это и есть
// граница между находкой и законным близнецом: упоминание движка в ПРОЗЕ
// (надгробие с причиной, историческая справка, разбор снятого) проверку не
// красит — красит его ДЕЙСТВУЮЩЕЕ объявление.
//
// Шаблоны разбору как YAML не поддаются (это go-шаблоны), поэтому у них
// читается исполняемая часть: строки-комментарии `#` и блоки `{{/* … */}}`
// отбрасываются до поиска.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТА ПРОБА НЕ УТВЕРЖДАЕТ (граница названа, чтобы «зелено» не читалось шире)
//
//   - она ничего не говорит про КОД служб: снятие вызовов движка — предмет
//     дерева `services/**`, и там свои гейты;
//   - она не запрещает МОДЕЛЬ прав (`fga_model.fga`) — модель осталась
//     источником истины ФОРМЫ, её разбирает сама служба. Снято то, что требует
//     ЖИВОГО движка, а не то, что называет модель;
//   - она не запрещает журнал намерений `kaname.fga_outbox` и его
//     обслуживание (автовакуум, наблюдаемость) — журнал сохранён решением Р7.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// r7RetiredSubchartDir — каталог подчарта начальной настройки движка.
// Корень зонтичного чарта — общая для этого каталога константа `umbrellaDir`
// (posture_parity_test.go): второй копии координаты здесь не заводится.
const r7RetiredSubchartDir = "helm/umbrella/charts/openfga-bootstrap"

// r7RetiredKeys — ИМЕНА КЛЮЧЕЙ, снятых вместе с движком. Ключ, а не подстрока:
// проверяется объявление, а не текст.
//
// Рубильник источника вердикта (`verdictFormTypes` / `shadowCompare`) уходит
// вместе с движком не «заодно»: он выбирал, ЧЬЁ решение о доступе считать
// действующим, а второй стороны сравнения больше не существует.
var r7RetiredKeys = map[string]string{
	"openfga":            "внешний движок отношений (внешний чарт)",
	"openfga-bootstrap":  "подчарт начальной настройки движка",
	"pg-openfga":         "выделенная база движка",
	"openfgaBootstrap":   "ручки задания начальной настройки движка",
	"openfgaPdb":         "бюджет недоступности движка",
	"openfgaPodSelector": "селектор пода движка в сетевой политике",
	"waitForExtAuth":     "init-контейнер ожидания движка",
	"verdictFormTypes":   "рубильник источника вердикта (сравнивать больше не с чем)",
	"shadowCompare":      "теневая сверка двух форм вердикта (второй формы нет)",
}

// r7RetiredTemplateTokens — то, чем посадка ПРОВЯЗЫВАЛА движок в шаблонах.
// Ключи конфигурации здесь записаны в той форме, в какой их читает шаблон и в
// какой они уезжают в конфигурацию службы.
var r7RetiredTemplateTokens = []string{
	"openfga",
	"verdictFormTypes",
	"shadowCompare",
	"verdict-form-types",
	"shadow-compare",
	"waitForExtAuth",
}

// r7TmplComment — блочный комментарий go-шаблона. Отбрасывается до поиска: гейт
// обязан читать исполняемую часть, иначе он покраснеет на объяснении самого
// себя и останется красным после починки.
var r7TmplComment = regexp.MustCompile(`(?s)\{\{-?\s*/\*.*?\*/\s*-?\}\}`)

// r7ExecutablePart — текст без строк-комментариев и без блочных комментариев
// шаблона.
func r7ExecutablePart(text string) string {
	text = r7TmplComment.ReplaceAllString(text, "")
	out := make([]string, 0, 64)
	for _, ln := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "#") {
			continue
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

// r7WalkYAML обходит разобранный документ и зовёт fn на каждом узле, передавая
// путь координатой. Путь нужен, чтобы находка НАЗЫВАЛА место, а не факт.
func r7WalkYAML(path string, node any, fn func(path string, key string, val any)) {
	switch v := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child := path + "." + k
			fn(child, k, v[k])
			r7WalkYAML(child, v[k], fn)
		}
	case []any:
		for i, item := range v {
			child := fmt.Sprintf("%s[%d]", path, i)
			fn(child, "", item)
			r7WalkYAML(child, item, fn)
		}
	}
}

// r7ReadTreeFile читает файл дерева. Нечитаемая координата — ОТКАЗ: проверка
// обязана падать на переезде файла, а не тихо переставать что-либо читать.
func r7ReadTreeFile(t *testing.T, rel string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Clean(rel))
	require.NoErrorf(t, err, "координата %q не резолвится", rel)
	require.NotEmptyf(t, body, "файл %q пуст — читать нечего, и это находка, а не ноль находок", rel)
	return string(body)
}

// r7LandingProfiles — профили зонтичного чарта и значения его вендоренных подчартов.
// ВЫВОДЯТСЯ обходом каталога, а не выписываются: рукописный перечень отстаёт
// молча, и отстанет он на профиле, который снятое значение как раз и сохранил.
func r7LandingProfiles(t *testing.T) []string {
	t.Helper()
	var out []string

	entries, err := os.ReadDir(umbrellaDir)
	require.NoError(t, err, "каталог зонтичного чарта не читается — судить не о чем")
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasPrefix(n, "values") && (strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml")) {
			out = append(out, filepath.Join(umbrellaDir, n))
		}
	}

	// Вендоренные подчарты — тоже посадка: рубильник источника вердикта жил
	// именно в значениях подчарта службы прав, а не в профиле зонтика.
	charts, err := os.ReadDir(filepath.Join(umbrellaDir, "charts"))
	require.NoError(t, err, "каталог сабчартов не читается — судить не о чем")
	for _, c := range charts {
		if !c.IsDir() {
			continue
		}
		v := filepath.Join(umbrellaDir, "charts", c.Name(), "values.yaml")
		if _, err := os.Stat(v); err == nil {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// r7LandingTemplates — шаблоны зонтика и его вендоренных подчартов плюс их файлы
// полезной нагрузки (`files/`). Тоже обходом, не перечнем.
func r7LandingTemplates(t *testing.T) []string {
	t.Helper()
	var out []string
	roots := []string{filepath.Join(umbrellaDir, "templates")}
	charts, err := os.ReadDir(filepath.Join(umbrellaDir, "charts"))
	require.NoError(t, err, "каталог сабчартов не читается — судить не о чем")
	for _, c := range charts {
		if !c.IsDir() {
			continue
		}
		roots = append(roots,
			filepath.Join(umbrellaDir, "charts", c.Name(), "templates"),
			filepath.Join(umbrellaDir, "charts", c.Name(), "files"))
	}
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			out = append(out, p)
			return nil
		})
		require.NoError(t, err, "обход %s сорвался — объём осмотренного неизвестен", root)
	}
	sort.Strings(out)
	return out
}

// TestR7_3_32_EngineSubchartAndDedicatedDatabaseAreGone — каталог подчарта и
// объявление выделенной базы.
//
// ДВА РАЗНЫХ УТВЕРЖДЕНИЯ, и оба обязательны: подчарт зависимостью НЕ объявлялся
// (helm грузит каталог целиком), а база — объявлялась псевдонимом. Проверить
// одно вместо другого значит проверить половину.
func TestR7_3_32_EngineSubchartAndDedicatedDatabaseAreGone(t *testing.T) {
	// (1) каталога подчарта в дереве нет.
	if st, err := os.Stat(r7RetiredSubchartDir); err == nil {
		require.Failf(t, "подчарт движка прав остался в дереве",
			"каталог %s существует (%v). helm загружает каталог сабчартов ЦЕЛИКОМ, поэтому "+
				"отсутствие строки в dependencies НЕ означает, что подчарт не поедет: он поедет. "+
				"Снимать надо каталог.", r7RetiredSubchartDir, st.Mode())
	}

	// (2) в объявлении зонтичного чарта нет ни движка, ни его выделенной базы.
	chartPath := filepath.Join(umbrellaDir, "Chart.yaml")
	var chart struct {
		Dependencies []struct {
			Name       string `yaml:"name"`
			Alias      string `yaml:"alias"`
			Repository string `yaml:"repository"`
		} `yaml:"dependencies"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(r7ReadTreeFile(t, chartPath)), &chart),
		"объявление зонтичного чарта не разобрано — судить не о чем")
	require.NotEmpty(t, chart.Dependencies,
		"в %s не разобрано НИ ОДНОЙ зависимости — сломан разбор, а не дерево: "+
			"«ноль находок» здесь было бы «ноль прочитанного»", chartPath)

	var found []string
	for _, d := range chart.Dependencies {
		for _, s := range []string{d.Name, d.Alias, d.Repository} {
			if strings.Contains(strings.ToLower(s), "openfga") {
				found = append(found, fmt.Sprintf("name=%q alias=%q repository=%q", d.Name, d.Alias, d.Repository))
				break
			}
		}
	}
	require.Emptyf(t, found,
		"в %s осталась зависимость движка прав либо его выделенной базы:\n  %s\n"+
			"Движок снят целиком (S6 эпика #747): решение о доступе вычисляет реляционная "+
			"форма в собственной базе службы прав.", chartPath, strings.Join(found, "\n  "))

	t.Logf("осмотрено: объявление зонтичного чарта, зависимостей разобрано %d", len(chart.Dependencies))
}

// TestR7_3_32_NoProfileNamesTheRetiredLanding — ни один профиль не называет
// снятые значения.
//
// Профиль, сохранивший их, — НАХОДКА, а не безобидный остаток: helm принимает
// незнакомый ключ молча, поэтому такой профиль выглядит исправным и объявляет
// посадку, которой в дереве больше нет. Ровно тот класс, что «принято и
// проигнорировано», только на стороне развёртывания.
func TestR7_3_32_NoProfileNamesTheRetiredLanding(t *testing.T) {
	profiles := r7LandingProfiles(t)
	require.NotEmpty(t, profiles, "не найдено ни одного профиля — осматривать нечего")

	type finding struct{ file, path, why string }
	var findings []finding
	bytesRead := 0

	for _, p := range profiles {
		body := r7ReadTreeFile(t, p)
		bytesRead += len(body)

		var doc any
		require.NoErrorf(t, yaml.Unmarshal([]byte(body), &doc),
			"профиль %s не разобран как YAML — судить о нём нечем", p)

		r7WalkYAML("", doc, func(path, key string, val any) {
			if why, ok := r7RetiredKeys[key]; ok {
				findings = append(findings, finding{p, strings.TrimPrefix(path, "."), why})
				return
			}
			// Значение-строка тоже объявление: имя секрета, строка соединения,
			// метка селектора, член перечня баз под ssl-полом.
			if s, ok := val.(string); ok && strings.Contains(strings.ToLower(s), "openfga") {
				findings = append(findings, finding{p, strings.TrimPrefix(path, "."),
					fmt.Sprintf("значение %q называет снятый движок", s)})
			}
		})
	}

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного».
	t.Logf("осмотрено: профилей прочитано %d, байт %d", len(profiles), bytesRead)
	for _, p := range profiles {
		t.Logf("  профиль %s", p)
	}

	if len(findings) == 0 {
		return
	}
	var b strings.Builder
	for _, f := range findings {
		fmt.Fprintf(&b, "\n  %s: %s — %s", f.file, f.path, f.why)
	}
	require.Failf(t, "профиль развёртывания называет снятую посадку движка прав",
		"%s\n\nДвижок снят целиком (S6 эпика #747). Профиль, сохранивший его значения, — "+
			"находка: helm молча принимает ключ, которого никто не читает, поэтому такой "+
			"профиль ОБЪЯВЛЯЕТ посадку, которой в дереве нет, и читается как действующая.",
		b.String())
}

// TestR7_3_32_NoTemplateWiresTheRetiredEngine — посадка не осталась в шаблонах.
//
// ЭТО И ЕСТЬ ТА ПОЛОВИНА, РАДИ КОТОРОЙ ПРОБА НАПИСАНА: «код движка снят, а
// посадка осталась» выглядит как исправное дерево — контракт чистый, службы
// собираются, а шаблон продолжает провязывать секрет, init-контейнер и
// переменные окружения того, чего больше нет.
//
// Читается ИСПОЛНЯЕМАЯ часть: строки-комментарии и блоки `{{/* … */}}`
// отбрасываются. Законный близнец — упоминание движка в прозе (надгробие с
// причиной) — обязан остаться незамеченным.
func TestR7_3_32_NoTemplateWiresTheRetiredEngine(t *testing.T) {
	templates := r7LandingTemplates(t)
	require.NotEmpty(t, templates, "не найдено ни одного шаблона — осматривать нечего")

	var findings []string
	bytesRead := 0
	for _, p := range templates {
		body := r7ReadTreeFile(t, p)
		bytesRead += len(body)
		code := r7ExecutablePart(body)
		low := strings.ToLower(code)
		for _, tok := range r7RetiredTemplateTokens {
			if !strings.Contains(low, strings.ToLower(tok)) {
				continue
			}
			for i, ln := range strings.Split(code, "\n") {
				if strings.Contains(strings.ToLower(ln), strings.ToLower(tok)) {
					findings = append(findings, fmt.Sprintf("%s:%d: %s", p, i+1, strings.TrimSpace(ln)))
				}
			}
		}
	}

	t.Logf("осмотрено: шаблонов прочитано %d, байт %d", len(templates), bytesRead)

	if len(findings) == 0 {
		return
	}
	sort.Strings(findings)
	require.Failf(t, "шаблон развёртывания всё ещё провязывает снятый движок прав",
		"\n  %s\n\nКод движка снят (S6 эпика #747), а посадка осталась: шаблон объявляет "+
			"секрет, контейнер или переменную того, чего в дереве больше нет. Снаружи это "+
			"неотличимо от исправного дерева — поэтому проверка и стоит здесь.",
		strings.Join(findings, "\n  "))
}
