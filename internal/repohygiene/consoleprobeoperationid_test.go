// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consoleprobeoperationid_test.go — гейт «идентификатор, взятый у ОПЕРАЦИИ,
// подтверждается чтением ресурса».
//
// # Предмет
//
// Мутации Kachō возвращают `Operation`, и идентификатор ресурса чеканится в её
// `metadata` ДО асинхронной части. Он приезжает в ответе и тогда, когда та
// отказала: `200` на создание означает «операция принята», а не «ресурс есть».
// Норма — `.claude/rules/testing.md` §«Fixture-seed обязан проверять `op.error`
// перед извлечением resource-id из `metadata`».
//
// Проба, взявшая такой идентификатор без подтверждения, идёт дальше по ФАНТОМУ —
// и цена здесь не в самом фантоме, а в АТРИБУЦИИ ОТКАЗА. Падает не шаг, который
// предмет не создал, а тот, что сделал ровно положенное при отсутствующем
// предмете: ожидание события, проверка формы, чтение списка. Разбирать будут
// механизм, которого дефект не касается (`testing.md` §«Диагноз ставится по
// ТЕКСТУ отказа»), и разбор уйдёт в сторону на весь свой срок.
//
// # Чем подтверждают ЗДЕСЬ, и почему не опросом операции
//
// Дерево выбрало приём СИЛЬНЕЕ предписанного нормой минимума: ресурс читается по
// СВОЕМУ адресу (`createdResourceId` в `ui-future/e2e/specs/fixtures.ts`).
// Проверка `op.error` судит запись операции — она принадлежит службе-владельцу и
// отвечает о ходе мутации; чтение ресурса судит сам предмет и не зависит ни от
// того, чья это операция, ни от того, доехала ли её запись. Гейт требует именно
// подтверждения чтением: требуй он опроса операции, он краснел бы на трёх местах
// дерева, которые сегодня верны и строже.
//
// # Что гейт держит
//
//	ПОДТВЕРЖДЕНИЕ  у каждого идентификатора, прочитанного из `metadata`, есть
//	               ПОСЛЕДУЮЩЕЕ чтение по адресу, называющее эту переменную.
//	ПОРЯДОК        подтверждение стоит ПОСЛЕ чтения: чтение чужого адреса выше
//	               по файлу ничего не говорит о свежем идентификаторе.
//	ОБЛАСТЬ        подтверждение стоит в области видимости ОБЪЯВЛЕНИЯ. За
//	               границей блока то же имя принадлежит другой переменной с
//	               другим значением: два помощника, каждый со своим `id`,
//	               сопоставлением по имени в пределах файла выглядят одним.
//	ПЕРЕПИСЬ       файлов прочитано, идентификаторов найдено, подтверждено.
//	               «Ноль находок» обязано быть отличимо от «ноль прочитанного».
//
// # Чего гейт НЕ держит, и это сказано, а не умолчано
//
// Он не судит, ЖДЁТ ли подтверждение условия (`expect.poll`) или спрашивает
// однажды: это вопрос устойчивости пробы, а не атрибуции отказа, и у него другой
// предмет. Он не судит и о том, тот ли адрес прочитан: адрес — строка, и его
// верность доказывает прогон, а не разбор.
//
// # Способность упасть
//
// Доказана инъекцией в обе стороны — `consoleprobeoperationid_injection_test.go`.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// consoleProbeOperationIDCensus — объём осмотренного.
type consoleProbeOperationIDCensus struct {
	Files     int // файлов пакета прочитано
	Reads     int // чтений идентификатора из metadata найдено
	Confirmed int // из них подтверждённых чтением ресурса

	// WiderThanBlock — чтений, объявленных через `var`. Предпосылка разбора
	// области видимости: она оценивается по БЛОКУ, что верно для `const`/`let`
	// и уже языка для `var`. Ноль означает «предпосылка держится»; не ноль —
	// «гейт судил бы по более узкому правилу, чем язык», и это объявляется, а
	// не молчится.
	WiderThanBlock int
}

