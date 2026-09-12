// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy

// stand_provenance_declaration_test.go — ревизию дерева несёт КАЖДЫЙ образ, и её
// проставляет КАЖДАЯ сборка.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТО ЛОВИТ И ПОЧЕМУ ЭТОГО НЕ ВИДНО БЕЗ ГЕЙТА
//
// «Спроси стенд, что он исполняет» — первое действие при разборе красного. Ответ
// даёт величина, зашитая в образ СБОРКОЙ; вывести её из ИМЕНИ образа нельзя —
// имя говорит, что собрали, а не из чего, и совпадают они лишь пока никто не
// пересобирал под прежним тегом.
//
// Дыра тихая с обеих сторон:
//
//   * образ без величины поднимается, работает и проходит все проверки. Узнать о
//     нём можно только тогда, когда ответ уже понадобился, — то есть посреди
//     разбора красного, когда цена времени максимальна;
//   * сборка, забывшая передать величину, тоже зелена: `ARG` с пустым умолчанием
//     не роняет `docker build`. Замер 2026-08-25: сборка сервисов в конвейере не
//     передавала ничего, и образы управляемого кластера ревизии не несли вовсе.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ДВА НОСИТЕЛЯ И ПОЧЕМУ ОНИ НЕ РАЗЪЕДУТСЯ
//
// Клеймо читается реестром и демоном сборки; файл — единственное, что читается у
// РАБОТАЮЩЕГО контейнера одним `kubectl exec`, без демона хоста и без реестра, то
// есть одинаково на kind и на управляемом кластере. Оба берут величину из ОДНОГО
// `ARG` в ОДНОМ Dockerfile — разъехаться нечему by construction, и гейт требует
// именно этого, а не «оба присутствуют».
//
// Путь величины объявлен ОДИН раз — в читателе (deploy/scripts/stand-provenance.sh).
// Гейт вычитывает его оттуда и сверяет с тем, что пишут Dockerfile'ы: выписанная
// здесь копия разошлась бы с читателем молча, и первым это заметил бы тот, кому
// провенанс понадобился.
//
// ─────────────────────────────────────────────────────────────────────────────
// СПИСКА ИСКЛЮЧЕНИЙ У ГЕЙТА НЕТ — И ЭТО РЕШЕНИЕ
//
// Первая редакция выводила из-под правила вызовы, чей `-f` называет несуществующий
// путь: «он и так ничего не соберёт». Довод сам себя опроверг на замере — среди
// девяти рецептов, собирающих `kacho-<svc>:dev`, четыре называли пути прежней
// топологии, а ещё четыре собирали из негодного контекста, и разделить их
// «резолвится путь» не получалось: признак пропускал вторую половину и оправдывал
// первую. Всякий список прощённых — место, куда следующая сборка без величины
// попадёт незамеченной.
//
// Поэтому правило одно и без изъятий: вызов, называющий Dockerfile, передаёт
// величину. Что при этом четыре рецепта названы путями, которых в дереве нет, —
// ОТДЕЛЬНЫЙ предмет, и он не закрыт здесь: величина им передана, путь остался
// чужой заботой.

