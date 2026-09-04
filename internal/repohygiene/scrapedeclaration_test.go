// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// scrapedeclaration_test.go — каждый процесс с диагностической поверхностью
// объявляет свой сбор, и объявляет его ТАМ, ГДЕ ОНО РЕНДЕРИТСЯ.
//
// # Предмет
//
// Поверхность, которую никто не читает, неотличима от отсутствующей: величины
// есть, а «ноль отказов» по-прежнему означает «никто не приходил». Объявление
// сбора — то, что превращает поднятую поверхность в наблюдаемую.
//
// # Почему по ОБЪЯВЛЕНИЯМ, а не по отрендеренному манифесту
//
// Рендер требует helm, которого в прогоне проб нет, и проверка, которая может
// «пропуститься», не проверяет ничего. Здесь читаются значения и шаблоны —
// то есть ровно то, что правит человек.
//
// # Почему перечень чартов ВЫВОДИТСЯ, а не выписывается
//
// Стенд поднимается одним релизом умбреллы, поэтому рендерится ровно то, что она
// тянет: зависимости `file://` плюс каталоги сабчартов. Выписанный список
// разошёлся бы с ней на первом же новом сервисе — и разошёлся бы молча, потому
// что «не проверяем» и «нечего проверять» выглядят одинаково.
//
// # Почему аннотация в НЕРЕНДЕРЯЩЕМСЯ каталоге — находка
//
// Такая правка неотличима от сделанной работы и не производит ничего. Ровно это
// и случилось: единственное объявление сбора, существовавшее в дереве, лежало в
// каталоге, который умбрелла не тянет, — и было засчитано как «процесс объявляет
// себя для сбора».

// scrapeAnnotationRe — строка объявления в шаблоне пода.
var (
	scrapeEnabledRe  = regexp.MustCompile(`prometheus\.io/scrape:\s*"true"`)
	scrapePathRe     = regexp.MustCompile(`prometheus\.io/path:\s*/metrics`)
	scrapePortRe     = regexp.MustCompile(`prometheus\.io/port:\s*\{\{\s*\.Values\.([A-Za-z0-9_.]+)`)
	metricsHandleRe  = regexp.MustCompile(`Handle\("(GET )?/metrics"`)
	metricsDefaultRe = regexp.MustCompile(
		`METRICS_ADDR"\s+default:":(\d+)"|SetDefault\("(?:api-server\.)?metrics[-.](?:endpoint|address)",\s*"[^"]*:(\d+)"`)
)

// renderedCharts — каталоги чартов, которые умбрелла ДЕЙСТВИТЕЛЬНО рендерит.
func renderedCharts(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}

	chartYAML, err := os.ReadFile(filepath.Join(root, "deploy/helm/umbrella/Chart.yaml"))
	if err != nil {
		t.Fatalf("объявление умбреллы: %v", err)
	}
	depRe := regexp.MustCompile(`repository:\s*file://([^\s]+)`)
	for _, m := range depRe.FindAllStringSubmatch(string(chartYAML), -1) {
		rel := filepath.Clean(filepath.Join("deploy/helm/umbrella", m[1]))
		out[filepath.ToSlash(rel)] = true
	}

	entries, err := os.ReadDir(filepath.Join(root, "deploy/helm/umbrella/charts"))
	if err != nil {
		t.Fatalf("каталог сабчартов умбреллы: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			out["deploy/helm/umbrella/charts/"+e.Name()] = true
		}
	}
	if len(out) == 0 {
		t.Fatal("умбрелла не тянет ни одного чарта — предпосылка проверки отпала: " +
			"либо объявление переехало, либо стенд поднимается уже не ею")
	}
	return out
}

// processSurface — процесс, несущий обработчик экспозиции.
type processSurface struct {
	name      string // vpc, iam, api-gateway…
	chartDir  string // каталог чарта, который его разворачивает
	defaultPt int    // порт, который процесс берёт из своей конфигурации
}

