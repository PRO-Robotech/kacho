// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package alerts_test — пробы правил тревог чарта notify (приёмка NTF-1, Р18;
// NTF1-G12, NTF1-G27; замысел #2915 З22, З27; полоса N8).
//
// ЧТО УТВЕРЖДАЕТСЯ. Правило тревоги — функция синтетического ряда: проба
// рендерит чарт notify (`helm template`, копия с одной строкой таблицы
// источников — иначе объектов notify 0, NTF1-N04), берёт из рендера объект
// `PrometheusRule`, и каждое правило `alert` вычисляет настоящим движком PromQL
// и автоматом состояний тревоги по семантике сервера правил Prometheus
// (pending → firing с `for`) над рядом, загруженным в хранилище пробы. Вердикт —
// состояние тревоги «firing» в названный момент, а не текст выражения.
//
// ПОРЯДОК ПРОВЕРОК НЕСУЩИЙ (change-graph §5). Сперва фикстура: helm в PATH,
// рендер копии прошёл и содержит Deployment notify, движок поднимает тревогу на
// контрольном правиле и молчит на его близнеце. Только после этого — предмет:
// объект PrometheusRule и правило с именем из приёмки. Сломанная фикстура
// печатает «НЕ ВЫПОЛНИЛОСЬ», отсутствие правила — «КРАСНЫЙ».
//
// ПЕРЕЧНИ — из corelib (`feed.Classes()`, `feed.Reasons()`, `feed.PutDefects()`),
// а не литералами (УК62, УК67, CX1-54 (в), CX1-67): новая причина словаря
// попадает в пробу без её правки, пустой перечень — красный, а не вакуумный
// зелёный.
package alerts_test

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/util/annotations"
	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/corelib/notify/feed"
)

const (
	notifyChartDir         = "../../helm/notify"
	notifyStandaloneValues = "../../testdata/notify-standalone/values.yaml"
	notifyStandaloneSample = "../../testdata/mail-node/operator.yaml"
	notifyRelease          = "kacho-notify"

	// Тело пустой таблицы подключаемых источников в дереве и строка формы N02,
	// которой копия его заменяет: с пустой таблицей объектов notify 0.
	notifyEmptyTableBody   = `{{- list | toJson -}}`
	notifyFixtureTableBody = `{{- list (dict "module" "probe-b" "feedAddr" "probe-b:9091" ` +
		`"san" "spiffe://kacho.cloud/ns/kacho/sa/probe-b" "classes" (list "notice" "security") ` +
		`"recipientForms" (list) "authorization" "resolveSend") | toJson -}}`

	// Имена тревог — слова приёмки и замысла (NTF1-G27, Р18; З27 CX1-67; З22 УК54).
	alertFeedExpired   = "feed_expired"
	alertFeedPutDefect = "feed_put_defect"
	alertTemplateSkew  = "template_skew"

	// Ряды источника (corelib notify/feed, metrics.go) и notify (З22, З27).
	seriesFeedOutcomes   = "kacho_notification_feed_outcomes_total"
	seriesFeedPutDefects = "kacho_notification_feed_put_defects_total"
	seriesFeedOldest     = "kacho_notification_feed_oldest_pending_seconds"
	seriesTemplateSkew   = "notify_template_skew_total"

	// seriesTemplateTTL — ряд, которым правило G12 узнаёт срок шаблонов класса
	// `security`. Р18 требует «половину наименьшего ttl шаблонов security», а
	// замысел З27 источника этого числа для правила не называет (вопрос к
	// замыслу в возврате полосы RED). Проба объявляет вход ОДНИМ местом: ответ
	// замысла меняет только эту константу и g12TTLSeries.
	seriesTemplateTTL = "notify_template_ttl_seconds"

	// Шаг вычисления правил в пробах счётчиков: первое вычисление после
	// приращения — момент появления ненулевого образца.
	evalStep = 30 * time.Second
)