// consoleProbeOperationIDFinding — идентификатор без подтверждения.
type consoleProbeOperationIDFinding struct {
	File string
	Line int
	Name string
}

func (f consoleProbeOperationIDFinding) String() string {
	return f.File + ":" + strconv.Itoa(f.Line) + " " + f.Name
}

// consoleProbeMetadataReadRe — чтение идентификатора из `metadata` операции.
// Известны ТРИ законные формы записи, и все три разбираются: доступ по имени
// поля (`body.metadata?.placementGroupId`), доступ по вычисленному ключу
// (`body.metadata?.[metadataField]`) и доступ без защиты от отсутствия
// (`op.metadata.groupId`). Форма, которой распознаватель не знает, не даёт ни
// красного, ни зелёного — она молчит, и записанное в ней оказывается вне
// наблюдения (`testing.md` §«Гейт на класс», п. 7).
var consoleProbeMetadataReadRe = regexp.MustCompile(
	`(?s)\b(const|let|var)\s+([A-Za-z_$][\w$]*)\s*(?::[^=;]*)?=\s*[^;]{0,400}?\.metadata\s*\??\s*[.\[]`)

// consoleProbeMetadataDestructureRe — четвёртая форма: разбор объекта.
// В дереве её сегодня нет; распознана она намеренно, потому что законна и
// появится молча — а слепая зона распознавателя не объявляет о себе ничем.
var consoleProbeMetadataDestructureRe = regexp.MustCompile(
	`(?s)\b(const|let|var)\s*\{([^}]{1,200})\}\s*=\s*[^;]{0,200}?\.metadata\b`)

// consoleProbeResourceReadRe — подтверждение: чтение по адресу. Обе живые формы
// аргумента — подстановка в шаблон (`/iam/v1/groups/${groupId}`) и построитель
// адреса (`addressOf(id)`) — разбираются одинаково, потому что вопрос к
// аргументу один: НАЗЫВАЕТ ли он этот идентификатор.
var consoleProbeResourceReadRe = regexp.MustCompile(`\brequest\s*\.\s*get\s*\(`)

// consoleProbeBlockEnd — смещение фигурной скобки, закрывающей ОБЪЕМЛЮЩИЙ блок
// того места, где стоит `from`. По ней ограничивается область видимости
// объявления: `const`/`let` живут до конца своего блока и ни строкой дальше.
//
// Считается по МАСКЕ, где тела строковых, шаблонных и регулярных литералов и
// комментарии обнулены, — иначе скобка внутри адреса (`${id}`) или внутри
// рассуждения о коде закрывала бы блок, которого не открывала.
func consoleProbeBlockEnd(mask []byte, from int) int {
	depth := 0
	for j := from; j < len(mask); j++ {
		switch mask[j] {
		case '{':
			depth++
		case '}':
			if depth == 0 {
				return j
			}
			depth--
		}
	}
	return len(mask)
}

// consoleProbeIdentRe — имя как отдельное слово: подстрока не годится, иначе
// `id` нашлось бы внутри `projectId` и подтверждение приписалось бы чужому
// идентификатору.
func consoleProbeMentions(arg, name string) bool {
	re, err := regexp.Compile(`(?:^|[^\w$])` + regexp.QuoteMeta(name) + `(?:[^\w$]|$)`)
	if err != nil {
		return false
	}
	return re.MatchString(arg)
}

