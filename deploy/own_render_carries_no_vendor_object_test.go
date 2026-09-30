// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// own_render_carries_no_vendor_object_test.go — РЕНДЕР КАЖДОЙ ЦЕПОЧКИ НЕ НЕСЁТ НИ
// ОДНОГО ОБЪЕКТА И НИ ОДНОГО ОБРАЗА ПОСТАВЩИКА ЛИЧНОСТИ, А ОБЕ ПОЛОВИНЫ СТЕНДА
// ПОЛУЧАЮТ ПОСАДКУ `own` (kacho#2929, вход #1276).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Снятие поставщика (#1276) опирается на свойство, которое на ветке волны уже
// выполнено, но держалось вниманием: человека на каждом стенде таблицы
// проверяет НАША полоса, и рендер ни одной цепочки не поднимает ни службы
// поставщика, ни его издателя, ни их баз, ни чужого экрана входа. Проба,
// судившая это прежде (`identity_file_keys_survive_the_environment_test.go`,
// строка 19 ведомости `docs/architecture/identity-probe-fate-census.md`),
// снимается вместе с предметом своей второй половины. Здесь свойство живёт
// отдельно и переживает снятие: после #1276 оно становится запретом на
// возврат.
//
// По каждой цепочке `deploy/stacks.txt` (перечень читается из таблицы, не
// выписывается) утверждается:
//
//  1. объектов поставщика 0 — объект узнаётся по УЗЛУ-ИДЕНТИФИКАТОРУ, а не по
//     строке текста: имя объекта, образ любого контейнера пода, метка чарта
//     (`helm.sh/chart`, `app.kubernetes.io/name`), путь подчарта в строке
//     `# Source:`, которую печатает helm. Строки с именем поставщика в рендере
//     есть и сегодня (комментарии, пустые заголовки шаблонов, пустые величины),
//     объектами они не являются — поиск по тексту краснел бы на них и молчал на
//     переименованном объекте;
//  2. образов поставщика 0 — отдельным числом, потому что образ несёт объект с
//     нашим именем тоже;
//  3. посадка `own` у службы доступа (ключ `authn.identity-provider` её карты
//     настроек — то, что читает процесс) и у края (переменная посадки его
//     пода). Ключ, снятый пином службы (`retired_settings.go` пиненного модуля)
//     или ведомостью края (`internal/retiredknobs`), обязан ОТСУТСТВОВАТЬ:
//     процесс, получивший снятый ключ, отказывает в старте, а посадка у него
//     одна по построению.
//
// Словарь поставщика — `internal/identityvendor`, тот же, что у потолка
// привязок: второй перечень здесь разошёлся бы с ним молча.
//
// ─────────────────────────────────────────────────────────────────────────────
// КОНТРОЛЬ
//
// На голове свойство выполнено, поэтому «ноль» обязан быть отличим от слепоты:
//
//   - живой контроль — каждая зависимость `Chart.yaml`, чьё имя, псевдоним или
//     репозиторий называет поставщика, поднимается своим условием на настоящей
//     цепочке; всякий объект, который она добавила к рендеру, обязан быть узнан.
//     Когда #1276 снимет зависимости, контролю станет нечего поднимать — число
//     поднятых печатается, и ноль назван вслух, а не молчит;
//   - инъекция по каждой оси распознавателя на каждой цепочке и перевод
//     посадки настоящей ручкой профиля — в файле ..._injection_test.go.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРОВЕРКА НЕ УТВЕРЖДАЕТ
//
// Что стенд ИСПОЛНЯЕТ этот рендер: провенанс поднятого стенда — отдельный
// предмет (`deploy/scripts/stand-provenance.sh`). Что внегитовый слой учётных
// данных боевой площадки не поднимает поставщика: его в дереве нет, и скрипт
// раскатки добавляет его сам. Что упоминаний поставщика в развёртывании ноль:
// это другой предикат (строки текста), и у него другой держатель.
package deploy_test

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/internal/identityvendor"
	"github.com/PRO-Robotech/kacho/internal/retiredknobs"
)

// Оси распознавателя — узлы-идентификаторы объекта рендера.
const (
	vendorAxisName   = "имя"
	vendorAxisImage  = "образ"
	vendorAxisChart  = "метка чарта"
	vendorAxisSource = "подчарт в Source"
)

