// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratordsnresolve.go — ПРИОРИТЕТ ИСТОЧНИКОВ DSN объявлен один раз на дерево.
//
// # Предмет: третья ось того же тракта (#1544)
//
// Соседний гейт [TestMigratorDbFreeTractIsDeclaredOnce] стережёт ДВЕ оси:
// тексты отказа предусловий и разбор `--target`. Он молчит о третьей — о том,
// ЧЕМ точка наката выбирает строку подключения. Копия, не переобъявившая ни
// одного текста и не заведшая своего разбора цели, вправе держать собственную
// цепочку `--dsn` → переменная окружения → конфигурация сервиса, и потолок
// первых двух осей от этого не сдвинется.
//
// Именно так оно и стояло: пятеро звали общий резолв, двое держали ту же
// цепочку своей копией. Сегодня обе копии дают тот же приоритет, что общая, —
// то есть дефекта поведения нет. Предмет здесь ТРЕТЬЕ ОБЪЯВЛЕНИЕ ОДНОГО
// ПРАВИЛА, и класс уже сработал рядом, на соседней оси того же тракта: тексты
// отказа жили тремя копиями, и КАЖДАЯ не называла одного из собственных
// источников DSN — каждая своего. Разошлись они ровно так: каждую правили
// отдельно.
//
// Цена конкретна и измеряется в местах правки: добавить источник или поменять
// порядок — три места вместо одного, и два из них не покрыты пробами общего
// пакета.
//
// # Что требуется — ДВЕ половины, и ни одна не заменяет другую
//
//  1. ПОЛОЖИТЕЛЬНАЯ: каждая точка наката зовёт [migratorcli.ResolveDSN].
//     Без неё точка вправе читать один `--dsn` и молча терять переменную
//     окружения с запасной конфигурацией — накат уедет не туда, а исход будет
//     выглядеть успехом.
//  2. ОТРИЦАТЕЛЬНАЯ: ни один файл тракта вне общего пакета не читает
//     переменную окружения DSN сам. Без неё точка вправе звать общий резолв И
//     держать рядом свою цепочку — то есть иметь два порядка на один предмет.
//
// # Почему разбор, а не поиск подстроки
//
// Имя переменной окружения DSN встречается в этом же дереве в тексте подсказки
// флага и в прозе шапок — в том числе в ЭТОЙ шапке. Гейт по подстроке краснел
// бы на собственном объяснении, а на настоящем чтении — молчал бы ровно так же.
// Поэтому чтение ищется УЗЛОМ ВЫЗОВА `os.Getenv`, а не словом, и аргумент
// разрешается по трём законным написаниям сразу: литерал, местная константа-
// псевдоним и обращение к константе общего пакета. Написание, о котором
// распознаватель не знает, — не редкость, а слепая зона (testing.md §«Гейт на
// класс», п.7), поэтому все три названы и доказаны инъекцией порознь.
//
// # Проверка собственной предпосылки
//
// Оба признака опираются на факт об общем пакете: что он объявляет функцию
// резолва и константу с этим именем переменной. Переименуй их — и положительная
// половина не найдёт ни одной делегирующей точки, а отрицательная ослепнет на
// литерале. Поэтому предпосылка утверждается ОТДЕЛЬНО и роняет прогон.
//
// # Чего гейт НЕ утверждает
//
// Что накат сведён: goose по-прежнему зовут семь точек, и их сведение ждёт проб
// на живой базе (предусловие названо в `docs/architecture/migrator-form.md`).
// Резолв DSN проверяется БЕЗ базы и потому под то предусловие не подпадает.
//
// И что запасная конфигурация где-то лишняя: у vpc и iam она законна и
// остаётся — общий резолв принимает её замыканием, чем и пользуется nlb.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
)

