// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// profileknobreader.go — гейт на КЛАСС: ключ, объявленный в профиле
// развёртывания, обязан иметь читателя.
//
// # Предмет
//
// Профиль развёртывания — это то, что оператор читает как заявление о посадке.
// Строка `breakglass: false` в боевом профиле сообщает ему: «аварийный обход
// закрыт». Если этот ключ не читает ни шаблон чарта, ни процесс, заявление
// ложно дважды: обхода нет вовсе (значит закрывать нечего), а выставленное
// `true` не открыло бы его и подавно. Оператор при этом уверен, что распоряжается
// поверхностью безопасности.
//
// Ровно это и стояло в дереве: ключ аварийного обхода авторизации `kacho-nlb`
// объявлялся тремя профилями (боевым, kind-боевым и dev), а читателя не имел ни
// одного — ни `.Values`-ссылки в шаблонах чарта, ни поля в описателе настроек
// процесса. Его прежний читатель был снят вместе с мёртвой фабрикой
// перехватчика (2f4ffc0e), а объявления пережили снятие.
//
// Класс шире одного ключа и одного сервиса: «объявлено и не читается» — это
// [[checks-with-form-but-no-substance]] на поверхности развёртывания. Поэтому
// проверка не знает слова «breakglass» и не ищет его: она сверяет ВСЕ ключи ВСЕХ
// профилей всех чартов, чьи исходники лежат в этом дереве.
//
// # Почему «читатель в шаблоне» — необходимое и достаточное условие
//
// Значение из профиля доходит до процесса ровно одним путём: шаблон чарта
// подставляет его в ConfigMap/env, процесс читает получившееся. Шаблона, который
// на ключ не ссылается, достаточно, чтобы значение не покинуло профиль, — до
// описателя настроек оно не доедет никогда. Поэтому читателем считается ссылка
// `.Values.<путь>` (либо `index .Values "a" "b"`) в шаблонах чарта, которому ключ
// адресован, либо в шаблонах его родителя. Ссылка на ПРЕФИКС пути читателем
// является: `with .Values.mtls` / `toYaml .Values.opaSidecar.networkPolicy`
// вводят в область видимости всё поддерево.
//
// # Предпосылка проверки — и её самопроверка
//
// Вывод «до процесса не доедет» верен, пока значения родителя попадают в
// сабчарт только тремя путями: по имени сабчарта, через `global`, либо через
// `import-values` в `Chart.yaml`. Третий путь этот вывод отменяет — он копирует
// произвольное поддерево родителя в сабчарт, — поэтому проверка ищет
// `import-values` сама и ОТКАЗЫВАЕТ, найдя: её предпосылки в этом дереве больше
// нет, и молчать она права не имеет.
//
// # Что не осматривается и почему это названо числом
//
// Ключи, адресованные ЧУЖОМУ сабчарту (postgresql, hydra, kratos, ingress-nginx,
// cert-manager), не осматриваются: их шаблонов в этом дереве нет,
// и «читателя не нашли» означало бы «не читали». Такие ключи считаются
// отдельно, чтобы «ноль находок» было отличимо от «ноль прочитанного».
// Поддерево `global` тоже вне охвата — оно видно каждому сабчарту, включая
// чужие.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// knobFinding — объявленный ключ, у которого нет читателя.
type knobFinding struct {
	// File — путь профиля относительно корня дерева.
	File string
	// Key — путь ключа в том виде, в каком он объявлен (с именем сабчарта).
	Key string
	// Chart — чарт, которому ключ адресован.
	Chart string
}

func (f knobFinding) String() string {
	return fmt.Sprintf("%s: %s (адресован чарту %q, читателя нет)", f.File, f.Key, f.Chart)
}

// knobCensus — объём осмотренного. Печатается всегда: без него «ноль находок»
// неотличимо от «ноль прочитанного».
type knobCensus struct {
	Charts        int
	TemplateFiles int
	ValueFiles    int
	// KeysEnforced — листья, к которым требование читателя применено.
	KeysEnforced int
	// KeysForeignChart — листья, адресованные чужому сабчарту (не осмотрены).
	KeysForeignChart int
	// KeysGlobal — листья поддерева `global` (не осмотрены).
	KeysGlobal int
	// Findings — сколько из осмотренных не имеют читателя.
	Findings int
}

func (c knobCensus) String() string {
	return fmt.Sprintf(
		"осмотрено: чартов %d, файлов шаблонов %d, файлов значений %d, "+
			"ключей под требованием читателя %d; не осмотрено: чужой сабчарт %d, global %d; "+
			"ОБЪЯВЛЕНО БЕЗ ЧИТАТЕЛЯ: %d",
		c.Charts, c.TemplateFiles, c.ValueFiles, c.KeysEnforced,
		c.KeysForeignChart, c.KeysGlobal, c.Findings)
}