// processesWithASurface — процессы, чей композиционный корень несёт обработчик
// экспозиции, вместе с портом их конфигурации.
//
// Перечень выводится по ДВУМ независимым признакам, и оба обязаны сойтись:
// обработчик в корне (поверхность есть) и умолчание адреса (её порт известен).
// Процесс, у которого есть первое и нет второго, — находка: поверхность
// поднимется неизвестно где.
func processesWithASurface(t *testing.T, root string) []processSurface {
	t.Helper()
	handlers := map[string]bool{}
	ports := map[string]int{}

	files, err := treecorpus.UnderWithSuffix(filepath.Join(root, "services"), ".go")
	if err != nil {
		t.Fatalf("состав дерева сервисов: %v", err)
	}
	gwFiles, err := treecorpus.UnderWithSuffix(filepath.Join(root, "gateway"), ".go")
	if err != nil {
		t.Fatalf("состав дерева края: %v", err)
	}
	for _, path := range append(files, gwFiles...) {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			t.Fatalf("относительный путь %s: %v", path, rerr)
		}
		rel = filepath.ToSlash(rel)
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("чтение %s: %v", rel, rerr)
		}
		name := processNameOf(rel)
		if name == "" {
			continue
		}
		if strings.Contains(rel, "/cmd/") && metricsHandleRe.Match(body) {
			handlers[name] = true
		}
		if m := metricsDefaultRe.FindSubmatch(body); m != nil {
			port := string(m[1])
			if port == "" {
				port = string(m[2])
			}
			if n, cerr := strconv.Atoi(port); cerr == nil {
				ports[name] = n
			}
		}
	}

	var out []processSurface
	for name := range handlers {
		port, ok := ports[name]
		if !ok {
			t.Errorf("%s несёт обработчик экспозиции, а умолчания адреса поверхности у него нет: "+
				"порт, на котором она поднимется, не назван нигде — объявить сбор не на что", name)
			continue
		}
		out = append(out, processSurface{name: name, chartDir: chartDirOf(name), defaultPt: port})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// processNameOf — имя процесса по пути файла.
func processNameOf(rel string) string {
	switch {
	case strings.HasPrefix(rel, "gateway/"):
		return "api-gateway"
	case strings.HasPrefix(rel, "services/"):
		parts := strings.Split(rel, "/")
		if len(parts) >= 2 {
			return parts[1]
		}
	}
	return ""
}

// chartDirOf — каталог чарта, разворачивающего процесс.
//
// Два вида по построению умбреллы: зависимость `file://` рядом с кодом и каталог
// сабчарта. Разъедется — проверка ниже найдёт каталог вне перечня рендерящихся и
// скажет об этом с координатой.
func chartDirOf(process string) string {
	switch process {
	case "iam", "geo":
		return "deploy/helm/umbrella/charts/kacho-" + process
	case "api-gateway":
		return "gateway/deploy"
	default:
		return "services/" + process + "/deploy"
	}
}

// valueAt резолвит `.Values.a.b` в значениях чарта.
func valueAt(values map[string]any, path string) (any, bool) {
	cur := any(values)
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[seg]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func readValues(t *testing.T, root, chartDir string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, chartDir, "values.yaml"))
	if err != nil {
		t.Fatalf("значения чарта %s: %v", chartDir, err)
	}
	var out map[string]any
	if uerr := yaml.Unmarshal(body, &out); uerr != nil {
		t.Fatalf("разбор значений %s: %v", chartDir, uerr)
	}
	return out
}

