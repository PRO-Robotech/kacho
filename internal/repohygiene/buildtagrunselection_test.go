// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// buildtagrunselection_test.go — проба под признаком сборки обязана попадать в
// ОТБОР прогона, а не только в его область.
//
// # Предмет, и почему он отдельный от соседнего гейта
//
// Соседний `buildtagrunreach_test.go` утверждает, что ПАКЕТ под признаком
// покрыт объявленным прогоном, и честно называет свою границу: «он утверждает,
// что пакет попадает в отбор прогона, а не что внутри прогона исполнилась
// каждая проба. Сужение `-run` остаётся вне его предмета: это отдельное
// свойство и у него отдельный владелец». Владельца у этого свойства не было —
// им становится этот файл.
//
// Разница не педантская, и она измерена. Пакет `deploy` попадал в область
// объявленного прогона, поэтому соседний гейт был зелён и остаётся правым.
// Внутри же прогона стоял `-run`, называвший ОДНО имя из двух объявленных под
// признаком, — и вторая проба не исполнялась ни разу за свою жизнь. У неё не
// было вердикта вовсе: ни зелёного, ни красного. Она числилась держателем
// свойства, а документ, ссылавшийся на неё как на держателя, тем самым
// утверждал неправду о том, чем свойство удержано.
//
// Это третья категория исхода («не выполнилось»), поданная как покрытие, —
// и снаружи она неотличима от зелёного.
//
// # Почему отбор ВЫВОДИТСЯ из объявления
//
// Перечень имён, выписанный в гейте, разошёлся бы с конвейером молча и
// разошёлся бы именно там, где расхождение не видно: обе стороны отвечают
// «исполняется» на пробе, которая исполняется и так. Поэтому гейт читает
// объявление, достаёт из него `-run`/`-skip` и сверяет их с ИМЕНАМИ проб,
// взятыми разбором дерева. Заведут третью пробу под тем же признаком — гейт
// потребует её отбора сам, без правки гейта. Это и есть самоистечение: сужение
// живёт, пока перечисляет всё, что под признаком объявлено.
//
// # Чего гейт НЕ утверждает
//
//   - что прогон кто-то запускает: задание может существовать и не вызываться.
//     Это третий вопрос, и у него свой владелец
//     (`deploy/identity_chart_premise_reachability_test.go` — для тега
//     `helmcharts`);
//   - что проба ПРОХОДИТ: гейт судит отбор, а не вердикт;
//   - что отобраны вложенные пробы: `-run` делит образец по `/`, гейт читает
//     только верхний сегмент и перечисляет только пробы верхнего уровня.
package repohygiene

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// runSelects — отбирает ли прогон пробу с этим именем.
//
// Пустой `-run` означает «все», а не «никого»: так его читает `go test`, и
// читать иначе значило бы судить отбор, которого исполнитель не применяет.
func runSelects(run taggedRun, name string) bool {
	// Образец объявлен, но не скомпилировался — отбор НЕ доказан.
	//
	// Направление послабления здесь выбрано осознанно: «не смогли прочесть»
	// обязано давать находку, а не молчание. Прочти гейт нечитаемый образец как
	// «берёт всё», он зеленел бы ровно там, где объявление сломано, — то есть
	// на прогоне, который и сам-то не запустится.
	if run.RunPattern != "" && run.Select == nil {
		return false
	}
	if run.SkipPattern != "" && run.Skip == nil {
		return false
	}
	if run.Select != nil && !run.Select.MatchString(name) {
		return false
	}
	if run.Skip != nil && run.Skip.MatchString(name) {
		return false
	}
	return true
}

// tagSelectionFinding — проба под признаком, которую не отбирает ни один прогон.
type tagSelectionFinding struct {
	Func taggedTestFunc
	Runs []string // что рассматривалось — иначе находку нечем опровергнуть
}

func (f tagSelectionFinding) String() string {
	return fmt.Sprintf(
		"%s: проба %s объявлена под признаком сборки %q, но НИ ОДИН прогон её не ОТБИРАЕТ.\n"+
			"    рассмотрены прогоны, покрывающие пакет %s с этим признаком: %s",
		f.Func.coord(), f.Func.Name, f.Func.Tag, f.Func.Pkg, strings.Join(f.Runs, " · "))
}

type tagSelectionCensus struct {
	DeclarationFiles int
	RunsFound        int
	FilesScanned     int
	FilesWithTag     int
	FuncsUnderTag    int
	FuncsSelected    int
	NarrowingRuns    []string // прогоны, реально сужающие отбор
}

