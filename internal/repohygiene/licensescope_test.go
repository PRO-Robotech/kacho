// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// licensescope_test.go — ОБЛАСТЬ, которую файл лицензии объявляет своей, и её
// отношение к соседним файлам лицензии в том же дереве.
//
// # Предмет: не какая лицензия, а НА ЧТО она
//
// Соседний держатель [TestLicenseSubjectMatchesItsDirectory] судит ПРЕДМЕТ —
// идентификатор в скобках строки `Licensed Work:` — и совпадение тела файла с
// уровнем. Оба его утверждения на этом дереве зелены. Область действия текста
// под них не подпадает НИ ОДНИМ утверждением, и потому расхождение прожило от
// переезда из полирепо до kacho#2161 при зелёном гейте рядом.
//
// Параметр `Licensed Work` в BUSL-1.1 определяет предмет лицензии ЦЕЛИКОМ —
// и чем он назван, и насколько далеко простирается. Вторая половина и есть
// область: «Kachō (kacho) and all … materials in this repository» объявляет
// своей территорией ВЕСЬ репозиторий, включая каталоги, лежащие под другой
// лицензией.
//
// # Почему это не педантизм
//
// Обычное соглашение «файл в подкаталоге главнее корневого» здесь не спасает:
// корневой текст говорит ЯВНО «all … in this repository», а явное заявление
// сильнее умолчания. Получатель, прочитавший корневой файл, заключит, что
// фундамент `pkg/` под BUSL. Фундамент же — Apache-2.0 намеренно: вынесенная
// служба под AGPL-3.0 линкует его, а §10 AGPL запрещает налагать на получателя
// дополнительные ограничения. Оба репозитория публичны, то есть текст уже
// роздан.
//
// # Класс восьмикратен, а не однократен
//
// Замер на release/kaname@f16f68e305: файлов лицензии в дереве 12, объявляли
// своей областью весь репозиторий — ВОСЕМЬ. Не только корневой: каждый
// компонентный файл заведён копированием соседнего, и вместе с телом переехала
// фраза об области. То есть `services/vpc/LICENSE` — самостоятельный документ,
// который получатель вправе прочесть отдельно, — объявлял своей территорией и
// `pkg/`, и `services/iam/`.
//
// # Что судит гейт: ОТНОШЕНИЕ текста к дереву, а не красоту формулировки
//
// Для каждого файла с параметром области:
//
//	территория  ← якорь области, объявленный в тексте (закрытый набор форм)
//	поглощаемые ← каталоги ВНУТРИ территории, несущие СВОЙ файл лицензии
//	оговорка    ← уступает ли текст этим каталогам (закрытый набор форм)
//
// Непустое множество поглощаемых без оговорки — находка, и она называет
// поглощённые каталоги поимённо. Это отношение к дереву, а не совпадение
// подстроки: тот же текст в дереве без соседних лицензий находкой не является,
// и наоборот — появление нового файла лицензии делает находкой текст, который
// вчера был верен.
//
// # Неизвестная форма — НАХОДКА, а не пропуск
//
// Якорь области ищется по закрытому набору форм, и незнакомая формулировка
// объявляется находкой, а не проходит молча. Иначе первая же правка текста
// вывела бы файл из наблюдения — ровно тот класс, ради которого гейт написан:
// распознаватель, не знающий формы, не даёт ни красного, ни зелёного.
//
// Оговорка узнаётся ПАРОЙ: отрицание (`except` / `excluding` / `other than`) И
// названный механизм (`own LICENSE file`). Порознь ни то, ни другое оговоркой
// не является: слово `except` стоит в теле BUSL и без всякой связи с областью,
// а «own LICENSE file» без отрицания способно и расширять.
//
// # Область поиска — АБЗАЦ ПАРАМЕТРА, а не файл целиком
//
// Слово `License` встречается в теле BUSL десятки раз («You must conspicuously
// display this License…»), поэтому поиск по всему файлу дал бы оговорку там,
// где её нет. Читается блок от строки `Licensed Work:` до следующего параметра
// — то есть ровно та часть документа, которая область и определяет.
//
// # Почему суждение лежит в тестовом файле
//
// Распознаватель строки `Licensed Work:` заведён СОСЕДНИМ держателем
// (licensesubject_test.go) и живёт в тестовом файле. Завести второй такой же
// ради не-тестового файла значило бы держать два места об одном предмете:
// разошлись бы они молча и разошлись бы именно в распознавателе — то есть
// незаметно для ОБОИХ гейтов сразу. Поэтому суждение лежит рядом с обходом.
//
// Найдено не чтением: `go test` и `go vet` включают тестовые файлы и потому
// собирались, а `go build ./...` — нет. Красное пришло от хука отправки.
//
// # Чего гейт НЕ судит, названо прямо
//
// Юридической верности формулировки. Текст лицензии — правовой инструмент, и
// его редакция есть решение владельца, а не вывод проверки. Гейт держит
// структурную половину: заявленная область не вправе накрывать каталог, у
// которого своя лицензия, — и молчание гейта означает ровно это, не шире.
package repohygiene

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// licenseScopeAnchor — объявленная форма, которой текст называет свою
// территорию. Набор ЗАКРЫТ: незнакомая форма — находка (см. шапку).
type licenseScopeAnchor struct {
	// Marker — форма в тексте, по которой якорь узнаётся (в нижнем регистре).
	Marker string
	// WholeRepo — территорией объявлен весь репозиторий, а не каталог файла.
	WholeRepo bool
}

