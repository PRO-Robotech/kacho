// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ciexemptionexpiry_test.go — послабление в конвейере обязано истекать САМО.
//
// # Предмет
//
// Послабление — это любое сужение охвата проверки: работа, замкнутая на ручной
// запуск; проверка, снятая для отдельного автора; каталог, выведенный из-под
// обновления. Каждое из них защитимо ровно до тех пор, пока у него есть
// ПРЕДМЕТ. Когда предмет исчезает, послабление не краснеет и не исчезает вместе
// с ним — оно продолжает действовать и выглядит решением, которое кто-то принял.
//
// Три экземпляра этого класса измерены по дереву 2026-08-12, и каждый держится
// здесь своим гейтом:
//
//  1. работа, исполняемая ТОЛЬКО ручным запуском, но живущая внутри конвейера с
//     автоматическими триггерами. Конвейер идёт, работа не идёт, и её молчание
//     неотличимо от зелёного среди зелёных соседей. Семь сквозных проб консоли
//     прожили так от написания до этой правки: слот в отчёте, а не проверка;
//  2. проверка, снятая для отдельного автора «ради единственного своего
//     ранера», при том что своих ранеров в дереве НЕТ НИ ОДНОГО. Предикат
//     снятия был при этом взят от числа таких ранеров — то есть от величины,
//     которую тот же переезд на ранеры площадки обнулил навсегда;
//  3. каталог npm, у которого нет своего файла блокировки: бот правит там
//     ОДИН манифест, общий файл блокировки остаётся прежним, и установка по
//     блокировке отказывается — работы такого предложения краснеют ПО
//     ПОСТРОЕНИЮ, сколько их ни пересобирай.
//
// # Почему гейты, а не запись в правилах
//
// Запись в правилах переживает свой предмет молча — этим классом и вызвана вся
// тройка. Гейт же вооружается сам: у каждого здесь предпосылка ПРОВЕРЯЕТСЯ, и
// послабление становится законным ровно тогда, когда предмет возвращается
// (появился свой ранер — сужение по автору снова защитимо; каталог завёл свой
// файл блокировки — он обязан ПОКИНУТЬ список исключений, иначе краснеет уже
// само исключение).
//
// # Перепись
//
// «Ноль находок» обязано отличаться от «ноль прочитанного»: каждый гейт
// печатает, сколько файлов прочитал, сколько работ разобрал и сколько предметов
// нашёл. Пустой обход — провал.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ─────────────────────────────────────────────────────────────────────────────
// ОБЩЕЕ: разбор конвейера.
// ─────────────────────────────────────────────────────────────────────────────

// yamlMapValue отдаёт значение ключа отображения по ТЕКСТУ ключа.
//
// Читается именно текст (`Node.Value`), а не разобранный тип: ключ `on` в
// YAML 1.1 разрешается как логическая истина, поэтому обращение по строке "on"
// к разобранному в map документу не находит ничего, и гейт молча считал бы, что
// у конвейера нет триггеров вовсе.
func yamlMapValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// yamlMapKeys отдаёт тексты ключей отображения в порядке объявления.
func yamlMapKeys(n *yaml.Node) []string {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	var out []string
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			out = append(out, n.Content[i].Value)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			out = append(out, c.Value)
		}
	case yaml.ScalarNode:
		out = append(out, n.Value)
	}
	return out
}

// wfJob — одна работа конвейера в том виде, в каком её судят гейты ниже.
type wfJob struct {
	File   string // относительный путь конвейера
	ID     string // имя работы
	Line   int    // строка объявления работы
	If     string // условие исполнения (пусто — безусловна)
	RunsOn string // площадка исполнения, склеенная в одну строку
}

// wfDoc — разобранный конвейер.
type wfDoc struct {
	File     string
	Triggers []string
	Jobs     []wfJob
}

