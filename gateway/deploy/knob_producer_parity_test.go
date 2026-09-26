// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// knob_producer_parity_test.go — ДВЕ КОЛОНКИ ОБ ОДНОЙ РУЧКЕ: что ОБЪЯВЛЯЕТ
// процесс края и что ЭМИТИРУЕТ чарт края.
//
// # Чего не хватало, пока этого файла не было
//
// Проба эмиссии адресов отзыва (`TestChart_EmitsRevocationEnv`) требовала
// эмиссии выписанных имён и не спрашивала, читает ли их процесс. Снятие
// читателя такой ручки оставляло пробу зелёной на эмиссии, которую не читает
// никто: оператор заполняет значение, видит его в поде и получает поведение по
// умолчанию. А изменение, снимающее ручку целиком, упиралось в эту же пробу
// красным посреди своей полосы (#2778).
//
// Соседний гейт (`internal/repohygiene`, TestDeclaredKnobHasAReader) спрашивает
// обратное — «у ключа профиля есть читатель в шаблоне?» — и на этом дефекте
// честно печатает ноль: у ключа профиля читатель в шаблоне ЕСТЬ, шаблон просто
// эмитирует имя, которого процесс не читает.
//
// # Что судится
//
//	(1) НАЗАД: имя, которое эмитирует шаблон чарта КРАЯ, обязано быть объявлено
//	    полем настроек края;
//	(2) СНЯТОЕ: имя из ведомости снятых ручек (`internal/retiredknobs`) не
//	    объявляет ни одно поле настроек и не эмитирует ни один шаблон края.
//
// Вперёд («эмитируй каждую объявленную») не судится намеренно: ручка с рабочим
// умолчанием, которую ни один профиль не задаёт, — законное состояние.
//
// # Читает ОБЪЯВЛЕНИЯ, а не рендер — и почему этого мало
//
// Исходник шаблона содержит ВСЕ имена, которые чарт умеет эмитировать, при
// любом условии. Имя, приезжающее в под через `extraEnv` профиля, исходнику
// неизвестно — его видит только рендер, и эту половину держит рендерная проба
// каждой цепочки (`deploy/edge_retired_knobs_render_test.go`).
package deploy_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/kelseyhightower/envconfig"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/internal/retiredknobs"
)

// knobEnvDecl — ОБЪЯВЛЕНИЕ переменной в шаблоне: элемент списка `env:` вида
// `- name: KACHO_…`. Строка, начинающаяся с решётки, отбрасывается выше по
// обходу: комментарий не исполняется, а шаблон полон прозы, объясняющей в том
// числе СНЯТЫЕ переменные.
var knobEnvDecl = regexp.MustCompile(`^\s*-\s*name:\s*([A-Z][A-Z0-9_]+)\s*$`)

// knobProductPrefixes — приставки имён окружения ЭТОГО продукта.
//
// envconfig консультирует для вложенного поля ДВА имени: выведенное из
// иерархии (`KACHO_API_GATEWAY_ADMISSION_PUBLIC_IN_FLIGHT`) и абсолютное из тега
// вложенного поля (`IN_FLIGHT`). Второе — законный вход процесса, но ручкой
// продукта не является и в чарте не эмитируется.
var knobProductPrefixes = []string{"KACHO_", "KANAME_"}