// consoleProbeStripComments возвращает текст, в котором обнулены ТОЛЬКО
// комментарии: содержимое строк сохранено намеренно — подтверждение живёт внутри
// шаблонной строки (`${groupId}`), и маска, обнуляющая строки, выбросила бы
// ровно предмет. Комментарии, наоборот, обязаны уйти: рассуждение О классе не
// является ни его экземпляром, ни его подтверждением.
func consoleProbeStripComments(src string) string {
	_, comments := consoleProbeScan(src)
	out := []byte(src)
	for _, c := range comments {
		for i := c.At - 2; i < c.At+len(c.Text) && i < len(out); i++ {
			if i >= 0 && out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	return string(out)
}

// consoleProbeArgSpan — текст аргументов вызова, начинающегося на `open`.
// Парность скобок считается по МАСКЕ (строки и регулярные литералы обнулены),
// а текст берётся из версии со строками: скобка внутри строки не должна
// закрывать вызов, но `${имя}` внутри неё — предмет вопроса.
func consoleProbeArgSpan(mask []byte, text string, open int) (string, bool) {
	depth := 0
	for j := open; j < len(mask); j++ {
		switch mask[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return text[open+1 : j], true
			}
		}
	}
	return "", false
}

// auditConsoleProbeOperationIDs — чистая функция над корпусом «путь → исходник».
// Гейт по дереву и инъекция зовут ЕЁ ЖЕ.
func auditConsoleProbeOperationIDs(sources map[string]string) (consoleProbeOperationIDCensus, []consoleProbeOperationIDFinding) {
	var (
		census   consoleProbeOperationIDCensus
		findings []consoleProbeOperationIDFinding
	)

	paths := make([]string, 0, len(sources))
	for p := range sources {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		src := sources[path]
		census.Files++
		text := consoleProbeStripComments(src)
		mask, _ := consoleProbeScan(src)

		type read struct {
			name string
			at   int
			// scopeEnd — граница области видимости объявления. Подтверждение,
			// стоящее за ней, относится к ДРУГОЙ переменной того же имени.
			scopeEnd int
		}
		var reads []read

		// scopeOf — граница видимости по ключевому слову объявления. Для `var`
		// она шире блока, и разбор её не строит: такое объявление отмечается
		// переписью как выход за предпосылку, а границей ему служит файл —
		// узкая граница дала бы находку на исправном коде.
		scopeOf := func(keyword string, at int) int {
			if keyword == "var" {
				census.WiderThanBlock++
				return len(text)
			}
			return consoleProbeBlockEnd(mask, at)
		}

		for _, m := range consoleProbeMetadataReadRe.FindAllStringSubmatchIndex(text, -1) {
			reads = append(reads, read{
				name:     text[m[4]:m[5]],
				at:       m[1],
				scopeEnd: scopeOf(text[m[2]:m[3]], m[1]),
			})
		}
		for _, m := range consoleProbeMetadataDestructureRe.FindAllStringSubmatchIndex(text, -1) {
			end := scopeOf(text[m[2]:m[3]], m[1])
			for _, part := range strings.Split(text[m[4]:m[5]], ",") {
				name := strings.TrimSpace(part)
				if i := strings.Index(name, ":"); i >= 0 {
					name = strings.TrimSpace(name[i+1:])
				}
				if name != "" {
					reads = append(reads, read{name: name, at: m[1], scopeEnd: end})
				}
			}
		}

		// Подтверждения: где по этому файлу читают ресурс и какое имя называют.
		type confirm struct {
			arg string
			at  int
		}
		var confirms []confirm
		for _, loc := range consoleProbeResourceReadRe.FindAllStringIndex(text, -1) {
			open := loc[1] - 1
			if arg, ok := consoleProbeArgSpan(mask, text, open); ok {
				confirms = append(confirms, confirm{arg: arg, at: open})
			}
		}

		for _, r := range reads {
			census.Reads++
			ok := false
			for _, c := range confirms {
				// Подтверждение обязано стоять ПОСЛЕ чтения и В ЕГО ОБЛАСТИ
				// ВИДИМОСТИ. Первое — потому что адрес, прочитанный выше по
				// файлу, о свежем идентификаторе не говорит ничего. Второе —
				// потому что за границей блока то же ИМЯ принадлежит другой
				// переменной с другим значением, и зачесть его подтверждением
				// значит объявить неподтверждённое чтение подтверждённым.
				if c.at > r.at && c.at < r.scopeEnd && consoleProbeMentions(c.arg, r.name) {
					ok = true
					break
				}
			}
			if ok {
				census.Confirmed++
				continue
			}
			findings = append(findings, consoleProbeOperationIDFinding{
				File: path,
				Line: 1 + strings.Count(text[:r.at], "\n"),
				Name: r.name,
			})
		}
	}
	return census, findings
}

// consoleProbeOperationIDSources — исходники пакета сквозных проб: и исполняемый
// набор, и каталог ожидания. Единица счёта — отслеживаемый git-элемент.
func consoleProbeOperationIDSources(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, rel := range trackedPaths(t, root) {
		if !strings.HasPrefix(rel, consoleProbePackageDir) || !strings.HasSuffix(rel, ".ts") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("%s: %v — состав пакета неизвестен, значит вердикт был бы утверждением ни о чём", rel, err)
		}
		out[rel] = string(b)
	}
	return out
}

