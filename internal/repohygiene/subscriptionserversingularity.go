// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionserversingularity.go — анализатор «сервер потока подписки в дереве
// ОДИН, и он в фундаменте».
//
// # Предмет
//
// Решение владельца: формат подписки один на всю платформу, определённый в
// `pkg/` как переиспользуемый механизм. Второй сервер заводится не злым умыслом:
// домену нужна подписка, общий сервер чем-то не подошёл, и рядом появляется свой
// — со своим курсором, своим пределом потоков и своим порядком отказов. Через
// семь доменов их семь, а «единый механизм» остаётся названием.
//
// Заметить это на обзоре нельзя: каждая отдельная реализация защитима, а сходство
// видно только сплошной переписью. Один раз это уже случилось — сервер потока был
// написан ДВАЖДЫ, предметно, в двух разных сервисах, и ни у одного не оказалось
// потребителя.
//
// # Что он считает — ДВЕ половины, и обе точны
//
//  1. ПОТОКОВЫЕ ГЛАГОЛЫ КОНТРАКТА. Их обязано быть ровно столько, сколько стоит
//     в ведомости послаблений, плюс один общий. Ноль означает «форма объявлена,
//     а взять её нечем»; два — второй язык подписки.
//  2. СЕРВЕРЫ ЭТОГО ГЛАГОЛА В ПРОД-КОДЕ. Тип, несущий метод общего глагола,
//     обязан быть один, и его файл — в `pkg/`. Второй — находка, ГДЕ БЫ ОН НИ
//     ЛЕЖАЛ, включая сам `pkg/`: два сервера в фундаменте — то же расхождение,
//     только ближе.
//
// # ЧЕГО ОН НЕ СУДИТ, и это сказано, чтобы его не «починили» в эвристику
//
// Он НЕ судит всякое употребление `LISTEN`. Постановка задачи требовала именно
// этого («чтение канала вне `pkg/` — находка»), и предикат был БЫ НЕВЕРЕН в обе
// стороны: пробуждение по каналу — общий механизм, которым пользуются дренаж
// очереди и сброс окна вердиктов, а они подпиской не являются. Замер на дереве
// фазы: исполнителей `LISTEN` в прод-коде ЧЕТЫРЕ, из них подписку обслуживал
// ОДИН, и тот мёртв (читатель снятой формы у nlb, kacho#1043).
//
// Текстовые дискриминаторы «журнал плюс курсорное чтение» тоже проверены и
// отвергнуты: на файловой зернистости они дают семь ложных находок из восьми
// (файл, где имя журнала стоит в комментарии, а курсорное чтение — у соседнего,
// ни к чему не относящегося списка). Инструмент, у которого почти все находки
// ложные, перестают читать; перестав читать, возвращаются к предикату, который
// зелен всегда.
//
// Поэтому здесь судится ТО, ЧТО ЕСТЬ ПРЕДМЕТ РЕШЕНИЯ, — глагол и его сервер, — а
// граница названа вслух вместо того, чтобы её маскировать.
//
// # Ведомость послаблений и её самоистечение
//
// Частный потоковый глагол, ещё не переведённый на общую форму, стоит в ведомости
// с причиной и предикатом истечения. Запись, которой в дереве больше нечего
// исключать, — САМА НАХОДКА: иначе она переживёт своё снятие и разрешит
// следующему завести свой поток под тем же именем.
//
// Падает анализатор на ПУСТОМ ОБХОДЕ: ноль прочитанных файлов контракта или ноль
// прочитанных файлов прод-кода — тогда «ноль находок» неотличимо от «ноль
// прочитанного».
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SubscriptionServerHome — каталог, в котором обязан лежать единственный сервер.
const SubscriptionServerHome = "pkg/"

// subscriptionStreamMethod — имя метода общего глагола.
const subscriptionStreamMethod = "Subscribe"

// subscriptionStreamServerSuffix — имя сгенерённого типа потока сервера. По нему
// метод общего глагола отличается от всякого другого метода `Subscribe` в дереве
// (их бывает много, и они не про подписку).
const subscriptionStreamServerSuffix = "InternalSubscriptionService_SubscribeServer"