// Половины стенда и их носители посадки.
const (
	ownPosture             = "own"
	accessServiceConfigMap = "kaname-config"
	accessConfigKey        = "config.yaml"
	accessPostureKeyPath   = "authn.identity-provider"
	edgePostureEnv         = "KACHO_API_GATEWAY_IDENTITY_PROVIDER"
)

var (
	errOwnRenderEmptyWalk = errors.New("обход пуст")
	errOwnRenderNoObjects = errors.New("рендер без единого объекта")
)

// brandBounded — бренд с границей буквы с обеих сторон, в нижнем регистре.
var brandBounded = regexp.MustCompile(`(^|[^a-z])` + regexp.QuoteMeta(identityvendor.Brand()) + `([^a-z]|$)`)

// vendorWordIn — слово поставщика в узле-идентификаторе либо пусто.
func vendorWordIn(s string) string {
	low := strings.ToLower(s)
	for _, m := range identityvendor.Marks() {
		if strings.Contains(low, m) {
			return m
		}
	}
	if brandBounded.MatchString(low) {
		return identityvendor.Brand()
	}
	return ""
}

// ownRenderDoc — документ рендера: путь `# Source:` и разобранное тело.
type ownRenderDoc struct {
	Source string
	Body   map[string]any
}

func (d ownRenderDoc) kind() string { s, _ := d.Body["kind"].(string); return s }

func (d ownRenderDoc) meta() map[string]any { m, _ := d.Body["metadata"].(map[string]any); return m }

func (d ownRenderDoc) name() string { s, _ := d.meta()["name"].(string); return s }

