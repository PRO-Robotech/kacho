// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_computedoctype.go — анализатор «тип, названный клиентской
// документацией в `@type`, существует в контрактах, и ровно там, где сказано».
//
// # Предмет
//
// `Operation.metadata` и `Operation.result.response` — это `google.protobuf.Any`,
// а `Any` адресуется ПОЛНЫМ именем типа (`type.googleapis.com/<пакет>.<Сообщение>`).
// Клиент разбирает ответ по этому имени: подставляет его в свой словарь стабов и
// достаёт поля. Имя, за которым в контрактах нет сообщения, поэтому не «неточная
// проза», а НЕРАЗБИРАЕМЫЙ ответ: у клиента нет типа, в который распаковывать,
// и он не получает идентификатор ресурса, ради которого читал операцию.
//
// Пакет судится наравне с именем сообщения, и это вторая половина предмета:
// `kacho.cloud.compute.v1.CreateVolumeMetadata` разбирается не хуже и не лучше
// выдуманного — такого типа нет, а есть `kacho.cloud.storage.v1.CreateVolumeMetadata`.
// Ошибка пакета вдобавок посылает вызывающего к чужому владельцу: край
// маршрутизирует полл операции по префиксу её идентификатора, и клиент,
// поверивший пакету, поллит сервис, который этой операции не держит.
//
// Замер на день заведения (kacho#1618): полных имён, названных клиентской
// документацией, — 26 уникальных; не резолвилось ОДНО — `CreateDiskMetadata`
// у вычислений, тип, которого в контрактах нет ни одного вхождения. Он стоял в
// быстром старте на шаге, который идёт к kacho-storage, вместе с полем чужого
// имени и идентификаторами чужих префиксов; клиент получал пустую переменную и
// строил на ней три следующих шага.
//
// # Что судит анализатор
//
// Словарь ВЫВОДИТСЯ из дерева контрактов: каждый `.proto` даёт `package` и все
// объявленные в нём сообщения, включая ВЛОЖЕННЫЕ (их в дереве девять; читать
// только верхний уровень значило бы объявлять вложенное несуществующим).
// Разбор идёт по объявлению: строковые литералы и комментарии — оба вида —
// снимаются до счёта скобок, иначе путь-шаблон REST (`"/v1/images/{image_id}"`)
// и проза о сообщениях считались бы за объявление.
//
// В документации распознаётся форма `type.googleapis.com/<полное имя>` — та
// самая, которую видит клиент в теле ответа.
//
// # ЧЕГО ОН НЕ СУДИТ, и это названо числом, а не умолчанием
//
//  1. ИМЯ ВНЕ ПРОСТРАНСТВА `kacho.` не судится: `google.protobuf.Empty`,
//     `google.rpc.Status` и прочие общеизвестные типы живут не в нашем дереве
//     контрактов, и «в контрактах нет» о них — не находка, а слепота
//     анализатора. Перепись печатает их число и перечень, поэтому граница
//     видна, а не подразумевается.
//
//  2. ГОЛОЕ ИМЯ типа, написанное прозой или ячейкой таблицы (`CreateDiskMetadata`
//     без `type.googleapis.com/`), вне охвата: у него нет пакета, и судить его
//     значило бы гадать о нём. Форма `@type` выбрана потому, что она полная и
//     ровно её разбирает клиент.
//
//  3. ПОЛНОТА не судится. Страница, назвавшая два типа из шести, верна в том,
//     что назвала; «назван ли каждый» — другой предикат с другим признаком.
//
// # Падает на ПУСТОМ ОБХОДЕ
//
// Ноль прочитанных контрактов, ноль сообщений в словаре, ноль страниц
// документации либо ноль рассуженных имён — «находок ноль» неотличимо от
// «прочитано ноль».
package repohygiene

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ClientDocsAnyTypeOptions — вход анализатора.
type ClientDocsAnyTypeOptions struct {
	// Root — корень дерева.
	Root string
	// ProtoRoot — каталог контрактов относительно Root. Словарь типов
	// выводится отсюда и только отсюда.
	ProtoRoot string
}

