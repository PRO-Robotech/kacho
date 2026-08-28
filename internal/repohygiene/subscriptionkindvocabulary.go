// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionkindvocabulary.go — анализатор «у вида предмета подписки ОДНО
// написание, и берётся оно у производителя».
//
// # Предмет
//
// Ось отбора по видам объявлена контрактом подписки, словарь видов объявлен
// принадлежащим владельцу и закрытым. Пока написание вида не привязано к
// единственному производителю, у одного предмета их столько, сколько у него
// авторов. Перепись на день заведения этого анализатора (два владельца журнала,
// четыре предмета) дала по ТРИ написания на каждый предмет:
//
//	предмет         слово журнала        тип объекта                  строка права
//	машина          Instance             compute_instance             compute.instances.list
//	балансировщик   nlb_load_balancer    nlb_network_load_balancer     loadbalancer.networkLoadBalancers.list
//	слушатель       nlb_listener         nlb_listener                 loadbalancer.listeners.list
//	группа целей    nlb_target_group     nlb_target_group             loadbalancer.targetGroups.list
//
// Две строки из четырёх совпадают в первых двух колонках — и это худшее, что
// могло случиться с наблюдаемостью класса: расхождение видно ровно у половины
// предметов, а на глаз выглядит как разнобой, который «где-то же совпадает».
//
// Решение: клиенту едет ТИП ОБЪЕКТА модели прав — единственное платформенное имя
// предмета, которым уже спрашивают о видимости строки и которое уже стоит в
// аннотациях контрактов домена. Слово журнала остаётся ключом словаря и наружу
// не выходит. Второе написание завести НЕКУДА: поля под него в объявлении
// владельца нет.
//
// # Что судит анализатор — ДВЕ половины
//
//  1. ОТКУДА ВЗЯТО СЛОВО. В объявлении журнала (`subscription.Mapping{Kinds: …}`)
//     тип объекта и действие обязаны быть КВАЛИФИЦИРОВАННЫМ ИМЕНЕМ чужого пакета
//     (`authzfilter.ResourceTypeInstance`), а не строковым литералом и не голым
//     именем своего пакета. Литерал есть второе написание чужого словаря, и
//     расходится оно молча: строка перестаёт доставляться, вопрос о видимости
//     уходит про несуществующий тип — без отказа и без пропуска в нумерации.
//     Голое имя своего пакета — та же копия, только с лишним шагом.
//
//  2. ОБЪЯВЛЕН ЛИ ТАКОЙ ТИП ВООБЩЕ. Тип объекта, попавший в словарь видов,
//     обязан встречаться `object_type:` хотя бы в одной аннотации контракта.
//     Иначе владелец объявил вид, которого платформа не знает, — и поток по нему
//     не доставит ничего, оставаясь «зелёным».
//
// # ЧЕГО ОН НЕ СУДИТ, и это названо, чтобы его не «починили» в эвристику
//
// Он НЕ сверяет тип объекта со строкой права. Проверено и отвергнуто ЗАМЕРОМ, а
// не по вкусу: `nlb_network_load_balancer` против
// `loadbalancer.networkLoadBalancers.list` расходятся уже в первом сегменте
// (`nlb_` против `loadbalancer.`), а хвост у одного змеиный, у другого
// верблюжий. Ни одно из двух не выводится из другого ни в какую сторону, и
// предикат, который это «сверял бы», был бы неверен на всей популяции nlb.
//
// Причина в том, что это РАЗНЫЕ вопросы, а не два написания одного: тип объекта
// отвечает «что это за предмет», строка права — «какой глагол над ним просят».
// Свести их в один словарь означало бы переименовать каталог прав целиком, и это
// не предмет подписки.
//
// # Падает на ПУСТОМ ОБХОДЕ
//
// Ноль прочитанных файлов Go, ноль найденных объявлений журнала либо ноль
// объявленных типов объекта в контрактах — «ноль находок» неотличимо от «ноль
// прочитанного».
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// subscriptionPkgPath — импорт, по которому опознаётся общая форма подписки.
// Опознание идёт по ПУТИ, а не по имени пакета: имя переименовывается алиасом, и
// анализатор, ключующийся на нём, ослеп бы от одной строки импорта.
const subscriptionPkgPath = "github.com/PRO-Robotech/kacho/pkg/subscription"

// Имена полей объявления владельца, которые обязаны нести чужое имя.
const (
	kindFieldObjectType = "ObjectType"
	kindFieldAction     = "Action"
	mappingFieldKinds   = "Kinds"
	mappingTypeName     = "Mapping"
)

