// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// licensesubject_test.go — предмет, объявленный файлом LICENSE, совпадает с
// каталогом, в котором файл лежит.
//
// # Предмет
//
// Параметр `Licensed Work` в BUSL-1.1 определяет ПРЕДМЕТ лицензии, и на него же
// ссылается `Additional Use Grant` в том же файле: права даются «на Licensed
// Work». Значит имя в этой строке — не заголовок и не украшение, а единственное
// место, где сказано, ЧТО именно лицензируется.
//
// В дереве девять таких файлов, и они байт-в-байт равны друг другу, кроме этой
// одной строки (предикат: `sed '12d' <файл> | md5sum` даёт одну сумму на все
// девять). То есть файл заводят копированием соседнего, а правят в нём ровно
// строку предмета — либо забывают её править. Забытая правка не даёт ни
// конфликта слияния, ни красного в сборке: файл валиден, лицензия настоящая,
// неверен только предмет.
//
// Так и вышло при переезде из полирепо (2026-07-15): корневой LICENSE — тот,
// который читает КАЖДЫЙ клонирующий публичный репозиторий, — объявлял предметом
// один сервис, а два сервисных называли третий, чужой (kacho#1761).
//
// # Предикат: парная скобка, а не проза
//
// Гейт судит АSCII-идентификатор в скобках (`(kacho-vpc)`) и сравнивает его на
// РАВЕНСТВО с выведенным из пути. Идентификатор — машинная половина строки: он
// и есть дискриминатор компонента (naming convention `kacho-<part>`), он не
// зависит от языка и не переносится по ширине.
//
// Чего гейт НЕ судит, названо прямо, чтобы границу не приняли за покрытие:
// человекочитаемое имя перед скобкой («Kachō Network Load Balancer»). Судить
// его пришлось бы картой «идентификатор → отображаемое имя» — вторым местом об
// одном предмете, которое разойдётся с деревом молча; а сокращения (`nlb` при
// «Network Load Balancer», `geo` при «Geography») сделали бы карту
// обязательной. Наблюдавшийся класс — копирование файла ЦЕЛИКОМ, где проза и
// идентификатор едут вместе, — идентификатором ловится полностью.
//
// # Откуда берётся ожидаемый предмет
//
// Из пути, механически, без карты:
//
//	LICENSE                   → kacho                (репозиторий целиком)
//	services/<svc>/LICENSE    → kacho-<svc>
//	<каталог из псевдонимов>  → объявленный там идентификатор
//
// Псевдоним нужен ровно там, где имя каталога и идентификатор компонента
// расходятся: `gateway/` несёт `kacho-api-gateway`. Новый сервис под
// `services/` карты НЕ требует и гейта не трогает — правило выводит его предмет
// само.
//
// LICENSE в месте, которого правило не знает (скажем, `pkg/` или `proto/`, чей
// предмет решает kacho#1103), — НАХОДКА, а не пропуск. Иначе первый же файл в
// новом месте выпал бы из наблюдения молча: ровно тот класс, ради которого гейт
// и написан.
//
// # Перепись
//
// Печатается: записей индекса осмотрено, файлов LICENSE найдено, предмет
// выведен у скольких. Ноль файлов — ОТКАЗ, а не чистота: гейт, чей предмет
// отсутствует, молчит одинаково и когда дерево чисто, и когда обход сломан.
package repohygiene

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// licenseSubjectAliases — каталоги, чьё имя НЕ совпадает с идентификатором
// компонента. Запись здесь заводится только под такое расхождение; каталог,
// чьё имя совпадает, в карте не появляется.
var licenseSubjectAliases = map[string]string{
	"gateway": "kacho-api-gateway",
}

var (
	licensedWorkLineRe = regexp.MustCompile(`^Licensed Work:\s*(\S.*)$`)
	licenseIdentRe     = regexp.MustCompile(`\(([a-z0-9]+(?:-[a-z0-9]+)*)\)`)
)

