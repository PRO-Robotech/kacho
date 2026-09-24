// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_dev_ceremony_band_injection_test.go — распознаватель полосы
// экранов входа в конфигурации сборщика краснеет на дефекте и молчит на
// законном близнеце (задача #2733).
//
// Вход — НАСТОЯЩАЯ конфигурация из дерева (`ui-future/host/vite.config.ts`), а
// не выдуманный текст: рядом с полосой в живом файле лежат другие полосы,
// адреса с `//` внутри строк и комментарии, и именно среди них распознаватель
// обязан найти свой предмет. Каждый вариант меняет РОВНО ОДИН факт против
// законного близнеца и правится по ПОЗИЦИЯМ разбора, а не заменой по образцу.
//
// Вход привязан к предмету: когда полоса будет снята из сборщика целиком
// (цель #2733), этой пробе станет нечего вносить. Тогда она снимается тем же
// изменением вместе с проверкой рядом — и говорит об этом отказом, а не
// молчаливым зелёным.
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// devBandInjectionInput — конфигурация сборщика, в которую вносятся дефекты.
var devBandInjectionInput = filepath.Join("ui-future", "host", "vite.config.ts")

// devBandVariant — вход с одним изменённым фактом и ожидаемый исход.
type devBandVariant struct {
	name     string
	src      string
	defects  int  // сколько находок обязано быть
	parseErr bool // разбор обязан ОТКАЗАТЬ
	mentions []string
}

// splice — замена отрезка [span) текста.
func splice(src string, span [2]int, with string) string {
	return src[:span[0]] + with + src[span[1]:]
}