// streamRPCRe — потоковый глагол в контракте. Комментарии вычищаются ДО поиска:
// слово `stream` встречается в объяснениях чаще, чем в объявлениях.
var streamRPCRe = regexp.MustCompile(`(?m)^\s*rpc\s+([A-Za-z0-9_]+)\s*\([^)]*\)\s*returns\s*\(\s*stream\s`)

// SubscriptionStreamAllowance — одно послабление: частный потоковый глагол,
// который ещё не переведён на общую форму.
type SubscriptionStreamAllowance struct {
	// Method — имя глагола, как оно объявлено в контракте.
	Method string
	// File — файл контракта относительно корня.
	File string
	// Because — причина и предикат истечения. Пустая причина объявлением не
	// является: закрыть глаз стало бы дешевле, чем перевести домен.
	Because string
}

// SubscriptionServerOptions — вход анализатора.
type SubscriptionServerOptions struct {
	Root      string
	ProtoRoot string
	// GoRoots — каталоги прод-кода, в которых ищется сервер.
	GoRoots []string
	Allow   []SubscriptionStreamAllowance
}

// SubscriptionServerCensus — объём осмотренного. Печатается ВСЕГДА.
type SubscriptionServerCensus struct {
	ProtoFiles  int
	StreamRPCs  int
	GoFiles     int
	ServerImpls int
	Expected    int
	Allowances  int
}

// SubscriptionServerFinding — одна находка.
type SubscriptionServerFinding struct {
	Kind  string
	Where string
	What  string
}

func (f SubscriptionServerFinding) String() string {
	return fmt.Sprintf("[%s] %s — %s", f.Kind, f.Where, f.What)
}

