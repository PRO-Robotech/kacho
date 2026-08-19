// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clusterdiagnosticsecho_test.go — диагностика кластера не краснеет эхом, когда
// стенда не было.
//
// # Предмет (#728)
//
// Шаг, читающий состояние кластера, объявляется с функцией состояния
// (`always()` / `!cancelled()` / `failure()`) — иначе он не исполнится ровно
// тогда, когда нужен: на упавшем прогоне. Но та же функция означает и
// «исполняйся, когда работа сорвалась ЗАДОЛГО до стенда». Кластера в этот
// момент нет, обращение к нему отказывает, и рядом с НАЗВАННОЙ причиной в
// сводке встаёт второй красный шаг с посторонним текстом.
//
// Читателю сводки один отказ выглядит как два, и второй из них не поломка
// вовсе: он эхо первого. Наблюдалось на PR #725 — предел шага на установке
// зависимостей браузера истёк, и следующим красным шагом стало «что поднялось»,
// печатавшее отказ обращения к кластеру. Это близнец #726 с другой стороны: там
// «условие не создано» было неотличимо от «продукт сломан», здесь лишний шум
// маскирует настоящую причину.
//
// # Что требуется, и почему трёх требований, а не одного
//
//  1. ПРИВЯЗКА. Условие шага ссылается на исход шага, поднимавшего стенд
//     (`steps.<id>.outcome`). Тогда шаг не исполняется, если стенд даже не
//     пробовали поднять.
//
//  2. ОБЪЯСНЕНИЕ. Тело зовёт общий страж
//     `.github/scripts/stand-present-or-explain.sh` ДО первого чтения. Одной
//     привязки мало: когда подъём стенда попробовали и он УПАЛ, диагностика
//     обязана исполниться (она для этого и написана) — и тогда кластера всё
//     равно нет. Без стража остаётся молчание: девять файлов «журнал
//     недоступен» без единого слова о причине, и она ищется в продукте.
//
//  3. НЕВЫНЕСЕНИЕ ВЕРДИКТА. Ни одно чтение кластера не может стать кодом
//     возврата шага: каждое либо стоит в условии, либо гасит свой код `||`.
//     Кластер может исчезнуть и между стражем и следующей строкой.
//
// Требования держатся вместе: любое поодиночке оставляет свою дыру, и все три
// проверены исполнением тел шагов в положении «кластера нет» (код 0 и
// названная причина) и «кластер отвечает» (диагностика работает как прежде).
//
// # Что этот гейт НЕ читает — граница названа, а не подразумевается
//
// Гейт судит по КОМАНДНОЙ ПОЗИЦИИ в теле шага, из которого вырезаны
// комментарии: полного разбора грамматики оболочки здесь нет и не будет.
// Отсюда три честно названных предела:
//
//   - инструмент, вызванный через переменную (`$KUBECTL get pods`), не
//     распознаётся;
//   - глагол, отделённый от инструмента неизвестным флагом со значением, будет
//     принят за неизвестный — и это НАХОДКА, а не пропуск: корзины «прочее» у
//     словаря нет;
//   - шаги уборки (`kind delete`, `kubectl delete`) предметом не являются:
//     отчитываться им не о чем, а привязка к исходу подъёма отняла бы у них
//     право снести кластер после обрыва. Классификация — по словарю ниже.
//
// # Перепись
//
// «Ноль находок» обязано быть отличимо от «ноль прочитанного»: гейт печатает,
// сколько файлов конвейеров прочитал, сколько шагов обошёл, сколько из них
// обращаются к кластеру, сколько читают его состояние и сколько из читающих
// объявлены с функцией состояния — то есть являются предметом. Пустой обход и
// нулевой предмет — провал, а не тишина.
package repohygiene

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// clusterDiagGuard — общий страж: единственное место, которое знает, как
// спросить кластер и что сказать, когда его нет.
const clusterDiagGuard = "stand-present-or-explain.sh"