// templateSkewDirections — значения метки direction (З22, CX1-48). В голове
// полосы экспортированного перечня нет: исполнитель N3 держит его в
// неэкспортированной `deliver.directions()` ветки 2915-n3-deliver-decide;
// литерал здесь — слова замысла, а не догадка.
var templateSkewDirections = []string{"older", "newer", "missing"}

// ─── фикстура: рендер ───────────────────────────────────────────────────────

func requireHelm(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: helm не в PATH — проба правил тревог рендерит чарт notify и без helm не исполняется")
	}
}

// fixtureChart — копия чарта notify с одной строкой таблицы источников.
func fixtureChart(t *testing.T) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "notify")
	err := filepath.WalkDir(notifyChartDir, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, _ := filepath.Rel(notifyChartDir, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		body, rerr := os.ReadFile(p) // #nosec G304 -- путь из обхода чарта дерева
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(target, body, 0o600)
	})
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: копия чарта %s не снята: %v", notifyChartDir, err)
	}
	p := filepath.Join(dst, "templates", "_sources.tpl")
	body, err := os.ReadFile(p) // #nosec G304 -- путь копии во временном каталоге пробы
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: таблица источников копии не прочитана: %v", err)
	}
	if n := strings.Count(string(body), notifyEmptyTableBody); n != 1 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: тело пустой таблицы источников встречается в _sources.tpl %d раз(а), ожидалось 1 — "+
			"копия не может включить notify в рендер", n)
	}
	next := strings.Replace(string(body), notifyEmptyTableBody, notifyFixtureTableBody, 1)
	if err := os.WriteFile(p, []byte(next), 0o600); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: правка таблицы источников копии: %v", err)
	}
	return dst
}

type renderedDoc struct {
	source string
	kind   string
	raw    string
}

var sourceLineRe = regexp.MustCompile(`(?m)^# Source: (\S+)\s*$`)

// renderNotify — рендер копии на ноге без зонтика. API PrometheusRule
// объявлен рендеру явно: шаблон, условный по `.Capabilities`, обязан отдать
// объект, если кластер его понимает.
func renderNotify(t *testing.T) []renderedDoc {
	t.Helper()
	requireHelm(t)
	chart := fixtureChart(t)
	args := []string{
		"template", notifyRelease, chart, "-n", "kacho",
		"-f", notifyStandaloneValues, "-f", notifyStandaloneSample,
		"--api-versions", "monitoring.coreos.com/v1",
		"--api-versions", "monitoring.coreos.com/v1/PrometheusRule",
	}
	out, err := exec.Command("helm", args...).CombinedOutput() // #nosec G204 -- фиксированный бинарь, аргументы пробы
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: рендер фикстурной копии чарта notify отказал: %v\n%s", err, out)
	}
	var docs []renderedDoc
	for _, chunk := range regexp.MustCompile(`(?m)^---\s*$`).Split(string(out), -1) {
		var head struct {
			Kind string `yaml:"kind"`
		}
		if err := yaml.Unmarshal([]byte(chunk), &head); err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: документ рендера не разобран (%v):\n%s", err, chunk)
		}
		if head.Kind == "" {
			continue
		}
		src := ""
		if m := sourceLineRe.FindStringSubmatch(chunk); m != nil {
			src = m[1]
		}
		docs = append(docs, renderedDoc{source: src, kind: head.Kind, raw: chunk})
	}
	deployments := 0
	for _, d := range docs {
		if d.kind == "Deployment" {
			deployments++
		}
	}
	if deployments == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: в рендере фикстурной копии нет Deployment notify (объектов %d) — "+
			"notify не вошёл в рендер, судить правила не у чего", len(docs))
	}
	t.Logf("перепись рендера: объектов %d, Deployment %d", len(docs), deployments)
	return docs
}

// ─── правила из рендера ─────────────────────────────────────────────────────

type promRule struct {
	Alert       string            `yaml:"alert"`
	Record      string            `yaml:"record"`
	Expr        string            `yaml:"expr"`
	For         string            `yaml:"for"`
	Labels      map[string]string `yaml:"labels"`
	Annotations map[string]string `yaml:"annotations"`
}

