// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package artifactgates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// headerCensus — объём осмотренного, по осям порознь. Одно суммарное число
// скрыло бы ровно тот случай, ради которого гейт заведён.
type headerCensus struct {
	generators int
	withShared int
	usageLines int
}

var (
	// reForeignGeneratorPath — координата ЧУЖОГО генератора: путь, оканчивающийся
	// на `.../scripts/gen.py`, с сегментом набора перед ним.
	reForeignGeneratorPath = regexp.MustCompile(`[\w./-]*(?:services/\w+|kacho-\w+|gateway)/tests/newman/scripts/gen\.py`)
	// reUsageModule — строка примера вызова: `python3 scripts/gen.py <модуль>`.
	// Пример без имени модуля (только флаг либо ничего) предметом не является.
	reUsageModule = regexp.MustCompile(`python3\s+scripts/gen\.py\s+([A-Za-z][\w.-]*)`)
	// reSharedLayer — упоминание общего слоя: имя модуля либо его каталог.
	reSharedLayer = regexp.MustCompile(`gen_shared|tests/newman/kacholib`)
)

// moduleDocstring — текст шапки модуля Python: первый тройной литерал файла.
//
// Разбор ГРУБЫЙ намеренно: гейт судит ШАПКУ, и брать её как «до первой строки
// кода» достаточно. Ошибка в эту сторону расширяет осмотренное, а не сужает его.
func moduleDocstring(src string) string {
	i := strings.Index(src, `"""`)
	if i < 0 {
		return ""
	}
	rest := src[i+3:]
	j := strings.Index(rest, `"""`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// auditGeneratorHeaders — судящая функция гейта.
//
// Выделена, чтобы инъекция гоняла ЕЁ, а не свою копию.
//
// `modules` — по каждому набору перечень имён его модулей кейсов: пример вызова
// в шапке обязан называть модуль, который у набора ЕСТЬ.
func auditGeneratorHeaders(headers map[string]string, modules map[string]map[string]bool) ([]string, headerCensus) {
	cen := headerCensus{generators: len(headers)}

	rels := make([]string, 0, len(headers))
	for rel := range headers {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var findings []string
	for _, rel := range rels {
		doc := headers[rel]

		if reSharedLayer.MatchString(doc) {
			cen.withShared++
		} else {
			findings = append(findings, fmt.Sprintf(
				"%s — шапка не называет общий слой: читатель не узнает, где живёт форма коллекции", rel))
		}

		own := "/" + strings.TrimSuffix(rel, "/scripts/gen.py") + "/scripts/gen.py"
		for _, m := range reForeignGeneratorPath.FindAllString(doc, -1) {
			if strings.HasSuffix(own, "/"+strings.TrimPrefix(m, "../")) || strings.HasSuffix("/"+rel, "/"+m) {
				continue
			}
			findings = append(findings, fmt.Sprintf(
				"%s — шапка называет ОБРАЗЦОМ чужой генератор (%s): сверяться надо с общим слоем", rel, m))
		}

		for _, m := range reUsageModule.FindAllStringSubmatch(doc, -1) {
			name := m[1]
			if strings.HasPrefix(name, "--") {
				continue
			}
			cen.usageLines++
			if modules[rel] == nil {
				continue
			}
			if !modules[rel][name] {
				findings = append(findings, fmt.Sprintf(
					"%s — пример вызова называет модуль %q, которого у набора НЕТ", rel, name))
			}
		}
	}
	return findings, cen
}

// Шапка генератора newman описывает СВОЙ набор и общий слой, а не соседа.
//
// ПРЕДМЕТ. Общий слой генератора сведён к одному экземпляру (#1367…#1474), но
// шапки продолжали объявлять себя «почти зеркалом» соседнего сервиса — то есть
// описывали раскладку, которой больше нет. Два места об одном предмете, из
// которых верно одно: читатель, поверив шапке, пойдёт сверять свой генератор с
// чужим вместо общего слоя, и вернёт форк, ради снятия которого всё и делалось.
//
// ЦЕНА ИЗМЕРЕНА, А НЕ ПРЕДПОЛОЖЕНА. Предикат задачи (#1418) искал одну фразу и
// находил два места. Перепись по СВОЙСТВУ нашла больше: два набора называли
// образцом чужой генератор той самой фразой, два — координатой репозитория,
// которого в дереве нет вовсе, один — соседний набор по имени. И один случай
// сильнее прочих: шапка registry была скопирована у nlb ЦЕЛИКОМ и называла
// чужой набор в первой же строке, а её пример вызова предлагал модуль,
// которого у registry не существует.
//
// ПОЧЕМУ ГЕЙТ, А НЕ ВНИМАНИЕ. Шапка пишется один раз — при заведении набора,
// копированием соседнего генератора, — и с тех пор её не перечитывает никто,
// кроме того, кто в этот набор пришёл впервые. Она стареет молча и не роняет
// ничего.
//
// ЧЕГО ГЕЙТ НЕ СУДИТ. Он не судит стиль и не требует определённых слов: три
// признака структурны — упомянут ли общий слой, названа ли координата ЧУЖОГО
// генератора, существует ли модуль из примера вызова.
func TestNewmanGeneratorHeaderDescribesItsOwnSuite(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	headers := map[string]string{}
	modules := map[string]map[string]bool{}
	for rel := range tt.files {
		if filepath.Base(rel) != "gen.py" || !strings.Contains(rel, "tests/newman/scripts/") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса git
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		headers[rel] = moduleDocstring(string(b))

		suite := filepath.Dir(filepath.Dir(rel))
		own := map[string]bool{}
		for f := range tt.files {
			if strings.HasPrefix(f, suite+"/cases/") && strings.HasSuffix(f, ".py") {
				own[strings.TrimSuffix(filepath.Base(f), ".py")] = true
			}
		}
		modules[rel] = own
	}
	if len(headers) == 0 {
		t.Fatalf("предпосылка гейта не выполняется: генераторов newman в индексе НОЛЬ — " +
			"чинить надо обход, а не молча выходить успехом.")
	}
	for rel, doc := range headers {
		if strings.TrimSpace(doc) == "" {
			t.Fatalf("%s: шапка модуля не прочитана — распознаватель не знает её формы, "+
				"и «ноль находок» означало бы «ноль прочитанного»", rel)
		}
	}

	findings, cen := auditGeneratorHeaders(headers, modules)

	t.Logf("осмотрено генераторов newman: %d; шапок, называющих общий слой: %d; примеров вызова с именем модуля: %d",
		cen.generators, cen.withShared, cen.usageLines)

	if cen.usageLines == 0 {
		t.Fatalf("предпосылка гейта не выполняется: примеров вызова с именем модуля НОЛЬ.\n" +
			"Либо форма примера изменилась, либо распознаватель её не знает — в обоих\n" +
			"случаях эта половина гейта вечнозелена, и это отказ, а не молчание.")
	}

	if len(findings) > 0 {
		t.Fatalf("шапка генератора описывает не свой набор.\n"+
			"Утверждение, пережившее свой предмет, не роняет ничего и потому живёт годами;\n"+
			"следующий читатель принимает его за действительность:\n  %s",
			strings.Join(findings, "\n  "))
	}
}
