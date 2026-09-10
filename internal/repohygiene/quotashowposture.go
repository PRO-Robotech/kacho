// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// quotashowposture.go — ВИТРИНА ЗАНЯТОГО ОБЯЗАНА ЗНАТЬ, ОБЪЯВЛЕН ЛИ ДОМЕН
// ВЕЛИЧИН ОТСУТСТВУЮЩИМ (#2515).
//
// # Предмет
//
// Ручка домена величин принимает два законных значения: адрес соседа либо слово
// «домена величин в этой установке нет». Второе — объявленный выбор оператора, и
// путь запроса ему подчиняется: считаемые виды списываются, но не отвергаются
// никогда.
//
// Полоса чтения квот собирается ТОЛЬКО под развёрнутый домен — спрашивать
// величины иначе не у кого. Поэтому на объявленном отсутствии владелец
// оставлял полосу несобранной, обработчика витрины не строил и метод не
// регистрировал вовсе; арендатор получал `Unimplemented`, то есть «такой
// возможности в этой сборке нет». Неправда дважды: метод существует, а
// отсутствует не он, а потолок.
//
// Класс — «недоступен ≠ не развёрнут» (`polyrepo.md`): два состояния с
// противоположными следствиями для арендатора имели одно представление
// (полосы нет) и один ответ.
//
// # Почему это свойство ДЕРЕВА, а не проба одного владельца
//
// Владельцев витрины пятеро, и шестой заведётся копией чужого композиционного
// корня. Проба одного об остальных не утверждает ничего и остаётся зелёной
// ровно на том дефекте, ради которого написана.
//
// # Что гейт требует от корня, который выставляет витрину
//
// Три вещи, и каждая закрывает свой способ вернуть дефект молча:
//
//	1. посадка ВЫВОДИТСЯ из разрешённого объявления — вызов общего перевода
//	   `quota.ReadPosture`. Без него корень решал бы за оператора своим
//	   признаком, а пять таких признаков разошлись бы там, где расхождения не
//	   видит ни один из пяти;
//	2. корень СПРАШИВАЕТ про объявленное отсутствие (`AuthorityIsAbsent`).
//	   Не спросив, он не может выставить витрину на такой посадке — и она
//	   останется `Unimplemented`, то есть при исходном дефекте;
//	3. корень НЕ подставляет посадку литералом (`AuthorityDeclared`). Это и есть
//	   тихий способ всё вернуть: посадка формально «передана», компилятор
//	   доволен, поведение — доисправленное. Пробам литерал законен, поэтому
//	   судятся только НЕ-тестовые файлы.
//
// # Почему признаком служит имя генерированного глагола регистрации
//
// Имена конструкторов обработчиков у владельцев РАЗНЫЕ (`NewHandler` против
// `NewQuotaHandler`), а имя `RegisterQuotaServiceServer` порождается контрактом
// и одинаково у всех: признак взят у того, что не зависит от вкуса владельца.
//
// # Почему судятся УЗЛЫ ВЫЗОВА, а не подстроки
//
// Все три имени стоят в прозе — в этом самом объяснении и в комментариях самих
// корней. Предикат по сырому тексту нашёл бы их в СОБСТВЕННОМ объяснении и
// остался бы зелёным на снятой защите (`testing.md` §«Гейт на класс», п. 4).
// Отдельно: `quotaEdge.ReadPosture` — обращение к ПОЛЮ, а не вызов перевода;
// разбор их различает, поиск по подстроке — нет.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// Имена, по которым судится корень. Все три — узлы вызова, не подстроки.
const (
	// quotaShowRegisterVerb — генерированный глагол регистрации витрины.
	quotaShowRegisterVerb = "RegisterQuotaServiceServer"
	// quotaShowPostureDeriveVerb — общий перевод объявления в посадку.
	quotaShowPostureDeriveVerb = "ReadPosture"
	// quotaShowAbsenceAsk — вопрос про объявленное отсутствие.
	quotaShowAbsenceAsk = "AuthorityIsAbsent"
	// quotaShowDeclaredLiteral — посадка, подставленная литералом.
	quotaShowDeclaredLiteral = "AuthorityDeclared"
)

// quotaShowFacts — что найдено в разобранном файле композиционного корня.
type quotaShowFacts struct {
	Registers  bool
	Derives    bool
	AsksAbsent bool
	Declares   bool
}

// merge складывает находки файлов одного корня.
func (f *quotaShowFacts) merge(o quotaShowFacts) {
	f.Registers = f.Registers || o.Registers
	f.Derives = f.Derives || o.Derives
	f.AsksAbsent = f.AsksAbsent || o.AsksAbsent
	f.Declares = f.Declares || o.Declares
}