// clusterDiagStateFuncs — функции состояния. Именно они дают шагу право
// исполниться на упавшем прогоне — и вместе с правом приносят предмет #728.
var clusterDiagStateFuncs = []string{"always()", "cancelled()", "failure()"}

// clusterDiagOutcomeRef — ссылка на исход другого шага. Ровно она и означает
// «шаг знает, был ли у него предмет».
var clusterDiagOutcomeRef = regexp.MustCompile(`steps\.[A-Za-z0-9_-]+\.(outcome|conclusion)`)

// Словарь глаголов. Корзины «прочее» нет намеренно: неизвестный глагол — это
// находка, а не умолчание. Иначе первый же новый вид обращения к кластеру
// проехал бы мимо гейта молча, и «ноль находок» перестало бы что-либо значить.
var (
	// ЧТЕНИЕ состояния кластера — предмет правила. Такой шаг ОТЧИТЫВАЕТСЯ, и
	// без кластера ему нечего сказать, кроме причины.
	clusterDiagRead = map[string]map[string]bool{
		"kubectl": {
			"get": true, "logs": true, "describe": true, "events": true,
			"top": true, "cluster-info": true, "api-resources": true,
			"explain": true, "auth": true, "exec": true, "wait": true,
			"rollout": true, "port-forward": true, "cp": true,
		},
		"helm": {
			"list": true, "status": true, "get": true, "history": true, "test": true,
		},
		"kind": {"get": true, "export": true},
	}

	// ИЗМЕНЕНИЕ состояния — не предмет: уборка и подъём отчитываться не обязаны,
	// а привязка отняла бы у уборки право снести кластер после обрыва.
	clusterDiagMutate = map[string]map[string]bool{
		"kubectl": {
			"apply": true, "create": true, "delete": true, "patch": true,
			"replace": true, "scale": true, "label": true, "annotate": true,
			"set": true, "cordon": true, "drain": true, "taint": true,
			"run": true, "expose": true,
		},
		"helm": {
			"install": true, "upgrade": true, "uninstall": true, "rollback": true,
			"dependency": true, "dep": true, "repo": true,
		},
		"kind": {"create": true, "delete": true, "load": true},
	}

	// ОФФЛАЙН — кластера не касается вовсе, обращением не считается.
	clusterDiagOffline = map[string]map[string]bool{
		"kubectl": {"config": true, "version": true, "completion": true, "kustomize": true},
		"helm":    {"lint": true, "template": true, "show": true, "version": true, "env": true},
		"kind":    {"version": true, "completion": true},
	}
)

// clusterDiagFlagTakesValue — флаги, у которых значение идёт ОТДЕЛЬНЫМ словом.
// Нужны затем, чтобы `kubectl -n kacho get pods` дал глагол `get`, а не `kacho`.
var clusterDiagFlagTakesValue = map[string]bool{
	"-n": true, "--namespace": true, "--context": true, "--kubeconfig": true,
	"-o": true, "--output": true, "-l": true, "--selector": true,
	"-f": true, "--filename": true, "-c": true, "--container": true,
	"--name": true, "--since": true, "--tail": true, "--request-timeout": true,
	"--sort-by": true, "--server": true, "--token": true, "--as": true,
}

// clusterDiagWrappers — слова, после которых следующее слово всё ещё стоит в
// командной позиции.
var clusterDiagWrappers = map[string]bool{
	"if": true, "then": true, "elif": true, "else": true, "do": true,
	"while": true, "until": true, "!": true, "time": true, "sudo": true,
	"env": true, "exec": true, "command": true, "xargs": true, "watch": true,
}