// ClientDocsAnyTypeCensus — объём осмотренного. Печатается всегда: «ноль
// находок» обязано быть отличимо от «ноль прочитанного».
type ClientDocsAnyTypeCensus struct {
	ProtoFiles    int
	ContractTypes int
	NestedTypes   int
	DocFiles      int
	TypeURLs      int
	Judged        int
	OutsideCount  int
	OutsideNames  []string
}

// ClientDocsAnyTypeFinding — одно полное имя, которое клиент не разберёт.
type ClientDocsAnyTypeFinding struct {
	File string
	Line int
	FQN  string
	// Elsewhere — полные имена того же сообщения в других пакетах, если они
	// есть. Отличает «типа нет вовсе» от «тип есть, но у другого владельца»:
	// у этих двух находок разная починка.
	Elsewhere []string
}

func (f ClientDocsAnyTypeFinding) String() string {
	if len(f.Elsewhere) > 0 {
		return fmt.Sprintf("%s:%d: `%s` — в контрактах такого типа нет; сообщение с этим "+
			"именем живёт в другом пакете: %s", f.File, f.Line, f.FQN, strings.Join(f.Elsewhere, ", "))
	}
	return fmt.Sprintf("%s:%d: `%s` — в контрактах нет ни одного вхождения этого типа",
		f.File, f.Line, f.FQN)
}

var (
	// clientDocsAnyTypeURLRe — форма, которую видит клиент в теле ответа.
	clientDocsAnyTypeURLRe = regexp.MustCompile(`type\.googleapis\.com/([A-Za-z_][A-Za-z0-9_.]*)`)
	// clientDocsProtoPackageRe — ОБЪЯВЛЕНИЕ пакета, а не упоминание.
	clientDocsProtoPackageRe = regexp.MustCompile(`(?m)^\s*package\s+([a-z0-9_.]+)\s*;`)
	// clientDocsProtoMessageRe — ОБЪЯВЛЕНИЕ сообщения. Отступ допускается:
	// вложенные сообщения объявляются именно так.
	clientDocsProtoMessageRe = regexp.MustCompile(`^\s*message\s+([A-Za-z_][A-Za-z0-9_]*)\s*\{`)
)

// AuditClientDocsAnyType выносит вердикт о дереве.
func AuditClientDocsAnyType(
	opts ClientDocsAnyTypeOptions,
	log io.Writer,
) ([]ClientDocsAnyTypeFinding, ClientDocsAnyTypeCensus, error) {
	var census ClientDocsAnyTypeCensus

	types, byShortName, protoFiles, nested, err := clientDocsAnyTypeDictionary(opts)
	if err != nil {
		return nil, census, err
	}
	census.ProtoFiles = protoFiles
	census.ContractTypes = len(types)
	census.NestedTypes = nested

	docs, err := clientDocsPages(opts.Root)
	if err != nil {
		return nil, census, err
	}
	census.DocFiles = len(docs)

	outside := map[string]bool{}
	var findings []ClientDocsAnyTypeFinding

	for _, rel := range docs {
		// #nosec G304 -- путь получен обходом дерева документации ЭТОГО репозитория, не извне
		raw, err := os.ReadFile(filepath.Join(opts.Root, rel))
		if err != nil {
			return nil, census, fmt.Errorf("%s: %w", rel, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			for _, m := range clientDocsAnyTypeURLRe.FindAllStringSubmatch(line, -1) {
				fqn := m[1]
				census.TypeURLs++
				if !strings.HasPrefix(fqn, "kacho.") {
					outside[fqn] = true
					census.OutsideCount++
					continue
				}
				census.Judged++
				if types[fqn] {
					continue
				}
				short := fqn[strings.LastIndexByte(fqn, '.')+1:]
				findings = append(findings, ClientDocsAnyTypeFinding{
					File: rel, Line: i + 1, FQN: fqn, Elsewhere: byShortName[short],
				})
			}
		}
	}

	for w := range outside {
		census.OutsideNames = append(census.OutsideNames, w)
	}
	sort.Strings(census.OutsideNames)

	if log != nil {
		_, _ = fmt.Fprintf(log,
			"перепись: контрактов %d · типов в словаре %d (из них вложенных %d) · "+
				"страниц документации %d · полных имён встречено %d · рассужено %d · "+
				"вне пространства kacho., НЕ судятся: %d %v\n",
			census.ProtoFiles, census.ContractTypes, census.NestedTypes,
			census.DocFiles, census.TypeURLs, census.Judged,
			census.OutsideCount, census.OutsideNames)
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].FQN < findings[j].FQN
	})
	return findings, census, nil
}