type promRuleObject struct {
	Spec struct {
		Groups []struct {
			Name  string     `yaml:"name"`
			Rules []promRule `yaml:"rules"`
		} `yaml:"groups"`
	} `yaml:"spec"`
}

// alertRule — правило тревоги, готовое к вычислению.
type alertRule struct {
	name string
	src  promRule
	expr parser.Expr
	hold time.Duration
}

func compileAlert(t *testing.T, r promRule, where string) alertRule {
	t.Helper()
	expr, err := parser.ParseExpr(r.Expr)
	if err != nil {
		t.Fatalf("КРАСНЫЙ: выражение тревоги %q (%s) не разбирается PromQL: %v\n%s", r.Alert, where, err, r.Expr)
	}
	var hold time.Duration
	if r.For != "" {
		d, err := model.ParseDuration(r.For)
		if err != nil {
			t.Fatalf("КРАСНЫЙ: `for` тревоги %q (%s) не разбирается: %v", r.Alert, where, err)
		}
		hold = time.Duration(d)
	}
	return alertRule{name: r.Alert, src: r, expr: expr, hold: hold}
}

// notifyAlertRules — тревоги объектов PrometheusRule рендера (предмет). Перед
// предметом — вся фикстура: рендер и самопроверка движка.
func notifyAlertRules(t *testing.T) []alertRule {
	t.Helper()
	docs := renderNotify(t)
	requireEngineFiresAndStaysSilent(t)

	var out []alertRule
	objects := 0
	for _, d := range docs {
		if d.kind != "PrometheusRule" {
			continue
		}
		objects++
		var obj promRuleObject
		if err := yaml.Unmarshal([]byte(d.raw), &obj); err != nil {
			t.Fatalf("КРАСНЫЙ: PrometheusRule %s не разобран: %v", d.source, err)
		}
		for _, g := range obj.Spec.Groups {
			for _, r := range g.Rules {
				if r.Alert == "" {
					continue
				}
				out = append(out, compileAlert(t, r, d.source+"/"+g.Name))
			}
		}
	}
	if objects == 0 {
		kinds := make([]string, 0, len(docs))
		for _, d := range docs {
			kinds = append(kinds, d.kind)
		}
		sort.Strings(kinds)
		t.Fatalf("КРАСНЫЙ: в рендере чарта notify нет объекта PrometheusRule — правил тревог Р18 нет "+
			"(виды рендера: %s)", strings.Join(kinds, ", "))
	}
	t.Logf("перепись правил: объектов PrometheusRule %d, тревог %d", objects, len(out))
	return out
}

func findAlert(t *testing.T, all []alertRule, name string) alertRule {
	t.Helper()
	var names []string
	var found []alertRule
	for _, r := range all {
		names = append(names, r.name)
		if r.name == name {
			found = append(found, r)
		}
	}
	switch len(found) {
	case 0:
		t.Fatalf("КРАСНЫЙ: тревоги %q в PrometheusRule чарта notify нет (тревоги рендера: [%s])",
			name, strings.Join(names, ", "))
	case 1:
		return found[0]
	default:
		t.Fatalf("КРАСНЫЙ: тревога %q объявлена %d раз — правило тревоги одно", name, len(found))
	}
	return alertRule{}
}

// ─── движок: настоящий PromQL, автомат `for` по семантике Prometheus ─────────

// firing — тревоги правила в состоянии firing после вычисления в момент ts.
type firing []map[string]string