// ─────────────────────────────────────────────────────────────────────────────
// ИМЯ ДОВОДА — КОНТРАКТ С ФАЙЛОМ СБОРКИ, И ГЕЙТ СВЕРЯЕТ ЕГО, А НЕ ТОЛЬКО НАЛИЧИЕ
//
// Довод, чьего имени файл сборки не объявляет, docker выбрасывает БЕЗ слова:
// `ARG` остаётся при пустом умолчании, образ уезжает без ревизии, а сборка
// зелена — класс «значение, которое пишут и не читают».
//
// Поэтому гейт сверяет СООТВЕТСТВИЕ: у вызова, чью цель `-f` можно разрешить в
// дереве, имя переданного довода обязано совпасть с именем, из которого ЭТА цель
// выводит клеймо ревизии. Имя цели читается У САМОЙ ЦЕЛИ — из строки клейма,
// которую гейт от неё и так требует, — а НЕ из перечня известных имён: перечень
// стареет молча, а строка клейма есть в каждом образе продукта by construction.
// Где цель собрана из переменной оболочки (`-f $$d/Dockerfile`) или матрицы
// конвейера, соответствие не проверяемо by construction — такие вызовы НЕ
// прощаются молча, а считаются и называются числом: «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗДЕСЬ СТОЯЛА ОСЬ ПРИЁМА ПРЕЖНЕГО ИМЕНИ — ОНА ИСТЕКЛА (#2583)
//
// Имён было ДВА: каноническое (названное внешним стандартом, а не продуктом) и
// прежнее, называвшее платформу. Прежнее принималось, ПОКА хоть один Dockerfile
// дерева его объявлял, — им был файл службы доступа. Служба вынесена отдельным
// репозиторием, объявивших стало НОЛЬ, и приём сам объявил себя находкой в тот
// же прогон, назвав все три места снятия. Ровно то самоистечение, ради которого
// он писался, — поэтому снят он ВМЕСТЕ с предметом: константа прежнего имени,
// функция самоистечения, поле переписи и оси инъекции, его проверявшие.
//
// СНЯТО, А НЕ ОСЛАБЛЕНО, и это разные вещи. Вместе с приёмом стали
// НЕПРЕДСТАВИМЫ два входа: «файл объявляет два имени одной величины» (имён
// осталось одно) и «вызов передаёт одно известное имя, а цель объявляет другое
// известное» (известное осталось одно). Первый утрачен вместе с предметом.
// Второй ПЕРЕНЕСЁН на признак, который дерево производит, — имя, из которого
// цель выводит клеймо, — и потому по-прежнему проверяем инъекцией.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// canonicalProvenanceArg — имя довода, под которым величина ездит в образ.
// Названо предметом, а предмет — внешним стандартом: довод кормит аннотацию
// `org.opencontainers.image.revision`, и потому имя ВЫВОДИМО из ключа клейма,
// а не выбрано вкусом. Объявление величины — provenance.mk.
const canonicalProvenanceArg = "OCI_IMAGE_REVISION"

func argDeclRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^ARG\s+` + name + `\b`)
}

// labelArgRe — имя довода, из которого образ выводит клеймо ревизии. Признак
// НАЗВАН ЦЕЛЬЮ, а не нашим перечнем: строку клейма гейт требует от каждого
// образа продукта, поэтому имя читается у любого Dockerfile — включая тот, чьё
// имя довода нам неизвестно. Перечень известных имён на этом месте старел бы
// молча, и сверка соответствия замолчала бы вместе с ним.
var labelArgRe = regexp.MustCompile(`org\.opencontainers\.image\.revision="\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?"`)

// revisionPathFromReader — путь величины, ПРОЧИТАННЫЙ у читателя. Единственное
// объявление в дереве; здесь его копии нет намеренно.
func revisionPathFromReader(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("scripts/stand-provenance.sh")
	if err != nil {
		t.Fatalf("читатель провенанса не прочитан: %v", err)
	}
	m := regexp.MustCompile(`(?m)^REVISION_PATH="([^"]+)"`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("в scripts/stand-provenance.sh не найдено объявление REVISION_PATH — " +
			"сверять Dockerfile'ы не с чем, и «сходится» здесь означало бы «не прочитано»")
	}
	return string(m[1])
}

// productDockerfilePatterns — где живут образы продукта. Перечень ОБРАЗЦОВ, а не
// файлов: выписанный перечень файлов отстал бы на первом же новом сервисе или
// модуле консоли.
var productDockerfilePatterns = []string{
	"../gateway/Dockerfile",
	"../services/*/Dockerfile",
	"../ui-future/*/Dockerfile",
}

// matchProductDockerfiles — чистое сопоставление образцов с деревом. Вынесено из
// обёртки ниже, чтобы проба инъекции могла подать ему образцы, ничего не находящие,
// и убедиться, что пустой обход ДОСТИЖИМ: функция, роняющая прогон сама,
// непроверяема — пробе пришлось бы падать, чтобы доказать, что она работает.
func matchProductDockerfiles(patterns []string) ([]string, error) {
	var out []string
	for _, pattern := range patterns {
		hits, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		out = append(out, hits...)
	}
	sort.Strings(out)
	return out, nil
}