// knobRefDotted — ссылка вида `.Values.a.b.c` (в т.ч. `$.Values.…`).
var knobRefDotted = regexp.MustCompile(`\.Values\.([A-Za-z_][\w]*(?:\.[A-Za-z_][\w]*)*)`)

// knobRefIndexed — ссылка вида `index .Values "a" "b"`. Нужна не для полноты:
// именно так родитель добирается до сабчарта, чьё имя содержит дефис
// (`index .Values "api-gateway"`), а дефис в точечной форме
// шаблонизатором не разбирается. Без этой формы такие ключи выглядели бы
// непрочитанными — то есть проверка ловила бы синтаксис записи, а не предмет.
var knobRefIndexed = regexp.MustCompile(`index\s+\$?\.Values((?:\s+"[^"]+")+)`)

var knobRefQuoted = regexp.MustCompile(`"([^"]+)"`)

// knobChart — чарт, исходники которого лежат в этом дереве.
type knobChart struct {
	Name string
	// Dir — путь каталога чарта относительно корня дерева.
	Dir string
	// Refs — пути значений, на которые ссылаются шаблоны ЭТОГО чарта.
	Refs map[string]bool
	// Templates — сколько файлов шаблонов прочитано.
	Templates int
	// Values — файлы значений чарта (values.yaml, values.<профиль>.yaml).
	Values []string
	// Subcharts — имя, под которым ключ адресуется сабчарту → чарт-получатель.
	// Пусто у чарта без сабчартов.
	Subcharts map[string]*knobChart
	// Foreign — имена сабчартов, чьих исходников в дереве нет.
	Foreign map[string]bool
	// Conditions — `condition:` зависимостей. Такой ключ читает сам helm,
	// решая, ставить сабчарт или нет; шаблон о нём ничего не знает.
	Conditions map[string]bool
	// declaredSubcharts — псевдонимы зависимостей до резолва в чарты дерева.
	declaredSubcharts knobChartDeps
}

// knobChartDeps — объявленные зависимости чарта: псевдоним → зависимость.
type knobChartDeps = map[string]knobDep

// auditProfileKnobReaders — перепись по дереву root.
//
// Возвращает находки (объявленный ключ без читателя), объём осмотренного и
// ошибку. Ошибка — это отказ, а не пустой результат: дерево без чартов и дерево
// с отменённой предпосылкой обязаны быть отличимы от чистого.
func auditProfileKnobReaders(root string) ([]knobFinding, knobCensus, error) {
	var census knobCensus

	tracked, err := treecorpus.Under(root)
	if err != nil {
		return nil, census, err
	}

	byDir := map[string]*knobChart{}
	for _, abs := range tracked {
		if filepath.Base(abs) != "Chart.yaml" {
			continue
		}
		dir := filepath.Dir(abs)
		rel, rerr := filepath.Rel(root, dir)
		if rerr != nil {
			return nil, census, fmt.Errorf("путь чарта %s относительно %s: %w", dir, root, rerr)
		}
		ch, cerr := loadKnobChart(abs, filepath.ToSlash(rel))
		if cerr != nil {
			return nil, census, cerr
		}
		byDir[dir] = ch
	}
	if len(byDir) == 0 {
		return nil, census, fmt.Errorf("в дереве %s не найдено ни одного Chart.yaml — "+
			"смотреть не на что; «ноль находок» на «ноль прочитанного» неотличимо от чистого дерева", root)
	}

	// Шаблоны и файлы значений — по отслеживаемому составу, а не обходом диска:
	// распакованные `.tgz` сабчартов и рабочие копии агентов в вердикт не входят.
	for _, abs := range tracked {
		dir := knobOwningChartDir(abs, byDir)
		if dir == "" {
			continue
		}
		ch := byDir[dir]
		relToChart, _ := filepath.Rel(dir, abs)
		relToChart = filepath.ToSlash(relToChart)
		switch {
		case strings.HasPrefix(relToChart, "templates/") &&
			knobIsTemplate(relToChart):
			body, rerr := os.ReadFile(abs) // #nosec G304 -- путь пришёл из индекса git ЭТОГО дерева (treecorpus), а не из ввода: включить посторонний файл нечем
			if rerr != nil {
				return nil, census, fmt.Errorf("шаблон %s: %w", abs, rerr)
			}
			ch.Templates++
			census.TemplateFiles++
			knobCollectRefs(string(body), ch.Refs)
		case knobIsValuesFile(relToChart):
			ch.Values = append(ch.Values, abs)
			census.ValueFiles++
		}
	}
	census.Charts = len(byDir)

	// Связь «родитель → сабчарт» выводится из дерева, а не из объявлений: helm
	// грузит из `<родитель>/charts/` ВСЁ, что является чартом, независимо от
	// того, объявлено оно зависимостью или нет. В этом дереве так лежат четыре
	// чарта, и объявлены они не были НАМЕРЕННО (объявление заставило бы helm
	// материализовать архив рядом с исходником — см. комментарий в Chart.yaml
	// зонтичного чарта). Проверка, знающая только объявления, приписала бы их
	// ключи родителю и объявила бы находкой каждый — то есть покраснела бы на
	// законном дереве.
	for dir, ch := range byDir {
		for otherDir, other := range byDir {
			if filepath.Dir(otherDir) == filepath.Join(dir, "charts") {
				ch.Subcharts[other.Name] = other
			}
		}
		for alias, dep := range ch.declaredSubcharts {
			if _, done := ch.Subcharts[alias]; done {
				continue
			}
			if sub := knobResolveSubchart(dir, dep, byDir); sub != nil {
				ch.Subcharts[alias] = sub
				continue
			}
			ch.Foreign[alias] = true
		}
	}

	var findings []knobFinding
	dirs := make([]string, 0, len(byDir))
	for dir := range byDir {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		ch := byDir[dir]
		sort.Strings(ch.Values)
		for _, vf := range ch.Values {
			rel, _ := filepath.Rel(root, vf)
			got, cerr := knobAuditValuesFile(vf, filepath.ToSlash(rel), ch, &census)
			if cerr != nil {
				return nil, census, cerr
			}
			findings = append(findings, got...)
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Key < findings[j].Key
	})
	census.Findings = len(findings)
	return findings, census, nil
}