// Виды находок.
const (
	// KindVocabularyLiteral — слово выписано на месте, а не взято у производителя.
	KindVocabularyLiteral = "KIND-VOCABULARY-LITERAL"
	// KindVocabularyLocal — слово взято у СВОЕГО пакета: та же копия, шагом дальше.
	KindVocabularyLocal = "KIND-VOCABULARY-LOCAL"
	// KindVocabularyUndeclared — типа объекта не знает ни одна аннотация контракта.
	KindVocabularyUndeclared = "KIND-VOCABULARY-UNDECLARED"
	// KindVocabularyUnresolved — имя взято у производителя, но значение не
	// добылось: тогда вторая половина вердикта не вынесена, и молчать об этом
	// нельзя — «не читали» неотличимо от «чисто».
	KindVocabularyUnresolved = "KIND-VOCABULARY-UNRESOLVED"
)

// objectTypeAnnotationRe — объявление типа объекта в аннотации контракта.
var objectTypeAnnotationRe = regexp.MustCompile(`object_type:\s*"([a-z0-9_]+)"`)

// SubscriptionKindOptions — вход анализатора.
type SubscriptionKindOptions struct {
	Root      string
	ProtoRoot string
	// GoRoots — каталоги прод-кода, в которых ищутся объявления журналов.
	GoRoots []string
}

// SubscriptionKindCensus — объём осмотренного. Печатается ВСЕГДА.
type SubscriptionKindCensus struct {
	// Root — корень дерева. Живёт в переписи, а не отдельным аргументом, чтобы
	// разрешение чужих констант шло от того же корня, от которого шёл обход.
	Root            string
	ProtoFiles      int
	DeclaredTypes   int
	GoFiles         int
	JournalMappings int
	KindEntries     int
	ObjectTypesUsed int
}

// SubscriptionKindFinding — одна находка.
type SubscriptionKindFinding struct {
	Kind  string
	Where string
	What  string
}

func (f SubscriptionKindFinding) String() string {
	return fmt.Sprintf("[%s] %s — %s", f.Kind, f.Where, f.What)
}

// AuditSubscriptionKindVocabulary судит дерево.
func AuditSubscriptionKindVocabulary(
	o SubscriptionKindOptions, log io.Writer,
) ([]SubscriptionKindFinding, SubscriptionKindCensus, error) {
	var census SubscriptionKindCensus
	census.Root = o.Root

	declared, protoFiles, err := collectDeclaredObjectTypes(filepath.Join(o.Root, o.ProtoRoot))
	if err != nil {
		return nil, census, err
	}
	census.ProtoFiles = protoFiles
	census.DeclaredTypes = len(declared)

	var findings []SubscriptionKindFinding
	used := map[string]struct{}{}

	for _, root := range o.GoRoots {
		files, ferr := collectFiles(filepath.Join(o.Root, root), ".go")
		if ferr != nil {
			return nil, census, ferr
		}
		for _, path := range files {
			rel, _ := filepath.Rel(o.Root, path)
			rel = filepath.ToSlash(rel)
			if strings.HasSuffix(rel, "_test.go") || strings.HasPrefix(rel, "pkg/api/") {
				continue
			}
			census.GoFiles++
			fs, ferr := auditOneFileForKindVocabulary(path, rel, declared, &census, used)
			if ferr != nil {
				return nil, census, ferr
			}
			findings = append(findings, fs...)
		}
	}
	census.ObjectTypesUsed = len(used)

	_, _ = fmt.Fprintf(log,
		"осмотрено: файлов контракта %d · типов объекта объявлено %d · файлов прод-кода Go %d · объявлений журнала %d · записей вида %d · типов объекта в словарях %d\n",
		census.ProtoFiles, census.DeclaredTypes, census.GoFiles,
		census.JournalMappings, census.KindEntries, census.ObjectTypesUsed)

	switch {
	case census.GoFiles == 0:
		return nil, census, fmt.Errorf(
			"обход пуст: файлов прод-кода Go 0 — «ноль находок» неотличимо от «ноль прочитанного»")
	case census.DeclaredTypes == 0:
		return nil, census, fmt.Errorf(
			"в контрактах не найдено ни одного объявления типа объекта — вторая половина вердикта беспредметна")
	case census.JournalMappings == 0:
		return nil, census, fmt.Errorf(
			"объявлений журнала подписки 0 при %d прочитанных файлах Go — разбор сломан либо форма объявления сменилась",
			census.GoFiles)
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Where != findings[j].Where {
			return findings[i].Where < findings[j].Where
		}
		return findings[i].What < findings[j].What
	})
	return findings, census, nil
}