// quotaShowFactsIn — разбор ОДНОГО файла: что он зовёт.
//
// Судится `*ast.CallExpr`, поэтому имя в комментарии, в строке и обращение к
// одноимённому полю вызовом не считаются.
func quotaShowFactsIn(file *ast.File) quotaShowFacts {
	var f quotaShowFacts
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch quotaShowCalleeName(call.Fun) {
		case quotaShowRegisterVerb:
			f.Registers = true
		case quotaShowPostureDeriveVerb:
			f.Derives = true
		case quotaShowAbsenceAsk:
			f.AsksAbsent = true
		case quotaShowDeclaredLiteral:
			f.Declares = true
		}
		return true
	})
	return f
}

// quotaShowCalleeName — имя вызываемого: и `Verb(...)`, и `pkg.Verb(...)`.
func quotaShowCalleeName(fun ast.Expr) string {
	switch v := fun.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	}
	return ""
}

// quotaShowOwner — один композиционный корень и его вердикт.
type quotaShowOwner struct {
	Service string
	Facts   quotaShowFacts
}

// Violations — чего корню не хватает, чтобы витрина знала посадку.
//
// Пусто у корня, витрину не выставляющего: требовать посадку от того, у кого
// витрины нет, значило бы завести освобождение без предмета.
func (o quotaShowOwner) Violations() []string {
	if !o.Facts.Registers {
		return nil
	}
	var out []string
	if !o.Facts.Derives {
		out = append(out, fmt.Sprintf(
			"посадка витрины не выводится из объявления: корень не зовёт %s — "+
				"значит он решает за оператора своим признаком", quotaShowPostureDeriveVerb))
	}
	if !o.Facts.AsksAbsent {
		out = append(out, fmt.Sprintf(
			"корень не спрашивает про объявленное отсутствие (%s): на посадке без домена "+
				"величин витрина остаётся незарегистрированной и отвечает «возможности нет»",
			quotaShowAbsenceAsk))
	}
	if o.Facts.Declares {
		out = append(out, fmt.Sprintf(
			"посадка подставлена литералом %s: компилятор доволен, а поведение — "+
				"доисправленное", quotaShowDeclaredLiteral))
	}
	return out
}

// quotaShowCensus — объём осмотренного. Печатается ВСЕГДА и НЕСКОЛЬКИМИ
// величинами: одно число («находок 0») скрывает ровно тот случай, ради которого
// гейт заведён, — обход не нашёл ни одного предмета.
type quotaShowCensus struct {
	Services int
	Files    int
	Showing  int
	Deriving int
	Asking   int
}

func (c quotaShowCensus) String() string {
	return fmt.Sprintf(
		"каталогов служб осмотрено %d · файлов композиционных корней прочитано %d · "+
			"выставляют витрину %d · выводят посадку из объявления %d · спрашивают про отсутствие %d",
		c.Services, c.Files, c.Showing, c.Deriving, c.Asking)
}

// quotaShowOwners обходит композиционные корни служб и собирает вердикт по
// каждому.
//
// Состав берётся у ИНДЕКСА git, а не с диска: вердикт обязан быть свойством
// коммита, а под деревом на машине сборки лежит и то, чего в репозитории нет.
func quotaShowOwners(root string) ([]quotaShowOwner, quotaShowCensus, error) {
	var census quotaShowCensus

	servicesDir := filepath.Join(root, "services")
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		return nil, census, fmt.Errorf("каталог служб: %w", err)
	}

	var owners []quotaShowOwner
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		census.Services++

		cmdDir := filepath.Join(servicesDir, e.Name(), "cmd")
		if _, serr := os.Stat(cmdDir); serr != nil {
			// Служба без композиционного корня предметом не является.
			continue
		}
		files, ferr := treecorpus.UnderWithSuffix(cmdDir, ".go")
		if ferr != nil {
			return nil, census, fmt.Errorf("состав %s: %w", cmdDir, ferr)
		}

		owner := quotaShowOwner{Service: e.Name()}
		for _, abs := range files {
			if strings.HasSuffix(abs, "_test.go") {
				// Литерал посадки пробам законен: они и обязаны подавать её
				// обеими сторонами.
				continue
			}
			body, rerr := os.ReadFile(abs) // #nosec G304 -- путь пришёл из индекса git этого дерева
			if rerr != nil {
				return nil, census, fmt.Errorf("%s: %w", abs, rerr)
			}
			census.Files++

			fset := token.NewFileSet()
			parsed, perr := parser.ParseFile(fset, abs, body, parser.SkipObjectResolution)
			if perr != nil {
				return nil, census, fmt.Errorf("разбор %s: %w", abs, perr)
			}
			owner.Facts.merge(quotaShowFactsIn(parsed))
		}

		if owner.Facts.Registers {
			census.Showing++
			if owner.Facts.Derives {
				census.Deriving++
			}
			if owner.Facts.AsksAbsent {
				census.Asking++
			}
		}
		owners = append(owners, owner)
	}

	sort.Slice(owners, func(i, j int) bool { return owners[i].Service < owners[j].Service })
	return owners, census, nil
}