const (
	// migratorDSNSharedImport — путь общего пакета. Назван, а не выведен: «пакет,
	// на который ссылаются» есть определение через тех, кого проверяем.
	migratorDSNSharedImport = "github.com/PRO-Robotech/kacho/pkg/migratorcli"

	// migratorDSNResolveFunc — общий резолв приоритета источников.
	migratorDSNResolveFunc = "ResolveDSN"

	// migratorDSNEnvConst — константа общего пакета с именем переменной.
	migratorDSNEnvConst = "EnvDSN"

	// migratorDSNEnvName — само имя переменной. Дублируется здесь НАМЕРЕННО и
	// сверяется с общим пакетом премисой ниже: без литерала отрицательная
	// половина не увидела бы чтения, записанного литералом, а без сверки
	// литерал пережил бы переименование.
	migratorDSNEnvName = "KACHO_MIGRATOR_DSN"
)

// migratorDSNFinding — одна находка с координатой.
type migratorDSNFinding struct {
	Rel  string
	What string
}

// migratorDSNCensus — объём осмотренного. Отдельное утверждение: «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
type migratorDSNCensus struct {
	FilesRead      int
	SharedFiles    int
	TractFiles     int
	EntryPoints    int
	Delegating     int
	OwnEnvReads    int
	PremiseResolve bool
	PremiseEnv     bool
}

func (c migratorDSNCensus) String() string {
	return fmt.Sprintf(
		"перепись: прочитано файлов %d (общий пакет %d · тракт %d) · "+
			"точек наката %d · зовут общий резолв %d · своих чтений переменной DSN %d · "+
			"предпосылка: %s объявлен %t, %s объявлена %t",
		c.FilesRead, c.SharedFiles, c.TractFiles,
		c.EntryPoints, c.Delegating, c.OwnEnvReads,
		migratorDSNResolveFunc, c.PremiseResolve,
		migratorDSNEnvConst, c.PremiseEnv)
}

// migratorDSNFacts — что файл делает с DSN. Одна структура на обе половины:
// разные проходы по одному файлу разошлись бы в том, что считают файлом тракта.
type migratorDSNFacts struct {
	// CallsSharedResolve — файл зовёт резолв общего пакета.
	CallsSharedResolve bool
	// OwnEnvReads — чтения переменной окружения DSN этим файлом, каждое с
	// объяснением, каким написанием оно найдено.
	OwnEnvReads []string
	// DeclaresResolve / DeclaresEnvName — предпосылка, если файл общего пакета.
	DeclaresResolve  bool
	DeclaresEnvName  bool
	DeclaredEnvValue string
}

// migratorDSNFactsOf разбирает ОДИН файл. Вынесено из обхода, чтобы инъекция
// звала ТО ЖЕ, что и гейт, подавая исходник строкой: доказательство, трогающее
// дерево, испортило бы чужую рабочую копию, а доказательство на копии разбора
// говорило бы о копии.
func migratorDSNFactsOf(rel, src string) (migratorDSNFacts, error) {
	var facts migratorDSNFacts

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return facts, fmt.Errorf("%s: разбор не удался: %w", rel, err)
	}

	// Псевдонимы импорта общего пакета. Квалификатор берётся у ОБЪЯВЛЕНИЯ, а не
	// предполагается равным последнему сегменту пути: переименованный импорт —
	// законная запись, и распознаватель, её не знающий, ослеп бы молча.
	sharedQualifiers := map[string]bool{}
	for _, spec := range file.Imports {
		p, uerr := strconv.Unquote(spec.Path.Value)
		if uerr != nil || p != migratorDSNSharedImport {
			continue
		}
		if spec.Name != nil {
			sharedQualifiers[spec.Name.Name] = true
			continue
		}
		sharedQualifiers[path.Base(p)] = true
	}

	// Местные константы-псевдонимы имени переменной. Обе законные формы:
	// присвоение литерала и присвоение константы общего пакета.
	envAliases := map[string]bool{}
	for _, d := range file.Decls {
		gen, ok := d.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, s := range gen.Specs {
			vs, ok := s.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				switch v := vs.Values[i].(type) {
				case *ast.BasicLit:
					if v.Kind != token.STRING {
						continue
					}
					unq, uerr := strconv.Unquote(v.Value)
					if uerr != nil {
						continue
					}
					if unq == migratorDSNEnvName {
						envAliases[name.Name] = true
					}
					if name.Name == migratorDSNEnvConst {
						facts.DeclaresEnvName = true
						facts.DeclaredEnvValue = unq
					}
				case *ast.SelectorExpr:
					if isSharedSelector(v, sharedQualifiers, migratorDSNEnvConst) {
						envAliases[name.Name] = true
					}
				}
			}
		}
	}

	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if ok && fn.Recv == nil && fn.Name.Name == migratorDSNResolveFunc {
			facts.DeclaresResolve = true
		}
	}

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if isSharedSelector(sel, sharedQualifiers, migratorDSNResolveFunc) {
			facts.CallsSharedResolve = true
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "os" || sel.Sel.Name != "Getenv" || len(call.Args) != 1 {
			return true
		}
		if how, named := migratorDSNArgNamesEnv(call.Args[0], sharedQualifiers, envAliases); named {
			facts.OwnEnvReads = append(facts.OwnEnvReads,
				fmt.Sprintf("%s: читает переменную окружения DSN сам — os.Getenv(%s)",
					fset.Position(call.Pos()), how))
		}
		return true
	})

	return facts, nil
}