// clusterDiagStripComments вырезает комментарии оболочки, уважая кавычки.
//
// Наивный срез по первому «#» здесь неверен и молча теряет исполняемый текст:
// в теле шага стоит `kacho#655` ВНУТРИ строки в кавычках, и срез унёс бы всё,
// что за ней, — в том числе `|| true`, от которого зависит вердикт гейта.
func clusterDiagStripComments(raw string) string {
	var out strings.Builder
	var inSingle, inDouble bool
	prevSpace := true // начало строки считается пробелом
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c == '\\' && inDouble && i+1 < len(raw):
			out.WriteByte(c)
			i++
			out.WriteByte(raw[i])
			prevSpace = false
			continue
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '#' && !inSingle && !inDouble && prevSpace:
			// комментарий до конца строки; перевод строки сохраняем
			for i < len(raw) && raw[i] != '\n' {
				i++
			}
			if i < len(raw) {
				out.WriteByte('\n')
			}
			prevSpace = true
			continue
		}
		out.WriteByte(c)
		prevSpace = c == ' ' || c == '\t' || c == '\n'
	}
	return out.String()
}

// clusterDiagSegments — исполняемые сегменты тела: строки склеиваются по
// переносу `\`, затем режутся по `;` и переводу строки. Сегмент — та единица,
// внутри которой имеет смысл спрашивать «погашен ли код возврата этого вызова».
//
// Резать НАИВНО нельзя, и это не теория: в теле шага стоит
// `awk '{split($2,a,"/"); if (…) n++}'` — точка с запятой ВНУТРИ программы awk,
// в одинарных кавычках. Наивный разрез разносит вызов и его `|| true` по разным
// сегментам, и гейт объявляет находкой ровно то, что он же и требует. Поймано
// первым же прогоном по дереву, а не вычитано.
func clusterDiagSegments(code string) []string {
	joined := strings.ReplaceAll(code, "\\\n", " ")
	var segs []string
	var cur strings.Builder
	var inSingle, inDouble bool
	depth := 0 // вложенность `$(` и `(`: `;` внутри подстановки не разделяет
	flush := func() {
		if strings.TrimSpace(cur.String()) != "" {
			segs = append(segs, cur.String())
		}
		cur.Reset()
	}
	for i := 0; i < len(joined); i++ {
		c := joined[i]
		switch {
		case c == '\\' && inDouble && i+1 < len(joined):
			cur.WriteByte(c)
			i++
			cur.WriteByte(joined[i])
			continue
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case !inSingle && !inDouble && c == '(':
			depth++
		case !inSingle && !inDouble && c == ')' && depth > 0:
			depth--
		case !inSingle && !inDouble && depth == 0 && (c == ';' || c == '\n'):
			flush()
			continue
		}
		cur.WriteByte(c)
	}
	flush()
	return segs
}

// clusterDiagInvocation — один распознанный вызов инструмента.
type clusterDiagInvocation struct {
	Tool    string
	Verb    string
	Class   string // "read" | "mutate" | "offline" | "unknown"
	Segment string
	At      int // смещение вызова в исполняемом тексте — нужен порядок относительно стража
	Fatal   bool
}

var clusterDiagToolRe = regexp.MustCompile(`(?:^|[\s;&|(){}` + "`" + `])((?:[A-Za-z0-9_./-]*/)?(kubectl|helm|kind))(\s|$)`)

// clusterDiagVerb достаёт глагол, пропуская флаги и их отдельно стоящие значения.
func clusterDiagVerb(rest string) string {
	words := strings.Fields(rest)
	for i := 0; i < len(words); i++ {
		w := words[i]
		if strings.HasPrefix(w, "-") {
			if !strings.Contains(w, "=") && clusterDiagFlagTakesValue[w] {
				i++ // значение флага — не глагол
			}
			continue
		}
		return w
	}
	return ""
}

// clusterDiagScan находит вызовы инструментов в исполняемом тексте.
func clusterDiagScan(code string) []clusterDiagInvocation {
	var out []clusterDiagInvocation
	offset := 0
	for _, seg := range clusterDiagSegments(code) {
		// смещение сегмента в исходном тексте нам нужно лишь для ПОРЯДКА,
		// поэтому счётчик монотонный, а не точный.
		base := offset
		offset += len(seg)
		for _, m := range clusterDiagToolRe.FindAllStringSubmatchIndex(seg, -1) {
			full := seg[m[2]:m[3]]
			tool := seg[m[4]:m[5]]
			before := strings.TrimSpace(seg[:m[2]])
			if !clusterDiagCommandPosition(before) {
				continue // аргумент или содержимое строки — не вызов
			}
			verb := clusterDiagVerb(seg[m[5]:])
			class := "unknown"
			switch {
			case clusterDiagOffline[tool][verb]:
				class = "offline"
			case clusterDiagRead[tool][verb]:
				class = "read"
			case clusterDiagMutate[tool][verb]:
				class = "mutate"
			}
			inv := clusterDiagInvocation{
				Tool: full, Verb: verb, Class: class,
				Segment: strings.TrimSpace(seg), At: base + m[2],
			}
			inv.Fatal = !clusterDiagSuppressed(seg, m[2])
			out = append(out, inv)
		}
	}
	return out
}

