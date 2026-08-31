// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package artifactgates

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// shapeCapabilityDriverRel — перепись живёт отдельным скриптом по той же
// причине, что у соседнего гейта мёртвых помощников: она читает РАЗОБРАННЫЙ
// Python. Предикат по подстроке здесь негоден дважды — имя помощника стоит и
// вызовом, и прозой (в шапках четырёх модулей кейсов), а корпус двуязычен, и
// поиск по слову на одном языке недобирает молча.
const shapeCapabilityDriverRel = "internal/repohygiene/artifactgates/newmanshapecapability_driver.py"

// shapeSubject — одна проверка формы параметра, найденная в генераторе.
type shapeSubject struct {
	File    string         `json:"file"`
	Func    string         `json:"func"`
	Param   string         `json:"param"`
	Kind    string         `json:"kind"` // capability | guard | unknown-form
	Line    int            `json:"line"`
	Index   int            `json:"index"`
	Aliases []string       `json:"aliases"`
	Shifted []string       `json:"shifted"`
	Shapes  map[string]int `json:"shapes"`
}

type shapeCapabilityReport struct {
	Generators int            `json:"generators"`
	Files      int            `json:"files"`
	Subjects   []shapeSubject `json:"subjects"`
}

// shapeCapabilityCensus — объём осмотренного, ПО ОСЯМ.
//
// Роды проверок названы порознь намеренно: одно суммарное число («проверок
// формы 4») скрыло бы ровно тот случай, ради которого гейт заведён. Возможность
// и страж выглядят в исходнике одинаково, различает их только то, что делает
// вторая ветвь, и переход предмета из одного рода в другой обязан быть виден
// числом.
type shapeCapabilityCensus struct {
	generators   int
	files        int
	subjects     int
	capabilities int
	guards       int
	unknownForm  int
	shifted      int
	shapesSeen   int // сколько форм возможностей имеют хотя бы одного вызывающего
	shapesTotal  int // сколько форм объявлено возможностями (по две на каждую)
}

// shapeNames — как форма называется в тексте находки. Перечень закрыт и совпадает
// с тем, что производит драйвер: форма, которой он не знает, приезжает как
// `unknown` и вызывающим НЕ засчитывается.
var shapeNames = map[string]string{
	"str": "одиночное имя (строка)",
	"seq": "перечень имён (список либо кортеж)",
}

// auditShapeCapabilities — судящая функция гейта.
//
// Выделена, чтобы инъекция гоняла ЕЁ, а не свою копию: проба, повторяющая логику
// гейта, доказывала бы свойство копии.
func auditShapeCapabilities(r shapeCapabilityReport) ([]string, shapeCapabilityCensus) {
	cen := shapeCapabilityCensus{generators: r.Generators, files: r.Files, subjects: len(r.Subjects)}

	subs := append([]shapeSubject(nil), r.Subjects...)
	sort.Slice(subs, func(i, j int) bool {
		if subs[i].File != subs[j].File {
			return subs[i].File < subs[j].File
		}
		return subs[i].Line < subs[j].Line
	})

	var findings []string
	for _, s := range subs {
		at := fmt.Sprintf("%s:%d %s(%s)", s.File, s.Line, s.Func, s.Param)
		switch s.Kind {
		case "guard":
			// Страж ОТВЕРГАЕТ вход. Второй принимаемой формы у него нет, и
			// требовать для неё вызывающего значило бы требовать вызова,
			// который обязан упасть.
			cen.guards++
			continue
		case "unknown-form":
			// Невидимость хуже находки: проверка формы, записанная не так, как
			// знает разбор, не даёт ни красного, ни зелёного.
			cen.unknownForm++
			findings = append(findings, fmt.Sprintf(
				"%s — проверка ФОРМЫ записана вне известных разбору форм (не `if`/`if-else`). "+
					"Такой предмет не попадает ни в возможности, ни в стражи, то есть остаётся ВНЕ "+
					"наблюдения. Чинить надо распознаватель %s, а не молча выходить успехом",
				at, shapeCapabilityDriverRel))
			continue
		}
		cen.capabilities++
		if len(s.Shifted) > 0 {
			cen.shifted++
			findings = append(findings, fmt.Sprintf(
				"%s — вызывающий %s привязывает ПОЗИЦИОННЫЙ аргумент, и индекс проверяемого "+
					"параметра сдвигается. Перепись форм считала бы не тот аргумент; это отказ, "+
					"а не догадка", at, strings.Join(s.Shifted, ", ")))
			continue
		}
		var dead []string
		for _, shape := range []string{"str", "seq"} {
			cen.shapesTotal++
			if s.Shapes[shape] > 0 {
				cen.shapesSeen++
				continue
			}
			dead = append(dead, shape)
		}
		for _, shape := range dead {
			note := ""
			if s.Shapes["unknown"] > 0 {
				note = fmt.Sprintf(" (%d вызовов подают аргумент выражением — форму статически "+
					"не определить, и вызывающим они НЕ засчитаны)", s.Shapes["unknown"])
			}
			findings = append(findings, fmt.Sprintf(
				"%s — принимает ДВЕ формы, а форму «%s» не зовёт ни один вызывающий в дереве. "+
					"Осмотрено вызовов: %s %d, %s %d%s. Имена вызывающих выведены: %s. "+
					"Исходов два — позвать её либо снять ветвление ВМЕСТЕ с формой и с прозой шапки, "+
					"которая её обещает",
				at, shapeNames[shape],
				shapeNames["str"], s.Shapes["str"], shapeNames["seq"], s.Shapes["seq"], note,
				strings.Join(s.Aliases, ", ")))
		}
	}
	return findings, cen
}

