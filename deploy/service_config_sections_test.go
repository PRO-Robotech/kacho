// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// service_config_sections_test.go — секция, которую чарт рендерит в конфигурацию
// сервиса, обязана иметь разбор у этого сервиса.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Конфигурация сервиса приезжает файлом: чарт рендерит `config.yaml`, бинарь
// читает его випером и раскладывает в структуру. Виперу нет дела до секции,
// которой в структуре нет, — он её МОЛЧА отбрасывает. Для оператора это
// неотличимо от работающей настройки: ключ объявлен в значениях чарта, шаблон
// его подставляет, рендер зелёный, в манифесте секция видна. Он правит строку и
// не получает ничего.
//
// Это тот же класс, что «принято-и-проигнорировано» у поля запроса
// (`api-conventions.md`), только этажом ниже — и исходов у него ровно три:
// разбирать · перестать рендерить · снять с контракта чарта. Молча принять и
// выбросить исходом НЕ является.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ ЭТО ОТЛИЧАЕТСЯ ОТ СОСЕДНЕЙ ПРОВЕРКИ ТОГО ЖЕ СЕМЕЙСТВА
//
// `internal/repohygiene` TestDeclaredKnobHasAReader спрашивает «есть ли у ключа
// профиля читатель в шаблоне». Здесь читатель ЕСТЬ — шаблон ключ подставляет.
// Вопрос этой проверки другой: доезжает ли подставленное до процесса. Ни одна из
// двух не покрывает предмет другой.
//
// ─────────────────────────────────────────────────────────────────────────────
// НАПРАВЛЕНИЕ ОТРИЦАНИЯ — ОДНО, И ЭТО НАМЕРЕННО
//
// Секция без ключа в структуре — находка. Ключ структуры без секции в шаблоне —
// МОЛЧАНИЕ: у виперовой конфигурации есть умолчания и переменные окружения, и
// сервис вправе читать ключ, которого чарт не объявляет (nlb: `internal-lifecycle`
// разбирается и не рендерится). Проверка, требующая паритета в обе стороны,
// покраснела бы на законной конструкции — и была бы снята первой же правкой.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР
//
// Та же причина, что у соседних posture_parity_test.go и dbtls_declaration_test.go:
// проверке не нужны ни `helm`, ни скачанные зависимости чартов, поэтому она не
// умеет пропуститься. Имена секций в этих шаблонах — литералы, не выражения;
// рендер их не меняет.
//
// ─────────────────────────────────────────────────────────────────────────────
// КАК СВЯЗЫВАЕТСЯ ЧАРТ С СЕРВИСОМ — ПО ДЕРЕВУ, НЕ ПО СПИСКУ
//
// Перечня чартов здесь нет. Обход находит их сам:
//
//	Chart.yaml → templates/*.yaml → путь вида /etc/kacho-<svc>/config.yaml
//	  → volumeMount с этим mountPath → имя тома → его configMap
//	  → шаблон, чей metadata.name совпадает → секции блока config.yaml
//	  → services/<svc>/…/config/config.go → теги mapstructure структуры Config
//
// Отсюда два следствия. Новый сервис со своей конфигурацией попадает под
// проверку сам. И конфигурация ЧУЖОГО бинаря (OPA-пристяжка монтирует свою в
// /etc/opa) под неё не попадает by construction — не по имени файла, а потому
// что её не читает наш процесс.
package deploy_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/servicelayout"
)

// Корень дерева читается от каталога deploy — константа `repoRoot` объявлена
// соседним stack_table_test.go того же пакета и здесь не переобъявляется.

// ─────────────────────────────────────────────────────────────────────────────
// Чистый предикат. Отделён от обхода дерева, чтобы его способность упасть
// показывалась инъекцией синтетики (TestConfigSectionPredicateCatchesAnOrphan),
// а не тем, что когда-то падало на дереве.