// evalTimeline — вычисляет правила подряд в моменты ts (возрастающие) над рядом
// load и возвращает firing каждого момента. Выражение вычисляет настоящий движок
// PromQL (`promql.Engine` над хранилищем пробы в памяти). Автомат тревоги — тот же,
// что у сервера правил Prometheus (rules/alerting.go): серия ответа выражения
// активна с первого вычисления, где она есть; firing, когда активна не меньше
// `for`; исчезла из ответа — сброшена. Метки тревоги — метки серии без
// `__name__` плюс `labels` правила. Пакет `rules` не взят намеренно: он тянет
// notifier → config → discovery (облачные SDK и client-go) в граф модуля ради
// одного автомата из трёх ветвей; по той же причине не взят `promqltest`
// (его хранилище — tsdb → config → discovery).
func evalTimeline(t *testing.T, load string, rs []alertRule, ts []time.Duration) []firing {
	t.Helper()
	store := parseLoad(t, load)
	engine := promql.NewEngine(promql.EngineOpts{MaxSamples: 1_000_000, Timeout: time.Minute, LookbackDelta: 5 * time.Minute})
	t.Cleanup(func() { _ = engine.Close() })

	activeAt := make([]map[string]time.Time, len(rs))
	for i := range activeAt {
		activeAt[i] = map[string]time.Time{}
	}
	out := make([]firing, 0, len(ts))
	for _, at := range ts {
		now := time.Unix(0, 0).UTC().Add(at)
		var f firing
		for i, r := range rs {
			q, err := engine.NewInstantQuery(context.Background(), store, nil, r.expr.String(), now)
			if err != nil {
				t.Fatalf("КРАСНЫЙ: тревога %q не вычисляется в %s: %v", r.name, at, err)
			}
			res := q.Exec(context.Background())
			if res.Err != nil {
				t.Fatalf("КРАСНЫЙ: тревога %q не вычисляется в %s: %v", r.name, at, res.Err)
			}
			vec, err := res.Vector()
			if err != nil {
				t.Fatalf("КРАСНЫЙ: тревога %q в %s вернула не вектор: %v", r.name, at, err)
			}
			seen := map[string]bool{}
			for _, smpl := range vec {
				lb := labels.NewBuilder(smpl.Metric)
				lb.Del(labels.MetricName)
				for k, v := range r.src.Labels {
					lb.Set(k, v)
				}
				lb.Set("alertname", r.name)
				ls := lb.Labels()
				key := ls.String()
				seen[key] = true
				since, ok := activeAt[i][key]
				if !ok {
					since = now
					activeAt[i][key] = now
				}
				if now.Sub(since) >= r.hold {
					f = append(f, ls.Map())
				}
			}
			for key := range activeAt[i] {
				if !seen[key] {
					delete(activeAt[i], key)
				}
			}
			q.Close()
		}
		out = append(out, f)
	}
	return out
}

// ─── хранилище пробы: ряды в памяти ─────────────────────────────────────────

// Форма входа — подмножество записи `load` тестов PromQL Prometheus:
//
//	load <шаг>
//	  <метрика>{<метки>} <значения>
//
// значения — `A`, `AxN` (A повторено N+1 раз), `A+BxN` (A, A+B, …, N+1 раз).
// Неразобранная строка — «НЕ ВЫПОЛНИЛОСЬ»: фикстура пробы не молчит о себе.

type floatSample struct {
	t int64
	f float64
}

func (s floatSample) T() int64                      { return s.t }
func (s floatSample) ST() int64                     { return 0 }
func (s floatSample) F() float64                    { return s.f }
func (s floatSample) H() *histogram.Histogram       { return nil }
func (s floatSample) FH() *histogram.FloatHistogram { return nil }
func (s floatSample) Type() chunkenc.ValueType      { return chunkenc.ValFloat }
func (s floatSample) Copy() chunks.Sample           { return s }

type memStore struct{ series []*storage.SeriesEntry }

func (m *memStore) Querier(_, _ int64) (storage.Querier, error) { return m, nil }
func (m *memStore) Close() error                                { return nil }
func (m *memStore) LabelValues(context.Context, string, *storage.LabelHints, ...*labels.Matcher) ([]string, annotations.Annotations, error) {
	return nil, nil, nil
}
func (m *memStore) LabelNames(context.Context, *storage.LabelHints, ...*labels.Matcher) ([]string, annotations.Annotations, error) {
	return nil, nil, nil
}
func (m *memStore) Select(_ context.Context, _ bool, _ *storage.SelectHints, ms ...*labels.Matcher) storage.SeriesSet {
	var out []storage.Series
	for _, s := range m.series {
		ok := true
		for _, mt := range ms {
			if !mt.Matches(s.Labels().Get(mt.Name)) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return labels.Compare(out[i].Labels(), out[j].Labels()) < 0 })
	return &sliceSet{s: out, i: -1}
}

