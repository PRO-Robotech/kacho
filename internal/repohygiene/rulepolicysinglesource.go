// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// rulepolicysinglesource.go — политика послабления подстановки выводится из
// строки роли ОДНИМ местом.
//
// Приёмка `services/iam/docs/engineering/acceptance/role-ownership-tier-apart-from-cluster-anchor.md`
// (APPROVED круга 2), §2.2 и §8 п. 6; задача продукта #1032.
//
// # Предмет
//
// До #1032 политика была одна булева — «системный контекст», — и признак
// `is_system` нёс два смысла сразу: «арендатор эту роль не правит» и «этой роли
// можно подставлять звёздочку». Роль модуля обязана быть системной в первом
// смысле и вместе с ним получала второй.
//
// Разделение держится ЗНАЧЕНИЕМ закрытого перечня (`domain.RulePolicy`),
// выводимым из строки функцией `domain.PolicyOfRole`. Второе объявление
// «системная ли роль для целей подстановки» — находка: два места об одном
// предмете разойдутся, и разойдутся молча, потому что на законном входе оба
// отвечают одинаково.
//
// # Что здесь считается находкой — ДВЕ оси
//
//  1. **составной литерал `RulePolicy{…}` вне своего файла.** Ось судит УЗЕЛ
//     разбора, а не текст: имя типа стоит в комментариях, в шапках функций и в
//     объявлениях параметров, и гейт по подстроке краснел бы на собственном
//     объяснении;
//  2. **второе объявление `PolicyOfRole`.** Одна функция вывода — то, что
//     делает перечень политик закрытым; вторая означает второй словарь.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
// Политику, собранную рефлексией либо приехавшую из другого пакета готовым
// значением. Первого в этом дереве нет; второе неопасно by construction:
// поля `RulePolicy` и весь перечень ярусов НЕ ЭКСПОРТИРОВАНЫ, поэтому собрать
// непустую политику вне пакета `domain` невозможно — это ловит компилятор, а не
// гейт. Ось 1 закрывает остаток: сам пакет `domain`.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// rulePolicyTypeName — имя типа политики. Часть предпосылки гейта: тип
// переименуют — обход перестанет находить литералы, и перепись это покажет
// нулём, а не молчанием.
const rulePolicyTypeName = "RulePolicy"

// rulePolicyDeriverName — имя функции вывода политики из строки.
const rulePolicyDeriverName = "PolicyOfRole"

// rulePolicyHomeFile — единственный файл, которому литералы политики законны.
const rulePolicyHomeFile = "services/iam/internal/domain/rule_policy.go"

// rulePolicyScanRoot — корень обхода. Уже, чем дерево: тип живёт в домене iam, и
// расширять обход значило бы читать восемь тысяч файлов ради шести.
const rulePolicyScanRoot = "services/iam/internal/domain"

// RulePolicySite — место, где политика СОБИРАЕТСЯ (составным литералом) либо
// ВЫВОДИТСЯ (объявлением функции вывода).
type RulePolicySite struct {
	File    string // путь от корня дерева
	Line    int
	Kind    string // "литерал" | "вывод"
	Snippet string
}

// RulePolicyCensus — объём осмотренного. Печатается всегда: «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
type RulePolicyCensus struct {
	FilesRead int
	Literals  int
	Derivers  int
}

// ScanRulePolicySites берёт состав каталога домена У ИНДЕКСА git и собирает
// места сборки политики.
//
// Пробные файлы ИСКЛЮЧЕНЫ: проба вправе собрать любую политику — она проверяет
// поведение, а не объявляет его. Включи их — и гейт запретил бы пробу нулевого
// значения, то есть ровно ту, что доказывает строгость самой строгой ветви.
func ScanRulePolicySites(root string) ([]RulePolicySite, RulePolicyCensus, error) {
	var (
		sites  []RulePolicySite
		census RulePolicyCensus
	)
	// Состав берётся у ИНДЕКСА git, а не у диска. Обход по диску под
	// `services/` подбирает то, что лежит на всякой машине, где поднимали стенд
	// либо собирали фронтенд, — и вердикт становится свойством машины, а не
	// коммита. Два обхода поддерева уже оказались дефектными по этой причине.
	dir := filepath.Join(root, filepath.FromSlash(rulePolicyScanRoot))
	files, err := treecorpus.UnderWithSuffix(dir, ".go")
	if err != nil {
		return nil, census, err
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, rerr := os.ReadFile(path) // #nosec G304 -- имя пришло из обхода ЭТОГО дерева, подставить посторонний файл извне нечем
		if rerr != nil {
			return nil, census, rerr
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
		if perr != nil {
			return nil, census, perr
		}
		census.FilesRead++
		rel := filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator)))

		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CompositeLit:
				id, ok := node.Type.(*ast.Ident)
				if !ok || id.Name != rulePolicyTypeName {
					return true
				}
				census.Literals++
				sites = append(sites, RulePolicySite{
					File: rel, Line: fset.Position(node.Pos()).Line,
					Kind: "литерал", Snippet: rulePolicyTypeName + "{…}",
				})
			case *ast.FuncDecl:
				if node.Recv != nil || node.Name == nil || node.Name.Name != rulePolicyDeriverName {
					return true
				}
				census.Derivers++
				sites = append(sites, RulePolicySite{
					File: rel, Line: fset.Position(node.Pos()).Line,
					Kind: "вывод", Snippet: "func " + rulePolicyDeriverName,
				})
			}
			return true
		})
	}
	return sites, census, nil
}

// RulePolicyFindings — предикат находки. Тот же зовёт инъекция.
func RulePolicyFindings(sites []RulePolicySite, census RulePolicyCensus) []string {
	var out []string
	for _, s := range sites {
		if s.Kind == "литерал" && s.File != rulePolicyHomeFile {
			out = append(out, s.File+":"+strconv.Itoa(s.Line)+
				": политика подстановки собирается ВТОРЫМ местом ("+s.Snippet+"). "+
				"Решение «системная ли роль для целей подстановки» принимает "+
				rulePolicyDeriverName+" в "+rulePolicyHomeFile+
				"; второе объявление разойдётся с первым молча — на законном входе "+
				"оба отвечают одинаково")
		}
	}
	if census.Derivers != 1 {
		out = append(out, "объявлений "+rulePolicyDeriverName+" — "+strconv.Itoa(census.Derivers)+
			", а обязано быть ровно одно: перечень политик закрыт ровно тем, что вывод у него один")
	}
	return out
}