// clusterDiagCommandPosition — стоит ли вызов в командной позиции. Решает
// ПОСЛЕДНЕЕ слово перед ним: `echo kubectl …` вызовом не является, `$(kubectl …)`
// является.
func clusterDiagCommandPosition(before string) bool {
	if before == "" {
		return true
	}
	fields := strings.Fields(before)
	last := fields[len(fields)-1]
	if clusterDiagWrappers[last] {
		return true
	}
	switch last[len(last)-1] {
	case ';', '&', '|', '(', '{', '`', '!':
		return true
	}
	// префиксное присваивание перед командой: `PATH=x kubectl …`
	if regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`).MatchString(last) && !strings.HasSuffix(last, "\"") {
		return true
	}
	return false
}

// clusterDiagSuppressed — может ли отказ этого вызова стать кодом возврата шага.
// Не может, если сегмент — условие (`if ! kubectl …`), либо если за вызовом в
// том же сегменте стоит запасной путь `||`.
func clusterDiagSuppressed(seg string, at int) bool {
	head := strings.Fields(strings.TrimSpace(seg))
	if len(head) > 0 && clusterDiagWrappers[head[0]] && head[0] != "then" && head[0] != "else" && head[0] != "do" {
		return true
	}
	return strings.Contains(seg[at:], "||")
}

// clusterDiagCensus — сколько чего осмотрено.
type clusterDiagCensus struct {
	Files    int
	Steps    int
	Touching int // шагов, обращающихся к инструментам кластера
	Reading  int // из них читающих его состояние
	Subject  int // из читающих — объявленных с функцией состояния
}

func (c *clusterDiagCensus) add(o clusterDiagCensus) {
	c.Files += o.Files
	c.Steps += o.Steps
	c.Touching += o.Touching
	c.Reading += o.Reading
	c.Subject += o.Subject
}

// clusterDiagDoc — то немногое из workflow, что нужно этому гейту.
type clusterDiagDoc struct {
	Jobs map[string]struct {
		Steps []struct {
			Name string `yaml:"name"`
			If   string `yaml:"if"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// checkClusterDiagnosticsEcho — находки одного файла плюс его перепись.
// Вынесено отдельно, чтобы обход можно было доказать инъекцией на синтетическом
// содержимом, не трогая дерево.
func checkClusterDiagnosticsEcho(path, raw string) ([]string, clusterDiagCensus) {
	var doc clusterDiagDoc
	census := clusterDiagCensus{Files: 1}
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return []string{path + ": не разобран YAML: " + err.Error() + " — файл НЕ проверен"}, census
	}

	var findings []string
	for job, j := range doc.Jobs {
		for i, st := range j.Steps {
			census.Steps++
			if st.Run == "" {
				continue
			}
			code := clusterDiagStripComments(st.Run)
			invs := clusterDiagScan(code)
			if len(invs) == 0 {
				continue
			}
			where := path + ": job " + job + ", шаг #" + strconv.Itoa(i+1) + " («" + st.Name + "»)"

			var reads []clusterDiagInvocation
			touching := false
			for _, inv := range invs {
				switch inv.Class {
				case "unknown":
					findings = append(findings, where+": глагол `"+inv.Tool+" "+inv.Verb+
						"` не отнесён ни к чтению кластера, ни к изменению, ни к оффлайну. "+
						"Корзины «прочее» у словаря нет намеренно: неотнесённый глагол проехал бы "+
						"мимо правила молча, и «ноль находок» перестало бы что-либо значить. "+
						"Допиши его в словарь `clusterDiagRead`/`clusterDiagMutate`/`clusterDiagOffline`")
					touching = true
				case "read":
					reads = append(reads, inv)
					touching = true
				case "mutate":
					touching = true
				}
			}
			if touching {
				census.Touching++
			}
			if len(reads) == 0 {
				continue
			}
			census.Reading++

			stateful := false
			for _, fn := range clusterDiagStateFuncs {
				if strings.Contains(st.If, fn) {
					stateful = true
					break
				}
			}
			if !stateful {
				// Без функции состояния шаг и так пропустится при упавшем
				// предшественнике — эха не будет by construction.
				continue
			}
			census.Subject++

			// (1) привязка к исходу шага, поднимавшего стенд
			if !clusterDiagOutcomeRef.MatchString(st.If) {
				findings = append(findings, where+": читает состояние кластера и объявлен с функцией "+
					"состояния в условии `"+strings.TrimSpace(st.If)+"`, но ни к какому исходу шага НЕ привязан. "+
					"Он исполнится и тогда, когда работа сорвалась ЗАДОЛГО до стенда, — и рядом с названной "+
					"причиной встанет второй красный шаг, у которого нет собственного предмета. "+
					"Привяжи условие к исходу шага подъёма (`steps.<id>.outcome`)")
			}

			// (2) страж зовётся ДО первого чтения, и его собственный отказ шаг не роняет
			guardAt, guardOK := clusterDiagGuardCall(code)
			firstRead := reads[0].At
			switch {
			case !guardOK:
				findings = append(findings, where+": читает состояние кластера, но не зовёт общий страж `"+
					clusterDiagGuard+"` в форме, которая не роняет шаг сама (в условии либо через `||`). "+
					"Без него на отсутствующем кластере шаг либо краснеет эхом, либо молчит — и причина "+
					"ищется в продукте, хотя названа шагом выше")
			case guardAt > firstRead:
				findings = append(findings, where+": страж `"+clusterDiagGuard+
					"` зовётся ПОСЛЕ первого чтения кластера. Порядок — часть свойства: до стража чтение "+
					"уже произошло, и объяснять нечего")
			}

			// (3) ни одно чтение не выносит вердикта
			for _, inv := range reads {
				if inv.Fatal {
					findings = append(findings, where+": чтение `"+inv.Tool+" "+inv.Verb+
						"` выносит вердикт — его код возврата становится кодом возврата шага. "+
						"Кластер может исчезнуть и между стражем и этой строкой; диагностика вердикта "+
						"не выносит. Погаси код возврата (`|| true`) либо поставь вызов в условие. "+
						"Сегмент: `"+clusterDiagShorten(inv.Segment)+"`")
				}
			}
		}
	}
	sort.Strings(findings)
	return findings, census
}