type sliceSet struct {
	s []storage.Series
	i int
}

func (x *sliceSet) Next() bool                        { x.i++; return x.i < len(x.s) }
func (x *sliceSet) At() storage.Series                { return x.s[x.i] }
func (x *sliceSet) Err() error                        { return nil }
func (x *sliceSet) Warnings() annotations.Annotations { return nil }

var valueItemRe = regexp.MustCompile(`^(-?[0-9.]+)(?:\+(-?[0-9.]+))?(?:x([0-9]+))?$`)

func parseLoad(t *testing.T, load string) *memStore {
	t.Helper()
	st := &memStore{}
	var step time.Duration
	for _, line := range strings.Split(load, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "load "); ok {
			d, err := model.ParseDuration(strings.TrimSpace(rest))
			if err != nil {
				t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: шаг ряда пробы %q не разобран: %v", rest, err)
			}
			step = time.Duration(d)
			continue
		}
		if step == 0 {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ряд пробы до строки load: %q", line)
		}
		end := strings.LastIndex(line, "}")
		if end < 0 {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: строка ряда пробы без меток: %q", line)
		}
		ms, err := parser.ParseMetricSelector(line[:end+1])
		if err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: метки ряда пробы %q не разобраны: %v", line[:end+1], err)
		}
		lb := labels.NewBuilder(labels.EmptyLabels())
		for _, m := range ms {
			if m.Type != labels.MatchEqual {
				t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ряд пробы задаёт метку не равенством: %s", m)
			}
			lb.Set(m.Name, m.Value)
		}
		var samples []chunks.Sample
		idx := 0
		for _, item := range strings.Fields(line[end+1:]) {
			g := valueItemRe.FindStringSubmatch(item)
			if g == nil {
				t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: значение ряда пробы %q не разобрано", item)
			}
			a, _ := strconv.ParseFloat(g[1], 64)
			inc := 0.0
			if g[2] != "" {
				inc, _ = strconv.ParseFloat(g[2], 64)
			}
			n := 0
			if g[3] != "" {
				n, _ = strconv.Atoi(g[3])
			}
			for k := 0; k <= n; k++ {
				samples = append(samples, floatSample{t: (time.Duration(idx) * step).Milliseconds(), f: a + float64(k)*inc})
				idx++
			}
		}
		st.series = append(st.series, storage.NewListSeries(lb.Labels(), samples))
	}
	if len(st.series) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: вход пробы без рядов")
	}
	return st
}

// requireEngineFiresAndStaysSilent — самопроверка движка пробы до предмета:
// контрольное правило формы З27 поднимает тревогу на приращении, его близнец
// (ряд без приращения) молчит. Иначе «тревоги нет» неотличимо от «движок не
// вычисляет».
func requireEngineFiresAndStaysSilent(t *testing.T) {
	t.Helper()
	ctl := compileAlert(t, promRule{
		Alert: "control",
		Expr:  `sum by (module, cause) (increase(` + seriesFeedPutDefects + `[5m])) > 0`,
	}, "контроль пробы")
	ts := stepTimes(0, 4)
	up := evalTimeline(t, counterLoad(seriesFeedPutDefects, `module="probe-b",cause="control"`, 2, 2), []alertRule{ctl}, ts)
	if len(up[2]) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: контрольное правило не поднялось на приращении — движок пробы не вычисляет тревоги")
	}
	flat := evalTimeline(t, counterLoad(seriesFeedPutDefects, `module="probe-b",cause="control"`, 2, 0), []alertRule{ctl}, ts)
	for i, f := range flat {
		if len(f) != 0 {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: контрольное правило поднялось на ряде без приращения в шаге %d: %v", i, f)
		}
	}
}

