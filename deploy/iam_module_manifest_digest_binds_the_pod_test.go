// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// iam_module_manifest_digest_binds_the_pod_test.go — правка карты манифестов
// ОБЯЗАНА перекатывать под iam (задача #1981).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Манифесты приезжают в под ИМЕНОВАННЫМ ConfigMap и читаются процессом ОДИН РАЗ,
// на старте (`services/iam/cmd/kaname/module_manifests.go`, перечитывания
// нет). Правка карты меняет файлы в томе и НЕ меняет шаблон пода: Kubernetes не
// видит причины перекатывать под, и процесс продолжает работать с каталогом,
// прочитанным при старте. Объявленное оператором состояние наступает не в момент
// правки, а при постороннем перезапуске — а «под Ready» этого не опровергает.
//
// Это класс `testing.md` §2а, и здесь он дороже обычного: вместе с отзывом права
// (#1969) снятие перестаёт наступать в названный момент.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЭТУ КАРТУ НЕ ЗАКРЫВАЕТ АННОТАЦИЯ ЦЕЛОСТНОСТИ ФОРМЫ
//
// Три соседние привязки того же пода считаются формой
// `include (print $.Template.BasePath "/…") | sha256sum` — то есть по ШАБЛОНУ
// чарта. У карты манифестов шаблона НЕТ by construction: объект порождается ВНЕ
// чарта (`make module-manifests-configmap`) и применяется до helm, потому что
// его содержимое есть данные, а не релиз службы. Хэшировать в чарте нечего.
//
// Поэтому привязка идёт тем же путём, каким в этом дереве уже привязан
// НЕИЗМЕННЫЙ тег локального образа: производитель кладёт отпечаток СОДЕРЖИМОГО в
// величину, профиль отдаёт величину helm, шаблон пода её штампует. Форма не
// изобретена здесь — она уже работает у `kacho.cloud/image-id`.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПО ОБЪЯВЛЕНИЮ, А НЕ ПО РЕНДЕРУ
//
// Рендер требует helm и распакованных зависимостей умбреллы; там, где их нет,
// проверка ПРОПУСКАЕТСЯ, а пропущенная проверка не краснеет никогда. Сверх того
// рендер собирается из упакованных зависимостей, и `.tgz`, скопированный из
// соседнего клона, дал бы вердикт о ЧУЖОМ дереве. Объявление — то, что правит
// человек и что уезжает в поставку.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗВЕНЬЕВ ТРИ, И КАЖДОЕ ПОРОЗНЬ ДАЁТ ЛОЖНОЕ ЗЕЛЁНОЕ
//
//	A  шаблон пода ШТАМПУЕТ величину — иначе привязки нет вовсе;
//	B  производитель ПИШЕТ в ту же величину отпечаток применённого объекта —
//	   иначе штампуется чужое либо ничто;
//	C  каждый путь выкатки ОТДАЁТ величину helm — иначе аннотация рендерится
//	   умолчанием «unset» ВСЕГДА: привязка стоит в шаблоне и не меняется никогда.
//
// Третье звено и есть класс «контроль, у которого нет механизма исполниться»
// (`security.md`): в диффе оно выглядит завершённой работой, а исполниться не
// может ни при каком входе.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ НЕ СУДИТСЯ
//
// Не судится, ОБЪЯВЛЯЕТ ли конкретная посадка доставку: это решение посадки
// (соседняя проверка пары, #1924). Привязка стоит под тем же условием, что и
// том, — где доставки нет, там и привязывать не к чему.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// digestAnnotationKey — ключ аннотации привязки. Имя названо ОДИН раз и
// сопоставляется как ЦЕЛЫЙ ключ YAML, а не как подстрока: сверка подстрокой
// осталась бы зелёной и на переименовании (`…-digest-old`), и на упоминании
// имени в комментарии, который эту же привязку объясняет.
const digestAnnotationKey = "kacho.cloud/module-manifests-digest"

// digestValuesVar — имя переменной Makefile, объявляющей файл значений с
// отпечатком. Путь берётся из ЕЁ значения, а не пишется здесь второй копией.
const digestValuesVar = "MODULE_MANIFESTS_VALUES"

// digestProducerTarget — цель, которая применяет объект доставки и обязана
// положить его отпечаток в файл значений. Та же цель, что зовут пути выкатки.
const digestProducerTarget = "module-manifests-configmap"

// iamDeploymentTemplate — шаблон пода iam от каталога развёртывания.
const iamDeploymentTemplate = umbrellaDir + "/charts/kaname/templates/deployment.yaml"