var licenseScopeAnchors = []licenseScopeAnchor{
	{Marker: "in this repository", WholeRepo: true},
	{Marker: "in the directory that contains this license file", WholeRepo: false},
}

// licenseScopeYieldNegations / licenseScopeYieldMechanism — половины оговорки.
// Требуются ОБЕ: порознь каждая встречается в тексте без отношения к области.
var licenseScopeYieldNegations = []string{"except", "excluding", "other than"}

const licenseScopeYieldMechanism = "own license file"

// licenseScopeFinding — файл, чья объявленная область накрывает чужую лицензию.
type licenseScopeFinding struct {
	file string
	why  string
}

func (f licenseScopeFinding) String() string { return f.file + " — " + f.why }

type licenseScopeCensus struct {
	// indexed — записей индекса, поданных на обход.
	indexed int
	// licenses — файлов LICENSE найдено.
	licenses int
	// withScope — из них несущих параметр области (форма BUSL). У Apache-2.0 и
	// AGPL-3.0 такого параметра нет ВОВСЕ, судить у них нечего.
	withScope int
	// anchored — область распознана; unanchored — форма якоря незнакома.
	anchored   int
	unanchored int
	// yielding — из распознанных несут оговорку о вложенных лицензиях.
	yielding int
	// swallowed — пар «файл поглощает чужой каталог» насчитано всего.
	swallowed int
}

func (c licenseScopeCensus) String() string {
	return fmt.Sprintf("записей индекса %d, файлов LICENSE %d "+
		"(из них с параметром области %d; у Apache-2.0 и AGPL-3.0 его нет вовсе), "+
		"якорь распознан у %d, не распознан у %d, оговорку несут %d, "+
		"поглощений насчитано %d",
		c.indexed, c.licenses, c.withScope, c.anchored, c.unanchored, c.yielding, c.swallowed)
}