// orphanSections — секции, которые рендерит шаблон и не разбирает сервис.
// Порядок сохраняется тот, в котором они стоят в шаблоне: он и есть координата
// для оператора, читающего файл сверху вниз.
func orphanSections(rendered, declared []string) []string {
	known := make(map[string]bool, len(declared))
	for _, d := range declared {
		known[d] = true
	}
	var out []string
	for _, r := range rendered {
		if !known[r] {
			out = append(out, r)
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Чтение дерева.

// serviceConfigChart — один чарт, рендерящий конфигурацию нашего сервиса.
type serviceConfigChart struct {
	// chartDir — каталог чарта относительно корня дерева.
	chartDir string
	// service — имя сервиса, выведенное из пути конфигурации (/etc/kacho-<svc>).
	service string
	// configMapFile — шаблон, чей ConfigMap монтируется как эта конфигурация.
	configMapFile string
	// sections — верхнеуровневые ключи блока `config.yaml`, в порядке файла.
	sections []string
}

var (
	// reConfigPath — путь конфигурации нашего бинаря. Он же называет сервис.
	// Форма одна на все чарты, хотя доносится по-разному: env CONFIG_PATH у
	// iam/vpc, аргумент `--config` у nlb.
	reConfigPath = regexp.MustCompile(`/etc/kacho-([a-z0-9][a-z0-9-]*)/config\.yaml`)
	// reListItemName — элемент списка, начинающийся с имени (том, монтирование).
	reListItemName = regexp.MustCompile(`^\s*-\s+name:\s*(\S+)\s*$`)
	// reMountPath — монтирование каталога.
	reMountPath = regexp.MustCompile(`^\s*mountPath:\s*"?([^"\s]+)"?\s*$`)
	// reMetaName — metadata.name объекта (значение может быть выражением helm).
	reMetaName = regexp.MustCompile(`^\s{2}name:\s*(.+?)\s*$`)
	// reTopKey — верхнеуровневый ключ блока конфигурации.
	reTopKey = regexp.MustCompile(`^([a-z][a-z0-9._-]*):`)
)

// chartTemplates — шаблоны чарта, отсортированные по имени (детерминизм обхода).
func chartTemplates(t *testing.T, chartDir string) []string {
	t.Helper()
	dir := filepath.Join(repoRoot, chartDir, "templates")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if n := e.Name(); strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml") {
			out = append(out, filepath.Join(chartDir, "templates", n))
		}
	}
	sort.Strings(out)
	return out
}

func readFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		t.Fatalf("прочитать %s: %v", rel, err)
	}
	return string(b)
}

// configMapNameForMount — имя ConfigMap (как оно записано в шаблоне, включая
// выражения helm), чей том смонтирован в mountDir. Пустая строка означает, что
// связь не прослеживается — и это находка вызывающего, а не молчание.
func configMapNameForMount(manifest, mountDir string) string {
	lines := strings.Split(manifest, "\n")

	// Шаг 1: mountPath: <mountDir> → имя тома (ближайший `- name:` выше).
	volume := ""
	for i, ln := range lines {
		m := reMountPath.FindStringSubmatch(ln)
		if m == nil || strings.TrimRight(m[1], "/") != strings.TrimRight(mountDir, "/") {
			continue
		}
		for j := i; j >= 0 && j > i-6; j-- {
			if n := reListItemName.FindStringSubmatch(lines[j]); n != nil {
				volume = n[1]
				break
			}
		}
		if volume != "" {
			break
		}
	}
	if volume == "" {
		return ""
	}

	// Шаг 2: том с этим именем → его configMap.name.
	for i, ln := range lines {
		n := reListItemName.FindStringSubmatch(ln)
		if n == nil || n[1] != volume {
			continue
		}
		inConfigMap := false
		for j := i + 1; j < len(lines) && j < i+8; j++ {
			s := strings.TrimSpace(lines[j])
			if s == "configMap:" {
				inConfigMap = true
				continue
			}
			if inConfigMap && strings.HasPrefix(s, "name:") {
				return strings.TrimSpace(strings.TrimPrefix(s, "name:"))
			}
			if strings.HasPrefix(s, "- name:") {
				break
			}
		}
	}
	return ""
}