func knobIsProductName(name string) bool {
	for _, p := range knobProductPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// knobColumns — ДВЕ КОЛОНКИ и объём осмотренного.
type knobColumns struct {
	// Declared — имена, которые консультирует загрузчик настроек края.
	Declared map[string]bool
	// Emitted — имя → координаты в шаблонах чарта края.
	Emitted map[string][]string
	// TemplateFiles / TemplateLines — сколько прочитано.
	TemplateFiles int
	TemplateLines int
}

func (c knobColumns) String() string {
	return fmt.Sprintf("перепись: ОБЪЯВЛЕНО процессом края %d · ЭМИТИРУЕТ чарт края %d; "+
		"прочитано файлов шаблонов края %d, строк %d",
		len(c.Declared), len(c.Emitted), c.TemplateFiles, c.TemplateLines)
}

// declaredEdgeKnobs — имена, которые РЕАЛЬНО консультирует загрузчик.
//
// Берётся у САМОГО загрузчика (envconfig по структуре Config — той же, что
// читает `config.Load`), а не из списка в пробе: список разъехался бы с кодом
// ровно так же, как разъехался чарт. Учитываются ОБА имени поля — из иерархии
// (Key) и абсолютное из тега (Alt): иначе живая переменная была бы объявлена
// мёртвой.
func declaredEdgeKnobs(t *testing.T) map[string]bool {
	t.Helper()
	var buf bytes.Buffer
	if err := envconfig.Usagef("", &config.Config{}, &buf,
		"{{range .}}{{.Key}}\n{{.Alt}}\n{{end}}"); err != nil {
		t.Fatalf("перечисление имён настроек не выполнено: %v — проверка НЕ ИСПОЛНЯЛАСЬ", err)
	}
	out := map[string]bool{}
	for _, line := range strings.Split(buf.String(), "\n") {
		if name := strings.TrimSpace(line); name != "" && knobIsProductName(name) {
			out[name] = true
		}
	}
	return out
}

// emittedEdgeKnobs обходит шаблоны чарта края и собирает эмитируемые имена.
func emittedEdgeKnobs(t *testing.T, dir string) (emitted map[string][]string, files, lines int) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("каталог шаблонов края %s не читается (%v) — посылка проверки исчезла, "+
			"а это НЕ то же самое, что «находок ноль»", dir, err)
	}
	emitted = map[string][]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch filepath.Ext(e.Name()) {
		case ".yaml", ".yml", ".tpl":
		default:
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, readErr := os.ReadFile(path) // #nosec G304 -- путь получен обходом каталога дерева
		if readErr != nil {
			t.Fatalf("шаблон %s не читается: %v", path, readErr)
		}
		files++
		emitted = mergeEmissions(emitted, templateEmissions(filepath.ToSlash(path), string(raw)))
		lines += strings.Count(string(raw), "\n")
	}
	return emitted, files, lines
}

// templateEmissions — эмиссии одного шаблона: имя → координаты. Комментарий
// (строка, начинающаяся решёткой) не исполняется и в перепись не попадает.
func templateEmissions(path, body string) map[string][]string {
	out := map[string][]string{}
	for i, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		m := knobEnvDecl.FindStringSubmatch(line)
		if m == nil || !knobIsProductName(m[1]) {
			continue
		}
		out[m[1]] = append(out[m[1]], fmt.Sprintf("%s:%d", path, i+1))
	}
	return out
}

func mergeEmissions(dst, src map[string][]string) map[string][]string {
	for k, v := range src {
		dst[k] = append(dst[k], v...)
	}
	return dst
}

// readKnobColumns собирает обе колонки дерева.
func readKnobColumns(t *testing.T) knobColumns {
	t.Helper()
	cols := knobColumns{Declared: declaredEdgeKnobs(t)}
	cols.Emitted, cols.TemplateFiles, cols.TemplateLines = emittedEdgeKnobs(t, "templates")
	return cols
}

// judgeEmittedWithoutReader — НАЗАД: чарт края эмитирует имя, которого не
// объявляет ни одно поле настроек.
//
// Вынесено функцией, чтобы инъекция звала ТО ЖЕ, что исполняется на дереве:
// своя копия предиката в инъекции разошлась бы с настоящей пробой молча.
func judgeEmittedWithoutReader(cols knobColumns) []string {
	var out []string
	for name, where := range cols.Emitted {
		if cols.Declared[name] {
			continue
		}
		sorted := append([]string(nil), where...)
		sort.Strings(sorted)
		out = append(out, fmt.Sprintf("%s — эмитируется чартом края (%s), а поле настроек "+
			"его не объявляет: оператор заполняет значение, видит его в поде и получает "+
			"поведение по умолчанию", name, strings.Join(sorted, ", ")))
	}
	sort.Strings(out)
	return out
}