// collectDeclaredObjectTypes собирает типы объекта, объявленные аннотациями
// контрактов, и число прочитанных файлов.
func collectDeclaredObjectTypes(protoRoot string) (map[string]struct{}, int, error) {
	files, err := collectFiles(protoRoot, ".proto")
	if err != nil {
		return nil, 0, err
	}
	out := map[string]struct{}{}
	for _, path := range files {
		// #nosec G304 -- путь получен обходом каталога контракта ЭТОГО дерева, не извне
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, 0, rerr
		}
		for _, m := range objectTypeAnnotationRe.FindAllStringSubmatch(stripProtoComments(string(raw)), -1) {
			out[m[1]] = struct{}{}
		}
	}
	return out, len(files), nil
}

// auditOneFileForKindVocabulary судит один файл прод-кода.
func auditOneFileForKindVocabulary(
	path, rel string,
	declared map[string]struct{},
	census *SubscriptionKindCensus,
	used map[string]struct{},
) ([]SubscriptionKindFinding, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		// Файл, который не разбирается, — не «чистый»: он просто не прочитан.
		return nil, fmt.Errorf("%s: не разобрался: %w", rel, err)
	}

	local, ok := subscriptionPkgAlias(f)
	if !ok {
		return nil, nil
	}
	imports := importAliases(f)

	var findings []SubscriptionKindFinding
	var walkErr error
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || !isQualifiedType(lit.Type, local, mappingTypeName) {
			return true
		}
		census.JournalMappings++
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != mappingFieldKinds {
				continue
			}
			kinds, ok := kv.Value.(*ast.CompositeLit)
			if !ok {
				continue
			}
			fs, ferr := auditKindsMap(kinds, fset, rel, imports, declared, census, used)
			if ferr != nil {
				walkErr = ferr
				return false
			}
			findings = append(findings, fs...)
		}
		return true
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return findings, nil
}

// auditKindsMap судит содержимое словаря видов.
func auditKindsMap(
	kinds *ast.CompositeLit,
	fset *token.FileSet,
	rel string,
	imports map[string]string,
	declared map[string]struct{},
	census *SubscriptionKindCensus,
	used map[string]struct{},
) ([]SubscriptionKindFinding, error) {
	var findings []SubscriptionKindFinding
	for _, entry := range kinds.Elts {
		kv, ok := entry.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		binding, ok := kv.Value.(*ast.CompositeLit)
		if !ok {
			continue
		}
		census.KindEntries++
		where := fmt.Sprintf("%s:%d", rel, fset.Position(entry.Pos()).Line)

		for _, field := range binding.Elts {
			fkv, ok := field.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			name, ok := fkv.Key.(*ast.Ident)
			if !ok || (name.Name != kindFieldObjectType && name.Name != kindFieldAction) {
				continue
			}
			switch value := fkv.Value.(type) {
			case *ast.SelectorExpr:
				if name.Name != kindFieldObjectType {
					continue
				}
				// Имя разрешается ДО ЗНАЧЕНИЯ, а не засчитывается за него.
				// Иначе вторая половина вердикта была бы вакуумной ровно на
				// зелёном дереве: у правильно объявленного владельца литералов
				// нет вовсе, значит нечего было бы сверять с аннотациями, и
				// «находок ноль» получалось бы даром.
				word, ok, rerr := resolveQualifiedConst(value, imports, census.Root)
				if rerr != nil {
					return nil, rerr
				}
				if !ok {
					findings = append(findings, SubscriptionKindFinding{
						Kind:  KindVocabularyUnresolved,
						Where: where,
						What: fmt.Sprintf(
							"%s взят у %s, но значение константы разбором не добылось — "+
								"проверить, объявлен ли такой тип платформой, НЕЧЕМ, "+
								"и молчание здесь означало бы «не читали», а не «чисто»",
							name.Name, kindVocabularySelectorText(value)),
					})
					continue
				}
				used[word] = struct{}{}
				findings = append(findings,
					judgeObjectTypeWord(word, where, name.Name, declared)...)
			case *ast.BasicLit:
				if value.Kind != token.STRING {
					continue
				}
				word, err := strconv.Unquote(value.Value)
				if err != nil {
					word = value.Value
				}
				findings = append(findings, SubscriptionKindFinding{
					Kind:  KindVocabularyLiteral,
					Where: where,
					What: fmt.Sprintf(
						"%s = %q выписан ЛИТЕРАЛОМ. Это второе написание чужого словаря, и расходится оно молча: "+
							"строка перестаёт доставляться, вопрос о видимости уходит про несуществующий тип — "+
							"ни отказа, ни пропуска в нумерации. Возьмите имя у производителя (`authzfilter`)",
						name.Name, word),
				})
				if name.Name == kindFieldObjectType {
					used[word] = struct{}{}
					findings = append(findings,
						judgeObjectTypeWord(word, where, name.Name, declared)...)
				}
			case *ast.Ident:
				findings = append(findings, SubscriptionKindFinding{
					Kind:  KindVocabularyLocal,
					Where: where,
					What: fmt.Sprintf(
						"%s взят у СВОЕГО пакета (%s) — это та же копия чужого словаря, только шагом дальше. "+
							"Возьмите имя прямо у производителя", name.Name, value.Name),
				})
			}
		}
	}
	return findings, nil
}