// digestAnnotationDecl — объявление аннотации ЦЕЛИКОМ: ключ как ключ и значение
// как ВЫЧИСЛЕНИЕ по величине из `.Values.global`.
//
// Захватываются обе ступени пути величины: они же требуются от производителя,
// поэтому величина остаётся ОДНИМ объявлением. Ключ, приравненный к литералу
// (`: "abc"`), под это выражение не подходит — и не должен: он не меняется от
// правки карты, то есть привязкой не является.
var digestAnnotationDecl = regexp.MustCompile(
	`(?m)^[\t ]*` + regexp.QuoteMeta(digestAnnotationKey) +
		`:[\t ]*\{\{-?[\t ]*dig[\t ]+"([A-Za-z0-9_]+)"[\t ]+"([A-Za-z0-9_]+)"[\t ]+"[^"]*"[\t ]+\(\.Values\.global\b[^)]*\)`)

// makeVarDecl — объявление переменной Makefile (`X := v` либо `X ?= v`).
func makeVarDecl(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `[\t ]*[:?]?=[\t ]*(\S+)[\t ]*$`)
}

// makeTargetHeaderOf — заголовок ИМЕННО этой цели. Присваивание целью не
// является, поэтому двоеточие обязано не быть частью `:=`.
func makeTargetHeaderOf(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `[\t ]*:(?:[^=]|$)`)
}

// digestBindingCensus — объём осмотренного. Печатается ВСЕГДА, на всяком исходе:
// «ноль находок» обязано быть отличимо от «ноль прочитанного».
type digestBindingCensus struct {
	ChartBytes      int // прочитано байт шаблона пода
	AnnotationsSeen int // аннотаций шаблона пода осмотрено
	RecipeLines     int // строк рецепта производителя прочитано
	Carriers        int // носителей выкатки осмотрено
	UmbrellaCalls   int // вызовов helm по чарту умбреллы
	CallsBinding    int // из них отдающих величину helm
}

// Summary — перепись одной строкой.
func (c digestBindingCensus) Summary() string {
	return fmt.Sprintf("шаблон пода %d байт · аннотаций %d · строк рецепта %s %d · "+
		"носителей %d · вызовов helm по умбрелле %d · из них отдают величину %d",
		c.ChartBytes, c.AnnotationsSeen, digestProducerTarget, c.RecipeLines,
		c.Carriers, c.UmbrellaCalls, c.CallsBinding)
}

// digestValuePath — путь величины, объявленный ШАБЛОНОМ ПОДА (потребителем).
type digestValuePath struct {
	Map string
	Key string
}