// retiredCensus — перепись ведомости снятых ручек против двух колонок.
type retiredCensus struct {
	Retired        int
	StillDeclared  int
	StillEmitted   int
	FindingsByKnob []string
}

// judgeRetiredKnobs — СНЯТОЕ: имя ведомости не объявлено процессом и не
// эмитируется шаблоном края. Обе половины — находки, и они разные: первая
// значит «снятую ручку вернули читателю», вторая — «производитель пережил
// читателя».
func judgeRetiredKnobs(retired map[string]string, cols knobColumns) retiredCensus {
	c := retiredCensus{Retired: len(retired)}
	for name, why := range retired {
		if strings.TrimSpace(why) == "" {
			c.FindingsByKnob = append(c.FindingsByKnob, fmt.Sprintf("%s — запись ведомости без "+
				"письменного обоснования: неотличима от упущения, и снять её потом будет не "+
				"по чему", name))
		}
		if cols.Declared[name] {
			c.StillDeclared++
			c.FindingsByKnob = append(c.FindingsByKnob, fmt.Sprintf("%s — ведомость называет "+
				"ручку СНЯТОЙ, а поле настроек края её объявляет: снятую ручку вернули "+
				"читателю, и ведомость лжёт о процессе", name))
		}
		if where, still := cols.Emitted[name]; still {
			c.StillEmitted++
			sorted := append([]string(nil), where...)
			sort.Strings(sorted)
			c.FindingsByKnob = append(c.FindingsByKnob, fmt.Sprintf("%s — ручка СНЯТА с "+
				"процесса, а чарт края её эмитирует (%s): производитель пережил читателя",
				name, strings.Join(sorted, ", ")))
		}
	}
	sort.Strings(c.FindingsByKnob)
	return c
}

// TestEdgeChartEmitsNoKnobTheProcessNeverReads — НАЗАД по дереву.
func TestEdgeChartEmitsNoKnobTheProcessNeverReads(t *testing.T) {
	t.Parallel()
	cols := readKnobColumns(t)
	t.Log(cols.String())

	if len(cols.Declared) == 0 {
		t.Fatal("процесс края не объявляет ни одного имени с приставкой продукта — " +
			"перечисление ничего не прочитало, и его молчание не является утверждением")
	}
	if cols.TemplateFiles == 0 {
		t.Fatal("прочитано ноль файлов шаблонов чарта края — эмиссию искать было негде")
	}
	if len(cols.Emitted) == 0 {
		t.Fatal("чарт края не эмитирует ни одного имени с приставкой продукта — обход " +
			"прочитал не то, и «находок ноль» означало бы «прочитано ноль»")
	}
	for _, f := range judgeEmittedWithoutReader(cols) {
		t.Error(f)
	}
}

// TestEdgeRetiredKnobsStayRetiredOnBothSides — СНЯТОЕ по дереву.
//
// На ПУСТОЙ ведомости проба проходит, объявляя перепись: проба не имеет права
// падать на достижении своей цели. Способность упасть доказана инъекцией на
// синтетике (knob_producer_parity_injection_test.go), а не живой записью.
func TestEdgeRetiredKnobsStayRetiredOnBothSides(t *testing.T) {
	t.Parallel()
	cols := readKnobColumns(t)
	if len(cols.Declared) == 0 || cols.TemplateFiles == 0 {
		t.Fatalf("колонки пусты (%s) — судить снятое не по чему", cols)
	}
	c := judgeRetiredKnobs(retiredknobs.Edge(), cols)
	t.Logf("перепись ведомости: снятых ручек объявлено %d · из них процесс ещё читает %d · "+
		"чарт края ещё эмитирует %d", c.Retired, c.StillDeclared, c.StillEmitted)
	for _, f := range c.FindingsByKnob {
		t.Error(f)
	}
}