// clusterDiagGuardCall — где в исполняемом тексте стоит вызов стража и стоит ли
// он в форме, которая сама не роняет шаг.
func clusterDiagGuardCall(code string) (int, bool) {
	offset := 0
	for _, seg := range clusterDiagSegments(code) {
		base := offset
		offset += len(seg)
		idx := strings.Index(seg, clusterDiagGuard)
		if idx < 0 {
			continue
		}
		if !clusterDiagSuppressed(seg, idx) {
			continue // вызван, но его собственный код возврата уронит шаг
		}
		return base + idx, true
	}
	return 0, false
}

func clusterDiagShorten(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

// TestClusterDiagnosticsNeverEchoAMissingStand — по дереву.
func TestClusterDiagnosticsNeverEchoAMissingStand(t *testing.T) {
	root := repoRoot(t)
	files := listWorkflows(t, root)
	if len(files) == 0 {
		t.Fatalf("в %s не найдено ни одного workflow — обход сломан, а не дерево чисто", workflowsDir)
	}

	var total clusterDiagCensus
	var all []string
	for _, rel := range files {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("не прочитан %s: %v", rel, err)
		}
		findings, census := checkClusterDiagnosticsEcho(rel, string(raw))
		total.add(census)
		all = append(all, findings...)
	}

	t.Logf("осмотрено: файлов конвейеров %d, шагов %d; обращаются к кластеру %d, "+
		"читают его состояние %d, из них с функцией состояния (предмет правила) %d",
		total.Files, total.Steps, total.Touching, total.Reading, total.Subject)

	// Предпосылка гейта: предмет существует. Ноль читающих шагов с функцией
	// состояния значит, что сторожить нечего, — и об этом надо сказать, а не
	// молча зеленеть, обещая защиту.
	if total.Subject == 0 {
		t.Fatalf("ни одного шага, читающего кластер под функцией состояния, в %s — "+
			"у гейта не осталось предмета. Либо стенд уехал из конвейера (тогда снимать надо "+
			"и этот гейт, вместе с его предметом), либо сломан обход", workflowsDir)
	}

	for _, f := range all {
		t.Error(f)
	}
}