// productDockerfiles — тот же перечень для гейта, с ОТКАЗОМ на пустом обходе:
// «образов не найдено» обязано ронять прогон, а не читаться как «находок нет».
func productDockerfiles(t *testing.T) []string {
	t.Helper()
	out, err := matchProductDockerfiles(productDockerfilePatterns)
	if err != nil {
		t.Fatalf("обход образцов %v: %v", productDockerfilePatterns, err)
	}
	if len(out) == 0 {
		t.Fatal("образов продукта не найдено — гейт, не прочитавший предмет, обязан падать, а не зеленеть")
	}
	return out
}

// checkDockerfile — находки по одному Dockerfile. Чистая функция над текстом:
// проба инъекции кормит её синтетикой, не трогая дерево.
//
// Имя довода у образа продукта ОДНО и каноническое; клеймо и файл ревизии
// обязаны выводиться ИМЕННО из него. Файл, объявивший канон и выводящий клеймо
// из чего-то другого, прошёл бы проверку, которая судит каждую строку в
// отдельности, — поэтому обе сверяются с одним и тем же именем.
func checkDockerfile(body, revisionPath string) (findings []string) {
	arg := canonicalProvenanceArg
	if !argDeclRe(arg).MatchString(body) {
		findings = append(findings, "не объявлен ARG "+arg)
		return findings
	}

	labelRe := regexp.MustCompile(`org\.opencontainers\.image\.revision="\$\{?` + arg + `\}?"`)
	if !labelRe.MatchString(body) {
		findings = append(findings,
			"клеймо org.opencontainers.image.revision не выводится из $"+arg)
	}

	// Файл пишется ИЗ ТОГО ЖЕ аргумента и ПО ТОМУ ЖЕ пути, что читает читатель.
	writeRe := regexp.MustCompile(`\$\{?` + arg + `\}?"?\s*>\s*` + regexp.QuoteMeta(revisionPath))
	if !writeRe.MatchString(body) {
		findings = append(findings,
			"величина не записывается в "+revisionPath+" из $"+arg)
	}

	// Последний USER образа обязан остаться непривилегированным: семейство
	// консоли поднимается до root ради записи величины, и незакрытый подъём
	// оставил бы контейнер работать под root — цена провенанса стала бы выше
	// его пользы.
	users := regexp.MustCompile(`(?m)^USER\s+(\S+)`).FindAllStringSubmatch(body, -1)
	if len(users) == 0 {
		findings = append(findings, "образ не объявляет USER — финальный пользователь неизвестен")
	} else {
		last := users[len(users)-1][1]
		if last == "root" || last == "0" {
			findings = append(findings, "последний USER — "+last+": подъём до root не закрыт")
		}
	}
	return findings
}

func TestEveryProductImageCarriesTheTreeRevision(t *testing.T) {
	revisionPath := revisionPathFromReader(t)
	files := productDockerfiles(t)

	total, carrying := 0, 0
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("чтение %s: %v", f, err)
		}
		total++
		findings := checkDockerfile(string(body), revisionPath)
		if len(findings) == 0 {
			carrying++
		}
		for _, finding := range findings {
			t.Errorf("НАХОДКА: %s — %s", f, finding)
		}
	}

	// Перепись печатает ДВЕ величины: сколько образов осмотрено и сколько несут
	// величину без единой находки. Одно число скрывало бы ровно тот случай, ради
	// которого перепись заведена, — «ноль находок» на нуле прочитанного. Пустой
	// перечень при этом до сюда не доходит: productDockerfiles роняет прогон.
	t.Logf("осмотрено: образов продукта %d, несут величину без находок %d, "+
		"имя довода %s, путь величины «%s» (прочитан у читателя)",
		total, carrying, canonicalProvenanceArg, revisionPath)
}

// ─────────────────────────────────────────────────────────────────────────────
// СТОРОНА СБОРКИ
// ─────────────────────────────────────────────────────────────────────────────

// buildInvocation — один вызов сборки, приведённый к одной строке.
type buildInvocation struct {
	site string // файл
	text string // текст вызова
}

var buildCmdRe = regexp.MustCompile(`docker\s+(buildx\s+)?build\b`)