// clientDocsAnyTypeDictionary строит словарь полных имён ИЗ КОНТРАКТОВ:
// «<пакет>.<Сообщение>» → есть, плюс обратный указатель «короткое имя → полные
// имена», по которому находка отличает «типа нет» от «тип у другого владельца».
func clientDocsAnyTypeDictionary(
	opts ClientDocsAnyTypeOptions,
) (types map[string]bool, byShortName map[string][]string, files, nested int, err error) {
	types = map[string]bool{}
	byShortName = map[string][]string{}
	root := filepath.Join(opts.Root, opts.ProtoRoot)

	err = rootedWalk(root,
		func(rel string) bool { return strings.HasSuffix(rel, ".proto") },
		func(_ string, body []byte) error {
			files++
			pkg := ""
			if m := clientDocsProtoPackageRe.FindSubmatch(body); m != nil {
				pkg = string(m[1])
			}
			if pkg == "" {
				return nil
			}
			for _, path := range clientDocsProtoMessages(string(body)) {
				fqn := pkg + "." + strings.Join(path, ".")
				if len(path) > 1 {
					nested++
				}
				types[fqn] = true
				short := path[len(path)-1]
				byShortName[short] = append(byShortName[short], fqn)
			}
			return nil
		})
	if err != nil {
		return nil, nil, 0, 0, err
	}
	for k := range byShortName {
		sort.Strings(byShortName[k])
	}
	return types, byShortName, files, nested, nil
}

// clientDocsProtoMessages — пути объявленных сообщений файла, включая вложенные
// (`["Outer","Inner"]`). Глубина считается по скобкам, а строковые литералы и
// оба вида комментариев снимаются ДО счёта: иначе путь-шаблон REST
// (`"/v1/images/{image_id}"`) и проза о сообщениях считались бы объявлением.
func clientDocsProtoMessages(body string) [][]string {
	var out [][]string
	var stack []string
	var openedAt []int
	depth := 0
	inBlockComment := false

	for _, raw := range strings.Split(body, "\n") {
		line, stillInBlock := clientDocsStripNonCode(raw, inBlockComment)
		inBlockComment = stillInBlock

		if m := clientDocsProtoMessageRe.FindStringSubmatch(line); m != nil {
			stack = append(stack, m[1])
			openedAt = append(openedAt, depth)
			path := make([]string, len(stack))
			copy(path, stack)
			out = append(out, path)
		}
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		for len(stack) > 0 && depth <= openedAt[len(stack)-1] {
			stack = stack[:len(stack)-1]
			openedAt = openedAt[:len(openedAt)-1]
		}
	}
	return out
}

// clientDocsStripNonCode снимает со строки строковые литералы и комментарии
// обоих видов, возвращая остаток и признак «блочный комментарий продолжается».
func clientDocsStripNonCode(line string, inBlockComment bool) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(line); {
		if inBlockComment {
			if strings.HasPrefix(line[i:], "*/") {
				inBlockComment = false
				i += 2
				continue
			}
			i++
			continue
		}
		switch {
		case strings.HasPrefix(line[i:], "//"):
			return b.String(), false
		case strings.HasPrefix(line[i:], "/*"):
			inBlockComment = true
			i += 2
		case line[i] == '"':
			i++
			for i < len(line) && line[i] != '"' {
				if line[i] == '\\' && i+1 < len(line) {
					i++
				}
				i++
			}
			if i < len(line) {
				i++
			}
		default:
			b.WriteByte(line[i])
			i++
		}
	}
	return b.String(), inBlockComment
}