// TestStandPresentOrExplainScriptDoesNotFailAndDoesNotStayQuiet — поведение
// САМОГО стража, а не утверждение о нём.
//
// Гейт выше требует, чтобы страж звался. Что страж при этом делает — вопрос
// отдельный, и ответ на него добывается исполнением: под отвечающим кластером
// он молчит и отвечает нулём, без кластера — называет причину и отвечает
// единицей, чтобы вызывающий вышел успехом. Обе стороны обязательны: молчащий
// на отказе страж бесполезен, а кричащий на живом кластере зашумил бы каждый
// прогон и был бы снят первым же, кому помешал.
func TestStandPresentOrExplainScriptDoesNotFailAndDoesNotStayQuiet(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, ".github", "scripts", clusterDiagGuard)
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("стража нет по координате %s: %v — гейт требует того, чего в дереве не существует", script, err)
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("bash недоступен (%v) — предпосылка исполняемой пробы не выполняется", err)
	}

	cases := []struct {
		name     string
		stub     string
		wantCode int
		wantSay  bool
	}{
		{
			name:     "кластера нет — страж называет причину и просит вызывающего выйти успехом",
			stub:     "#!/bin/sh\necho \"E: connection to the server was refused\" >&2\nexit 1\n",
			wantCode: 1,
			wantSay:  true,
		},
		{
			name:     "кластер отвечает — страж молчит и пропускает диагностику",
			stub:     "#!/bin/sh\necho \"Kubernetes control plane is running\"\nexit 0\n",
			wantCode: 0,
			wantSay:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			if err := os.MkdirAll(bin, 0o750); err != nil {
				t.Fatalf("не создан каталог заглушек: %v", err)
			}
			stub := filepath.Join(bin, "kubectl")
			if err := os.WriteFile(stub, []byte(tc.stub), 0o500); err != nil {
				t.Fatalf("не записана заглушка: %v", err)
			}
			cmd := exec.Command(bash, script, "проба")
			cmd.Env = append(os.Environ(), "PATH="+bin, "KACHO_CLUSTER_PROBE_TIMEOUT=2s")
			raw, err := cmd.CombinedOutput()
			out := string(raw)
			code := 0
			if err != nil {
				var ee *exec.ExitError
				if !errors.As(err, &ee) {
					t.Fatalf("страж не запустился: %v", err)
				}
				code = ee.ExitCode()
			}
			if code != tc.wantCode {
				t.Errorf("код возврата стража %d, ожидался %d; вывод:\n%s", code, tc.wantCode, out)
			}
			said := strings.Contains(out, "УСЛОВИЕ НЕ СОЗДАНО") && strings.Contains(out, "::notice")
			if said != tc.wantSay {
				t.Errorf("страж %s назвал причину, а должен был %s; вывод:\n%s",
					map[bool]string{true: "", false: "НЕ"}[said],
					map[bool]string{true: "назвать", false: "промолчать"}[tc.wantSay], out)
			}
		})
	}
}