// joinContinuations — склеивает строки, перенесённые обратной косой.
func joinContinuations(body string) []string {
	var out []string
	var cur strings.Builder
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimRight(line, " \t")
		if strings.HasSuffix(trimmed, `\`) {
			cur.WriteString(strings.TrimSuffix(trimmed, `\`))
			cur.WriteString(" ")
			continue
		}
		cur.WriteString(trimmed)
		out = append(out, cur.String())
		cur.Reset()
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// makeBuildInvocations — вызовы `docker build` в рецепте (комментарии отброшены:
// гейт, считающий совпадения в комментарии, покраснел бы на собственном
// объяснении).
func makeBuildInvocations(site, body string) []buildInvocation {
	var out []buildInvocation
	for _, line := range joinContinuations(body) {
		code := line
		if i := strings.Index(code, "#"); i >= 0 && strings.TrimSpace(code[:i]) == "" {
			continue // строка целиком комментарий
		}
		if !buildCmdRe.MatchString(code) || !strings.Contains(code, "Dockerfile") {
			continue
		}
		out = append(out, buildInvocation{site: site, text: code})
	}
	return out
}

// workflowStep — минимум полей шага, нужный гейту.
type workflowStep struct {
	Uses string         `yaml:"uses"`
	Run  string         `yaml:"run"`
	With map[string]any `yaml:"with"`
}

type workflowFile struct {
	Jobs map[string]struct {
		Steps []workflowStep `yaml:"steps"`
	} `yaml:"jobs"`
}

// workflowBuildInvocations — вызовы сборки в процессе конвейера. Читается
// РАЗОБРАННЫЙ YAML: имя действия встречается и в комментариях, и проверка по
// подстроке краснела бы на объяснении рядом с собой.
func workflowBuildInvocations(t *testing.T, site, body string) []buildInvocation {
	t.Helper()
	var wf workflowFile
	if err := yaml.Unmarshal([]byte(body), &wf); err != nil {
		t.Fatalf("разбор %s: %v", site, err)
	}
	var out []buildInvocation
	for _, job := range wf.Jobs {
		for _, step := range job.Steps {
			switch {
			case strings.HasPrefix(step.Uses, "docker/build-push-action"):
				text := step.Uses
				for _, k := range []string{"build-args", "file", "tags"} {
					if v, ok := step.With[k]; ok {
						text += " " + k + "=" + toText(v)
					}
				}
				out = append(out, buildInvocation{site: site, text: text})
			case buildCmdRe.MatchString(step.Run) && strings.Contains(step.Run, "Dockerfile"):
				for _, inv := range makeBuildInvocations(site, step.Run) {
					out = append(out, inv)
				}
			}
		}
	}
	return out
}

func toText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		b, _ := yaml.Marshal(x)
		return string(b)
	}
}

// buildSites — файлы, зовущие сборку. ВЫВОДЯТСЯ из дерева.
func buildSites(t *testing.T) (makefiles, workflows []string) {
	t.Helper()
	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "vendor", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		base := info.Name()
		isMake := base == "Makefile" || strings.HasSuffix(base, ".mk")
		isFlow := strings.HasPrefix(filepath.ToSlash(path), "../.github/workflows/") &&
			(strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml"))
		if !isMake && !isFlow {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil || !buildCmdRe.Match(raw) && !strings.Contains(string(raw), "docker/build-push-action") {
			return nil
		}
		if isMake {
			makefiles = append(makefiles, path)
		} else {
			workflows = append(workflows, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}
	sort.Strings(makefiles)
	sort.Strings(workflows)
	if len(makefiles)+len(workflows) == 0 {
		t.Fatal("вызовов сборки в дереве не найдено — предмет гейта исчез, и это находка, а не чистота")
	}
	return makefiles, workflows
}

// passesArg — передаёт ли вызов довод ПОД ЭТИМ именем: буквально либо через
// переменную, чьё объявление его несёт. Вторая форма нужна рецепту стенда: там
// аргументы собраны в одну переменную, и требовать буквального имени в каждой
// строке значило бы требовать копий.
//
// Имя приходит ПАРАМЕТРОМ, от цели вызова, а не из перечня известных: перечень
// на этом месте старел бы молча, и вопрос «совпало ли» превратился бы в «есть ли
// хоть что-то из набора» — то есть перестал бы быть вопросом о соответствии.
func passesArg(text string, carriers map[string]map[string]bool, name string) bool {
	if strings.Contains(text, name) {
		return true
	}
	for v, names := range carriers {
		if strings.Contains(text, v) && names[name] {
			return true
		}
	}
	return false
}

// dockerfileArgOf — имя довода, из которого цель вызова выводит клеймо ревизии.
// Второй результат — разрешилась ли цель вовсе: путь, собранный из переменной
// оболочки или матрицы конвейера, не разрешается by construction, и это НЕ
// находка, а граница.
func dockerfileArgOf(site, text string) (arg string, resolved bool) {
	m := regexp.MustCompile(`(?:-f|file=)\s*"?([^\s"]*Dockerfile[^\s"]*)"?`).FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	rel := m[1]
	if strings.ContainsAny(rel, "$*{") {
		return "", false // путь собран из переменной либо матрицы
	}
	base := filepath.Dir(site)
	// `cd <X> && docker build …` меняет базу пути. Берём ПОСЛЕДНИЙ переход перед
	// сборкой: рецепты стенда пишут `( cd ../ui-future && docker build … )`.
	if i := buildCmdRe.FindStringIndex(text); i != nil {
		for _, cd := range regexp.MustCompile(`cd\s+([^\s&;]+)`).FindAllStringSubmatchIndex(text[:i[0]], -1) {
			base = filepath.Join(base, text[cd[2]:cd[3]])
		}
	}
	// Процессы конвейера исполняются из корня дерева, а не из каталога своего файла.
	if strings.Contains(filepath.ToSlash(site), "/.github/workflows/") {
		base = ".."
	}
	path := filepath.Join(base, rel)
	body, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	if m := labelArgRe.FindSubmatch(body); m != nil {
		return string(m[1]), true
	}
	// Файл есть, а клейма из довода не выводит — это находка СТОРОНЫ ОБРАЗА, и её
	// уже назвал TestEveryProductImageCarriesTheTreeRevision. Здесь сверять нечего.
	return "", false
}

var makeVarDeclRe = regexp.MustCompile(`^([A-Z_][A-Z0-9_]*)\s*[:?]?=`)

// buildArgNameRe — имя довода в строке `--build-arg <ИМЯ>=…`.
var buildArgNameRe = regexp.MustCompile(`--build-arg\s+"?([A-Za-z_][A-Za-z0-9_]*)`)
var makeIncludeRe = regexp.MustCompile(`^-?include\s+(\S+)`)

// revisionCarryingVars — переменные, чьё ОБЪЯВЛЕНИЕ содержит величину, ВКЛЮЧАЯ
// объявленные во включаемых файлах. Значение — набор ИМЁН, которые переменная
// несёт: переходная пара несёт два, и сверка соответствия обязана знать какие.
//
// Включение обходится, а не игнорируется, и это существенно: величина объявлена
// ОДИН раз на дерево (provenance.mk), поэтому рецепт, судимый только по
// собственному тексту, выглядел бы нарушителем ровно за то, что не держит копии
// предиката, — гейт толкал бы к тому, против чего заведён. Обратная сторона тоже
// верна: включение файла, величины не объявляющего, ничего не оправдывает.
// Возвращает носители И находки; о находках отчитывается ВЫЗЫВАЮЩИЙ. Функция,
// роняющая прогон сама, непроверяема: пробе инъекции пришлось бы падать, чтобы
// доказать, что она работает.
func revisionCarryingVars(path string, depth int) (carriers map[string]map[string]bool, findings []string) {
	carriers = map[string]map[string]bool{}
	if depth > 4 {
		return carriers, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return carriers, nil
	}
	dir := filepath.Dir(path)
	for _, line := range joinContinuations(string(raw)) {
		trimmed := strings.TrimSpace(line)
		if m := makeIncludeRe.FindStringSubmatch(trimmed); m != nil {
			included := filepath.Join(dir, m[1])
			if _, err := os.Stat(included); err != nil {
				findings = append(findings, path+" включает «"+m[1]+"», которого нет — "+
					"включение в пустоту не приносит ни величины, ни отказа")
				continue
			}
			c, f := revisionCarryingVars(included, depth+1)
			for v, names := range c {
				if carriers[v] == nil {
					carriers[v] = map[string]bool{}
				}
				for n := range names {
					carriers[v][n] = true
				}
			}
			findings = append(findings, f...)
			continue
		}
		// Имена читаются как ИМЕНА ДОВОДОВ, а не сверяются с перечнем известных:
		// переменная-носитель тем и является, что строит `--build-arg <ИМЯ>=…`, и
		// сверка соответствия обязана уметь спросить про ЛЮБОЕ имя, включая то,
		// которого мы не знаем. Прежняя редакция считала носителем и переменную,
		// чьё объявление лишь СОДЕРЖАЛО имя (величину провенанса), — она доводов
		// не строит, и оправдывать вызов ей было нечем.
		var carried []string
		for _, m := range buildArgNameRe.FindAllStringSubmatch(line, -1) {
			carried = append(carried, m[1])
		}
		if len(carried) == 0 {
			continue
		}
		if m := makeVarDeclRe.FindStringSubmatch(trimmed); m != nil {
			v := "$(" + m[1] + ")"
			if carriers[v] == nil {
				carriers[v] = map[string]bool{}
			}
			for _, n := range carried {
				carriers[v][n] = true
			}
		}
	}
	return carriers, findings
}

func TestEveryImageBuildPassesTheTreeRevision(t *testing.T) {
	makefiles, workflows := buildSites(t)

	var seen, checked, unresolved int
	judge := func(inv buildInvocation, carriers map[string]map[string]bool) {
		seen++
		// ТРЕБУЕМОЕ ИМЯ ПРИХОДИТ ОТ ЦЕЛИ, а канон — только там, где цели нет.
		// Цель разрешилась ⇒ спрашиваем ровно то имя, из которого она выводит
		// клеймо: это и есть сверка соответствия. Не разрешилась ⇒ спрашиваем
		// канон, потому что всякий образ продукта этого дерева объявляет его
		// (держит проверка выше), и другого требования у нас нет.
		want, resolved := dockerfileArgOf(inv.site, inv.text)
		need := canonicalProvenanceArg
		if resolved {
			checked++
			need = want
		} else {
			unresolved++
		}
		if passesArg(inv.text, carriers, need) {
			return
		}
		why := "образ уедет без ревизии, и провенанс стенда ответит «не установлена»"
		if resolved && need != canonicalProvenanceArg {
			why = "её цель выводит клеймо из $" + need + ", а docker непотреблённый довод " +
				"выбрасывает МОЛЧА: ARG останется пустым, а сборка — зелёной"
		}
		t.Errorf("НАХОДКА: %s — сборка не передаёт --build-arg %s=…; %s", inv.site, need, why)
	}

	for _, f := range makefiles {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("чтение %s: %v", f, err)
		}
		carriers, findings := revisionCarryingVars(f, 0)
		for _, finding := range findings {
			t.Errorf("НАХОДКА: %s", finding)
		}
		for _, inv := range makeBuildInvocations(f, string(raw)) {
			judge(inv, carriers)
		}
	}
	for _, f := range workflows {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("чтение %s: %v", f, err)
		}
		for _, inv := range workflowBuildInvocations(t, f, string(raw)) {
			judge(inv, nil)
		}
	}

	if seen == 0 {
		t.Fatal("вызовов сборки ноль — «все передают величину» здесь означало бы «ни один не прочитан»")
	}
	// Соответствие имён проверено НЕ у всех вызовов, и это надо назвать числом:
	// цель, собранная из переменной оболочки, не разрешается by construction.
	if checked == 0 {
		t.Error("НАХОДКА: ни у одного вызова цель не разрешилась — сверка имён не выполнена ни " +
			"разу, и «соответствие держится» здесь означало бы «не прочитано»")
	}
	t.Logf("осмотрено: рецептов %d, процессов конвейера %d, вызовов сборки %d; "+
		"соответствие имени сверено у %d, цель не разрешается у %d",
		len(makefiles), len(workflows), seen, checked, unresolved)
}