// topLevelSections — верхнеуровневые ключи блока `config.yaml: |`.
func topLevelSections(manifest string) []string {
	lines := strings.Split(manifest, "\n")
	start, baseIndent := -1, 0
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if trimmed != "config.yaml: |" && trimmed != "config.yaml: |-" {
			continue
		}
		start = i + 1
		baseIndent = len(ln) - len(strings.TrimLeft(ln, " ")) + 2
		break
	}
	if start < 0 {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, ln := range lines[start:] {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		indent := len(ln) - len(strings.TrimLeft(ln, " "))
		if indent < baseIndent {
			break // блок кончился
		}
		if indent > baseIndent {
			continue // вложенное — не наш уровень
		}
		m := reTopKey.FindStringSubmatch(ln[baseIndent:])
		if m == nil {
			continue // комментарий, выражение helm, элемент списка
		}
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// serviceConfigCharts — чарты дерева, рендерящие конфигурацию нашего сервиса.
// Второе возвращаемое — сколько чартов осмотрено всего: «ноль находок» обязано
// быть отличимо от «ноль прочитанного».
func serviceConfigCharts(t *testing.T) (found []serviceConfigChart, chartsSeen int) {
	t.Helper()
	var chartDirs []string
	err := filepath.WalkDir(filepath.Join(repoRoot), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "out", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "Chart.yaml" {
			return nil
		}
		rel, rerr := filepath.Rel(repoRoot, filepath.Dir(path))
		if rerr == nil {
			chartDirs = append(chartDirs, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}
	sort.Strings(chartDirs)
	chartsSeen = len(chartDirs)

	for _, chartDir := range chartDirs {
		tmpls := chartTemplates(t, chartDir)

		// Путь конфигурации нашего бинаря — он же называет сервис.
		svc, mountDir := "", ""
		for _, tf := range tmpls {
			if m := reConfigPath.FindStringSubmatch(readFile(t, tf)); m != nil {
				svc, mountDir = m[1], "/etc/kacho-"+m[1]
				break
			}
		}
		if svc == "" {
			continue // чарт не поднимает наш бинарь с YAML-конфигурацией
		}

		// Том, смонтированный по этому пути, живёт в шаблоне рабочей нагрузки —
		// то есть в ДРУГОМ файле, чем ConfigMap. Поэтому связь ищется по всем
		// шаблонам чарта, а не в том, где впервые встретился путь: в этом дереве
		// путь называет и заголовок самого ConfigMap.
		cmName := ""
		for _, tf := range tmpls {
			if n := configMapNameForMount(readFile(t, tf), mountDir); n != "" {
				cmName = n
				break
			}
		}
		if cmName == "" {
			t.Errorf("%s: конфигурация сервиса объявлена по пути %s/config.yaml, но том, "+
				"смонтированный в %s, не прослеживается до ConfigMap — предпосылка "+
				"проверки не выполняется, и она не может судить этот чарт",
				chartDir, mountDir, mountDir)
			continue
		}

		cmFile, sections := "", []string(nil)
		for _, tf := range tmpls {
			body := readFile(t, tf)
			if !strings.Contains(body, "config.yaml: |") {
				continue
			}
			if !strings.Contains(body, "kind: ConfigMap") {
				continue
			}
			named := ""
			for _, ln := range strings.Split(body, "\n") {
				if m := reMetaName.FindStringSubmatch(ln); m != nil {
					named = m[1]
					break
				}
			}
			if named != cmName {
				continue
			}
			cmFile, sections = tf, topLevelSections(body)
			break
		}
		if cmFile == "" {
			t.Errorf("%s: том конфигурации ссылается на ConfigMap %q, но шаблона с таким "+
				"metadata.name и блоком config.yaml в чарте нет — связь оборвана, "+
				"судить нечего", chartDir, cmName)
			continue
		}

		found = append(found, serviceConfigChart{
			chartDir:      chartDir,
			service:       svc,
			configMapFile: cmFile,
			sections:      sections,
		})
	}
	return found, chartsSeen
}

// declaredSections — теги mapstructure корневой структуры Config сервиса.
func declaredSections(t *testing.T, service string) ([]string, string) {
	t.Helper()
	rel := filepath.Join("services", service, "internal", "apps", servicelayout.UseCaseSegment(service), "config", "config.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(repoRoot, rel), nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("разобрать %s: %v", rel, err)
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != "Config" {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return true
		}
		for _, fld := range st.Fields.List {
			if fld.Tag == nil {
				continue
			}
			raw, uerr := strconv.Unquote(fld.Tag.Value)
			if uerr != nil {
				continue
			}
			tag := reflect.StructTag(raw).Get("mapstructure")
			if tag == "" || tag == "-" {
				continue
			}
			out = append(out, strings.Split(tag, ",")[0])
		}
		return false
	})
	return out, rel
}

// ─────────────────────────────────────────────────────────────────────────────
// Проверка дерева.

// TestRenderedConfigSectionIsParsedByTheService — верхнеуровневая секция,
// которую чарт рендерит в config.yaml сервиса, обязана иметь одноимённый тег
// mapstructure в корневой структуре Config этого сервиса.
func TestRenderedConfigSectionIsParsedByTheService(t *testing.T) {
	charts, chartsSeen := serviceConfigCharts(t)
	if chartsSeen == 0 {
		t.Fatal("в дереве не найдено ни одного Chart.yaml — обход прочитал ноль, " +
			"и «ноль находок» здесь означало бы «ноль осмотренного»")
	}
	if len(charts) == 0 {
		t.Fatalf("осмотрено чартов: %d, из них рендерят конфигурацию нашего сервиса: 0 — "+
			"предпосылка проверки исчезла (сменилась форма пути конфигурации?)", chartsSeen)
	}

	sectionsRead, orphans := 0, 0
	for _, c := range charts {
		declared, cfgFile := declaredSections(t, c.service)
		if len(declared) == 0 {
			t.Errorf("%s: у сервиса %s не прочитано ни одного тега mapstructure в Config "+
				"(%s) — сравнивать не с чем", c.chartDir, c.service, cfgFile)
			continue
		}
		sectionsRead += len(c.sections)
		for _, s := range orphanSections(c.sections, declared) {
			orphans++
			t.Errorf("%s: секция %q рендерится в конфигурацию сервиса %s, а %s её не "+
				"объявляет — випер отбросит её молча, и правка ключа не изменит ничего.\n"+
				"    Исходов три: (а) сервис начинает её разбирать; (б) шаблон перестаёт "+
				"её рендерить; (в) ключ снимается с контракта чарта. Молча принять и "+
				"выбросить исходом не является.",
				c.configMapFile, s, c.service, cfgFile)
		}
	}

	t.Logf("осмотрено: чартов в дереве %d, из них с конфигурацией сервиса %d; "+
		"прочитано верхнеуровневых секций %d; секций без разбора %d",
		chartsSeen, len(charts), sectionsRead, orphans)
	for _, c := range charts {
		t.Logf("  %s → сервис %s, секции: %s", c.configMapFile, c.service,
			strings.Join(c.sections, " "))
	}
}

// TestConfigSectionPredicateCatchesAnOrphan — способность предиката упасть,
// показанная инъекцией синтетики В ОБЕ СТОРОНЫ.
//
// Без этого теста пустой список находок на дереве был бы неотличим от предиката,
// который не умеет находить ничего.
func TestConfigSectionPredicateCatchesAnOrphan(t *testing.T) {
	declared := []string{"logger", "api-server", "repository", "authn", "internal-lifecycle"}

	// (а) дефект — секция, которой нет среди тегов: находка с именем.
	got := orphanSections([]string{"logger", "extapi", "authn", "metrics"}, declared)
	want := []string{"extapi", "metrics"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("предикат не назвал осиротевшие секции: получено %v, ожидалось %v", got, want)
	}

	// (б) законный близнец той же формы — тег без секции в шаблоне
	// (`internal-lifecycle` у nlb: разбирается, чартом не рендерится): молчание.
	if got := orphanSections([]string{"logger", "api-server"}, declared); len(got) != 0 {
		t.Errorf("предикат покраснел на законной конструкции — ключ структуры без секции "+
			"в шаблоне это молчание, а не находка: %v", got)
	}

	// (в) вырожденный вход: пустой перечень тегов не имеет права выглядеть
	// «всё в порядке» — иначе сервис, чью структуру не удалось прочитать,
	// проходил бы проверку молча.
	if got := orphanSections([]string{"logger"}, nil); len(got) != 1 {
		t.Errorf("на пустом перечне тегов предикат обязан назвать всякую секцию, получено %v", got)
	}
}