// readWorkflows разбирает все конвейеры каталога dir.
//
// Отказ разбора — ОШИБКА, а не пропуск: конвейер, который гейт не смог
// прочитать, нельзя ни засчитать в перепись, ни молча обойти — и то и другое
// вернуло бы неразличимость «ноль находок» и «ноль прочитанного».
func readWorkflows(dir string) ([]wfDoc, error) {
	var docs []wfDoc
	err := rootedWalk(dir,
		func(rel string) bool {
			return strings.HasSuffix(rel, ".yml") || strings.HasSuffix(rel, ".yaml")
		},
		func(abs string, body []byte) error {
			var root yaml.Node
			if err := yaml.Unmarshal(body, &root); err != nil {
				return fmt.Errorf("%s: конвейер не разбирается: %w", abs, err)
			}
			doc := wfDoc{File: filepath.Base(abs)}
			doc.Triggers = yamlMapKeys(yamlMapValue(&root, "on"))
			jobs := yamlMapValue(&root, "jobs")
			if jobs != nil && jobs.Kind == yaml.MappingNode {
				for i := 0; i+1 < len(jobs.Content); i += 2 {
					k, v := jobs.Content[i], jobs.Content[i+1]
					j := wfJob{File: doc.File, ID: k.Value, Line: k.Line}
					if cond := yamlMapValue(v, "if"); cond != nil {
						j.If = cond.Value
					}
					if ro := yamlMapValue(v, "runs-on"); ro != nil {
						j.RunsOn = strings.Join(yamlMapKeys(ro), " ")
					}
					doc.Jobs = append(doc.Jobs, j)
				}
			}
			docs = append(docs, doc)
			return nil
		})
	if err != nil {
		return nil, err
	}
	sort.Slice(docs, func(a, b int) bool { return docs[a].File < docs[b].File })
	return docs, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// ГЕЙТ 1. Работа, замкнутая на ручной запуск, внутри автоматического конвейера.
// ─────────────────────────────────────────────────────────────────────────────

// eventEquals ловит сравнение имени события с конкретным значением.
// Читается ИСПОЛНЯЕМАЯ часть условия — то, что вычисляет площадка, — а не
// прозаический комментарий рядом с ним (комментария в разобранном документе не
// существует как узла, поэтому подмены текстом здесь быть не может).
var eventEquals = regexp.MustCompile(`github\.event_name\s*==\s*['"]([a-z_]+)['"]`)

// manualOnly — условие допускает ИСКЛЮЧИТЕЛЬНО ручной запуск.
//
// Разбирается направление сравнения и полнота набора событий, а не наличие
// слова: `event_name != 'workflow_dispatch'` сужает В ДРУГУЮ СТОРОНУ, а
// `== 'workflow_dispatch' || == 'push'` называет ещё и автоматическое событие —
// ни то ни другое находкой не является.
func manualOnly(cond string) bool {
	m := eventEquals.FindAllStringSubmatch(cond, -1)
	if len(m) == 0 {
		return false
	}
	for _, g := range m {
		if g[1] != "workflow_dispatch" {
			return false
		}
	}
	// Отрицание того же события возвращает работе автоматические запуски.
	return !strings.Contains(cond, "github.event_name !=")
}

// scanManualOnlyJobs отдаёт работы, замкнутые на ручной запуск ВНУТРИ
// конвейера, у которого есть хотя бы один автоматический триггер, плюс перепись
// осмотренного.
//
// Конвейер, у которого автоматических триггеров нет вовсе (разведка ранера —
// такой), находкой НЕ является: он честно ручной, и это видно по его заголовку.
// Опасен именно замкнутый на руки ОДИНОЧКА среди работающих соседей — его
// молчание неотличимо от зелёного.
func scanManualOnlyJobs(dir string) (finds []wfJob, files, jobs, autoFiles int, err error) {
	docs, err := readWorkflows(dir)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	for _, d := range docs {
		files++
		auto := 0
		for _, t := range d.Triggers {
			if t != "workflow_dispatch" {
				auto++
			}
		}
		if auto > 0 {
			autoFiles++
		}
		for _, j := range d.Jobs {
			jobs++
			if auto > 0 && manualOnly(j.If) {
				finds = append(finds, j)
			}
		}
	}
	return finds, files, jobs, autoFiles, nil
}

func TestManualOnlyJobNeverHidesInsideAutomaticWorkflow(t *testing.T) {
	dir := filepath.Join(repoRoot(t), ".github", "workflows")
	finds, files, jobs, autoFiles, err := scanManualOnlyJobs(dir)
	if err != nil {
		t.Fatalf("обход конвейеров: %v", err)
	}
	if files == 0 || jobs == 0 {
		t.Fatalf("перепись пуста: файлов %d, работ %d — «ноль находок» здесь означало бы "+
			"«ноль прочитанного», а это разные вещи", files, jobs)
	}
	t.Logf("осмотрено: конвейеров %d (из них с автоматическими триггерами %d), работ %d",
		files, autoFiles, jobs)

	if len(finds) > 0 {
		var b strings.Builder
		for _, f := range finds {
			fmt.Fprintf(&b, "\n  %s:%d  работа %q  if: %s", f.File, f.Line, f.ID, strings.TrimSpace(f.If))
		}
		t.Fatalf("работ, замкнутых на ручной запуск внутри автоматически запускаемого "+
			"конвейера: %d.%s\n\n"+
			"Такая работа не исполняется НИ ОДНИМ автоматическим триггером, а её соседи "+
			"исполняются — поэтому её молчание неотличимо от зелёного, и проверка "+
			"превращается в слот в отчёте.\n"+
			"Исходов три: (а) дать работе автоматический триггер; (б) если ей нужна "+
			"поднятая среда, которой в конвейере нет, — вынести её в СВОЙ конвейер, "+
			"создающий это условие (своя волна, а не маска); (в) снять работу вместе с "+
			"тем, что она проверяла.\n"+
			"Конвейер, целиком объявленный ручным, находкой не является — он честно "+
			"ручной, и это видно по его заголовку.", len(finds), b.String())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ГЕЙТ 2. Сужение по автору живёт, пока у него есть предмет — свой ранер.
// ─────────────────────────────────────────────────────────────────────────────

// scanActorExemptions отдаёт работы, сужающие охват проверки по АВТОРУ события,
// и число работ, исполняемых на своём (self-hosted) ранере.
func scanActorExemptions(dir string) (finds []wfJob, selfHosted, files, jobs int, err error) {
	docs, err := readWorkflows(dir)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	for _, d := range docs {
		files++
		for _, j := range d.Jobs {
			jobs++
			if strings.Contains(j.RunsOn, "self-hosted") {
				selfHosted++
			}
			if strings.Contains(j.If, "github.actor") {
				finds = append(finds, j)
			}
		}
	}
	return finds, selfHosted, files, jobs, nil
}

func TestActorExemptionRequiresAScarceRunnerThatExists(t *testing.T) {
	dir := filepath.Join(repoRoot(t), ".github", "workflows")
	finds, selfHosted, files, jobs, err := scanActorExemptions(dir)
	if err != nil {
		t.Fatalf("обход конвейеров: %v", err)
	}
	if files == 0 || jobs == 0 {
		t.Fatalf("перепись пуста: файлов %d, работ %d", files, jobs)
	}
	t.Logf("осмотрено: конвейеров %d, работ %d; на своём ранере %d; сужений по автору %d",
		files, jobs, selfHosted, len(finds))

	// ПРЕДПОСЫЛКА ГЕЙТА ПРОВЕРЯЕТСЯ, А НЕ ПОДРАЗУМЕВАЕТСЯ. Запрет обоснован тем,
	// что своей дефицитной площадки в дереве нет. Появится — предмет у сужения
	// вернётся, и запрет обязан отступить САМ, без чьей-либо памяти.
	if selfHosted > 0 {
		t.Logf("предпосылка запрета отсутствует: работ на своём ранере %d — "+
			"дефицитная площадка снова есть, сужение по автору защитимо", selfHosted)
		return
	}

	if len(finds) > 0 {
		var b strings.Builder
		for _, f := range finds {
			fmt.Fprintf(&b, "\n  %s:%d  работа %q  if: %s", f.File, f.Line, f.ID, strings.TrimSpace(f.If))
		}
		t.Fatalf("сужений охвата по автору события: %d, при том что работ на своём "+
			"ранере в дереве %d.%s\n\n"+
			"Такое сужение защитимо ровно одним доводом — своя площадка дефицитна и её "+
			"занял бы поток обновлений. Площадки нет: все работы идут на ранерах "+
			"площадки, где очередь общая и ничья.\n"+
			"Довод при этом сам себя обнулил: предикат снятия был взят от ЧИСЛА своих "+
			"ранеров, а переезд на ранеры площадки сделал это число нулём навсегда — "+
			"«снимем, когда их станет больше одного» перестало наступать вовсе.\n"+
			"Исходов два: снять сужение вместе с его предметом либо вернуть предмет "+
			"(свой ранер), и тогда гейт отступит сам.", len(finds), selfHosted, b.String())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ГЕЙТ 3. Бот не правит каталог npm, у которого нет своего файла блокировки.
// ─────────────────────────────────────────────────────────────────────────────

// globDepth переводит шаблон каталога вида `/*`, `/*/*`, … в глубину.
// Шаблон другой формы — ОТКАЗ: предпосылка гейта (перечень выводится из глубины
// шаблонов) перестала бы держаться, а молчаливый пропуск дал бы неполный вывод,
// неотличимый от «нечего исключать».
func globDepth(g string) (int, error) {
	segs := strings.Split(strings.Trim(g, "/"), "/")
	for _, s := range segs {
		if s != "*" {
			return 0, fmt.Errorf("шаблон %q не состоит из односегментных `*`: гейт выводит "+
				"глубину покрытия из формы шаблона, и на другой форме его предпосылка не держится", g)
		}
	}
	return len(segs), nil
}

// npmDirsWithoutOwnLock отдаёт каталоги, которые бот считает своим предметом
// (их манифест попадает под объявленные шаблоны), но у которых НЕТ своего файла
// блокировки: их зависимости разрешает общий файл участника рабочего
// пространства уровнем выше.
//
// Правка одного манифеста без этого общего файла делает установку по блокировке
// невозможной — предложение бота краснеет по построению и зелёным не станет
// никогда.
func npmDirsWithoutOwnLock(tracked []string, depths map[int]bool) []string {
	manifests := map[string]bool{}
	locks := map[string]bool{}
	for _, p := range tracked {
		switch filepath.Base(p) {
		case "package.json":
			manifests[filepath.ToSlash(filepath.Dir(p))] = true
		case "package-lock.json":
			locks[filepath.ToSlash(filepath.Dir(p))] = true
		}
	}
	var out []string
	for d := range manifests {
		if locks[d] {
			continue
		}
		if !depths[len(strings.Split(d, "/"))] {
			continue // глубже объявленных шаблонов — не предмет бота
		}
		out = append(out, "/"+d)
	}
	sort.Strings(out)
	return out
}

// dependabotNpmEntry достаёт из объявления бота запись экосистемы npm.
func dependabotNpmEntry(body []byte) (*yaml.Node, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("объявление бота не разбирается: %w", err)
	}
	ups := yamlMapValue(&root, "updates")
	if ups == nil || ups.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("в объявлении бота нет списка `updates`")
	}
	for _, e := range ups.Content {
		if eco := yamlMapValue(e, "package-ecosystem"); eco != nil && eco.Value == "npm" {
			return e, nil
		}
	}
	return nil, fmt.Errorf("в объявлении бота нет записи экосистемы npm")
}

// npmExclusionDrift сверяет объявленный список исключений с выведенным из
// дерева и отдаёт расхождение в ОБЕ стороны.
func npmExclusionDrift(tracked []string, dependabot []byte) (missing, stale []string, err error) {
	entry, err := dependabotNpmEntry(dependabot)
	if err != nil {
		return nil, nil, err
	}
	dirs := yamlMapValue(entry, "directories")
	if dirs == nil {
		return nil, nil, fmt.Errorf("у записи npm нет ключа `directories` — глубину покрытия вывести не из чего")
	}
	depths := map[int]bool{}
	for _, g := range yamlMapKeys(dirs) {
		d, derr := globDepth(g)
		if derr != nil {
			return nil, nil, derr
		}
		depths[d] = true
	}

	declared := map[string]bool{}
	for _, p := range yamlMapKeys(yamlMapValue(entry, "exclude-paths")) {
		declared["/"+strings.Trim(p, "/")] = true
	}

	want := npmDirsWithoutOwnLock(tracked, depths)
	wantSet := map[string]bool{}
	for _, d := range want {
		wantSet[d] = true
		if !declared[d] {
			missing = append(missing, d)
		}
	}
	for d := range declared {
		if !wantSet[d] {
			stale = append(stale, d)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	return missing, stale, nil
}

func TestDependabotSkipsNpmDirectoriesWithoutOwnLock(t *testing.T) {
	root := repoRoot(t)

	var tracked []string
	for _, line := range gitLsFiles(t, root) {
		if _, p, ok := parseLsFiles(line); ok {
			tracked = append(tracked, p)
		}
	}
	if len(tracked) == 0 {
		t.Fatal("индекс git пуст — гейт судил бы о дереве, которого не читал")
	}

	body, err := os.ReadFile(filepath.Join(root, ".github", "dependabot.yml"))
	if err != nil {
		t.Fatalf("объявление бота не читается: %v", err)
	}

	missing, stale, err := npmExclusionDrift(tracked, body)
	if err != nil {
		t.Fatalf("сверка исключений: %v", err)
	}
	t.Logf("осмотрено: отслеживаемых записей %d; каталогов npm без своего файла блокировки "+
		"под объявленными шаблонами — сверка дала %d недостающих и %d просроченных исключений",
		len(tracked), len(missing), len(stale))

	if len(missing) > 0 {
		t.Errorf("каталогов npm без своего файла блокировки, НЕ выведенных из-под бота: %d\n  %s\n\n"+
			"Бот правит там один манифест, а общий файл блокировки участника рабочего "+
			"пространства остаётся прежним, поэтому установка по блокировке отказывается: "+
			"предложение краснеет по построению и зелёным не станет никогда, сколько его ни "+
			"пересобирай. Обновления таких участников приезжают через корень рабочего "+
			"пространства, где правится и манифест, и общий файл блокировки.\n"+
			"Внеси перечисленные каталоги в `exclude-paths` записи npm.",
			len(missing), strings.Join(missing, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("исключений, которым больше нечего исключать: %d\n  %s\n\n"+
			"У каталога появился свой файл блокировки (либо каталога больше нет) — значит "+
			"предмет исключения исчез, а исключение осталось и молча выводит из-под "+
			"обновления то, что обновлять уже можно. Убери перечисленные пути из "+
			"`exclude-paths`.", len(stale), strings.Join(stale, "\n  "))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ИНЪЕКЦИЯ: каждый из трёх гейтов доказывается в ОБЕ стороны — он обязан
// покраснеть на настоящем экземпляре и смолчать на законном близнеце той же
// формы. Гейт, не доказавший, что умеет и то и другое, доказательством не
// является: первый же ложный срабат его отключит, а первый же пропуск сделает
// зелёным на сломанном.
// ─────────────────────────────────────────────────────────────────────────────

func writeWorkflow(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("фикстура %s: %v", name, err)
	}
}

func TestManualOnlyGateProvenByInjection(t *testing.T) {
	dir := t.TempDir()

	// ЛОВИТСЯ: работа замкнута на ручной запуск, а конвейер идёт автоматически.
	writeWorkflow(t, dir, "caught.yml", `
name: caught
on:
  pull_request:
    branches: [main]
  workflow_dispatch:
jobs:
  probes:
    if: github.event_name == 'workflow_dispatch'
    runs-on: ubuntu-latest
    steps: [{ run: "true" }]
`)

	// МОЛЧИТ: конвейер целиком ручной — работа честно ручная, и это видно.
	writeWorkflow(t, dir, "twin-all-manual.yml", `
name: twin-all-manual
on:
  workflow_dispatch:
jobs:
  recon:
    if: github.event_name == 'workflow_dispatch'
    runs-on: ubuntu-latest
    steps: [{ run: "true" }]
`)

	// МОЛЧИТ: сужение в ДРУГУЮ сторону — работа идёт на всех автоматических событиях.
	writeWorkflow(t, dir, "twin-negated.yml", `
name: twin-negated
on:
  push:
    branches: [main]
  workflow_dispatch:
jobs:
  auto:
    if: github.event_name != 'workflow_dispatch'
    runs-on: ubuntu-latest
    steps: [{ run: "true" }]
`)

	// МОЛЧИТ: перечислено и автоматическое событие тоже.
	writeWorkflow(t, dir, "twin-plus-push.yml", `
name: twin-plus-push
on:
  push:
    branches: [main]
  workflow_dispatch:
jobs:
  both:
    if: ${{ github.event_name == 'workflow_dispatch' || github.event_name == 'push' }}
    runs-on: ubuntu-latest
    steps: [{ run: "true" }]
`)

	finds, files, jobs, autoFiles, err := scanManualOnlyJobs(dir)
	if err != nil {
		t.Fatalf("обход фикстур: %v", err)
	}
	if files != 4 || jobs != 4 || autoFiles != 3 {
		t.Fatalf("перепись фикстур разошлась: файлов %d, работ %d, автоматических %d "+
			"(ждали 4/4/3) — гейт судил бы не о том наборе", files, jobs, autoFiles)
	}
	if len(finds) != 1 || finds[0].ID != "probes" {
		t.Fatalf("инъекция не воспроизведена: находок %d %v — ждали ровно одну, работу %q",
			len(finds), finds, "probes")
	}
}

func TestActorExemptionGateProvenByInjection(t *testing.T) {
	exempt := `
name: exempt
on:
  pull_request:
    branches: [main]
jobs:
  heavy:
    if: ${{ github.actor != 'dependabot[bot]' }}
    runs-on: ubuntu-latest
    steps: [{ run: "true" }]
`

	// ЛОВИТСЯ: сужение по автору есть, своего ранера нет.
	noRunner := t.TempDir()
	writeWorkflow(t, noRunner, "exempt.yml", exempt)
	finds, selfHosted, files, jobs, err := scanActorExemptions(noRunner)
	if err != nil {
		t.Fatalf("обход фикстур: %v", err)
	}
	if files != 1 || jobs != 1 {
		t.Fatalf("перепись фикстур разошлась: файлов %d, работ %d", files, jobs)
	}
	if len(finds) != 1 || selfHosted != 0 {
		t.Fatalf("инъекция не воспроизведена: сужений %d, своих ранеров %d — ждали 1 и 0",
			len(finds), selfHosted)
	}

	// МОЛЧИТ: предмет вернулся — в дереве появилась работа на своём ранере,
	// значит дефицитная площадка снова есть и сужение защитимо. Послабление
	// истекает и ВОЗВРАЩАЕТСЯ само, без чьей-либо памяти.
	withRunner := t.TempDir()
	writeWorkflow(t, withRunner, "exempt.yml", exempt)
	writeWorkflow(t, withRunner, "own-runner.yml", `
name: own-runner
on:
  push:
    branches: [main]
jobs:
  stand:
    runs-on: [self-hosted, linux]
    steps: [{ run: "true" }]
`)
	_, selfHosted2, _, _, err := scanActorExemptions(withRunner)
	if err != nil {
		t.Fatalf("обход фикстур: %v", err)
	}
	if selfHosted2 != 1 {
		t.Fatalf("предпосылка не считана: своих ранеров %d — ждали 1, и на этом гейт "+
			"обязан отступить сам", selfHosted2)
	}
}

func TestDependabotExclusionGateProvenByInjection(t *testing.T) {
	const decl = `
version: 2
updates:
  - package-ecosystem: npm
    directories: ['/*', '/*/*']
    exclude-paths:
      - '/app/member'
`
	tracked := []string{
		"app/package.json", "app/package-lock.json", // корень рабочего пространства — свой файл блокировки есть
		"app/member/package.json",                                         // участник без своего файла блокировки → обязан быть исключён
		"app/standalone/package.json", "app/standalone/package-lock.json", // самостоятельный — исключать нечего
		"deep/a/b/package.json", // глубже объявленных шаблонов — не предмет бота
	}

	// МОЛЧИТ: объявленное совпадает с выведенным из дерева.
	missing, stale, err := npmExclusionDrift(tracked, []byte(decl))
	if err != nil {
		t.Fatalf("сверка: %v", err)
	}
	if len(missing) != 0 || len(stale) != 0 {
		t.Fatalf("законный близнец помечен: недостающих %v, просроченных %v", missing, stale)
	}

	// ЛОВИТСЯ (сторона «не исключили»): участник без своего файла блокировки
	// остался под ботом — его предложения краснеют по построению.
	missing, _, err = npmExclusionDrift(tracked, []byte(`
version: 2
updates:
  - package-ecosystem: npm
    directories: ['/*', '/*/*']
`))
	if err != nil {
		t.Fatalf("сверка: %v", err)
	}
	if len(missing) != 1 || missing[0] != "/app/member" {
		t.Fatalf("инъекция не воспроизведена: недостающих %v — ждали ровно /app/member", missing)
	}

	// ЛОВИТСЯ (сторона «исключению нечего исключать»): у каталога появился свой
	// файл блокировки, а исключение осталось.
	_, stale, err = npmExclusionDrift(append(tracked, "app/member/package-lock.json"), []byte(decl))
	if err != nil {
		t.Fatalf("сверка: %v", err)
	}
	if len(stale) != 1 || stale[0] != "/app/member" {
		t.Fatalf("самоистечение не воспроизведено: просроченных %v — ждали ровно /app/member", stale)
	}

	// ОТКАЗ: шаблон другой формы — предпосылка «глубина выводится из шаблона»
	// не держится, и гейт обязан сказать это, а не выдать неполный вывод.
	if _, _, err = npmExclusionDrift(tracked, []byte(`
version: 2
updates:
  - package-ecosystem: npm
    directories: ['/**']
`)); err == nil {
		t.Fatal("шаблон неизвестной формы принят молча — гейт выдал бы неполный перечень, " +
			"неотличимый от «нечего исключать»")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ЧЕТВЁРТЫЙ ЭКЗЕМПЛЯР КЛАССА: ПОДПОРКА, ВНЕСЁННАЯ МОЛЧА.
//
// Три экземпляра выше — послабления, сужающие ОХВАТ проверки. Этот — послабление,
// зеленящее ВЕРДИКТ: поднятый предел ресурса, проглоченный отказ шага, повтор.
// Оно отличается от них тем, что после него конвейер становится зелёным, то есть
// исчезает единственный сигнал, что работа не закончена.
//
// Ban #11 такое не ловит: он держится маркером отложенной работы (`TODO:`), а
// подпорка маркера не несёт — это настройка.
//
// # Что требует гейт — и чего НЕ требует
//
// Требует ОДНОГО: рядом с послаблением сказано, зачем оно. Не «номер задачи» —
// потому что решение и отсрочка машинно неразличимы: `continue-on-error` бывает
// законным исходом (отсутствие артефакта шарда), а бывает проглоченным отказом.
// Различает их только человек, и он обязан это написать.
//
// Значит гейт ловит ровно один, зато главный путь: послабление, внесённое МОЛЧА.
// Граница названа честно — молчаливое внесение он ловит, неверное обоснование нет.
func relaxationsWithoutReason(dir string) (finds []string, files, relaxations int, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, 0, err
	}
	// Формы, в которых подпорка приходит в объявление конвейера. Перечень — часть
	// предпосылки гейта: появится новая форма, и она пройдёт молча, поэтому список
	// стоит рядом с проверкой, а не спрятан.
	forms := []string{"continue-on-error: true", "max-old-space-size", "max_attempts", "retries:"}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") && !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(dir, e.Name())) // #nosec G304 — путь из каталога конвейеров
		if readErr != nil {
			return nil, 0, 0, readErr
		}
		files++
		lines := strings.Split(string(raw), "\n")
		for i, line := range lines {
			code := line
			if idx := strings.Index(line, "#"); idx >= 0 {
				code = line[:idx] // сказанное в комментарии послаблением не является
			}
			var hit string
			for _, f := range forms {
				if strings.Contains(code, f) {
					hit = f
					break
				}
			}
			if hit == "" {
				continue
			}
			relaxations++
			// Объяснение ищем в шести строках выше: столько занимает шаг вместе с
			// его именем, и дальше начинается уже соседний.
			explained := false
			for back := i - 1; back >= 0 && back >= i-6; back-- {
				t := strings.TrimSpace(lines[back])
				if strings.HasPrefix(t, "#") && len(t) > 3 {
					explained = true
					break
				}
			}
			if !explained {
				finds = append(finds, fmt.Sprintf("%s:%d: %s — послабление внесено молча", e.Name(), i+1, hit))
			}
		}
	}
	return finds, files, relaxations, nil
}

func TestCiRelaxationSaysWhyItIsThere(t *testing.T) {
	dir := filepath.Join(repoRoot(t), ".github", "workflows")
	finds, files, relaxations, err := relaxationsWithoutReason(dir)
	if err != nil {
		t.Fatalf("обход конвейеров: %v", err)
	}
	t.Logf("перепись: файлов конвейера прочитано %d, послаблений найдено %d, без объяснения %d",
		files, relaxations, len(finds))
	if files == 0 {
		t.Fatal("прочитано НОЛЬ файлов конвейера — «ноль находок» означало бы «ноль прочитанного»")
	}
	if len(finds) > 0 {
		t.Errorf("послабление обязано говорить, зачем оно, — найдено %d:\n  %s\n\n"+
			"Подпорка зеленит вердикт, поэтому после неё исчезает единственный сигнал, что "+
			"работа не закончена. Скажи рядом: это решение (и почему исход законный) либо "+
			"отсрочка (и тогда с номером задачи и предикатом снятия).",
			len(finds), strings.Join(finds, "\n  "))
	}
}

// Собственная предпосылка: гейт ловит молчаливое послабление и молчит на объяснённом.
func TestCiRelaxationGateProvenByInjection(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("фикстура %s: %v", name, err)
		}
	}
	// Молчаливое: ни слова рядом.
	write("silent.yml", "jobs:\n  a:\n    steps:\n      - run: make\n        continue-on-error: true\n")
	// Объяснённое: та же форма, но сказано зачем. Гейт обязан молчать — иначе он
	// ловил бы форму, а не существо, и первый же ложный срабат его отключил бы.
	write("explained.yml", "jobs:\n  a:\n    steps:\n      # отсутствие артефакта шарда — законный исход скачивания\n      - run: make\n        continue-on-error: true\n")
	// Слово в комментарии послаблением не является: иначе гейт краснел бы на
	// собственном объяснении, как это уже случалось с проверками по подстроке.
	write("prose.yml", "jobs:\n  a:\n    steps:\n      # здесь когда-то стоял continue-on-error: true\n      - run: make\n")

	finds, files, relaxations, err := relaxationsWithoutReason(dir)
	if err != nil {
		t.Fatalf("обход синтетики: %v", err)
	}
	if files != 3 {
		t.Fatalf("перепись синтетики разошлась: файлов %d, ожидалось 3", files)
	}
	if relaxations != 2 {
		t.Errorf("послаблений в синтетике %d, ожидалось 2 — проза комментария не должна считаться", relaxations)
	}
	if len(finds) != 1 {
		t.Errorf("находок %d, ожидалась одна (молчаливое послабление): %v", len(finds), finds)
	}
	if len(finds) == 1 && !strings.Contains(finds[0], "silent.yml") {
		t.Errorf("находка обязана называть молчаливый файл, а названо: %s", finds[0])
	}
}
