// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consoledropdownoption_test.go — гейт: вариант выпадающего списка выбирается
// ПО КЛАССУ ВАРИАНТА, а не по тексту внутри списка.
//
// # Предмет
//
// Список выбора рисует рядом с настоящими вариантами их НЕВИДИМОЕ ЗЕРКАЛО для
// доступности. Смотреть надо в исходник библиотеки, а не в разметку страницы:
// `@rc-component/select`, `OptionList`, ветвь `virtual` —
//
//	<div role="listbox" style="height:0;width:0;overflow:hidden">
//	  … три <div role="option" id="…_list_N" aria-label="<подпись>">значение</div>
//
// У зеркала НЕТ класса варианта, его текст — ЗНАЧЕНИЕ, подпись лежит в
// `aria-label`, и в дереве оно стоит РАНЬШЕ настоящего списка. Поэтому «первый
// совпавший по тексту внутри списка» попадает именно в зеркало.
//
// # Почему это стоит гейта, а не внимания
//
// Промах не виден ни в обзоре (запись выглядит совершенно обычной), ни на
// красном (щелчок не падает — он ЖДЁТ видимости у элемента нулевого размера,
// то есть свойства, которого у того нет by construction). Наблюдалось на
// прогоне 31988819931: три пробы одного файла съели по 240 с каждая, вместе с
// ними — весь бюджет шага, и отчёта не осталось ВООБЩЕ. Вердикта не получила ни
// одна из 28 проб: одна опечатка в селекторе перевела всю суиту в третью
// категорию исхода («не выполнилось», `e2e-flow.md` §1).
//
// # Что гейт держит
//
//	ФОРМА     исходник сквозных проб, чьи СТРОКОВЫЕ ЛИТЕРАЛЫ называют список
//	          (`ant-select-dropdown`), обязан называть и класс варианта
//	          (`ant-select-item-option`).
//	ПЕРЕПИСЬ  сколько файлов прочитано, сколько из них трогают список. «Ноль
//	          находок» обязано быть отличимо от «ноль прочитанного», а пустой
//	          корпус — поломка обхода, а не чистота.
//
// # Комментарий под гейт не подпадает — и это проверено, а не заявлено
//
// Разбор идёт по `tsScan`: комментарии удалены, и рассматриваются ТОЛЬКО
// строковые литералы. Иначе объяснение этого самого класса, написанное рядом с
// починкой, само стало бы находкой (`testing.md` §«Гейт на класс», п. 4).
//
// # Чего гейт НЕ держит, и это сказано, а не умолчано
//
// Он рассуждает о ФАЙЛЕ, а не о цепочке: файл, называющий оба класса, но
// применивший их порознь, гейт пропустит. Более точный разбор потребовал бы
// сшивать литералы с местом вызова, а `tsScan` их намеренно разделяет. Граница
// названа потому, что предмет запрета — не стиль цепочки, а ЗНАНИЕ о зеркале:
// файл, который о классе варианта не знает вовсе, — вот кого ловит гейт.
//
// # Способность упасть
//
// Доказана инъекцией в обе стороны — `consoledropdownoption_injection_test.go`.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

const (
	// consoleDropdownClass — контейнер открытого списка выбора.
	consoleDropdownClass = "ant-select-dropdown"
	// consoleOptionClass — класс НАСТОЯЩЕГО варианта. Зеркало доступности его
	// не несёт: у зеркала классов нет вовсе.
	consoleOptionClass = "ant-select-item-option"
)

// consoleProbeDropdownFinding — файл, знающий про список и не знающий про вариант.
type consoleProbeDropdownFinding struct {
	File string
}

// consoleProbeSourcesUnder — исходники сквозных проб консоли из ИНДЕКСА дерева.
//
// Состав берётся у индекса, а не обходом диска: обход увидел бы установленные
// пакеты и чужие рабочие копии, а гейт обходчиков требует единого источника
// состава.
func consoleProbeSourcesUnder(root string) ([]string, error) {
	return treecorpus.UnderWithSuffix(filepath.Join(root, "ui-future", "e2e"), ".ts")
}

// consoleDropdownFindings — разбор корпуса. Возвращает находки и объём осмотренного.
func consoleDropdownFindings(files []string) ([]consoleProbeDropdownFinding, int, int, error) {
	var found []consoleProbeDropdownFinding
	read, touching := 0, 0
	for _, path := range files {
		blob, err := os.ReadFile(path)
		if err != nil {
			return nil, read, touching, err
		}
		read++
		_, literals := tsScan(string(blob))
		joined := strings.Join(literals, "\n")
		if !strings.Contains(joined, consoleDropdownClass) {
			continue
		}
		touching++
		if strings.Contains(joined, consoleOptionClass) {
			continue
		}
		found = append(found, consoleProbeDropdownFinding{File: path})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].File < found[j].File })
	return found, read, touching, nil
}

func TestConsoleProbePicksADropdownOptionByItsOptionClass(t *testing.T) {
	root := repoRoot(t)
	files, err := consoleProbeSourcesUnder(root)
	if err != nil {
		t.Fatalf("состав исходников сквозных проб консоли: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("корпус сквозных проб консоли ПУСТ — обход сломан либо каталог переехал; " +
			"молчаливый успех здесь означал бы «проверено», не прочитав ни одного файла")
	}

	found, read, touching, err := consoleDropdownFindings(files)
	if err != nil {
		t.Fatalf("чтение исходника пробы: %v", err)
	}

	t.Logf("перепись: прочитано исходников %d, трогают список выбора %d, находок %d",
		read, touching, len(found))

	for _, f := range found {
		rel, rerr := filepath.Rel(root, f.File)
		if rerr != nil {
			rel = f.File
		}
		t.Errorf("%s: проба открывает %q и НЕ называет %q — «первый совпавший по тексту» "+
			"попадёт в невидимое зеркало варианта (`role=\"option\"` нулевого размера, "+
			"без класса, в дереве раньше настоящего списка), и щелчок будет ждать у него "+
			"видимости до конца бюджета пробы, унося вердикт всей суиты",
			rel, "."+consoleDropdownClass, "."+consoleOptionClass)
	}
}

// consoleDropdownFindingNames — имена находок, для инъекции.
func consoleDropdownFindingNames(found []consoleProbeDropdownFinding) string {
	names := make([]string, 0, len(found))
	for _, f := range found {
		names = append(names, filepath.Base(f.File))
	}
	return fmt.Sprint(names)
}
