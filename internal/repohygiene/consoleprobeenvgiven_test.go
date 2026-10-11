// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consoleprobeenvgiven_test.go — переменную, которую читает набор проб консоли,
// отдаёт шагу прогона тот конвейер, что этот набор гоняет.
//
// # Предмет
//
// Пробы распорядителя над записью человека (Ф3-24, Ф12-45) берут удостоверение
// администратора облака стенда из `KACHO_CLOUD_ADMIN_EMAIL` и
// `KACHO_CLOUD_ADMIN_PASSWORD`; без них проба честно помечает себя «условие не
// создано». На своём стенде прогонщика (подъём `own-up`) они зеленели, а
// конвейер `console-e2e` поднимает `dev-up` и этих переменных шагу проб не
// отдавал: `KACHO_CLOUD_ADMIN_*` в `.github` — ноль вхождений. Итог — две пробы
// «не выполнилось» на КАЖДОМ прогоне, гейт отчёта с кодом 3, и шаг падает на
// запросах в ветки линии и в ствол. Условие было объявлено, но его никто не
// создавал (`testing.md`, e2e-missing-dep-own-job: «дай ему свою job/волну,
// создающую условие»).
//
// Класс шире двух имён: всякая переменная, которую читает набор, — вход
// прогона, и конвейер, который набор исполняет, либо её отдаёт, либо набор
// получает «не выполнилось» на каждом прогоне по построению.
//
// # Что читается
//
// Набор — то, что исполняет `npx playwright test`: объявление
// `ui-future/e2e/playwright.config.ts` и каталоги проб `specs/` и
// `preconditions/`. Каталог `scripts/` — входы ДРУГИХ шагов (самопроверки), их
// переменные шагу проб не нужны и здесь не судятся.
//
// Законных форм чтения переменной в коде набора две, и обе известны:
//   - имя константой — `export const X_ENV = "KACHO_…"` (так названы
//     удостоверение администратора и адрес приёмника писем);
//   - прямое чтение — `process.env.KACHO_…` либо `process.env["KACHO_…"]`.
//
// Строки-комментарии кода (`//`, ` * `, `/*`) не читаются: имя, названное в
// объяснении, чтением не является.
//
// Законных форм ОТДАЧИ в конвейере три: ключ `env:` уровня файла, джобы или
// самого шага проб, и присваивание в исполняемой части тела шага проб
// (`export KACHO_…=` либо `KACHO_…=` в начале команды). Упоминание в
// комментарии тела отдачей не является.
//
// # Необязательные ручки
//
// `KACHO_CHROMIUM` — ручка удобства: браузер уже стоит в среде, и качать его
// нечем; без неё набор берёт свой браузер, и условие не теряется. Такие ручки
// перечислены ниже поимённо, с причиной, и запись ИСТЕКАЕТ сама: ручка, которую
// набор больше не читает, — находка (exemption-expires-with-subject).
//
// # Перепись
//
// Гейт печатает, сколько файлов набора прочёл, сколько переменных в них нашёл
// и сколько джоб с прогоном проб осмотрел. Ноль файлов, ноль переменных или
// ноль джоб — провал: «ноль находок» обязано отличаться от «ноль прочитанного».
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// probeEnvOptional — ручки набора, без которых условие прогона не теряется.
// Ключ — имя, величина — почему без неё набор исполняется полностью.
var probeEnvOptional = map[string]string{
	"KACHO_CHROMIUM": "браузер среды вместо добытого набором; без ручки playwright запускает свой — условие не теряется",
	"KACHO_CONSOLE_MEASURE": "выбор набора ЗАМЕРОВ (`measurements/`, kacho#1281) вместо набора проб; конвейер её не отдаёт намеренно — " +
		"без ручки исполняется весь набор `specs/`, а замер тратит ось источника сверх бюджета набора по построению",
}

// reProbeEnvConst — имя переменной константой: `const X = "KACHO_…"`.
var reProbeEnvConst = regexp.MustCompile(`\bconst\s+[A-Za-z_][A-Za-z0-9_]*\s*(?::\s*[A-Za-z]+\s*)?=\s*["'](KACHO_[A-Z0-9_]+)["']`)

// reProbeEnvRead — прямое чтение: `process.env.KACHO_…` либо `process.env["KACHO_…"]`.
var reProbeEnvRead = regexp.MustCompile(`process\.env(?:\.(KACHO_[A-Z0-9_]+)|\[\s*["'](KACHO_[A-Z0-9_]+)["']\s*\])`)

// reShellGive — присваивание переменной в исполняемой части тела шага.
var reShellGive = regexp.MustCompile(`(?m)(?:^\s*|[;&|]\s*|\bexport\s+)(KACHO_[A-Z0-9_]+)=`)