// knobDep — объявленная зависимость чарта.
type knobDep struct {
	Name       string
	Repository string
}

// loadKnobChart читает Chart.yaml и заводит описание чарта.
//
// Здесь же проверяется предпосылка: `import-values` копирует поддерево родителя
// в сабчарт и тем самым отменяет вывод «шаблон не ссылается ⇒ значение никуда
// не доедет». Найдя его, проверка отказывает — запрет, чьё обоснование
// перестало быть верным, обязан об этом сказать, а не молчать.
func loadKnobChart(abs, relDir string) (*knobChart, error) {
	body, err := os.ReadFile(abs) // #nosec G304 -- путь пришёл из индекса git ЭТОГО дерева (treecorpus), а не из ввода: включить посторонний файл нечем
	if err != nil {
		return nil, fmt.Errorf("чарт %s: %w", abs, err)
	}
	var meta struct {
		Name         string `yaml:"name"`
		Dependencies []struct {
			Name         string `yaml:"name"`
			Alias        string `yaml:"alias"`
			Repository   string `yaml:"repository"`
			Condition    string `yaml:"condition"`
			ImportValues any    `yaml:"import-values"`
		} `yaml:"dependencies"`
	}
	if err := yaml.Unmarshal(body, &meta); err != nil {
		return nil, fmt.Errorf("чарт %s: %w", abs, err)
	}
	if meta.Name == "" {
		return nil, fmt.Errorf("чарт %s не называет себя (поле name) — "+
			"под каким именем ему адресуются ключи родителя, вывести неоткуда", abs)
	}
	ch := &knobChart{
		Name:              meta.Name,
		Dir:               relDir,
		Refs:              map[string]bool{},
		Subcharts:         map[string]*knobChart{},
		Foreign:           map[string]bool{},
		Conditions:        map[string]bool{},
		declaredSubcharts: map[string]knobDep{},
	}
	for _, d := range meta.Dependencies {
		if d.ImportValues != nil {
			return nil, fmt.Errorf("%s: зависимость %q объявляет import-values — "+
				"предпосылка этой проверки отменена. Она заключает «шаблон на ключ не "+
				"ссылается ⇒ значение до процесса не доедет», а import-values копирует "+
				"поддерево родителя в сабчарт в обход имён. Пока это объявление стоит, "+
				"проверка обязана отказывать, а не молчать: научите её читать import-values "+
				"либо снимите его", abs, d.Name)
		}
		alias := d.Alias
		if alias == "" {
			alias = d.Name
		}
		ch.declaredSubcharts[alias] = knobDep{Name: d.Name, Repository: d.Repository}
		if d.Condition != "" {
			ch.Conditions[d.Condition] = true
		}
	}
	return ch, nil
}

// knobResolveSubchart находит чарт-получатель для объявленной зависимости.
//
// Источник один — `file://`-путь: исходники сабчарта лежат в этом дереве, и его
// шаблоны можно прочитать. Всё остальное (публичные репозитории чартов) —
// чужое: шаблонов в дереве нет, искать читателя негде.
func knobResolveSubchart(parentDir string, dep knobDep, byDir map[string]*knobChart) *knobChart {
	if !strings.HasPrefix(dep.Repository, "file://") {
		return nil
	}
	target := filepath.Clean(filepath.Join(parentDir, strings.TrimPrefix(dep.Repository, "file://")))
	if sub, ok := byDir[target]; ok {
		return sub
	}
	return nil
}

