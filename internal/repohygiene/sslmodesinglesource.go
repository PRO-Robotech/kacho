// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	coredb "github.com/PRO-Robotech/corelib/db"
)

// sslModeHomeDir — единственный дом перечня режимов шифрования до собственной
// базы. Каталог, а не файл: `pkg/db` — дом семантики строки подключения целиком
// (разбор DSN, умолчание libpq, пул), и требовать от него ОДНОГО файла значило
// бы судить раскладку вместо предмета.
const sslModeHomeDir = "pkg/db"

// sslModeFinding — одно место, где перечень объявлен вне общего дома.
type sslModeFinding struct {
	Where  string
	Values []string
}

// sslModeCensus — объём осмотренного. Печатается всегда: «ноль находок» обязано
// быть отличимо от «ноль прочитанного», и по осям порознь — иначе одно
// суммарное число скрыло бы ровно тот случай, ради которого ось заведена.
type sslModeCensus struct {
	FilesRead    int            // не-тестовых файлов Go, разобранных обходом
	HomeFiles    int            // файлов ДОМА, несущих словарь
	LiteralsSeen int            // всего литералов словаря по дереву (дом включён)
	ByValue      map[string]int // какое значение сколько раз встретилось
}

// auditSSLModeSingleSource — судящая функция гейта «перечень режимов шифрования
// до базы объявлен в дереве ОДИН раз».
//
// # Что судится
//
// СТРОКОВЫЙ ЛИТЕРАЛ разобранного дерева, а не текст файла. Различие несущее:
// словарь `require|verify-ca|verify-full` стоит в шапках десятка функций и в
// текстах отказа, и поиск по подстроке краснел бы на собственном объяснении
// (`testing.md` §«Гейт на класс», п.4). Комментарий, называющий режимы, законен
// и остаётся.
//
// # Две оси, и вторая не украшение
//
//   - ОДНОЗНАЧНАЯ: литерал, чьё имя несёт дефис. Такое имя в дереве не значит
//     ничего, кроме режима шифрования до базы, поэтому одного хватает: файл,
//     который его называет, перечисляет режимы. Признак ВЫВЕДЕН из формы имени,
//     а не выписан — иначе у гейта завёлся бы собственный перечень;
//   - ПЕРЕЧИСЛЕНИЕ: два и более РАЗНЫХ значения словаря в одном файле.
//     Нужна потому, что копия бывает УЖЕ общего перечня — `case "disable",
//     "require"` не содержит однозначных слов вовсе, а решает ту же ось. Одно
//     значение находкой не считается: `require` и `disable` — обычные слова, и
//     ось из одного литерала дала бы ложные находки на первом же соседнем
//     предмете.
//
// Словарь берётся у САМОГО ДОМА (`coredb.SSLModes()`), а не выписывается здесь:
// иначе гейт стал бы вторым местом об одном предмете — тем самым, которое он и
// ловит. Заведут в доме новое значение — распознаватель увидит его тем же
// прогоном.
func auditSSLModeSingleSource(root string, files []string) ([]sslModeFinding, sslModeCensus, error) {
	return auditSSLModeSingleSourceWithExtra(root, files, nil)
}

// auditSSLModeSingleSourceWithExtra — то же самое, плюс файлы, уже прочитанные
// вызывающим под своим синтетическим путём (пакет db общего фундамента,
// github.com/PRO-Robotech/corelib, читаемый из кэша модулей —
// corelibPackageGoFiles, см. corelibsource_test.go), — вместо диска. Дом
// переехал из pkg/db туда; синтетические деревья инъекции продолжают писать
// "pkg/db/..." напрямую и extra им не нужен.
func auditSSLModeSingleSourceWithExtra(
	root string, files []string, extra map[string][]byte,
) ([]sslModeFinding, sslModeCensus, error) {
	vocabulary := map[string]struct{}{}
	for _, mode := range coredb.SSLModes() {
		vocabulary[mode] = struct{}{}
	}
	// ОДНОЗНАЧНОЕ значение выводится из формы имени, а не выписывается: дефис
	// в имени режима встречается только у него (`verify-ca`, `verify-full`), и
	// обычным словом такое имя быть не может. Выписанный перечень был бы
	// вторым местом об одном предмете — ровно тем классом, который этот гейт
	// ловит; выведенный — расширяется вместе с домом сам. Режим без дефиса
	// (`disable`, `require`) остаётся за осью перечисления: как одиночный
	// литерал он неотличим от обычного слова.
	unambiguous := func(mode string) bool { return strings.Contains(mode, "-") }

	cen := sslModeCensus{ByValue: map[string]int{}}
	var findings []sslModeFinding

	// scan принимает src ЛИТЕРАЛЬНЫМ any, а не []byte: параметр статического
	// типа []byte, переданный в parser.ParseFile(..., any, ...), даёт НЕПУСТОЙ
	// интерфейс даже при nil-слайсе — parser.ParseFile читает его как "источник
	// есть, и это пустые байты" и падает на EOF вместо чтения диска. Диском
	// управляет ИМЕННО нетипизированный nil, дошедший до ParseFile как есть.
	scan := func(slashed, abs string, src any) error {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, abs, src, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("разбор %s: %w", slashed, err)
		}
		cen.FilesRead++

		seen := map[string]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				return true
			}
			if _, known := vocabulary[strings.ToLower(v)]; !known {
				return true
			}
			seen[strings.ToLower(v)] = true
			cen.LiteralsSeen++
			cen.ByValue[strings.ToLower(v)]++
			return true
		})
		if len(seen) == 0 {
			return nil
		}

		values := make([]string, 0, len(seen))
		for v := range seen {
			values = append(values, v)
		}
		sort.Strings(values)

		if inSSLModeHome(slashed) {
			cen.HomeFiles++
			return nil
		}

		enumerates := len(seen) > 1
		for v := range seen {
			if unambiguous(v) {
				enumerates = true
			}
		}
		if enumerates {
			findings = append(findings, sslModeFinding{Where: slashed, Values: values})
		}
		return nil
	}

	for _, rel := range files {
		slashed := filepath.ToSlash(rel)
		// Сгенерённые стабы руками не правят — требовать от вывода генератора
		// свойства исходника значило бы судить не того автора.
		if strings.HasPrefix(slashed, "pkg/api/") {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(slashed))
		if err := scan(slashed, abs, nil); err != nil {
			return nil, cen, err
		}
	}
	for slashed, body := range extra {
		if err := scan(slashed, slashed, body); err != nil {
			return nil, cen, err
		}
	}

	sort.Slice(findings, func(i, j int) bool { return findings[i].Where < findings[j].Where })
	return findings, cen, nil
}

// sslModeCorelibHomeDir — синтетический путь, которым corelibPackageGoFiles
// метит файлы пакета db, прочитанные из кэша модулей общего фундамента
// (github.com/PRO-Robotech/corelib), — второй законный дом наравне с
// sslModeHomeDir. Обе формы принимаются РАЗОМ: дерево судит по фактическому
// написанию, а не по одному из двух периодов миграции.
const sslModeCorelibHomeDir = "corelib/db"

// inSSLModeHome — лежит ли файл в доме перечня, в любой из двух форм.
func inSSLModeHome(slashed string) bool {
	for _, home := range []string{sslModeHomeDir, sslModeCorelibHomeDir} {
		if slashed == home || strings.HasPrefix(slashed, home+"/") {
			return true
		}
	}
	return false
}