// isSharedSelector — обращение к имени общего пакета через любой его псевдоним.
func isSharedSelector(sel *ast.SelectorExpr, qualifiers map[string]bool, name string) bool {
	id, ok := sel.X.(*ast.Ident)
	return ok && qualifiers[id.Name] && sel.Sel.Name == name
}

// migratorDSNArgNamesEnv — аргумент os.Getenv называет переменную DSN.
//
// Три законных написания, и все три судятся: голый литерал, местная
// константа-псевдоним и обращение к константе общего пакета. Возвращается ещё и
// то, КАКИМ написанием найдено, — находка обязана называть причину, а не
// симптом, иначе на неё потратят прогон и снимут гейт как непонятный.
func migratorDSNArgNamesEnv(arg ast.Expr, qualifiers, aliases map[string]bool) (string, bool) {
	switch v := arg.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		unq, err := strconv.Unquote(v.Value)
		if err != nil || unq != migratorDSNEnvName {
			return "", false
		}
		return strconv.Quote(unq) + " (литерал)", true
	case *ast.Ident:
		if !aliases[v.Name] {
			return "", false
		}
		return v.Name + " (местная константа-псевдоним)", true
	case *ast.SelectorExpr:
		if !isSharedSelector(v, qualifiers, migratorDSNEnvConst) {
			return "", false
		}
		id, _ := v.X.(*ast.Ident)
		return id.Name + "." + v.Sel.Name + " (константа общего пакета)", true
	}
	return "", false
}

// migratorDSNEntryDir — каталог точки наката для файла тракта, либо пусто.
//
// Точкой наката зовётся `cmd/migrator` сервиса, а не весь тракт: делегировать
// резолв обязан тот, кто собирает накат из командной строки. Слой
// `internal/apps/migrator` от него значение принимает и своего источника не
// имеет — но отрицательная половина его тоже осматривает, потому что чтение
// переменной там было бы тем же вторым объявлением приоритета.
func migratorDSNEntryDir(rel string) string {
	const marker = "/cmd/migrator/"
	i := strings.Index(rel, marker)
	if i < 0 {
		return ""
	}
	return rel[:i+len(marker)-1]
}

// migratorDSNFindingText формулирует находку так, чтобы читатель понял, что
// делать, не открывая этот файл.
func migratorDSNFindingText(f migratorDSNFinding) string {
	return fmt.Sprintf("%s: %s. Приоритет источников DSN (--dsn > %s > конфигурация "+
		"сервиса) объявлен в %s.%s — делегируй туда, передав запасную конфигурацию "+
		"замыканием, а не заводи второе объявление порядка (%s)",
		f.Rel, f.What, migratorDSNEnvName,
		migratorDSNSharedImport, migratorDSNResolveFunc, migratorTractDecisionDoc)
}

func sortedMigratorDSNTexts(findings []migratorDSNFinding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, migratorDSNFindingText(f))
	}
	sort.Strings(out)
	return out
}