// TestProbeIdentifiersFromOperationMetadataAreConfirmedByReading — гейт нормы
// `testing.md` §«Fixture-seed обязан проверять `op.error`…».
func TestProbeIdentifiersFromOperationMetadataAreConfirmedByReading(t *testing.T) {
	root := repoRoot(t)
	sources := consoleProbeOperationIDSources(t, root)
	if len(sources) == 0 {
		t.Fatalf("в %s не найдено ни одного отслеживаемого .ts — гейт беспредметен. "+
			"«Ноль находок» здесь неотличимо от «ноль прочитанного»", consoleProbePackageDir)
	}

	census, findings := auditConsoleProbeOperationIDs(sources)

	// Предпосылка гейта: предмет в дереве ЕСТЬ. Ноль чтений при непустом корпусе
	// означает либо что пробы перестали создавать ресурсы мутацией (тогда гейту
	// нечего охранять), либо что распознаватель ослеп на всех формах разом.
	// Различить эти два состояния по молчанию нельзя, поэтому оно объявляется.
	if census.Reads == 0 {
		t.Errorf("в %d файлах пакета не найдено НИ ОДНОГО чтения идентификатора из metadata. "+
			"Либо предмет исчез, либо разбор сломан — и второе выглядит точно так же",
			census.Files)
	}

	for _, f := range findings {
		t.Errorf("%s:%d — идентификатор %q взят из metadata операции и НЕ подтверждён чтением ресурса.\n\n"+
			"`200` на мутацию означает «операция принята», а не «ресурс есть»: идентификатор "+
			"чеканится ДО асинхронной части и приезжает в ответе даже тогда, когда та отказала.\n"+
			"Цена — не фантом, а АТРИБУЦИЯ: падает шаг, сделавший положенное при отсутствующем "+
			"предмете (ожидание события, чтение списка), а шаг, предмет не создавший, молчит. "+
			"Разбирать будут механизм, которого дефект не касается.\n"+
			"Подтверждением НЕ считается: чтение выше объявления; чтение за границей блока, "+
			"где это имя принадлежит уже другой переменной.\n"+
			"Исход: подтвердить ресурс чтением по его собственному адресу — этим занят "+
			"`createdResourceId` в ui-future/e2e/specs/fixtures.ts. "+
			"Норма: .claude/rules/testing.md §«Fixture-seed обязан проверять `op.error`».",
			f.File, f.Line, f.Name)
	}

	// Предпосылка разбора области видимости, объявленная и проверенная: границу
	// видимости гейт считает по БЛОКУ, и это верно для `const`/`let`. У `var`
	// она шире, поэтому такому объявлению гейт границей не ограничивает вовсе —
	// и говорит об этом, вместо того чтобы молча судить по узкому правилу.
	if census.WiderThanBlock > 0 {
		t.Errorf("%d чтений объявлены через `var`: область видимости у него шире блока, "+
			"и разбор её не строит — подтверждение из другого блока гейт зачтёт. "+
			"Исход: объявлять `const`/`let`, для которых граница блока и есть граница видимости",
			census.WiderThanBlock)
	}

	t.Logf("перепись: файлов пакета %d · чтений идентификатора из metadata %d · "+
		"из них подтверждено чтением ресурса %d · объявлено через `var` (шире блока) %d · находок %d",
		census.Files, census.Reads, census.Confirmed, census.WiderThanBlock, len(findings))
}