// TestEveryRenderedChartDeclaresItsScrape — у каждого процесса с поверхностью
// объявлен сбор, порт объявления совпадает с портом его конфигурации, а
// объявление лежит в рендерящемся чарте.
func TestEveryRenderedChartDeclaresItsScrape(t *testing.T) {
	root := repoRoot(t)
	rendered := renderedCharts(t, root)
	processes := processesWithASurface(t, root)

	t.Logf("чартов, которые рендерит умбрелла: %d; процессов с диагностической поверхностью: %d",
		len(rendered), len(processes))
	if len(processes) == 0 {
		t.Fatal("ни один процесс не несёт обработчика экспозиции — предмет проверки отпал")
	}

	var findings []string
	disabled := 0
	for _, p := range processes {
		if !rendered[p.chartDir] {
			findings = append(findings, p.name+" — его чарт "+p.chartDir+
				" не входит в перечень рендерящихся: объявление сбора, положенное туда, "+
				"не доедет до кластера ни в одном профиле")
			continue
		}
		values := readValues(t, root, p.chartDir)
		enabled, ok := valueAt(values, "metricsScrape.enabled")
		if !ok {
			findings = append(findings, p.chartDir+
				" — в значениях нет ключа metricsScrape.enabled: сбор не объявлен ни включённым, "+
				"ни выключенным, а умолчание здесь было бы решением, принятым никем")
			continue
		}
		if on, _ := enabled.(bool); !on {
			disabled++
			because, _ := valueAt(values, "metricsScrape.disabledBecause")
			reason, _ := because.(string)
			if strings.TrimSpace(reason) == "" {
				findings = append(findings, p.chartDir+
					" — сбор выключен БЕЗ причины: «забыли» и «здесь его нет намеренно» "+
					"неотличимы, а снятие обязано требовать слов")
			}
			continue
		}
		body, rerr := os.ReadFile(filepath.Join(root, p.chartDir, "templates/deployment.yaml"))
		if rerr != nil {
			findings = append(findings, p.chartDir+" — шаблон пода не прочитан: "+rerr.Error())
			continue
		}
		text := string(body)
		if !scrapeEnabledRe.MatchString(text) || !scrapePathRe.MatchString(text) {
			findings = append(findings, p.chartDir+
				" — у пода нет объявления сбора (prometheus.io/scrape + prometheus.io/path): "+
				"поверхность поднята, и к ней никто не придёт")
			continue
		}
		m := scrapePortRe.FindStringSubmatch(text)
		if m == nil {
			findings = append(findings, p.chartDir+
				" — порт сбора объявлен не значением чарта: он обязан читаться из значений, "+
				"иначе объявление и конфигурация процесса разъедутся молча")
			continue
		}
		declared, ok := valueAt(values, m[1])
		if !ok {
			findings = append(findings, p.chartDir+" — аннотация читает .Values."+m[1]+
				", а такого ключа в значениях нет: объявление указывает в пустоту")
			continue
		}
		port, _ := declared.(int)
		if port != p.defaultPt {
			findings = append(findings, p.chartDir+" — объявленный порт сбора "+strconv.Itoa(port)+
				" не равен адресу, который берёт процесс ("+strconv.Itoa(p.defaultPt)+
				"): собиратель придёт туда, где никто не отвечает")
		}
	}

	// Объявление сбора ВНЕ перечня рендерящихся — находка, а не пропуск.
	orphans := scrapeDeclarationsOutside(t, root, rendered)
	findings = append(findings, orphans...)

	// Перепись выключений: пустой перечень — цель, а не поломка.
	t.Logf("объявлений сбора выключено: %d", disabled)

	sort.Strings(findings)
	if len(findings) > 0 {
		t.Fatalf("объявление сбора не сходится с поверхностью:\n  %s", strings.Join(findings, "\n  "))
	}
}

// scrapeDeclarationsOutside — объявления сбора в каталогах, которые умбрелла не
// тянет.
func scrapeDeclarationsOutside(t *testing.T, root string, rendered map[string]bool) []string {
	t.Helper()
	files, err := treecorpus.UnderWithSuffix(root, ".yaml")
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}
	var out []string
	for _, path := range files {
		body, rerr := os.ReadFile(path)
		if rerr != nil || !scrapeEnabledRe.Match(body) {
			continue
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		inRendered := false
		for chart := range rendered {
			if strings.HasPrefix(rel, chart+"/") {
				inRendered = true
				break
			}
		}
		if inRendered {
			continue
		}
		// Чужие чарты объявляют себя сами; предмет проверки — НАШИ каталоги.
		if !strings.HasPrefix(rel, "services/") && !strings.HasPrefix(rel, "gateway/") &&
			!strings.HasPrefix(rel, "deploy/") {
			continue
		}
		out = append(out, rel+" — объявление сбора в каталоге, который умбрелла НЕ тянет: "+
			"оно не рендерится ни в одном профиле и потому не производит ничего. "+
			"Правка такого чарта неотличима от сделанной работы")
	}
	return out
}