// splitRender — документы вывода helm в порядке печати. Разделитель —
// строка `---` с нулевого отступа: внутри блочного скаляра она всегда с
// отступом. Неразборный документ — ОТКАЗ: пропустить его значило бы сузить
// перепись молча.
func splitRender(rendered string) ([]ownRenderDoc, error) {
	var (
		docs  []ownRenderDoc
		chunk []string
	)
	flush := func() error {
		defer func() { chunk = chunk[:0] }()
		text := strings.Join(chunk, "\n")
		if strings.TrimSpace(text) == "" {
			return nil
		}
		d := ownRenderDoc{}
		for _, l := range chunk {
			if s, ok := strings.CutPrefix(l, "# Source: "); ok {
				d.Source = strings.TrimSpace(s)
				break
			}
		}
		if err := yaml.Unmarshal([]byte(text), &d.Body); err != nil {
			return fmt.Errorf("документ %d (%s) не разбирается: %w", len(docs)+1, d.Source, err)
		}
		docs = append(docs, d)
		return nil
	}
	for _, l := range strings.Split(rendered, "\n") {
		if strings.TrimRight(l, " \t\r") == "---" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		chunk = append(chunk, l)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return docs, nil
}

// ownPodSpec — спецификация пода объекта любого вида, несущего под.
func ownPodSpec(body map[string]any) map[string]any {
	spec, _ := body["spec"].(map[string]any)
	if k, _ := body["kind"].(string); k == "Pod" {
		return spec
	}
	if jt, ok := spec["jobTemplate"].(map[string]any); ok {
		spec, _ = jt["spec"].(map[string]any)
	}
	tpl, _ := spec["template"].(map[string]any)
	ps, _ := tpl["spec"].(map[string]any)
	return ps
}

// ownContainers — все контейнеры пода: основные, init и эфемерные.
func ownContainers(body map[string]any) []map[string]any {
	ps := ownPodSpec(body)
	var out []map[string]any
	for _, group := range []string{"initContainers", "containers", "ephemeralContainers"} {
		list, _ := ps[group].([]any)
		for _, c := range list {
			if m, ok := c.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

// vendorHit — узел-идентификатор, назвавший поставщика.
type vendorHit struct{ Axis, Value string }

// vendorHitsOf — узлы объекта, называющие поставщика.
func vendorHitsOf(d ownRenderDoc) []vendorHit {
	var hits []vendorHit
	add := func(axis, v string) {
		if v != "" && vendorWordIn(v) != "" {
			hits = append(hits, vendorHit{axis, v})
		}
	}
	add(vendorAxisName, d.name())
	for _, c := range ownContainers(d.Body) {
		img, _ := c["image"].(string)
		add(vendorAxisImage, img)
	}
	labels, _ := d.meta()["labels"].(map[string]any)
	for _, k := range []string{"helm.sh/chart", "app.kubernetes.io/name"} {
		v, _ := labels[k].(string)
		add(vendorAxisChart, v)
	}
	// Путь подчарта — сегмент после каждого `charts/`; имя файла шаблона
	// принадлежит чарту, в котором он лежит, и подчартом не является.
	segs := strings.Split(d.Source, "/")
	for i := 0; i+1 < len(segs); i++ {
		if segs[i] == "charts" {
			add(vendorAxisSource, segs[i+1])
		}
	}
	return hits
}

// postureRules — что пины говорят о ключах посадки.
type postureRules struct {
	AccessKeyRetired bool // пин службы доступа снял ключ посадки
	EdgeKnobRetired  bool // ведомость края сняла ручку посадки
}

// chainRender — рендер одной цепочки.
type chainRender struct {
	Name     string
	Profiles []string
	Text     string
}

// chainVerdict — вердикт и перепись одной цепочки.
type chainVerdict struct {
	Name          string
	Profiles      []string
	Objects       int
	Images        int
	VendorObjects []string
	VendorImages  []string
	AccessPosture string
	EdgePosture   string
	AccessImages  []string
	Findings      []string
}

// accessPostureOf — посадка службы доступа по её карте настроек.
func accessPostureOf(docs []ownRenderDoc) (value string, present, found bool, err error) {
	for _, d := range docs {
		if d.kind() != "ConfigMap" || d.name() != accessServiceConfigMap {
			continue
		}
		data, _ := d.Body["data"].(map[string]any)
		body, _ := data[accessConfigKey].(string)
		var cfg map[string]any
		if e := yaml.Unmarshal([]byte(body), &cfg); e != nil || cfg == nil {
			return "", false, true, fmt.Errorf("%s карты %s не разбирается: %v", accessConfigKey, accessServiceConfigMap, e)
		}
		raw, ok := lookup(cfg, strings.Split(accessPostureKeyPath, ".")...)
		v, _ := raw.(string)
		return strings.TrimSpace(v), ok, true, nil
	}
	return "", false, false, nil
}

// edgePostureOf — посадка края по переменной его пода.
func edgePostureOf(docs []ownRenderDoc) (value string, present, found bool) {
	for _, d := range docs {
		if d.kind() != "Deployment" || d.name() != edgeDeploymentName {
			continue
		}
		found = true
		for _, c := range ownContainers(d.Body) {
			env, _ := c["env"].([]any)
			for _, e := range env {
				em, _ := e.(map[string]any)
				if n, _ := em["name"].(string); n == edgePostureEnv {
					v, _ := em["value"].(string)
					return strings.TrimSpace(v), true, true
				}
			}
		}
	}
	return "", false, found
}

// imageRepository — ссылка образа без дайджеста и тега. Двоеточие до последней
// косой черты — порт реестра, а не тег.
func imageRepository(ref string) string {
	ref, _, _ = strings.Cut(ref, "@")
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		ref = ref[:i]
	}
	return ref
}

// judgeOwnRenders — ЯДРО гейта, чистое от helm и файловой системы.
func judgeOwnRenders(renders []chainRender, rules postureRules) ([]chainVerdict, error) {
	if len(renders) == 0 {
		return nil, fmt.Errorf("%w: цепочек 0 — «находок ноль» здесь означало бы «не прочитано ничего»", errOwnRenderEmptyWalk)
	}
	out := make([]chainVerdict, 0, len(renders))
	for _, r := range renders {
		docs, err := splitRender(r.Text)
		if err != nil {
			return nil, fmt.Errorf("цепочка %s: %w", r.Name, err)
		}
		v := chainVerdict{Name: r.Name, Profiles: r.Profiles}
		label := fmt.Sprintf("цепочка %s (%s)", r.Name, strings.Join(r.Profiles, " + "))
		accessSeen, vendorSeen := map[string]bool{}, map[string]bool{}
		for _, d := range docs {
			if d.kind() == "" {
				continue
			}
			v.Objects++
			for _, c := range ownContainers(d.Body) {
				img, _ := c["image"].(string)
				if img == "" {
					continue
				}
				v.Images++
				if repo := imageRepository(img); strings.HasSuffix(repo, "/"+kanameModulePart) && !accessSeen[img] {
					accessSeen[img] = true
					v.AccessImages = append(v.AccessImages, img)
				}
			}
			hits := vendorHitsOf(d)
			if len(hits) == 0 {
				continue
			}
			obj := d.kind() + "/" + d.name()
			var parts []string
			for _, h := range hits {
				parts = append(parts, fmt.Sprintf("%s=%q", h.Axis, h.Value))
				if h.Axis == vendorAxisImage && !vendorSeen[h.Value] {
					vendorSeen[h.Value] = true
					v.VendorImages = append(v.VendorImages, h.Value)
				}
			}
			v.VendorObjects = append(v.VendorObjects, obj)
			v.Findings = append(v.Findings, fmt.Sprintf("%s: объект поставщика %s (%s; источник %s) — человека "+
				"на этой цепочке проверяет НАША полоса, и объект поставщика здесь — вторая дверь, о "+
				"которой никто не решал", label, obj, strings.Join(parts, ", "), d.Source))
		}
		if v.Objects == 0 {
			return nil, fmt.Errorf("%s: %w — судить было нечего", label, errOwnRenderNoObjects)
		}

		acc, present, found, err := accessPostureOf(docs)
		switch {
		case err != nil:
			v.Findings = append(v.Findings, fmt.Sprintf("%s: служба доступа — %v", label, err))
		case !found:
			v.Findings = append(v.Findings, fmt.Sprintf("%s: служба доступа — карты настроек %s в рендере нет, "+
				"посадку судить не по чему", label, accessServiceConfigMap))
		case rules.AccessKeyRetired && present:
			v.Findings = append(v.Findings, fmt.Sprintf("%s: служба доступа получает %s=%q, а пин службы этот "+
				"ключ снял — процесс откажет в старте", label, accessPostureKeyPath, acc))
		case rules.AccessKeyRetired:
			v.AccessPosture = ownPosture + " (ключ снят пином — посадка одна)"
		case acc != ownPosture:
			v.Findings = append(v.Findings, fmt.Sprintf("%s: служба доступа получает посадку %q (карта %s, ключ %s) "+
				"— ждали %s", label, acc, accessServiceConfigMap, accessPostureKeyPath, ownPosture))
		default:
			v.AccessPosture = acc
		}

		edge, present, found := edgePostureOf(docs)
		switch {
		case !found:
			v.Findings = append(v.Findings, fmt.Sprintf("%s: край — пода %s в рендере нет, посадку судить не по чему",
				label, edgeDeploymentName))
		case rules.EdgeKnobRetired && present:
			v.Findings = append(v.Findings, fmt.Sprintf("%s: край получает %s=%q, а ручка снята ведомостью края",
				label, edgePostureEnv, edge))
		case rules.EdgeKnobRetired:
			v.EdgePosture = ownPosture + " (ручка снята — посадка одна)"
		case edge != ownPosture:
			v.Findings = append(v.Findings, fmt.Sprintf("%s: край получает посадку %q (под %s, переменная %s) — ждали %s",
				label, edge, edgeDeploymentName, edgePostureEnv, ownPosture))
		default:
			v.EdgePosture = edge
		}
		out = append(out, v)
	}
	return out, nil
}

// retiredSettingKeys — ключи перечня `retiredSettings` файла Go, прочитанные
// разбором объявления. Файла нет — перечня нет (exists=false): пин, на котором
// ключ посадки ещё жив. Файл есть, а объявления нет — ОТКАЗ: форма сменилась,
// и «ключ жив» было бы неправдой.
func retiredSettingKeys(path string) (keys []string, exists bool, err error) {
	src, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	f, err := parser.ParseFile(token.NewFileSet(), path, src, 0)
	if err != nil {
		return nil, true, fmt.Errorf("%s не разбирается: %w", path, err)
	}
	declared := false
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, _ := spec.(*ast.ValueSpec)
			for i, n := range vs.Names {
				if n.Name != "retiredSettings" || i >= len(vs.Values) {
					continue
				}
				declared = true
				lit, _ := vs.Values[i].(*ast.CompositeLit)
				if lit == nil {
					return nil, true, fmt.Errorf("%s: retiredSettings объявлен не составным литералом", path)
				}
				for _, el := range lit.Elts {
					row, _ := el.(*ast.CompositeLit)
					if row == nil {
						continue
					}
					for _, fld := range row.Elts {
						kv, _ := fld.(*ast.KeyValueExpr)
						if kv == nil {
							continue
						}
						if id, _ := kv.Key.(*ast.Ident); id == nil || id.Name != "Key" {
							continue
						}
						bl, _ := kv.Value.(*ast.BasicLit)
						if bl == nil || bl.Kind != token.STRING {
							return nil, true, fmt.Errorf("%s: ключ снятой настройки записан не строкой", path)
						}
						k, err := strconv.Unquote(bl.Value)
						if err != nil {
							return nil, true, err
						}
						keys = append(keys, k)
					}
				}
			}
		}
	}
	if !declared {
		return nil, true, fmt.Errorf("%s: объявления retiredSettings нет — форма перечня сменилась", path)
	}
	sort.Strings(keys)
	return keys, true, nil
}

// currentPostureRules — что пины ЭТОГО дерева говорят о ключах посадки.
func currentPostureRules(t *testing.T) postureRules {
	t.Helper()
	path := filepath.Join(kanameModuleDir(t, ".."), "internal", "apps", "kaname", "config", "retired_settings.go")
	keys, _, err := retiredSettingKeys(path)
	if err != nil {
		t.Fatalf("перечень снятых настроек пиненной службы не прочитан: %v", err)
	}
	var r postureRules
	for _, k := range keys {
		if k == accessPostureKeyPath {
			r.AccessKeyRetired = true
		}
	}
	_, r.EdgeKnobRetired = retiredknobs.Edge()[edgePostureEnv]
	return r
}

var (
	ownRenderMu    sync.Mutex
	ownRenderCache = map[string]string{}
)

// renderChainCached — рендер умбреллы цепочкой профилей и ручками `--set`.
// Берётся ТОЛЬКО стандартный вывод: предупреждение helm в потоке ошибок,
// смешанное с документами, сделало бы рендер неразборным. Отказ helm — отказ
// пробы: условие не создано, вердикта нет.
func renderChainCached(t *testing.T, chain []string, sets ...string) string {
	t.Helper()
	key := strings.Join(chain, ",") + "|" + strings.Join(sets, ",")
	ownRenderMu.Lock()
	defer ownRenderMu.Unlock()
	if out, ok := ownRenderCache[key]; ok {
		return out
	}
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatalf("helm не в PATH — рендерная проба под тегом helmcharts обязана исполняться, " +
			"а не пропускаться: без helm условие этого задания не создано")
	}
	args := []string{"template", "kacho-umbrella", umbrellaDir, "-n", "kacho"}
	for _, p := range chain {
		args = append(args, "-f", filepath.Join(umbrellaDir, p))
	}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("helm", args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("рендер цепочки %v %v не выполнен (%v) — условие не создано, вердикта нет:\n%s",
			chain, sets, err, stderr.String())
	}
	ownRenderCache[key] = stdout.String()
	return stdout.String()
}

