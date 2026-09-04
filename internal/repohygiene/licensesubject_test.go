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
// # Откуда берётся ожидаемое
//
// Из ОДНОГО отображения путь→лицензия (licensemap.go), которое читает и гейт
// заголовков, — второго места об этом предмете в дереве нет:
//
//	LICENSE                   → BUSL-1.1, предмет `kacho`
//	services/<svc>/LICENSE    → BUSL-1.1, предмет `kacho-<svc>`
//	<каталог из псевдонимов>  → BUSL-1.1, объявленный там идентификатор
//	корень уровня             → лицензия уровня (pkg/ и proto/ — Apache-2.0,
//	                            services/iam/ — AGPL-3.0-or-later)
//
// Псевдоним нужен ровно там, где имя каталога и идентификатор компонента
// расходятся: `gateway/` несёт `kacho-api-gateway`. Новый сервис под
// `services/` карты НЕ требует и гейта не трогает — правило выводит его предмет
// само.
//
// LICENSE в месте, которого отображение не знает (скажем, `pkg/internal/`), —
// НАХОДКА, а не пропуск. Иначе первый же файл в новом месте выпал бы из
// наблюдения молча: ровно тот класс, ради которого гейт и написан.
//
// # Предмет есть НЕ у всякой лицензии, и требовать его от всех нельзя
//
// Параметр `Licensed Work:` — форма BUSL-1.1. У Apache-2.0 и AGPL-3.0 такого
// параметра нет ВОВСЕ: их текст неизменен и предмета в себе не называет.
// Поэтому у не-BUSL уровней гейт судит другое — что файл СОДЕРЖИТ текст той
// лицензии, которую объявляет уровень. Без этой половины `pkg/LICENSE` с телом
// BUSL прошёл бы молча: он валиден для всякого читателя, кроме юриста.
//
// До 2026-09-04 гейт объявлял находкой САМО появление файла лицензии у `pkg/`
// или `proto/` — потому что предмет этих каталогов был не решён. Решение
// владельца его закрыло, и расхождение двух гейтов снято ровно так: оба читают
// одно отображение.
//
// # Перепись
//
// Печатается: записей индекса осмотрено, файлов LICENSE найдено, ожидание
// выведено у скольких, и из них — сколько формы BUSL (у которых есть предмет).
// Ноль файлов — ОТКАЗ, а не чистота: гейт, чей предмет отсутствует, молчит
// одинаково и когда дерево чисто, и когда обход сломан.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	licensedWorkLineRe = regexp.MustCompile(`^Licensed Work:\s*(\S.*)$`)
	licenseIdentRe     = regexp.MustCompile(`\(([a-z0-9]+(?:-[a-z0-9]+)*)\)`)
)

type licenseSubjectFinding struct {
	file string
	why  string
}

func (f licenseSubjectFinding) String() string { return f.file + " — " + f.why }

type licenseSubjectCensus struct {
	indexed  int
	licenses int
	derived  int
	withSubj int
}

func (c licenseSubjectCensus) String() string {
	return fmt.Sprintf("записей индекса %d, файлов LICENSE %d, ожидание выведено у %d "+
		"(из них форм с предметом %d)", c.indexed, c.licenses, c.derived, c.withSubj)
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

		want, known := licenseFileWantFor(rel)
		if !known {
			findings = append(findings, licenseSubjectFinding{file: rel,
				why: "файл LICENSE в месте, для которого лицензия не объявлена: отображение " +
					"путь→лицензия выводит её для корня каждого уровня и для services/<сервис>, " +
					"псевдонимы — в licenseSubjectAliases. Объявите уровень либо снимите файл — " +
					"молча такой файл не наблюдается ничем"})
			continue
		}
		census.derived++
		if want.Subject != "" {
			census.withSubj++
		}

		body, err := read(rel)
		if err != nil {
			findings = append(findings, licenseSubjectFinding{file: rel,
				why: "не прочитан: " + err.Error()})
			continue
		}
		text := string(body)

		// Первое утверждение: файл СОДЕРЖИТ текст той лицензии, которую ждёт
		// уровень. Оно относится ко всем формам — идентификатор в заголовках
		// файлов уровня ссылается именно на этот текст.
		markers, declared := licenseTextMarkers[want.SPDX]
		if !declared {
			findings = append(findings, licenseSubjectFinding{file: rel,
				why: fmt.Sprintf("уровень %q ждёт лицензию %s, а по чему узнаётся её ТЕЛО — "+
					"не объявлено в licenseTextMarkers", want.Tier, want.SPDX)})
			continue
		}
		if miss := missingMarkers(text, markers); miss != "" {
			findings = append(findings, licenseSubjectFinding{file: rel,
				why: fmt.Sprintf("уровень %q ждёт текст %s, а в теле файла нет %s",
					want.Tier, want.SPDX, miss)})
			continue
		}

		// Второе утверждение — только у формы BUSL: параметра `Licensed Work:`
		// у Apache-2.0 и AGPL-3.0 не бывает вовсе, требовать его от них значило
		// бы требовать строки, которой в лицензии нет.
		if want.Subject == "" {
			continue
		}
		line, ok := licensedWorkLine(text)
		if !ok {
			findings = append(findings, licenseSubjectFinding{file: rel,
				why: "нет строки `Licensed Work:` — предмет лицензии не объявлен вовсе"})
			continue
		}
		m := licenseIdentRe.FindStringSubmatch(line)
		if m == nil {
			findings = append(findings, licenseSubjectFinding{file: rel,
				why: fmt.Sprintf("в строке предмета нет идентификатора в скобках (ожидался `(%s)`): %q",
					want.Subject, line)})
			continue
		}
		if got := m[1]; got != want.Subject {
			findings = append(findings, licenseSubjectFinding{file: rel,
				why: fmt.Sprintf("предмет лицензии — `%s`, а файл лежит в каталоге компонента `%s`. "+
					"Строка: %q", got, want.Subject, line)})
		}
	}

	sort.Slice(findings, func(i, j int) bool { return findings[i].file < findings[j].file })
	return findings, census
}

// missingMarkers — первый маркер тела лицензии, которого в файле нет. Пустая
// строка означает «все на месте».
func missingMarkers(text string, markers []string) string {
	for _, mk := range markers {
		if !strings.Contains(text, mk) {
			return fmt.Sprintf("строки %q", mk)
		}
	}
	return ""
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
	if census.withSubj == 0 {
		t.Fatal("ни у одного файла LICENSE не оказалось формы с предметом: полоса проверки " +
			"предмета не исполнялась вовсе. Это отказ, а не чистота")
	}

	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("\n  " + f.String())
		}
		t.Errorf("%d файл(ов) LICENSE расходятся со своим уровнем:%s", len(findings), b.String())
	}
}