// knobOwningChartDir — каталог чарта, которому принадлежит файл: САМЫЙ ДЛИННЫЙ
// подходящий префикс. Иначе `charts/kaname/templates/…` засчитался бы
// родителю, и ссылки сабчарта стали бы ссылками родителя — гейт замолчал бы
// ровно там, где вложенность глубже.
func knobOwningChartDir(abs string, byDir map[string]*knobChart) string {
	best := ""
	for dir := range byDir {
		if !strings.HasPrefix(abs, dir+string(filepath.Separator)) {
			continue
		}
		if len(dir) > len(best) {
			best = dir
		}
	}
	return best
}

func knobIsTemplate(rel string) bool {
	for _, s := range []string{".yaml", ".yml", ".tpl", ".txt", ".json"} {
		if strings.HasSuffix(rel, s) {
			return true
		}
	}
	return false
}

// knobIsValuesFile — файл значений чарта: `values.yaml` и профили
// `values.<имя>.yaml` в корне каталога чарта.
func knobIsValuesFile(rel string) bool {
	if strings.Contains(rel, "/") {
		return false
	}
	return strings.HasPrefix(rel, "values") && strings.HasSuffix(rel, ".yaml")
}

func knobCollectRefs(body string, into map[string]bool) {
	for _, m := range knobRefDotted.FindAllStringSubmatch(body, -1) {
		into[m[1]] = true
	}
	for _, m := range knobRefIndexed.FindAllStringSubmatch(body, -1) {
		var parts []string
		for _, q := range knobRefQuoted.FindAllStringSubmatch(m[1], -1) {
			parts = append(parts, q[1])
		}
		if len(parts) > 0 {
			into[strings.Join(parts, ".")] = true
		}
	}
}

// knobAuditValuesFile разбирает один файл значений и относит каждый лист к
// чарту-получателю.
func knobAuditValuesFile(abs, rel string, owner *knobChart, census *knobCensus) ([]knobFinding, error) {
	body, err := os.ReadFile(abs) // #nosec G304 -- путь пришёл из индекса git ЭТОГО дерева (treecorpus), а не из ввода: включить посторонний файл нечем
	if err != nil {
		return nil, fmt.Errorf("значения %s: %w", abs, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("значения %s: %w", abs, err)
	}

	var findings []knobFinding
	for _, path := range knobLeaves(doc, nil) {
		top := path[0]
		if top == "global" {
			census.KeysGlobal++
			continue
		}
		target, rest := owner, path
		if sub, ok := owner.Subcharts[top]; ok {
			target, rest = sub, path[1:]
		} else if owner.Foreign[top] {
			census.KeysForeignChart++
			continue
		}
		census.KeysEnforced++

		full := strings.Join(path, ".")
		// Ключ включения сабчарта читает сам helm по `condition:` — шаблону он
		// не виден by construction, и требовать от шаблона ссылку значило бы
		// объявить находкой каждое `<сабчарт>.enabled`.
		if owner.Conditions[full] {
			continue
		}
		if knobCovered(strings.Join(rest, "."), target.Refs) {
			continue
		}
		// Родитель вправе читать значения сабчарта напрямую — тогда читатель
		// есть, просто он этажом выше.
		if target != owner && knobCovered(full, owner.Refs) {
			continue
		}
		findings = append(findings, knobFinding{File: rel, Key: full, Chart: target.Name})
	}
	return findings, nil
}

// knobLeaves — пути до листьев отображения. Пустое отображение считается
// листом: оператор объявил ключ, и вопрос «кто его читает» к нему применим.
func knobLeaves(node any, prefix []string) [][]string {
	m, ok := node.(map[string]any)
	if !ok || len(m) == 0 {
		if len(prefix) == 0 {
			return nil
		}
		return [][]string{append([]string(nil), prefix...)}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out [][]string
	for _, k := range keys {
		out = append(out, knobLeaves(m[k], append(prefix, k))...)
	}
	return out
}

// knobCovered — есть ли среди ссылок такая, что путь key через неё доступен.
//
// Совпадение считается в ОБЕ стороны: ссылка на префикс (`with .Values.mtls`,
// `toYaml .Values.opaSidecar.networkPolicy`) вводит в область видимости всё
// поддерево, а ссылка на потомка означает, что узел на пути к нему прочитан.
func knobCovered(key string, refs map[string]bool) bool {
	if key == "" {
		return true
	}
	for ref := range refs {
		if ref == key ||
			strings.HasPrefix(key, ref+".") ||
			strings.HasPrefix(ref, key+".") {
			return true
		}
	}
	return false
}