func TestNoStackRendersAnIdentityVendorObject(t *testing.T) {
	stacks := deployStacks(t)
	rules := currentPostureRules(t)
	names := sortedStackNames(stacks)
	renders := make([]chainRender, 0, len(names))
	for _, n := range names {
		renders = append(renders, chainRender{Name: n, Profiles: stacks[n], Text: renderChainCached(t, stacks[n])})
	}
	verdicts, err := judgeOwnRenders(renders, rules)
	if err != nil {
		t.Fatalf("вердикта нет: %v", err)
	}
	objects, vendorObjects, vendorImages, own := 0, 0, 0, 0
	for _, v := range verdicts {
		objects += v.Objects
		vendorObjects += len(v.VendorObjects)
		vendorImages += len(v.VendorImages)
		if v.AccessPosture != "" && v.EdgePosture != "" {
			own++
		}
		t.Logf("  %-11s (%s): объектов %d · образов %d · объектов поставщика %d · образов поставщика %d · "+
			"посадка: служба доступа %s, край %s · образ службы доступа %v",
			v.Name, strings.Join(v.Profiles, " + "), v.Objects, v.Images, len(v.VendorObjects),
			len(v.VendorImages), orDash(v.AccessPosture), orDash(v.EdgePosture), v.AccessImages)
		for _, f := range v.Findings {
			t.Error(f)
		}
	}
	t.Logf("перепись: цепочек %d (таблица %s) · объектов осмотрено %d · объектов поставщика %d · "+
		"образов поставщика %d · посадка own у обеих половин на %d из %d · словарь: отметок %d + бренд",
		len(verdicts), stacksTable, objects, vendorObjects, vendorImages, own, len(verdicts),
		len(identityvendor.Marks()))
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// vendorDep — зависимость Chart.yaml, называющая поставщика.
type vendorDep struct{ Key, Condition string }

// vendorDependencies — зависимости Chart.yaml, чьё имя, псевдоним или
// репозиторий называет поставщика. Перечня зависимостей нет — отказ.
func vendorDependencies(chart []byte) ([]vendorDep, error) {
	var c struct {
		Dependencies []struct {
			Name, Alias, Repository, Condition string
		} `yaml:"dependencies"`
	}
	if err := yaml.Unmarshal(chart, &c); err != nil {
		return nil, err
	}
	if len(c.Dependencies) == 0 {
		return nil, errors.New("в Chart.yaml нет ни одной зависимости — обход пуст")
	}
	var out []vendorDep
	for _, d := range c.Dependencies {
		if vendorWordIn(d.Name) == "" && vendorWordIn(d.Alias) == "" && vendorWordIn(d.Repository) == "" {
			continue
		}
		key := d.Name
		if d.Alias != "" {
			key = d.Alias
		}
		out = append(out, vendorDep{Key: key, Condition: d.Condition})
	}
	return out, nil
}

// objectKeys — «Вид/имя» объектов рендера.
func objectKeys(t *testing.T, text string) map[string]bool {
	t.Helper()
	docs, err := splitRender(text)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, d := range docs {
		if d.kind() != "" {
			out[d.kind()+"/"+d.name()] = true
		}
	}
	return out
}

// Живой контроль: зависимость поставщика, поднятая своим условием на настоящей
// цепочке, добавляет объекты — и каждый из них узнан.
func TestOwnRenderControl_RaisedVendorDependencyIsFound(t *testing.T) {
	chartYAML, err := os.ReadFile(filepath.Join(umbrellaDir, "Chart.yaml"))
	if err != nil {
		t.Fatalf("Chart.yaml зонта не читается: %v", err)
	}
	deps, err := vendorDependencies(chartYAML)
	if err != nil {
		t.Fatal(err)
	}
	stacks := deployStacks(t)
	chainName := sortedStackNames(stacks)[0]
	chain := stacks[chainName]
	if len(deps) == 0 {
		t.Logf("живой контроль БЕЗ ПРЕДМЕТА: зависимостей поставщика в Chart.yaml 0 — поднимать нечего; " +
			"способность упасть держат инъекции по каждой оси (..._injection_test.go)")
		return
	}
	rules := currentPostureRules(t)
	base := objectKeys(t, renderChainCached(t, chain))
	var all []string
	for _, d := range deps {
		if d.Condition == "" {
			t.Errorf("зависимость поставщика %s без условия — поднимается безусловно и видна гейту сама, "+
				"контроль ставить нечем", d.Key)
			continue
		}
		all = append(all, d.Condition+"=true")
		raised := renderChainCached(t, chain, d.Condition+"=true")
		added := 0
		verdict := judgeOne(t, chainName, chain, raised, rules)
		found := map[string]bool{}
		for _, o := range verdict.VendorObjects {
			found[o] = true
		}
		for k := range objectKeys(t, raised) {
			if base[k] {
				continue
			}
			added++
			if !found[k] {
				t.Errorf("цепочка %s, поднята %s: объект %s добавлен зависимостью поставщика, а распознаватель "+
					"его не узнал — «ноль» гейта был бы слепотой", chainName, d.Key, k)
			}
		}
		if added == 0 {
			t.Fatalf("цепочка %s, поднята %s: рендер не добавил ни одного объекта — контроль не создан", chainName, d.Key)
		}
		t.Logf("  контроль %s (%s=true): объектов добавлено %d · объектов поставщика узнано %d · образов поставщика %d",
			d.Key, d.Condition, added, len(verdict.VendorObjects), len(verdict.VendorImages))
	}
	// Все зависимости поставщика разом — на КАЖДОЙ цепочке: находка называет цепочку.
	for _, n := range sortedStackNames(stacks) {
		v := judgeOne(t, n, stacks[n], renderChainCached(t, stacks[n], all...), rules)
		if len(v.VendorObjects) == 0 || !strings.Contains(strings.Join(v.Findings, "\n"), "цепочка "+n+" ") {
			t.Errorf("цепочка %s с поднятым поставщиком: объектов поставщика %d, находка цепочку не называет",
				n, len(v.VendorObjects))
			continue
		}
		t.Logf("  контроль на цепочке %-11s: поставщик поднят (%d условий) — объектов поставщика %d, образов %d",
			n, len(all), len(v.VendorObjects), len(v.VendorImages))
	}
	t.Logf("перепись контроля: зависимостей поставщика в Chart.yaml %d · цепочка поимённого подъёма %s · цепочек общего подъёма %d",
		len(deps), chainName, len(stacks))
}
