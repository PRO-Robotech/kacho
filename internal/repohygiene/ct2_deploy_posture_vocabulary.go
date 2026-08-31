// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
)

// postureHomeDir — единственный дом словаря посадок. Каталог, а не файл:
// `pkg/servicecontract` — дом семантики объявления процесса о себе целиком, и
// требовать от него ОДНОГО файла значило бы судить раскладку вместо предмета.
const postureHomeDir = "pkg/servicecontract"

// postureFinding — одно место, где словарь объявлен вне общего дома.
type postureFinding struct {
	Where  string
	Values []string
}

// postureCensus — объём осмотренного, ПО ОСЯМ. Одно суммарное число скрыло бы
// ровно тот случай, ради которого ось заведена.
type postureCensus struct {
	FilesRead    int            // не-тестовых файлов Go, разобранных обходом
	HomeFiles    int            // файлов ДОМА, несущих словарь
	LiteralsSeen int            // всего литералов словаря по дереву (дом включён)
	ByValue      map[string]int // какое написание сколько раз встретилось
}

// auditPostureVocabularySingleSource — судящая функция гейта «словарь допустимых
// значений посадки объявлен в дереве ОДИН раз».
//
// # Что судится
//
// СТРОКОВЫЙ ЛИТЕРАЛ разобранного дерева, а не текст файла. Различие несущее:
// словарь `dev|production|production-strict` стоит в шапках десятка функций и
// внутри текстов отказа, и поиск по подстроке краснел бы на собственном
// объяснении (`testing.md` §«Гейт на класс», п.4). Текст отказа при этом
// литералом словаря НЕ является: `"unknown mode %q (allowed: dev, …)"` — одна
// строка целиком, и ни одному написанию она не равна.
//
// # Две оси, и вторая не украшение
//
//   - ОДНОЗНАЧНАЯ: литерал, чьё имя несёт дефис. `production-strict` в этом
//     дереве не значит ничего, кроме посадки, поэтому одного хватает: файл,
//     который его называет, объявляет словарь. Признак ВЫВЕДЕН из формы имени, а
//     не выписан — иначе у гейта завёлся бы собственный перечень;
//   - ПЕРЕЧИСЛЕНИЕ: два и более РАЗНЫХ написания словаря в одном файле. Нужна
//     потому, что копия бывает УЖЕ общего словаря — ровно так и выглядел
//     разошедшийся: он знал два значения из трёх. Одно написание находкой не
//     считается: `production` в объявлении умолчания ручки называет ОДНО значение,
//     а не перечисляет словарь, и `dev` порознь — вообще обычное слово (метка
//     окружения, версия сборки).
//
// Словарь берётся у САМОГО ДОМА (`servicecontract.Modes()`), а не выписывается
// здесь: иначе гейт стал бы вторым местом об одном предмете — тем самым, которое
// он и ловит. Заведут в доме четвёртую посадку — распознаватель увидит её тем же
// прогоном.
//
// # Чего гейт НЕ судит, названо прямо
//
// НЕ судит, ЧТО вызывающий делает с ответом разбора: перевод общего значения в
// своё (`switch mode { case servicecontract.ModeDev: … }`) литералов не содержит
// и остаётся законным — сервис вправе иметь свой перечень посадок, пока он не
// заводит своих ПИСЬМЕН для них.
func auditPostureVocabularySingleSource(root string, files []string) ([]postureFinding, postureCensus, error) {
	vocabulary := map[string]struct{}{}
	for _, mode := range servicecontract.Modes() {
		vocabulary[mode] = struct{}{}
	}
	// ОДНОЗНАЧНОЕ написание выводится из формы имени, а не выписывается: дефис в
	// имени посадки встречается только у неё, и обычным словом такое имя быть не
	// может. Выписанный перечень был бы вторым местом об одном предмете — ровно
	// тем классом, который этот гейт ловит; выведенный расширяется вместе с домом.
	unambiguous := func(mode string) bool { return strings.Contains(mode, "-") }

	cen := postureCensus{ByValue: map[string]int{}}
	var findings []postureFinding
	fset := token.NewFileSet()

	for _, rel := range files {
		slashed := filepath.ToSlash(rel)
		// Сгенерённые стабы руками не правят — требовать от вывода генератора
		// свойства исходника значило бы судить не того автора.
		if strings.HasPrefix(slashed, "pkg/api/") {
			continue
		}
		// #nosec G304 -- путь пришёл из индекса git ЭТОГО дерева (treecorpus, через
		// trackedGoFiles) либо из синтетического корня инъекции; постороннего ввода
		// тут нет.
		src, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return nil, cen, err
		}
		file, perr := parser.ParseFile(fset, rel, src, 0)
		if perr != nil {
			return nil, cen, perr
		}
		cen.FilesRead++

		seen := map[string]struct{}{}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				return true
			}
			if _, known := vocabulary[value]; !known {
				return true
			}
			cen.LiteralsSeen++
			cen.ByValue[value]++
			seen[value] = struct{}{}
			return true
		})
		if len(seen) == 0 {
			continue
		}

		values := make([]string, 0, len(seen))
		for v := range seen {
			values = append(values, v)
		}
		sort.Strings(values)

		if strings.HasPrefix(slashed, postureHomeDir+"/") {
			cen.HomeFiles++
			continue
		}
		var declares bool
		if len(values) >= 2 {
			declares = true
		}
		for _, v := range values {
			if unambiguous(v) {
				declares = true
			}
		}
		if declares {
			findings = append(findings, postureFinding{Where: slashed, Values: values})
		}
	}
	return findings, cen, nil
}