// recipeOf — рецепт цели Makefile: строки с табуляции до первой строки, не
// принадлежащей рецепту. Комментарии рецепта отбрасываются: исполняются они не
// оболочкой, а глазами.
func recipeOf(makefile, target string) []string {
	loc := makeTargetHeaderOf(target).FindStringIndex(makefile)
	if loc == nil {
		return nil
	}
	var out []string
	lines := strings.Split(makefile[loc[0]:], "\n")
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, "\t") {
			break
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// joinContinuations — логическая команда, начинающаяся строкой i.
//
// Форма записи вызова в этом дереве МНОГОСТРОЧНАЯ, и распознаватель, судящий
// одну строку, не увидит ни одного `-f`: они стоят на строках-продолжениях.
// Ровно тот класс, ради которого `testing.md` §«Гейт на класс» п.7 требует
// назвать все законные формы записи предмета.
//
// Команда кончается строкой БЕЗ продолжения либо строкой, чей исполняемый хвост
// закрыт `;` — так записана цепочка `; \` рецепта Makefile.
func joinContinuations(lines []string, i int) (string, int) {
	var b strings.Builder
	j := i
	for ; j < len(lines); j++ {
		raw := strings.TrimRight(lines[j], " \t")
		cont := strings.HasSuffix(raw, `\`)
		body := strings.TrimSpace(strings.TrimSuffix(raw, `\`))
		b.WriteString(" ")
		b.WriteString(body)
		if !cont || strings.HasSuffix(body, ";") {
			break
		}
	}
	return strings.TrimSpace(b.String()), j
}

// valuesArgs — аргументы `-f` (и `--values`) команды.
func valuesArgs(cmd string) []string {
	fields := strings.Fields(cmd)
	var out []string
	for i, f := range fields {
		if (f == "-f" || f == "--values") && i+1 < len(fields) {
			out = append(out, strings.Trim(fields[i+1], `"'`))
		}
		if v, ok := strings.CutPrefix(f, "--values="); ok {
			out = append(out, strings.Trim(v, `"'`))
		}
	}
	return out
}

// shellAssign — присваивание литерала переменной оболочки (`VAR="…"`, `VAR=…`).
//
// Имя переменной сопоставляется ЦЕЛИКОМ: без якорей `X=` совпало бы с хвостом
// `PREFIX_X=`, и подстановка вернула бы значение чужой переменной.
var shellAssign = regexp.MustCompile(`(?m)^[\t ]*([A-Za-z_][A-Za-z0-9_]*)=["']?([^"'\s]+)["']?[\t ]*$`)

// shellLiterals — литеральные присваивания носителя. Нужны потому, что имя
// файла записывается в этом дереве ТРЕМЯ законными формами, а не одной.
func shellLiterals(text string) map[string]string {
	out := map[string]string{}
	for _, m := range shellAssign.FindAllStringSubmatch(text, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// namesDigestValues — аргумент называет ФАЙЛ ЗНАЧЕНИЙ с отпечатком.
//
// ФОРМ ЗАПИСИ ТРИ, И ВСЕ ТРИ ВЫВЕДЕНЫ ИЗ ТОГО, ОТКУДА ЗОВУТ (`testing.md`
// §«Гейт на класс», п.7 — распознаватель обязан знать ВСЕ законные формы):
//
//	$(MODULE_MANIFESTS_VALUES)  — рецепт Makefile называет свою переменную;
//	values.module-manifests.yaml — скрипт из каталога чарта называет имя файла;
//	$VAR, где VAR присвоен этот литерал в ТОМ ЖЕ носителе.
//
// Третья форма стоила отдельного круга: без неё боевой путь выкатки —
// единственный, где ошибка дороже всего, — читался как непривязанный, при живой
// привязке. Пропуск такой формы даёт не красное и не зелёное, а МОЛЧАНИЕ о целом
// виде записи предмета.
//
// Сверка ПО ГРАНИЦЕ, а не подстрокой: `values.module-manifests.yaml.bak` не есть
// объявленный файл, и переименование не оставит проверку зелёной.
func namesDigestValues(arg, relPath string, vars map[string]string) bool {
	a := strings.Trim(arg, `"'`)
	if v, ok := strings.CutPrefix(a, "$"); ok {
		v = strings.TrimSuffix(strings.TrimPrefix(v, "{"), "}")
		if v == digestValuesVar {
			return true // $(VAR) уже снят ниже; здесь — форма ${VAR} оболочки
		}
		if lit, seen := vars[v]; seen {
			a = lit
		}
	}
	a = strings.TrimPrefix(a, "./")
	if a == "$("+digestValuesVar+")" {
		return true
	}
	return a == relPath || a == filepath.Base(relPath)
}

// auditDigestBinding — судья трёх звеньев. Функция ЧИСТАЯ: инъекция подаёт ей
// синтетические тела, не трогая дерева.
func auditDigestBinding(chart, makefile string, carriers []deployCarrier) ([]string, digestBindingCensus) {
	var census digestBindingCensus
	var findings []string

	census.ChartBytes = len(chart)
	for _, line := range strings.Split(chart, "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, "{{") {
			continue
		}
		if k, _, ok := strings.Cut(s, ":"); ok && strings.Contains(k, "/") {
			census.AnnotationsSeen++
		}
	}

	// ── A. шаблон пода штампует величину ────────────────────────────────────
	var path digestValuePath
	if m := digestAnnotationDecl.FindStringSubmatch(chart); m != nil {
		path = digestValuePath{Map: m[1], Key: m[2]}
	} else {
		findings = append(findings, fmt.Sprintf(
			"%s: шаблон пода не штампует %q вычислением по величине из .Values.global — "+
				"правка карты манифестов меняет файлы в томе и НЕ меняет шаблон пода, "+
				"поэтому под не перекатывается, а каталог прочитан на старте (kacho#1981)",
			iamDeploymentTemplate, digestAnnotationKey))
	}

	// ── B. производитель пишет отпечаток в ТУ ЖЕ величину ───────────────────
	relPath := ""
	if m := makeVarDecl(digestValuesVar).FindStringSubmatch(makefile); m != nil {
		relPath = m[1]
	} else {
		findings = append(findings, fmt.Sprintf(
			"deploy/Makefile: переменная %s не объявлена — файл значений с отпечатком "+
				"назвать нечем, и каждый путь выкатки назвал бы его своей копией",
			digestValuesVar))
	}

	recipe := recipeOf(makefile, digestProducerTarget)
	census.RecipeLines = len(recipe)
	switch {
	case len(recipe) == 0:
		findings = append(findings, fmt.Sprintf(
			"deploy/Makefile: цели %s нет либо её рецепт пуст — предпосылка проверки "+
				"исчезла, а не дерево стало чистым: отпечаток класть некому", digestProducerTarget))
	case path.Map != "":
		body := strings.Join(recipe, "\n")
		switch {
		case relPath != "" && !strings.Contains(body, "$("+digestValuesVar+")"):
			findings = append(findings, fmt.Sprintf(
				"deploy/Makefile:%s — рецепт не пишет в $(%s): объект применяется, а его "+
					"отпечаток никуда не кладётся, и штамповать шаблону пода нечего",
				digestProducerTarget, digestValuesVar))
		case !strings.Contains(body, path.Map) || !strings.Contains(body, path.Key+":"):
			findings = append(findings, fmt.Sprintf(
				"deploy/Makefile:%s — рецепт не объявляет величину %s.%s, которую штампует "+
					"шаблон пода: два объявления об одном предмете разошлись бы молча, и "+
					"аннотация штамповала бы умолчание при живой правке карты",
				digestProducerTarget, path.Map, path.Key))
		case !strings.Contains(body, "sha256sum"):
			findings = append(findings, fmt.Sprintf(
				"deploy/Makefile:%s — величина кладётся, но НЕ вычисляется по содержимому "+
					"(sha256sum отсутствует): имя переменной подделать легко, вычисление — нет",
				digestProducerTarget))
		}
	}

	// ── C. каждый путь выкатки отдаёт величину helm ─────────────────────────
	for _, c := range carriers {
		census.Carriers++
		vars := shellLiterals(c.Text)
		lines := strings.Split(c.Text, "\n")
		for i := 0; i < len(lines); i++ {
			if isCommentLine(lines[i]) || !helmUpgradeInvocation(lines[i]) {
				continue
			}
			cmd, end := joinContinuations(lines, i)
			if !namesUmbrellaChart(cmd, c.Path) {
				i = end
				continue
			}
			census.UmbrellaCalls++
			bound := false
			for _, a := range valuesArgs(cmd) {
				if relPath != "" && namesDigestValues(a, relPath, vars) {
					bound = true
					break
				}
			}
			if bound {
				census.CallsBinding++
			} else {
				findings = append(findings, fmt.Sprintf(
					"%s:%d — вызов helm по чарту умбреллы не получает файл значений с "+
						"отпечатком доставки (%s): аннотация отрендерится умолчанием и не "+
						"изменится НИКОГДА — привязка стоит в шаблоне и не может исполниться",
					c.Path, i+1, relPath))
			}
			i = end
		}
	}

	sort.Strings(findings)
	return findings, census
}

// readIAMDeploymentTemplate — шаблон пода iam.
func readIAMDeploymentTemplate(t *testing.T) string {
	t.Helper()
	p := filepath.Join(repoRoot, "deploy", iamDeploymentTemplate)
	// #nosec G304 -- путь собран из констант собственного дерева.
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("шаблон пода iam не прочитан (%s): %v — вердикт беспредметен", p, err)
	}
	return string(raw)
}

// readDeployMakefile — Makefile развёртывания.
func readDeployMakefile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(repoRoot, "deploy", "Makefile")
	// #nosec G304 -- путь собран из констант собственного дерева.
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("deploy/Makefile не прочитан: %v — вердикт беспредметен", err)
	}
	return string(raw)
}

// TestEditingTheModuleManifestMapRollsTheIAMPod — три звена привязки целы.
func TestEditingTheModuleManifestMapRollsTheIAMPod(t *testing.T) {
	chart := readIAMDeploymentTemplate(t)
	makefile := readDeployMakefile(t)
	carriers := bringUpCarriers(t)

	findings, census := auditDigestBinding(chart, makefile, carriers)
	t.Logf("осмотрено: %s", census.Summary())

	if census.ChartBytes == 0 {
		t.Fatal("шаблон пода прочитан пустым — судить было нечего")
	}
	if census.AnnotationsSeen == 0 {
		t.Fatal("в шаблоне пода не найдено ни одной аннотации — распознаватель перестал " +
			"узнавать предмет, а не дерево стало чистым")
	}
	if census.Carriers == 0 {
		t.Fatal("носителей выкатки не прочитано ни одного — «ноль находок» здесь означало бы " +
			"«ноль прочитанного»")
	}
	if census.UmbrellaCalls == 0 {
		t.Fatal("вызовов helm по чарту умбреллы не найдено ни одного — предпосылка " +
			"проверки исчезла: умбреллу чем-то катят")
	}
	for _, f := range findings {
		t.Errorf("привязка доставки манифестов: %s", f)
	}
}