// counterLoad — ряд счётчика с шагом evalStep: `before` образцов нуля, затем
// значение delta (0 — близнец без приращения), держится до конца ленты.
func counterLoad(series, lbls string, before int, delta int) string {
	return fmt.Sprintf("load %s\n  %s{%s} 0x%d %dx%d\n", model.Duration(evalStep), series, lbls, before-1, delta, 8)
}

func stepTimes(from, to int) []time.Duration {
	var out []time.Duration
	for i := from; i <= to; i++ {
		out = append(out, time.Duration(i)*evalStep)
	}
	return out
}

func hasLabels(f firing, want map[string]string) bool {
	for _, a := range f {
		ok := true
		for k, v := range want {
			if a[k] != v {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// ─── NTF1-G27 ───────────────────────────────────────────────────────────────

// TestNotifyAlertRules_NTF1G27_FeedExpiredEveryPairButNoticeRecipientNet —
// тревога feed_expired на каждую пару (класс, причина) из перечней corelib с
// kind="expired" в первом же вычислении, метки называют источник, класс и
// причину; исключённая пара (notice, recipient_net) — тишина; близнец — ряд,
// где растут только sent и defer любой причины, — тишина.
func TestNotifyAlertRules_NTF1G27_FeedExpiredEveryPairButNoticeRecipientNet(t *testing.T) {
	classes, reasons := feed.Classes(), feed.Reasons()
	if len(classes) == 0 || len(reasons) == 0 {
		t.Fatalf("КРАСНЫЙ: перечни corelib пусты (классов %d, причин %d) — судить нечего", len(classes), len(reasons))
	}
	classSet := map[feed.Class]bool{}
	for _, c := range classes {
		classSet[c] = true
	}
	if !classSet[feed.ClassNotice] || !containsReason(reasons, feed.ReasonRecipientNet) {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: исключённой пары (notice, recipient_net) нет в перечнях corelib — предпосылка G27 ложна")
	}

	rule := findAlert(t, notifyAlertRules(t), alertFeedExpired)
	ts := stepTimes(0, 6)
	const at = 2 // первое вычисление, видящее приращение

	pairs, alerted, silent := 0, 0, 0
	for _, c := range classes {
		for _, r := range reasons {
			if r == feed.ReasonNone {
				continue
			}
			pairs++
			lbls := fmt.Sprintf(`module="probe-b",class=%q,kind="expired",reason=%q`, c, r)
			got := evalTimeline(t, counterLoad(seriesFeedOutcomes, lbls, at, 1), []alertRule{rule}, ts)
			isExcluded := c == feed.ClassNotice && r == feed.ReasonRecipientNet
			if isExcluded {
				for i, f := range got {
					if len(f) != 0 {
						t.Errorf("КРАСНЫЙ: (%s, %s) — исключённая пара подняла %s в шаге %d: %v", c, r, alertFeedExpired, i, f)
					}
				}
				silent++
				continue
			}
			want := map[string]string{"alertname": alertFeedExpired, "module": "probe-b", "class": string(c), "reason": string(r)}
			if !hasLabels(got[at], want) {
				t.Errorf("КРАСНЫЙ: (%s, %s) — %s не в firing в первом вычислении после приращения (шаг %d); firing: %v",
					c, r, alertFeedExpired, at, got[at])
				continue
			}
			alerted++
		}
	}

	// Близнец: растут только sent и defer любой причины.
	var twin strings.Builder
	fmt.Fprintf(&twin, "load %s\n", model.Duration(evalStep))
	for _, c := range classes {
		fmt.Fprintf(&twin, "  %s{module=\"probe-b\",class=%q,kind=\"sent\",reason=\"\"} 0x%d 1x8\n", seriesFeedOutcomes, c, at-1)
		for _, r := range reasons {
			fmt.Fprintf(&twin, "  %s{module=\"probe-b\",class=%q,kind=\"defer\",reason=%q} 0x%d 1x8\n", seriesFeedOutcomes, c, r, at-1)
		}
	}
	for i, f := range evalTimeline(t, twin.String(), []alertRule{rule}, ts) {
		if len(f) != 0 {
			t.Errorf("КРАСНЫЙ: близнец (растут только sent и defer) поднял %s в шаге %d: %v", alertFeedExpired, i, f)
		}
	}
	t.Logf("перепись G27: пар %d (классов %d × причин без пустой), тревог %d, исключённых тихих %d", pairs, len(classes), alerted, silent)
	if silent != 1 {
		t.Errorf("КРАСНЫЙ: исключённых пар %d, ожидалась ровно одна (notice, recipient_net)", silent)
	}
}

func containsReason(rs []feed.Reason, r feed.Reason) bool {
	for _, x := range rs {
		if x == r {
			return true
		}
	}
	return false
}

// ─── NTF1-G12 ───────────────────────────────────────────────────────────────

// g12TTLSeries — вход «наименьший ttl шаблонов security» для фикстурного
// шаблона kaname/recovery, ttl = 5m (см. seriesTemplateTTL).
func g12TTLSeries(seconds int) string {
	return fmt.Sprintf(`  %s{source="kaname",template="recovery",class="security"} %dx400`+"\n", seriesTemplateTTL, seconds)
}

// TestNotifyAlertRules_NTF1G12_SecurityHalfTTLAge — строка security шаблона
// с ttl = 5m ждёт `pending`: на возрасте 2:30 тревога в firing, на 2:29 — нет.
func TestNotifyAlertRules_NTF1G12_SecurityHalfTTLAge(t *testing.T) {
	all := notifyAlertRules(t)
	if len(all) == 0 {
		t.Fatalf("КРАСНЫЙ: в PrometheusRule чарта notify ни одной тревоги — тревоги половины срока security нет")
	}
	load := "load 1s\n" +
		fmt.Sprintf(`  %s{module="kaname",class="security"} 0+1x400`+"\n", seriesFeedOldest) +
		g12TTLSeries(300)
	var ts []time.Duration
	for s := 0; s <= 150; s++ {
		ts = append(ts, time.Duration(s)*time.Second)
	}
	got := evalTimeline(t, load, all, ts)
	if f := got[149]; len(f) != 0 {
		t.Errorf("КРАСНЫЙ: на возрасте 2:29 (меньше половины ttl 5m) тревоги в firing: %v", f)
	}
	f := got[150]
	if len(f) == 0 {
		t.Fatalf("КРАСНЫЙ: на возрасте 2:30 (половина ttl 5m шаблона security) ни одна тревога notify не в firing")
	}
	named := false
	for _, a := range f {
		if a["module"] == "kaname" || a["source"] == "kaname" {
			named = true
		}
	}
	if !named {
		t.Errorf("КРАСНЫЙ: тревога половины срока на 2:30 не называет источник kaname: %v", f)
	}
}

// ─── CX1-67: feed_put_defect ────────────────────────────────────────────────

// TestNotifyAlertRules_CX167_FeedPutDefectByEveryCause — по каждому значению
// `feed.PutDefects()` ненулевой прирост счётчика дефектов → feed_put_defect в
// firing с метками module и cause; близнец — ноль → тишина.
func TestNotifyAlertRules_CX167_FeedPutDefectByEveryCause(t *testing.T) {
	causes := feed.PutDefects()
	if len(causes) == 0 {
		t.Fatalf("КРАСНЫЙ: перечень feed.PutDefects() пуст — судить нечего")
	}
	rule := findAlert(t, notifyAlertRules(t), alertFeedPutDefect)
	ts := stepTimes(0, 6)
	const at = 2
	for _, c := range causes {
		lbls := fmt.Sprintf(`module="probe-b",cause=%q`, c)
		got := evalTimeline(t, counterLoad(seriesFeedPutDefects, lbls, at, 1), []alertRule{rule}, ts)
		want := map[string]string{"alertname": alertFeedPutDefect, "module": "probe-b", "cause": string(c)}
		if !hasLabels(got[at], want) {
			t.Errorf("КРАСНЫЙ: cause=%s — %s не в firing в первом вычислении после приращения; firing: %v", c, alertFeedPutDefect, got[at])
		}
		for i, f := range evalTimeline(t, counterLoad(seriesFeedPutDefects, lbls, at, 0), []alertRule{rule}, ts) {
			if len(f) != 0 {
				t.Errorf("КРАСНЫЙ: cause=%s — близнец (ноль) поднял %s в шаге %d: %v", c, alertFeedPutDefect, i, f)
			}
		}
	}
	t.Logf("перепись CX1-67: причин дефекта %d", len(causes))
}

// ─── УК54: template_skew с меткой direction ─────────────────────────────────

// TestNotifyAlertRules_UK54_TemplateSkewEveryDirection — приращение
// notify_template_skew_total в любом направлении → template_skew в firing в
// первом же вычислении («сразу», G25), метки называют источник и направление
// («правило различает направления», З22); близнец — ноль → тишина.
func TestNotifyAlertRules_UK54_TemplateSkewEveryDirection(t *testing.T) {
	rule := findAlert(t, notifyAlertRules(t), alertTemplateSkew)
	ts := stepTimes(0, 6)
	const at = 2
	for _, d := range templateSkewDirections {
		lbls := fmt.Sprintf(`source="probe-b",direction=%q`, d)
		got := evalTimeline(t, counterLoad(seriesTemplateSkew, lbls, at, 1), []alertRule{rule}, ts)
		want := map[string]string{"alertname": alertTemplateSkew, "source": "probe-b", "direction": d}
		if !hasLabels(got[at], want) {
			t.Errorf("КРАСНЫЙ: direction=%s — %s не в firing в первом вычислении после приращения; firing: %v", d, alertTemplateSkew, got[at])
		}
		for i, f := range evalTimeline(t, counterLoad(seriesTemplateSkew, lbls, at, 0), []alertRule{rule}, ts) {
			if len(f) != 0 {
				t.Errorf("КРАСНЫЙ: direction=%s — близнец (ноль) поднял %s в шаге %d: %v", d, alertTemplateSkew, i, f)
			}
		}
	}
}

// TestNotifyAlertRules_HarnessSelfCheck — движок пробы на синтетике, без
// чарта: правило feed_expired формы З27 (исключение одной пары) поднимается на
// (security, unclaimed) и молчит на (notice, recipient_net). Доказывает, что
// красный проб выше — про чарт, а не про движок.
func TestNotifyAlertRules_HarnessSelfCheck(t *testing.T) {
	requireEngineFiresAndStaysSilent(t)
	ref := compileAlert(t, promRule{
		Alert: alertFeedExpired,
		Expr: `sum by (module,class,reason) (increase(` + seriesFeedOutcomes + `{kind="expired"}[2m])) > 0 ` +
			`unless sum by (module,class,reason) (increase(` + seriesFeedOutcomes + `{kind="expired",class="notice",reason="recipient_net"}[2m]))`,
	}, "опора З27")
	ts := stepTimes(0, 6)
	up := evalTimeline(t, counterLoad(seriesFeedOutcomes, `module="probe-b",class="security",kind="expired",reason="unclaimed"`, 2, 1), []alertRule{ref}, ts)
	if !hasLabels(up[2], map[string]string{"class": "security", "reason": "unclaimed"}) {
		t.Fatalf("опора З27 не поднялась на (security, unclaimed): %v", up[2])
	}
	for i, f := range evalTimeline(t, counterLoad(seriesFeedOutcomes, `module="probe-b",class="notice",kind="expired",reason="recipient_net"`, 2, 1), []alertRule{ref}, ts) {
		if len(f) != 0 {
			t.Fatalf("опора З27 поднялась на (notice, recipient_net) в шаге %d: %v", i, f)
		}
	}
}
