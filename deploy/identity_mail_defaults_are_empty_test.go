// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_mail_defaults_are_empty_test.go — MAIL-12 приёмки ID-MAIL-1.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// У почтовых величин службы личности ВСТРОЕННОГО УМОЛЧАНИЯ НЕТ (решение Р3), и
// это не аккуратность, а условие, при котором оба стража вообще что-то значат:
// величина, которую построение подставляет молча, предметом стража быть не
// может — он зелен при ЛЮБОМ входе, потому что незаданной величина не бывает.
//
// Цена измерена, а не предположена. Здесь стояло
// `smtp://<узел>:1025/?disable_starttls=true`, и в одной строке было ДВА ложных
// утверждения о дереве: узла не поднимал ни один манифест поставки, а полоса до
// него объявлялась незашифрованной. Профиль, ничего о почте не сказавший, молча
// получал координату несуществующего узла и стартовал: письма уходили в никуда,
// и сигнала не было ни одного.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО СУДИТСЯ — СЛОИ УМОЛЧАНИЙ, А НЕ ПРОФИЛИ
//
// Умолчание — это то, что применяется, когда профиль промолчал: базовые значения
// зонтичного чарта и значения подчартов. ПРОФИЛЬ объявлять почтовые величины
// обязан и делает это законно (стенд объявляет узел-приёмник), поэтому обход
// профилей дал бы гейт, красный на исправном дереве, — а такой снимают первым.
//
// Величин ПЯТЬ, и пятая заведена вместе с выносом удостоверения в секрет
// (решение Р6): источник удостоверения — тоже почтовая величина, и умолчания у
// него нет по той же причине, что у остальных четырёх.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧИТАЕТСЯ ЛИСТ ДЕРЕВА ЗНАЧЕНИЙ, А НЕ ТЕКСТ ФАЙЛА
//
// Те же слова стоят в прозе рядом (разбор снятого умолчания занимает в
// `values.yaml` два десятка строк) и в самом страже. Предикат по подстроке
// краснел бы на собственном объяснении — ровно класс `testing.md` §«Гейт на
// класс», п. 4.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ГЕЙТ НЕ УТВЕРЖДАЕТ (граница названа, чтобы её не приняли шире)
//
//   - что профиль объявил почтовые величины — это предмет стража рендера (С1) и
//     шага подстановки (С2), у каждого своя досягаемость;
//   - что величина, доехавшая до пода слоем учётных данных вне git, годна —
//     дерево её не видит by construction.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// mailDefaultKnobs — почтовые величины под `global.kacho.identity.smtp`, у
// каждой путь ОТ этого узла. Перечень выписан, и это осознанно: он есть
// утверждение о контракте ручек, а не замер дерева. Ручка, заведённая мимо
// него, останется вне наблюдения — поэтому §12 DoD требует, чтобы новая
// почтовая величина правила и этот перечень.
var mailDefaultKnobs = [][]string{
	{"connectionURI"},
	{"fromAddress"},
	{"fromName"},
	{"credentialSecret", "name"},
	{"credentialSecret", "key"},
}

// mailDefaultFinding — непустая встроенная величина: файл, путь, значение.
type mailDefaultFinding struct {
	file  string
	knob  string
	value string
}

// mailDefaultCensus — объём осмотренного. Печатается всегда: без него «ноль
// находок» неотличимо от «ноль прочитанного».
type mailDefaultCensus struct {
	layersRead int // слоёв умолчаний прочитано
	nodesFound int // из них объявляют узел почтовой полосы
	knobsSeen  int // почтовых величин осмотрено
}

// defaultLayerFiles — слои умолчаний дерева: базовые значения зонтичного чарта
// и значения КАЖДОГО подчарта. Перечень ВЫВОДИТСЯ обходом, а не выписывается:
// подчарт, заведённый завтра, попадёт под наблюдение сам.
func defaultLayerFiles(t *testing.T) []string {
	t.Helper()
	out := []string{filepath.Join(umbrellaDir, "values.yaml")}
	sub, err := filepath.Glob(filepath.Join(umbrellaDir, "charts", "*", "values.yaml"))
	if err != nil {
		t.Fatalf("обход значений подчартов: %v", err)
	}
	sort.Strings(sub)
	out = append(out, sub...)
	return out
}