// tsCodePart — исходник без строк-комментариев.
func tsCodePart(src string) string {
	var b strings.Builder
	inBlock := false
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if inBlock {
			if strings.Contains(trimmed, "*/") {
				inBlock = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "/*") {
			if !strings.Contains(trimmed, "*/") {
				inBlock = true
			}
			continue
		}
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// probeEnvReads — переменные, которые читает код набора: имя → файлы-читатели.
func probeEnvReads(files map[string]string) map[string][]string {
	reads := map[string][]string{}
	add := func(name, file string) {
		for _, f := range reads[name] {
			if f == file {
				return
			}
		}
		reads[name] = append(reads[name], file)
	}
	for file, src := range files {
		code := tsCodePart(src)
		for _, m := range reProbeEnvConst.FindAllStringSubmatch(code, -1) {
			add(m[1], file)
		}
		for _, m := range reProbeEnvRead.FindAllStringSubmatch(code, -1) {
			if m[1] != "" {
				add(m[1], file)
			} else {
				add(m[2], file)
			}
		}
	}
	for _, v := range reads {
		sort.Strings(v)
	}
	return reads
}

type probeEnvWorkflowDoc struct {
	Env  map[string]any `yaml:"env"`
	Jobs map[string]struct {
		Env   map[string]any `yaml:"env"`
		Steps []struct {
			ID  string         `yaml:"id"`
			Run string         `yaml:"run"`
			Env map[string]any `yaml:"env"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

type probeEnvCensus struct {
	SuiteFiles int
	Variables  int
	ProbeJobs  int
}

// checkProbeEnvGiven — находки: переменная набора, которую шаг проб не получает,
// и необязательная ручка, которую набор больше не читает.
func checkProbeEnvGiven(path, workflow string, suite map[string]string) ([]string, probeEnvCensus) {
	census := probeEnvCensus{SuiteFiles: len(suite)}
	reads := probeEnvReads(suite)
	census.Variables = len(reads)

	var findings []string
	for knob := range probeEnvOptional {
		if _, ok := reads[knob]; !ok {
			findings = append(findings, "необязательная ручка `"+knob+"` записана в перечне гейта, а набор её "+
				"больше не читает: записи нечего исключать — снять её вместе с предметом")
		}
	}

	var doc probeEnvWorkflowDoc
	if err := yaml.Unmarshal([]byte(workflow), &doc); err != nil {
		return append(findings, path+": не разобран YAML: "+err.Error()+" — файл НЕ проверен"), census
	}
	for jobName, job := range doc.Jobs {
		for _, st := range job.Steps {
			exec := shellExecutablePart(st.Run)
			if !reProbeRunStep.MatchString(exec) {
				continue
			}
			census.ProbeJobs++
			given := map[string]bool{}
			for _, m := range []map[string]any{doc.Env, job.Env, st.Env} {
				for k := range m {
					given[k] = true
				}
			}
			for _, m := range reShellGive.FindAllStringSubmatch(exec, -1) {
				given[m[1]] = true
			}
			for name, readers := range reads {
				if given[name] {
					continue
				}
				if _, optional := probeEnvOptional[name]; optional {
					continue
				}
				findings = append(findings, path+": job "+jobName+", шаг проб `"+st.ID+"` — переменную `"+name+
					"` читает набор ("+strings.Join(readers, ", ")+"), а шаг её не получает: ни ключом `env:`, "+
					"ни присваиванием в теле. Условие, которое она несёт, на этом прогоне не создаётся, "+
					"и её пробы уходят в «не выполнилось» по построению")
			}
			break
		}
	}
	sort.Strings(findings)
	return findings, census
}

// probeSuiteDirs — что исполняет `npx playwright test` из ui-future/e2e.
var probeSuiteDirs = []string{"specs", "preconditions"}

func readProbeSuite(t *testing.T, root string) map[string]string {
	t.Helper()
	base := filepath.Join(root, "ui-future", "e2e")
	suite := map[string]string{}
	read := func(rel string) {
		raw, err := os.ReadFile(filepath.Join(base, rel)) // #nosec G304 -- путь собран из корня репозитория
		if err != nil {
			t.Fatalf("%s не прочитан: %v", rel, err)
		}
		suite[filepath.ToSlash(filepath.Join("ui-future/e2e", rel))] = string(raw)
	}
	read("playwright.config.ts")
	for _, dir := range probeSuiteDirs {
		entries, err := os.ReadDir(filepath.Join(base, dir))
		if err != nil {
			t.Fatalf("каталог набора %s не прочитан: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".ts") {
				continue
			}
			read(filepath.Join(dir, e.Name()))
		}
	}
	return suite
}

// TestConsoleProbeSuiteVariablesAreGivenByItsConveyor — по дереву.
func TestConsoleProbeSuiteVariablesAreGivenByItsConveyor(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	suite := readProbeSuite(t, root)
	rel := filepath.ToSlash(filepath.Join(workflowsDir, "console-e2e.yml"))
	raw, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь собран из корня репозитория
	if err != nil {
		t.Fatalf("%s не прочитан: %v", rel, err)
	}
	findings, census := checkProbeEnvGiven(rel, string(raw), suite)
	t.Logf("осмотрено: файлов набора %d, переменных набора %d, джоб с прогоном проб %d",
		census.SuiteFiles, census.Variables, census.ProbeJobs)
	if census.SuiteFiles == 0 || census.Variables == 0 || census.ProbeJobs == 0 {
		t.Fatalf("пустой обход — не вердикт: файлов %d, переменных %d, джоб с прогоном проб %d",
			census.SuiteFiles, census.Variables, census.ProbeJobs)
	}
	for _, f := range findings {
		t.Error(f)
	}
}