// AuditSubscriptionServerSingularity судит дерево.
func AuditSubscriptionServerSingularity(
	o SubscriptionServerOptions, log io.Writer,
) ([]SubscriptionServerFinding, SubscriptionServerCensus, error) {
	var census SubscriptionServerCensus
	census.Allowances = len(o.Allow)
	census.Expected = 1 + len(o.Allow)

	protoFiles, err := collectFiles(filepath.Join(o.Root, o.ProtoRoot), ".proto")
	if err != nil {
		return nil, census, err
	}
	census.ProtoFiles = len(protoFiles)

	declared := map[string]string{} // глагол → файл
	for _, path := range protoFiles {
		// #nosec G304 -- путь получен обходом каталога контракта ЭТОГО дерева, не извне
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, census, rerr
		}
		rel, _ := filepath.Rel(o.Root, path)
		for _, m := range streamRPCRe.FindAllStringSubmatch(stripProtoComments(string(raw)), -1) {
			declared[m[1]] = filepath.ToSlash(rel)
		}
	}
	census.StreamRPCs = len(declared)

	var goFiles []string
	for _, root := range o.GoRoots {
		files, ferr := collectFiles(filepath.Join(o.Root, root), ".go")
		if ferr != nil {
			return nil, census, ferr
		}
		goFiles = append(goFiles, files...)
	}
	var impls []string
	for _, path := range goFiles {
		rel, _ := filepath.Rel(o.Root, path)
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(rel, "_test.go") || strings.HasPrefix(rel, "pkg/api/") {
			continue
		}
		census.GoFiles++
		has, herr := fileImplementsSubscribe(path)
		if herr != nil {
			return nil, census, herr
		}
		if has {
			impls = append(impls, rel)
		}
	}
	sort.Strings(impls)
	census.ServerImpls = len(impls)

	_, _ = fmt.Fprintf(log, "осмотрено: файлов контракта %d · потоковых глаголов %d · файлов прод-кода Go %d · серверов глагола %d · послаблений %d\n",
		census.ProtoFiles, census.StreamRPCs, census.GoFiles, census.ServerImpls, census.Allowances)

	if census.ProtoFiles == 0 || census.GoFiles == 0 {
		return nil, census, fmt.Errorf(
			"обход пуст: файлов контракта %d, файлов прод-кода %d — «ноль находок» неотличимо от «ноль прочитанного»",
			census.ProtoFiles, census.GoFiles)
	}

	allowed := make(map[string]SubscriptionStreamAllowance, len(o.Allow))
	var findings []SubscriptionServerFinding
	for _, a := range o.Allow {
		if a.Because == "" {
			findings = append(findings, SubscriptionServerFinding{
				Kind: "ПОСЛАБЛЕНИЕ-БЕЗ-ПРИЧИНЫ", Where: a.Method,
				What: "запись ведомости без причины и предиката истечения объявлением не является",
			})
			continue
		}
		allowed[a.Method] = a
		if _, ok := declared[a.Method]; !ok {
			findings = append(findings, SubscriptionServerFinding{
				Kind: "ПОСЛАБЛЕНИЕ-ИСТЕКЛО", Where: a.Method,
				What: "глагола нет в контракте — записи больше нечего исключать, снимите её",
			})
		}
	}

	names := make([]string, 0, len(declared))
	for name := range declared {
		names = append(names, name)
	}
	sort.Strings(names)

	var common int
	for _, name := range names {
		if _, ok := allowed[name]; ok {
			continue
		}
		if name == subscriptionStreamMethod {
			common++
			continue
		}
		findings = append(findings, SubscriptionServerFinding{
			Kind: "ВТОРОЙ-ГЛАГОЛ", Where: declared[name],
			What: fmt.Sprintf("потоковый глагол %q вне ведомости послаблений — второй язык подписки", name),
		})
	}
	if common == 0 {
		findings = append(findings, SubscriptionServerFinding{
			Kind: "ГЛАГОЛА-НЕТ", Where: "proto",
			What: "общего потокового глагола не объявлено — форма подписки объявлена, а взять её нечем",
		})
	}

	switch {
	case len(impls) == 0:
		findings = append(findings, SubscriptionServerFinding{
			Kind: "СЕРВЕРА-НЕТ", Where: "prod",
			What: "ни один тип прод-кода не реализует общий глагол — глагол объявлен, сервера нет",
		})
	default:
		for _, rel := range impls {
			if !strings.HasPrefix(rel, SubscriptionServerHome) {
				findings = append(findings, SubscriptionServerFinding{
					Kind: "СЕРВЕР-НЕ-В-ФУНДАМЕНТЕ", Where: rel,
					What: "сервер потока обязан жить в " + SubscriptionServerHome + ", а не в сервисе",
				})
			}
		}
		if len(impls) > 1 {
			findings = append(findings, SubscriptionServerFinding{
				Kind: "ВТОРОЙ-СЕРВЕР", Where: strings.Join(impls, ", "),
				What: fmt.Sprintf("серверов общего глагола %d — механизм обязан быть один", len(impls)),
			})
		}
	}

	return findings, census, nil
}

// fileImplementsSubscribe — несёт ли файл метод ОБЩЕГО глагола.
//
// Разбор по узлу, а не по тексту: метод `Subscribe` в дереве не один, и отличает
// нужный ИМЕННО ТИП ПОТОКА во втором параметре — имя, которое производит
// генератор и которое нельзя написать случайно.
func fileImplementsSubscribe(path string) (bool, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		// Файл, который не разбирается, судить нечем — и молчать о нём нельзя.
		return false, fmt.Errorf("разбор %s: %w", path, err)
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != subscriptionStreamMethod {
			return true
		}
		if fn.Type.Params == nil || len(fn.Type.Params.List) != 2 {
			return true
		}
		if strings.HasSuffix(typeName(fn.Type.Params.List[1].Type), subscriptionStreamServerSuffix) {
			found = true
		}
		return true
	})
	return found, nil
}

// typeName — имя типа выражения без пакета и указателей.
func typeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return typeName(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name
	default:
		return ""
	}
}

// collectFiles — файлы с нужным расширением под корнем.
func collectFiles(root, ext string) ([]string, error) {
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ext {
			out = append(out, path)
		}
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return out, err
}