// У КАЖДОЙ формы, которую помощник newman принимает, есть вызывающий в дереве.
//
// ПРЕДМЕТ. Параметр, по ФОРМЕ которого помощник ветвится, объявляет две
// принимаемые формы. Каждая — самостоятельная возможность: её пишут под
// конкретный дефект. Форма, которую никто не зовёт, не проверяется НИЧЕМ:
// генерация её не касается, коллекции она не излучает, сборка о ней не знает.
// Она стареет вместе с деревом и при первом же использовании работает не так,
// как ожидал тот, кто её писал.
//
// ЦЕНА ИЗМЕРЕНА, А НЕ ПРЕДПОЛОЖЕНА (#1781). Многоимённое ожидание
// `retry_until_present` заведено 4b856fb58 ВМЕСТЕ со своим единственным
// вызывающим (`services/vpc/.../network.py`); 29ad7b78d снял контракт, ради
// которого тот кейс писался, и вызывающий ушёл вместе с ним — законно. С этого
// момента и до сведения форка форма со списком имён не звалась НИ РАЗУ, и
// заметить это было нечем: обнаружилось случайно, при сведении, когда первый
// вызывающий появился в ДРУГОМ наборе.
//
// ПОЧЕМУ ГЕЙТ, А НЕ ЗАПИСАННЫЙ ПРЕДИКАТ ПОЯВЛЕНИЯ ВЫЗЫВАЮЩЕГО. У предиката,
// который никто не исполняет, нет производителя: он повторил бы ровно ту
// историю, из которой выведен, — форма умерла молча и всплыла по случаю.
// Отдельно: соседний гейт (`TestNewmanInjectedHelperHasACallerInItsSuite`) уже
// держит класс «помощник без вызывающего», но к ФОРМАМ он слеп by construction —
// он считает вызовы, не глядя на аргумент. Здесь та же норма на осью ниже.
//
// ЧЕГО ГЕЙТ НЕ СУДИТ. Он не требует вызывающего для СТРАЖА: `if not
// isinstance(x, str): raise` второй принимаемой формы не объявляет — отказ формой
// не является. Дискриминатор — что делает вторая ветвь, а не что стоит в условии.
//
// ПОПУЛЯЦИЯ НАЗВАНА ЧЕСТНО. На дереве заведения: генераторов 9, проверок формы
// параметра 4 — из них возможностей 1, стражей 3. Популяция мала, и узкая
// популяция предпосылку не подтверждает, а СКРЫВАЕТ (`testing.md` §«Гейт на
// класс» п.3). Поэтому дискриминатор доказан не деревом, а инъекцией по обеим
// сторонам, а перепись печатает роды порознь: переход предмета из рода в род
// виден числом, а не по памяти.
func TestNewmanHelperShapeCapabilityHasACaller(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	driver := filepath.Join(root, shapeCapabilityDriverRel)
	if _, err := os.Stat(driver); err != nil {
		t.Fatalf("перепись %s не найдена (%v): гейт без своего разбора судил бы пустоту, "+
			"а «ноль находок» обязано быть отличимо от «ноль прочитанного»",
			shapeCapabilityDriverRel, err)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("python3 не найден (%v): генераторы сквозных проб не разбираемы, "+
			"и это «не выполнилось», а не согласие", err)
	}

	// Перечни ВЫВОДЯТСЯ из индекса git. Выписанный разошёлся бы с деревом молча,
	// и новый набор остался бы вне обхода — та же форма дефекта, что гейт судит.
	var decls []string
	callDirs := map[string]bool{}
	if tt.files[sharedHelperRel] {
		decls = append(decls, filepath.Join(root, sharedHelperRel))
	}
	for rel := range tt.files {
		if !strings.Contains(rel, "/tests/newman/") || !strings.HasSuffix(rel, ".py") {
			continue
		}
		dir := filepath.Dir(rel)
		if strings.HasSuffix(dir, "/tests/newman/cases") || strings.HasSuffix(dir, "/tests/newman/scripts") {
			callDirs[filepath.Join(root, dir)] = true
		}
		if filepath.Base(rel) == "gen.py" && strings.HasSuffix(dir, "/tests/newman/scripts") {
			decls = append(decls, filepath.Join(root, rel))
		}
	}
	sort.Strings(decls)
	dirs := make([]string, 0, len(callDirs))
	for d := range callDirs {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	if len(decls) == 0 || len(dirs) == 0 {
		t.Fatalf("предпосылка гейта не выполняется: генераторов newman %d, каталогов вызывающих %d — "+
			"либо раскладка сменилась, либо обход смотрит не туда; чинить надо гейт, "+
			"а не молча выходить успехом", len(decls), len(dirs))
	}

	args := append([]string{driver, "--decl"}, decls...)
	args = append(args, "--calls")
	args = append(args, dirs...)
	cmd := exec.Command(python, args...) // #nosec G204 -- пути из индекса git
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		t.Fatalf("перепись не исполнилась (%v) — предмет НЕ ПРОВЕРЕН, и это не «ноль находок»\n%s",
			err, stderr)
	}
	var r shapeCapabilityReport
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("разбор вывода переписи: %v", err)
	}
	// Координата в находке — путь ОТ КОРНЯ репозитория, а не от корня машины.
	// Абсолютный путь читатель не может ни сравнить с деревом, ни привести в
	// задачу: он говорит о рабочем каталоге того, кто прогнал.
	for i := range r.Subjects {
		if rel, err := filepath.Rel(root, r.Subjects[i].File); err == nil {
			r.Subjects[i].File = filepath.ToSlash(rel)
		}
	}

	findings, cen := auditShapeCapabilities(r)

	t.Logf("осмотрено генераторов newman %d, модулей вызывающих %d; проверок формы параметра %d — "+
		"возможностей %d, стражей %d, вне известных форм %d; форм у возможностей %d, "+
		"из них со своим вызывающим %d (сдвинутых индексов %d)",
		cen.generators, cen.files, cen.subjects,
		cen.capabilities, cen.guards, cen.unknownForm,
		cen.shapesTotal, cen.shapesSeen, cen.shifted)

	if cen.files == 0 {
		t.Fatal("предпосылка гейта не выполняется: перепись не прочла ни одного модуля вызывающих")
	}
	if cen.subjects == 0 {
		t.Fatal("предпосылка гейта не выполняется: проверок формы параметра в генераторах НОЛЬ.\n" +
			"Либо разбор смотрит не туда, либо предмет исчез — в обоих случаях это отказ:\n" +
			"гейт, потерявший предмет, вечнозелен.")
	}
	if cen.capabilities == 0 {
		t.Fatal("предпосылка гейта не выполняется: многоформенных возможностей в дереве НОЛЬ.\n" +
			"Утверждение здесь ОТРИЦАТЕЛЬНОЕ («форма без вызывающего»), и на исчезнувшем предмете\n" +
			"оно замолкает, а не краснеет (`testing.md` §«Гейт на класс» п.9). Исходов два:\n" +
			"снять гейт ВМЕСТЕ с предметом (файл, инъекцию и эту строку одним изменением) либо\n" +
			"перевести на признак, который дерево производит.")
	}

	if len(findings) > 0 {
		t.Fatalf("форма, которую помощник принимает, не имеет вызывающего:\n  %s",
			strings.Join(findings, "\n  "))
	}
}
