// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestBackendPortIsReachableOnlyFromTheReconciler — порт плоскости данных
// достижим ТОЛЬКО из сверщика и его адаптера.
//
// # Что этот гейт охраняет на самом деле
//
// Не чистоту слоёв. Он охраняет предпосылку разрешителя осиротевших операций:
// «writer-TX атомарна, частичных состояний нет». Разрешитель судит по НАШЕЙ базе —
// ресурс есть, значит работа доведена, — и это верно ровно до тех пор, пока предмет
// операции есть запись строки, а не провижининг у бэкенда.
//
// Перенеси обращение к бэкенду в функцию операции — и предпосылка станет ложной
// молча: длинное выделение упрётся в потолок исполнителя операций, функцию убьют, а
// через окно ожидания разрешитель прочитает строку, увидит ресурс и объявит клиенту
// «готово» при ОТСУТСТВУЮЩЕМ объекте. Ни одна проба при этом не покраснеет: и строка
// на месте, и операция завершена успехом. Ошибка обнаружится у арендатора.
//
// Поэтому запрет держится обходом дерева, а не комментарием: комментарий переживёт
// первый же вызов, добавленный «чтобы дождаться готовности прямо здесь».
//
// # Что РАЗРЕШЕНО и почему
//
// Слой use-case вправе ссылаться на пакет порта ради вывода ИМЕНИ объекта: это
// чистая функция от идентификатора, она никуда не ходит и никакой работы не длит.
// Запрещено держать сам ПОРТ — тип, через который делается вызов.
func TestBackendPortIsReachableOnlyFromTheReconciler(t *testing.T) {
	const portType = "blockbackend.Backend"

	// Кому порт держать позволено — и почему именно им.
	allowed := map[string]string{
		"internal/reconciler": "сверщик и есть единственный исполнитель обращений к бэкенду",
		"internal/clients":    "адаптер реализует порт — это его предмет",
		"internal/blockbackend": "объявление порта, его дублёр и контрактная суита живут " +
			"в самом пакете",
	}

	root := serviceRoot(t)
	var (
		scannedFiles int
		scannedDirs  = map[string]struct{}{}
		findings     []string
	)

	err := filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		dir := filepath.Dir(rel)
		scannedFiles++
		scannedDirs[dir] = struct{}{}

		for prefix := range allowed {
			if dir == prefix || strings.HasPrefix(dir, prefix+string(filepath.Separator)) {
				return nil
			}
		}

		// Разбор AST, а не поиск по тексту: имя порта встречается в комментариях,
		// объясняющих ЭТОТ ЖЕ запрет, и текстовый поиск нашёл бы их первыми.
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			return perr
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "blockbackend" || sel.Sel.Name != "Backend" {
				return true
			}
			findings = append(findings, rel+":"+fset.Position(sel.Pos()).String())
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("обход дерева не выполнен: %v", err)
	}

	// Объём осмотренного печатается всегда: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного» — обход, сломавшийся на первом каталоге, иначе выглядел бы
	// подтверждением запрета.
	t.Logf("осмотрено: файлов %d, каталогов %d, разрешённых слоёв %d",
		scannedFiles, len(scannedDirs), len(allowed))
	if scannedFiles == 0 {
		t.Fatal("обход не прочитал ни одного файла — гейт ничего не утверждает")
	}

	// Проверка предпосылки: разрешённые слои обязаны СУЩЕСТВОВАТЬ. Исчезнувший
	// сверщик оставил бы запрет формально выполненным и полностью бессмысленным.
	for prefix := range allowed {
		if _, err := os.Stat(filepath.Join(root, prefix)); err != nil {
			t.Fatalf("разрешённый слой %s отсутствует: запрет охраняет предмет, которого нет (%v)", prefix, err)
		}
	}

	sort.Strings(findings)
	for _, f := range findings {
		t.Errorf("порт %s достижим из %s. Предмет операции — запись строки, а не провижининг: "+
			"обращение к бэкенду из слоя, исполняющего операции, делает предпосылку разрешителя "+
			"осиротевших операций ложной, и он объявит «готово» при отсутствующем объекте", portType, f)
	}
}

// serviceRoot возвращает корень сервиса относительно этого пакета.
func serviceRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("рабочий каталог не определён: %v", err)
	}
	// internal/check → корень сервиса на два уровня выше.
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}