func TestDevCeremonyBandGate_ProvenByInjection(t *testing.T) {
	root := repoRootFromTest(t)
	raw, err := os.ReadFile(filepath.Join(root, devBandInjectionInput)) // #nosec G304 -- путь выписан выше, внутри дерева
	if err != nil {
		t.Fatalf("вход инъекции %s не читается: %v", devBandInjectionInput, err)
	}
	src := string(raw)
	base, err := readDevCeremonyBand(src)
	if err != nil {
		t.Fatalf("настоящий вход %s не разобран: %v", devBandInjectionInput, err)
	}
	if !base.declared {
		t.Fatalf("%s не объявляет полосу экранов входа — пробе нечего вносить. Полоса снята из сборщика: "+
			"снимите эту пробу вместе с TestDevCeremonyBandIsMountedOnlyWhereItsAddressIsDeclared тем же изменением",
			devBandInjectionInput)
	}

	call := src[base.callSpan[0]:base.callSpan[1]]
	mount := func(form devBandForm) string {
		switch form {
		case devBandTernary:
			return "(" + base.target + "\n        ? " + call + "\n        : {})"
		case devBandAnd:
			return "(" + base.target + " && " + call + ")"
		}
		return call
	}
	// withForm / withInit — законный близнец с одним изменённым фактом.
	// Инициализатор правится первым: он стоит в файле выше монтирования, и
	// позиции монтирования после его правки сдвигаются, а не наоборот.
	build := func(init string, form devBandForm) string {
		out := splice(src, base.mountSpan, mount(form))
		return splice(out, base.initSpan, init)
	}
	if base.initSpan[1] > base.mountSpan[0] {
		t.Fatalf("адресат объявлен НИЖЕ монтирования — порядок правок пробы неверен для этого входа")
	}
	lawfulInit := "process.env." + base.knob
	defaultInit := lawfulInit + ` || "http://127.0.0.1:9"`
	// Комментарий, называющий дефектную форму, стоит перед объявлением
	// адресата: читателем он не является.
	declLineStart := strings.LastIndex(src[:base.initSpan[0]], "\n") + 1
	commented := splice(build(lawfulInit, devBandTernary), [2]int{declLineStart, declLineStart},
		"// const "+base.target+" = "+defaultInit+"; ...Object.fromEntries(…)\n")

	variants := []devBandVariant{
		{name: "законный близнец: условие `? … : {}`, умолчания нет", src: build(lawfulInit, devBandTernary)},
		{name: "законный близнец: условие `&&`, умолчания нет", src: build(lawfulInit, devBandAnd)},
		{name: "законный близнец: пустое умолчание `?? \"\"`", src: build(lawfulInit+` ?? ""`, devBandTernary)},
		{name: "законный близнец: дефектная форма названа в комментарии", src: commented},
		{
			name: "дефект: непустое умолчание адреса", src: build(defaultInit, devBandTernary), defects: 1,
			mentions: []string{`"http://127.0.0.1:9"`, "умолчание"},
		},
		{
			name: "дефект: непустое умолчание через `??`", src: build(lawfulInit+` ?? "http://127.0.0.1:9"`, devBandTernary),
			defects: 1, mentions: []string{"умолчание"},
		},
		{
			name: "дефект: монтирование без условия", src: build(lawfulInit, devBandBare), defects: 1,
			mentions: []string{"БЕЗУСЛОВНО"},
		},
		{
			name: "дефект: прежняя форма дерева — умолчание И безусловно", src: build(defaultInit, devBandBare), defects: 2,
			mentions: []string{"умолчание", "БЕЗУСЛОВНО"},
		},
		{
			name:     "непонятая форма: адресат не из переменной окружения",
			src:      build("resolveUpstream()", devBandTernary),
			parseErr: true,
		},
		{
			name:     "непонятая форма: вторая ветвь условия не пуста",
			src:      splice(src, base.mountSpan, "("+base.target+" ? "+call+" : "+call+")"),
			parseErr: true,
		},
	}

	for _, v := range variants {
		b, perr := readDevCeremonyBand(v.src)
		switch {
		case v.parseErr:
			if perr == nil {
				t.Errorf("%s: разбор ПРИНЯЛ форму, которой не понимает (прочитано: %s) — непонятое обязано краснеть", v.name, b.form)
			}
			continue
		case perr != nil:
			t.Errorf("%s: разбор отказал на законной форме: %v", v.name, perr)
			continue
		case !b.declared:
			t.Errorf("%s: полоса не найдена во входе, где она есть", v.name)
			continue
		}
		got := b.defects(devBandInjectionInput)
		if len(got) != v.defects {
			t.Errorf("%s: находок %d, ожидалось %d: %v", v.name, len(got), v.defects, got)
			continue
		}
		joined := strings.Join(got, "\n")
		for _, m := range v.mentions {
			if !strings.Contains(joined, m) {
				t.Errorf("%s: находка не называет %q — причина вместо симптома не сказана: %s", v.name, m, joined)
			}
		}
		if v.defects > 0 && !strings.Contains(joined, devBandInjectionInput+":") {
			t.Errorf("%s: находка без координаты файла и строки: %s", v.name, joined)
		}
	}

	// Полосы нет вовсе — не находка и не отказ, а «не объявлена».
	none, err := readDevCeremonyBand("export default {};\n")
	if err != nil || none.declared {
		t.Errorf("конфигурация без полосы: declared=%v err=%v — ожидалось «не объявлена» без отказа", none.declared, err)
	}
}

// TestStripTSCommentsKeepsStringsAndLines — снятие комментариев не трогает
// строковых литералов с `//` внутри и не сдвигает номера строк.
func TestStripTSCommentsKeepsStringsAndLines(t *testing.T) {
	in := "const a = \"http://x\"; // хвост\n/* блок\n строки */ const b = 'y//z';\nconst c = `q//w`;\n" +
		"const r = /https?:\\/\\//; const d = e / f; // деление\n"
	got := stripTSComments(in)
	for _, keep := range []string{`"http://x"`, `'y//z'`, "`q//w`", "const b", "const c",
		`/https?:\/\//`, "const d = e / f;"} {
		if !strings.Contains(got, keep) {
			t.Errorf("исполняемая часть %q потеряна: %q", keep, got)
		}
	}
	for _, drop := range []string{"хвост", "блок", "строки */", "деление"} {
		if strings.Contains(got, drop) {
			t.Errorf("комментарий %q остался в исполняемой части: %q", drop, got)
		}
	}
	if strings.Count(got, "\n") != strings.Count(in, "\n") {
		t.Errorf("номера строк сдвинулись: было %d переводов, стало %d", strings.Count(in, "\n"), strings.Count(got, "\n"))
	}
}