// licenseSubjectFor — идентификатор, который файл LICENSE по этому пути обязан
// называть. Второй результат false означает «правило этого места не знает» —
// это находка, а не пропуск (см. шапку).
func licenseSubjectFor(rel string) (string, bool) {
	if rel == "LICENSE" {
		return "kacho", true
	}
	dir := path.Dir(rel)
	if alias, ok := licenseSubjectAliases[dir]; ok {
		return alias, true
	}
	if svc, ok := strings.CutPrefix(dir, "services/"); ok && svc != "" && !strings.Contains(svc, "/") {
		return "kacho-" + svc, true
	}
	return "", false
}

type licenseSubjectFinding struct {
	file string
	why  string
}

func (f licenseSubjectFinding) String() string { return f.file + " — " + f.why }

type licenseSubjectCensus struct {
	indexed  int
	licenses int
	derived  int
}

func (c licenseSubjectCensus) String() string {
	return fmt.Sprintf("записей индекса %d, файлов LICENSE %d, предмет выведен у %d",
		c.indexed, c.licenses, c.derived)
}

// scanLicenseSubjects — чистая функция над перечнем путей и читателем, чтобы
// доказательство способности упасть шло по синтетическому корпусу и НЕ писало
// в живое дерево.
func scanLicenseSubjects(paths []string, read func(string) ([]byte, error)) ([]licenseSubjectFinding, licenseSubjectCensus) {
	var findings []licenseSubjectFinding
	census := licenseSubjectCensus{indexed: len(paths)}

	for _, rel := range paths {
		if filepath.Base(rel) != "LICENSE" {
			continue
		}
		census.licenses++

		want, known := licenseSubjectFor(rel)
		if !known {
			findings = append(findings, licenseSubjectFinding{file: rel,
				why: "файл LICENSE в месте, для которого предмет не объявлен: правило выводит его " +
					"для корня и для services/<сервис>, псевдонимы — в licenseSubjectAliases. " +
					"Объявите предмет либо снимите файл — молча такой файл не наблюдается ничем"})
			continue
		}
		census.derived++

		body, err := read(rel)
		if err != nil {
			findings = append(findings, licenseSubjectFinding{file: rel,
				why: "не прочитан: " + err.Error()})
			continue
		}

		line, ok := licensedWorkLine(string(body))
		if !ok {
			findings = append(findings, licenseSubjectFinding{file: rel,
				why: "нет строки `Licensed Work:` — предмет лицензии не объявлен вовсе"})
			continue
		}
		m := licenseIdentRe.FindStringSubmatch(line)
		if m == nil {
			findings = append(findings, licenseSubjectFinding{file: rel,
				why: fmt.Sprintf("в строке предмета нет идентификатора в скобках (ожидался `(%s)`): %q", want, line)})
			continue
		}
		if got := m[1]; got != want {
			findings = append(findings, licenseSubjectFinding{file: rel,
				why: fmt.Sprintf("предмет лицензии — `%s`, а файл лежит в каталоге компонента `%s`. "+
					"Строка: %q", got, want, line)})
		}
	}

	sort.Slice(findings, func(i, j int) bool { return findings[i].file < findings[j].file })
	return findings, census
}

func licensedWorkLine(body string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		if m := licensedWorkLineRe.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
			return strings.TrimSpace(m[1]), true
		}
	}
	return "", false
}

func TestLicenseSubjectMatchesItsDirectory(t *testing.T) {
	root := repoRoot(t)

	var paths []string
	for _, line := range gitLsFiles(t, root) {
		if _, rel, ok := parseLsFiles(line); ok {
			paths = append(paths, rel)
		}
	}

	findings, census := scanLicenseSubjects(paths, func(rel string) ([]byte, error) {
		return os.ReadFile(filepath.Join(root, rel))
	})

	t.Logf("осмотрено: %s; расхождений %d", census, len(findings))

	if census.indexed == 0 {
		t.Fatal("индекс git пуст — обход не дошёл ни до одного файла. Это отказ, а не чистота")
	}
	if census.licenses == 0 {
		t.Fatal("в индексе нет НИ ОДНОГО файла LICENSE: предикат разошёлся с деревом. " +
			"Это отказ, а не чистота")
	}

	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("\n  " + f.String())
		}
		t.Errorf("%d файл(ов) LICENSE объявляют предметом не свой компонент:%s", len(findings), b.String())
	}
}