// TestScrapeIsDeclaredByExactlyOneMechanism — объявление сбора у процесса ровно
// ОДНО, и второго механизма в дереве нет.
//
// ЗАЧЕМ. Механизмов объявления сбора в этой экосистеме два: аннотация на поде
// (её читает сборщик с обнаружением по аннотациям) и отдельный объект описи
// (его читает оператор). Они не складываются: сборщик читает СВОЙ и не знает о
// чужом. Держать оба — значит иметь два места об одном предмете, из которых
// работает в лучшем случае одно, а какое именно — зависит от того, что
// развёрнуто в кластере, то есть от знания, которого у дерева нет.
//
// ЗАМЕР, ИЗ КОТОРОГО ВЫВЕДЕНО (2026-08-22, задача #955). Первый механизм —
// аннотации — покрывал 8 поверхностей из 8 и был включён везде. Второй — опись —
// был объявлен у 2 из 8 и НИГДЕ не включён: ни один профиль не ставил его ручку
// в истину. То есть задача звала размножить до восьми объявление, которое не
// действует ни в одном профиле, а настоящий предмет был другим: сборщика не
// развёрнуто ни одного — ни в дереве, ни в кластере (проверено отсутствием
// определения типа описи).
//
// ЧТО ЭТА ПРОВЕРКА ДЕЛАЕТ И ЧЕГО НЕ ДЕЛАЕТ. Она держит ЕДИНСТВЕННОСТЬ
// механизма. Она НЕ утверждает, что собранное кем-то читается: развёрнутый
// сборщик — отдельный предмет, и подменять его этой проверкой значило бы
// объявить наблюдаемость достигнутой ровно там, где её нет.
func TestScrapeIsDeclaredByExactlyOneMechanism(t *testing.T) {
	root := repoRoot(t)

	// Второй механизм ищется по имени вида объекта в объявлениях чартов —
	// разбирать шаблон не нужно: само присутствие такого документа и есть
	// заведение второго механизма.
	// Состав берётся у индекса git, а не с диска: неотслеживаемый файл не
	// доедет ни до одного кластера, и считать его заведением механизма значило
	// бы краснеть на чужом черновике.
	tracked, err := treecorpus.UnderWithSuffix(root, ".yaml", ".yml")
	if err != nil {
		t.Fatalf("состав дерева не прочитан: %v", err)
	}

	var findings []string
	scanned := 0
	for _, abs := range tracked {
		rel, rerr := filepath.Rel(root, abs)
		if rerr != nil {
			continue
		}
		if !strings.Contains(rel, "/deploy/") && !strings.HasPrefix(rel, "deploy/") {
			continue
		}
		body, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		scanned++
		for _, line := range strings.Split(string(body), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				continue // объяснение о механизме — не его заведение
			}
			if strings.HasPrefix(trimmed, "kind: ServiceMonitor") ||
				strings.HasPrefix(trimmed, "kind: PodMonitor") {
				findings = append(findings, rel+" — заводит ВТОРОЙ механизм объявления сбора ("+
					trimmed+"): аннотация на поде уже объявляет его у каждого процесса, "+
					"и сборщик читает ровно один из двух")
				break
			}
		}
	}

	t.Logf("осмотрено объявлений посадки: %d; заведений второго механизма: %d", scanned, len(findings))
	if scanned == 0 {
		t.Fatal("объявлений посадки не прочитано ни одного — «ноль находок» здесь означало бы «ноль прочитанного»")
	}
	for _, f := range findings {
		t.Error(f)
	}
}