// subscriptionPkgAlias отдаёт локальное имя пакета общей формы подписки.
func subscriptionPkgAlias(f *ast.File) (string, bool) {
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != subscriptionPkgPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name, true
		}
		return "subscription", true
	}
	return "", false
}

// isQualifiedType отвечает, есть ли выражение `<pkg>.<name>`.
func isQualifiedType(expr ast.Expr, pkg, name string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
}

// judgeObjectTypeWord судит РАЗРЕШЁННОЕ слово: форму и объявленность.
//
// Обе половины нужны, и они ловят разное. Форма закрывает «написание одно»:
// `Instance` с заглавной есть слово хранилища, попавшее наружу. Объявленность
// закрывает «такой предмет платформе известен»: тип, которого нет ни в одной
// аннотации, даёт поток, не доставляющий ничего и остающийся зелёным.
func judgeObjectTypeWord(
	word, where, field string, declared map[string]struct{},
) []SubscriptionKindFinding {
	var out []SubscriptionKindFinding
	if !kindObjectTypeForm.MatchString(word) {
		out = append(out, SubscriptionKindFinding{
			Kind:  KindVocabularyShape,
			Where: where,
			What: fmt.Sprintf(
				"%s = %q написан не как имя типа модели прав (%s). Это слово едет КЛИЕНТУ, "+
					"и написание у него одно на всё дерево", field, word, kindObjectTypeForm),
		})
	}
	if _, ok := declared[word]; !ok {
		out = append(out, SubscriptionKindFinding{
			Kind:  KindVocabularyUndeclared,
			Where: where,
			What: fmt.Sprintf(
				"%s = %q не объявлен ни одной аннотацией контракта: платформа такого предмета не знает, "+
					"и поток по нему не доставит ничего, оставаясь зелёным", field, word),
		})
	}
	return out
}

// kindObjectTypeForm — то же написание, которое требует общий сервер при подъёме
// (`pkg/subscription`, `objectTypeForm`). Здесь оно судится по дереву, там — по
// объявлению: первое ловит правку, второе — поднявшийся процесс.
var kindObjectTypeForm = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// KindVocabularyShape — слово написано не как имя типа модели прав.
const KindVocabularyShape = "KIND-VOCABULARY-SHAPE"

// importAliases — карта «локальное имя пакета → путь импорта» для одного файла.
func importAliases(f *ast.File) map[string]string {
	out := make(map[string]string, len(f.Imports))
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if imp.Name != nil {
			out[imp.Name.Name] = p
			continue
		}
		out[path.Base(p)] = p
	}
	return out
}

// resolveQualifiedConst добывает ЗНАЧЕНИЕ константы `<pkg>.<Name>`, прочитав
// пакет-производитель.
//
// Читается пакет ЭТОГО модуля и только он: чужой модуль лежит вне дерева, и
// вердикт о нём был бы вердиктом о другом дереве. Не наш путь — не находка, а
// «не разрешилось»; исходы эти разные, и вызывающий их различает.
func resolveQualifiedConst(
	sel *ast.SelectorExpr, imports map[string]string, root string,
) (string, bool, error) {
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok || sel.Sel == nil {
		return "", false, nil
	}
	importPath, ok := imports[pkgIdent.Name]
	if !ok || !strings.HasPrefix(importPath, modulePathPrefix) {
		return "", false, nil
	}
	dir := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(importPath, modulePathPrefix)))
	files, err := collectFiles(dir, ".go")
	if err != nil {
		return "", false, err
	}
	want := sel.Sel.Name
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return "", false, fmt.Errorf("%s: не разобрался: %w", path, perr)
		}
		if value, found := constStringValue(f, want); found {
			return value, true, nil
		}
	}
	return "", false, nil
}

// modulePathPrefix — префикс импортов ЭТОГО модуля.
const modulePathPrefix = "github.com/PRO-Robotech/kacho/"

// constStringValue отдаёт строковое значение константы по её имени.
func constStringValue(f *ast.File, name string) (string, bool) {
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, id := range vs.Names {
				if id.Name != name || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return "", false
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					return "", false
				}
				return value, true
			}
		}
	}
	return "", false
}

// kindVocabularySelectorText — исходный вид выражения `<pkg>.<Name>` для сообщения находки.
func kindVocabularySelectorText(sel *ast.SelectorExpr) string {
	if id, ok := sel.X.(*ast.Ident); ok && sel.Sel != nil {
		return id.Name + "." + sel.Sel.Name
	}
	return "<выражение>"
}