// String — перепись печатает ОБЕ величины: сколько проб под признаком и сколько
// из них отбирается. Одно число скрывает ровно тот случай, ради которого гейт
// заведён: «проб 19» молчит о том, что отбирается 18.
func (c tagSelectionCensus) String() string {
	narrow := "нет — ни один прогон не сужает отбор внутри пакета"
	if len(c.NarrowingRuns) > 0 {
		narrow = strings.Join(c.NarrowingRuns, " · ")
	}
	return fmt.Sprintf(
		"перепись: объявлений прочитано %d · прогонов с признаком найдено %d · "+
			"файлов проб прочитано %d · из них с признаком %d · "+
			"проб под признаком %d · из них отбирается %d · сужающие прогоны: %s",
		c.DeclarationFiles, c.RunsFound, c.FilesScanned, c.FilesWithTag,
		c.FuncsUnderTag, c.FuncsSelected, narrow)
}

// auditTaggedTestsAreSelected — судья. Вынесен из тела гейта, чтобы ТОТ ЖЕ
// судья судил синтетическое дерево пробы инъекции.
func auditTaggedTestsAreSelected(root, modulePath string) ([]tagSelectionFinding, tagSelectionCensus, error) {
	var census tagSelectionCensus

	scan, err := collectTaggedTestPackages(root)
	if err != nil {
		return nil, census, err
	}
	census.FilesScanned = scan.FilesScanned
	census.FilesWithTag = scan.FilesWithTag
	census.FuncsUnderTag = len(scan.Funcs)

	runs, declFiles, err := extractTaggedRuns(root)
	if err != nil {
		return nil, census, err
	}
	census.DeclarationFiles = declFiles
	census.RunsFound = len(runs)

	narrowing := map[string]bool{}
	for _, r := range runs {
		if r.RunPattern != "" || r.SkipPattern != "" {
			narrowing[fmt.Sprintf("%s (-run %q -skip %q)", r.Source, r.RunPattern, r.SkipPattern)] = true
		}
	}
	census.NarrowingRuns = buildTagSortedKeys(narrowing)

	funcs := append([]taggedTestFunc(nil), scan.Funcs...)
	sort.Slice(funcs, func(i, j int) bool {
		if funcs[i].Rel != funcs[j].Rel {
			return funcs[i].Rel < funcs[j].Rel
		}
		return funcs[i].Name < funcs[j].Name
	})

	var findings []tagSelectionFinding
	for _, fn := range funcs {
		var considered []string
		selected := false
		for _, r := range runs {
			if r.Tag != fn.Tag || !runCovers(r, fn.Pkg, modulePath) {
				continue
			}
			considered = append(considered,
				fmt.Sprintf("%s (-run %q)", r.Source, r.RunPattern))
			if runSelects(r, fn.Name) {
				selected = true
				break
			}
		}
		if selected {
			census.FuncsSelected++
			continue
		}
		if len(considered) == 0 {
			// Пакет вне всякой области — это предмет СОСЕДНЕГО гейта, и он о нём
			// уже говорит. Второе место об одном предмете здесь завело бы две
			// находки на один дефект, из которых чинить надо одну.
			continue
		}
		findings = append(findings, tagSelectionFinding{Func: fn, Runs: considered})
	}
	return findings, census, nil
}

// TestBuildTagTestsAreSelectedByTheDeclaredRun — гейт класса.
//
// Пустое дерево признаков — законный исход: гейт печатает перепись и проходит,
// потому что «под признаком нет ни одной пробы» есть цель, а не поломка.
// Отказом является «ноль прочитанных файлов» и «ноль прочитанных объявлений»:
// тогда молчание означает, что судья не работал.
func TestBuildTagTestsAreSelectedByTheDeclaredRun(t *testing.T) {
	root := repoRoot(t)

	findings, census, err := auditTaggedTestsAreSelected(root, "github.com/PRO-Robotech/kacho")
	if err != nil {
		t.Fatalf("обход дерева сорвался — вердикта нет: %v", err)
	}
	t.Log(census.String())

	if census.FilesScanned == 0 {
		t.Fatal("прочитано ноль файлов проб — предпосылка гейта не выполнена. " +
			"«Ноль находок» здесь означало бы «ноль прочитанного»")
	}
	if census.DeclarationFiles == 0 {
		t.Fatal("прочитано ноль объявлений — отбор не из чего вывести: судья не работал")
	}

	if len(findings) == 0 {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "проб под признаком сборки, не попадающих в отбор ни одного прогона: %d\n\n",
		len(findings))
	for _, f := range findings {
		fmt.Fprintf(&b, "%s\n\n", f)
	}
	b.WriteString("Такая проба не исполняется НИ РАЗУ: пакет в область прогона попадает,\n")
	b.WriteString("поэтому соседний гейт достижимости молчит, — а `-run` внутри прогона\n")
	b.WriteString("называет другие имена. У пробы нет вердикта вовсе: ни зелёного, ни\n")
	b.WriteString("красного, и снаружи это неотличимо от зелёного.\n")
	b.WriteString("Исходов два: внести имя пробы в отбор объявленного прогона либо снять\n")
	b.WriteString("пробу вместе с утверждением о том, что она что-то держит.\n")
	b.WriteString(census.String())
	t.Fatal(b.String())
}