// licensedWorkBlock — абзац параметра `Licensed Work:`: от его строки до
// следующего параметра. Именно он определяет область, и только он читается.
func licensedWorkBlock(body string) (string, bool) {
	var b strings.Builder
	inside := false
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimRight(raw, "\r")
		if !inside {
			if licensedWorkLineRe.MatchString(line) {
				inside = true
				b.WriteString(line)
				b.WriteByte('\n')
			}
			continue
		}
		// Следующий параметр начинается со столбца 0 и несёт двоеточие.
		if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") &&
			strings.Contains(line, ":") {
			break
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String(), inside
}

// licenseScopeAnchorOf — какую территорию объявляет блок. Второй результат
// false означает «форма якоря не распознана» — находка, а не пропуск.
func licenseScopeAnchorOf(block string) (licenseScopeAnchor, bool) {
	low := strings.ToLower(strings.Join(strings.Fields(block), " "))
	for _, a := range licenseScopeAnchors {
		if strings.Contains(low, a.Marker) {
			return a, true
		}
	}
	return licenseScopeAnchor{}, false
}

// licenseScopeYields — уступает ли блок вложенным каталогам со своей лицензией.
func licenseScopeYields(block string) bool {
	low := strings.ToLower(strings.Join(strings.Fields(block), " "))
	if !strings.Contains(low, licenseScopeYieldMechanism) {
		return false
	}
	for _, neg := range licenseScopeYieldNegations {
		if strings.Contains(low, neg) {
			return true
		}
	}
	return false
}

// withinTerritory — лежит ли каталог dir внутри территории с корнем root,
// НЕ совпадая с ним. Корень "." означает весь репозиторий.
func withinTerritory(root, dir string) bool {
	if dir == root {
		return false
	}
	if root == "." {
		return true
	}
	return strings.HasPrefix(dir, root+"/")
}

// scanLicenseScopes — чистая функция над перечнем путей и читателем: обход и
// суждение разделены, поэтому доказательство идёт по синтетическому корпусу и
// не зависит от того, что сегодня лежит в дереве.
func scanLicenseScopes(paths []string, read func(string) ([]byte, error)) ([]licenseScopeFinding, licenseScopeCensus) {
	census := licenseScopeCensus{indexed: len(paths)}

	// Каталоги, несущие СВОЙ файл лицензии, — то, чему всякая объемлющая
	// область обязана уступить. Выводятся из того же обхода, а не выписываются.
	var licenseFiles []string
	ownLicense := map[string]bool{}
	for _, rel := range paths {
		if path.Base(rel) != "LICENSE" {
			continue
		}
		census.licenses++
		licenseFiles = append(licenseFiles, rel)
		ownLicense[path.Dir(rel)] = true
	}
	sort.Strings(licenseFiles)

	var findings []licenseScopeFinding
	for _, rel := range licenseFiles {
		body, err := read(rel)
		if err != nil {
			findings = append(findings, licenseScopeFinding{file: rel,
				why: "не прочитан: " + err.Error()})
			continue
		}
		block, ok := licensedWorkBlock(string(body))
		if !ok {
			// Формы без параметра области (Apache-2.0, AGPL-3.0, копия чужой
			// лицензии): область в них не объявляется вовсе, судить нечего.
			continue
		}
		census.withScope++

		anchor, known := licenseScopeAnchorOf(block)
		if !known {
			census.unanchored++
			findings = append(findings, licenseScopeFinding{file: rel,
				why: "область действия объявлена формой, которой распознаватель не знает: " +
					"добавьте её в licenseScopeAnchors вместе с осью инъекции — иначе файл " +
					"выпадет из наблюдения молча. Абзац: " + oneLine(block)})
			continue
		}
		census.anchored++

		yields := licenseScopeYields(block)
		if yields {
			census.yielding++
		}

		root := path.Dir(rel)
		if anchor.WholeRepo {
			root = "."
		}

		// Собственный каталог файла из поглощаемых исключён: он и управляется
		// этим самым файлом, конфликта тут нет by construction.
		own := path.Dir(rel)
		var swallowed []string
		for dir := range ownLicense {
			if dir != own && withinTerritory(root, dir) {
				swallowed = append(swallowed, dir)
			}
		}
		sort.Strings(swallowed)
		census.swallowed += len(swallowed)

		if len(swallowed) == 0 || yields {
			continue
		}
		findings = append(findings, licenseScopeFinding{file: rel,
			why: fmt.Sprintf("объявляет своей областью %s и не уступает вложенным каталогам, "+
				"у которых СВОЙ файл лицензии: %s. Получатель, прочитавший этот файл, "+
				"заключит, что и они под ним. Назовите исключение в абзаце `Licensed Work:` "+
				"(отрицание + «own LICENSE file»)",
				territoryName(root), strings.Join(swallowed, ", "))})
	}

	sort.Slice(findings, func(i, j int) bool { return findings[i].file < findings[j].file })
	return findings, census
}

func territoryName(root string) string {
	if root == "." {
		return "ВЕСЬ репозиторий"
	}
	return "каталог " + root + " и всё внутри него"
}

// oneLine — абзац одной строкой, чтобы находка читалась в выводе прогона.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func TestNoLicenseClaimsTerritoryGovernedByAnother(t *testing.T) {
	root := repoRoot(t)

	var paths []string
	for _, line := range gitLsFiles(t, root) {
		if _, rel, ok := parseLsFiles(line); ok {
			paths = append(paths, rel)
		}
	}

	read := func(rel string) ([]byte, error) {
		return os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	}

	findings, census := scanLicenseScopes(paths, read)

	t.Logf("осмотрено: %s; расхождений %d", census, len(findings))

	if census.indexed == 0 {
		t.Fatal("индекс git пуст — обход не дошёл ни до одного файла. Это отказ, а не чистота")
	}
	if census.licenses == 0 {
		t.Fatal("в индексе нет НИ ОДНОГО файла LICENSE: предикат разошёлся с деревом. " +
			"Это отказ, а не чистота")
	}
	if census.withScope == 0 {
		t.Fatal("ни один файл LICENSE не объявляет области: полоса проверки не исполнялась " +
			"вовсе. Это отказ, а не чистота")
	}
	if census.swallowed == 0 {
		t.Fatal("ни одна объявленная область не накрывает соседнего файла лицензии — " +
			"значит уступать нечему и утверждение вакуумно. В дереве несколько уровней " +
			"со своими файлами лицензии, поэтому ноль здесь означает сломанный обход, " +
			"а не чистоту")
	}

	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("\n  " + f.String())
		}
		t.Errorf("%d файл(ов) LICENSE объявляют своей областью территорию, у которой "+
			"своя лицензия:%s", len(findings), b.String())
	}
}