// mailSmtpNode — узел `global.kacho.identity.smtp` документа, либо nil.
func mailSmtpNode(doc map[string]any) map[string]any {
	node := any(doc)
	for _, k := range []string{"global", "kacho", "identity", "smtp"} {
		m, ok := node.(map[string]any)
		if !ok {
			return nil
		}
		node, ok = m[k]
		if !ok {
			return nil
		}
	}
	m, _ := node.(map[string]any)
	return m
}

// leafString — лист по пути внутри узла: значение и признак «нашли».
func leafString(node map[string]any, path []string) (string, bool) {
	cur := any(node)
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		cur, ok = m[k]
		if !ok {
			return "", false
		}
	}
	s, ok := cur.(string)
	if !ok {
		// Не-строка (число, отображение) величиной полосы быть не может, но
		// ПУСТОЙ она тоже не является: молча пропустить её значило бы завести
		// слепую зону там, где гейт объявляет наблюдение.
		return fmt.Sprintf("%v", cur), true
	}
	return s, true
}

// ─────────────────────────────────────────────────────────────────────────────
// ЯДРО — чистая функция над разобранными слоями. Обе стороны утверждения
// проверяются на ней же, поэтому положительный контроль не требует дерева.

func scanMailDefaults(layers map[string]map[string]any) ([]mailDefaultFinding, mailDefaultCensus) {
	var findings []mailDefaultFinding
	c := mailDefaultCensus{layersRead: len(layers)}

	files := make([]string, 0, len(layers))
	for f := range layers {
		files = append(files, f)
	}
	sort.Strings(files)

	for _, f := range files {
		node := mailSmtpNode(layers[f])
		if node == nil {
			continue
		}
		c.nodesFound++
		for _, knob := range mailDefaultKnobs {
			v, ok := leafString(node, knob)
			if !ok {
				continue
			}
			c.knobsSeen++
			// Вырожденное считается НЕЗАДАННЫМ тем же предикатом, что у стража
			// рендера и у шага подстановки: пустая строка, пробелы, одинокая
			// запятая. Разные предикаты у трёх мест разошлись бы ровно там, где
			// расхождение опасно.
			if trimmed := strings.Trim(strings.TrimSpace(v), ","); trimmed != "" {
				findings = append(findings, mailDefaultFinding{
					file:  f,
					knob:  strings.Join(knob, "."),
					value: v,
				})
			}
		}
	}
	return findings, c
}

// ─────────────────────────────────────────────────────────────────────────────

func TestMailDefaultsAreEmpty(t *testing.T) {
	layers := map[string]map[string]any{}
	for _, f := range defaultLayerFiles(t) {
		raw, err := os.ReadFile(f) // #nosec G304 -- путь получен обходом собственного дерева
		if err != nil {
			t.Fatalf("чтение слоя умолчаний %s: %v", f, err)
		}
		var doc map[string]any
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("разбор слоя умолчаний %s: %v", f, err)
		}
		layers[f] = doc
	}

	findings, c := scanMailDefaults(layers)
	t.Logf("перепись: слоёв умолчаний прочитано %d · объявляют почтовую полосу %d · величин осмотрено %d · непустых %d",
		c.layersRead, c.nodesFound, c.knobsSeen, len(findings))

	if c.layersRead == 0 {
		t.Fatal("прочитано ноль слоёв умолчаний — «нарушений нет» означало бы «ничего не прочитано»")
	}
	if c.nodesFound == 0 {
		t.Fatal("ни один слой умолчаний не объявляет `global.kacho.identity.smtp` — " +
			"обход ослеп либо узел переименован; в обоих случаях этот гейт больше ничего не стережёт")
	}
	if c.knobsSeen == 0 {
		t.Fatal("осмотрено ноль почтовых величин при найденном узле — перечень ручек пережил свой предмет")
	}

	for _, f := range findings {
		t.Errorf("%s: `global.kacho.identity.smtp.%s` несёт ВСТРОЕННОЕ умолчание %q.\n"+
			"Умолчания у почтовых величин нет намеренно (решение Р3 приёмки ID-MAIL-1): величина,\n"+
			"которую построение подставляет молча, предметом стража быть не может — он зелен при\n"+
			"любом входе, потому что незаданной величина не бывает. Задавать эти величины обязан\n"+
			"ПРОФИЛЬ, поднимающий службу личности, а не слой умолчаний.", f.file, f.knob, f.value)
	}
}
