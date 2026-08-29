// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratorform.go — форм наката миграций в дереве ровно две, и обе объявлены.
//
// # Предмет: различие, которое НАКОПИЛОСЬ, а не было решено
//
// Точек наката семь (`services/*/cmd/migrator`), и живут они в двух формах:
// три делегируют собственному пакету-обёртке `internal/apps/migrator`, четыре
// зовут goose прямо из `main.go`. Различие не решал никто — оно образовалось от
// того, что сервисы заводились в разное время, и каждый следующий копировал
// ближайшего соседа.
//
// Цена этого не гипотетическая: всякая правка тракта миграций платит за две
// формы вместо одной, и платит тем дороже, чем сильнее разойдутся копии.
// Соседний гейт [TestEveryMigrationRunnerAdmitsANonChronologicalNumber] уже
// вынужден знать ОБЕ формы — иначе он объявил бы одну из них нарушением.
//
// Решение принято и записано в [migratorFormDecisionDoc]: целевая форма —
// делегирующая, один общий пакет. Сведение семи в один идёт своей задачей, у
// неё названо предусловие. Этот гейт держит то, что связывает УЖЕ СЕГОДНЯ:
// накопление остановлено.
//
// # Что требуется
//
//  1. У каждой точки наката форма РАСПОЗНАНА — либо делегирующая, либо прямая.
//     Третья форма (ни та, ни другая) — находка: она означает, что кто-то завёл
//     ещё один способ, не решив вопроса.
//  2. Обе сразу — тоже находка: точка наката, и импортирующая свою обёртку, и
//     зовущая goose напрямую, не даёт сказать, какая форма исполняется.
//  3. Копий обёртки не больше [migratorWrapperCeiling]. Это ПОТОЛОК, КОТОРЫЙ
//     МОЖЕТ ТОЛЬКО УБЫВАТЬ: новая копия запрещена, а сведение существующих
//     гейт не роняет — оно и есть цель.
//  4. Решение существует и называет действующую форму. Документ, потерявший
//     утверждение, ради которого на него ссылаются, — тот же класс, что и
//     отсутствие решения.
//
// # Почему потолок числом, а не «копий ровно три»
//
// Равенство краснело бы на сведении — то есть ровно тогда, когда работа сделана
// правильно. Потолок самоистекает: он проходит на трёх, на одной и на нуле, и
// падает только на четвёртой. Гейт, краснеющий на достижении своей цели, ставит
// исполнителя перед выбором «сделать верно или получить зелёное», и его снимают.
//
// # Чего гейт НЕ утверждает, названо честно
//
// Что две формы ведут себя одинаково: сравнение поведения — предмет проб самих
// миграторов, а их у четырёх сервисов из семи нет вовсе (замер в решении).
// Что CLI у семи одинаков: он НЕ одинаков (`--target` есть у трёх из семи), и
// это отдельный предмет, названный в решении открытым остатком. Здесь судится
// ровно одно: не появилось ли ТРЕТЬЕЙ формы и ЧЕТВЁРТОЙ копии — молча.
//
// # Слепая зона, названная числом, а не умолчанием
//
// Корпус строится из ИНДЕКСА git (`treecorpus`, `git ls-files`), поэтому файл,
// лежащий на диске и НИ РАЗУ не добавленный в индекс, гейт не видит. Проверено
// живой инъекцией: четвёртая копия обёртки, созданная и не добавленная, гейт НЕ
// роняет; после `git add -N` — роняет и называет число («копий 4 при потолке
// 3»). Окно это закрывается на первом же `git add`, то есть до слияния; предел
// общий для всех гейтов дерева этого репозитория, а не свойство этой проверки.
// Он назван здесь потому, что слепая зона, на которую полагаются молча, — тот
// самый класс, ради которого гейты и заводятся.
package repohygiene

import (
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// migratorFormDecisionDoc — единственное место, где форма мигратора
	// ОБЪЯВЛЕНА. Путь назван, а не выведен: «документ, на который ссылаются» —
	// определение через тех, кого проверяем, и оно поехало бы вместе с ними.
	migratorFormDecisionDoc = "docs/architecture/migrator-form.md"

	// migratorWrapperCeiling — потолок числа per-service копий обёртки.
	// Убывает вместе со сведением; расти не вправе.
	migratorWrapperCeiling = 3

	// delegatingImportSuffix — признак делегирующей формы: точка наката
	// импортирует СВОЙ пакет-обёртку.
	delegatingImportSuffix = "/internal/apps/migrator"

	// directDriverImport — признак прямой формы: goose зовётся из main.go.
	directDriverImport = "github.com/pressly/goose/v3"
)

// migratorForm — распознанная форма одной точки наката.
type migratorForm struct {
	Service    string
	Rel        string
	Delegating bool
	Direct     bool
}

// Recognised — форма распознана ровно одна.
func (f migratorForm) Recognised() bool { return f.Delegating != f.Direct }

// migratorFormCensus — объём осмотренного. Отдельное утверждение: «ноль
// находок» обязано быть отличимо от «ноль прочитанного».
type migratorFormCensus struct {
	EntryPoints   int
	Delegating    int
	Direct        int
	WrapperCopies int
}

func (c migratorFormCensus) String() string {
	return fmt.Sprintf(
		"точек наката %d · делегирующих %d · прямых %d · копий обёртки %d (потолок %d)",
		c.EntryPoints, c.Delegating, c.Direct, c.WrapperCopies, migratorWrapperCeiling)
}

// classifyMigratorEntryPoint читает ИМПОРТЫ точки наката разбором, а не
// подстрокой: имя goose встречается и в комментариях (в compute/main.go — в
// длинном разборе про пропущенные миграции), и гейт по тексту засчитал бы форму
// по объяснению, а не по вызову.
func classifyMigratorEntryPoint(service, rel, src string) (migratorForm, error) {
	f := migratorForm{Service: service, Rel: rel}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, parser.ImportsOnly)
	if err != nil {
		return f, fmt.Errorf("%s: разбор не удался: %w", rel, err)
	}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if strings.HasSuffix(path, delegatingImportSuffix) {
			f.Delegating = true
		}
		if path == directDriverImport {
			f.Direct = true
		}
	}
	return f, nil
}

// migratorFormFindings формулирует находки по распознанным формам и числу копий.
func migratorFormFindings(forms []migratorForm, wrapperCopies int) []string {
	var out []string
	for _, f := range forms {
		switch {
		case f.Delegating && f.Direct:
			out = append(out, fmt.Sprintf(
				"%s: обе формы сразу — импортирует свою обёртку И зовёт goose напрямую; "+
					"какая исполняется, по коду не сказать. Оставь одну (%s)",
				f.Rel, migratorFormDecisionDoc))
		case !f.Delegating && !f.Direct:
			out = append(out, fmt.Sprintf(
				"%s: ТРЕТЬЯ форма — ни делегирующая, ни прямая. Форм наката две, "+
					"и обе объявлены в %s; третья заводится решением, а не молча",
				f.Rel, migratorFormDecisionDoc))
		}
	}
	if wrapperCopies > migratorWrapperCeiling {
		out = append(out, fmt.Sprintf(
			"копий пакета-обёртки %d при потолке %d: новая per-service копия запрещена — "+
				"целевая форма делегирующая, но пакет ОДИН общий (%s)",
			wrapperCopies, migratorWrapperCeiling, migratorFormDecisionDoc))
	}
	sort.Strings(out)
	return out
}

// serviceOfMigratorPath — имя сервиса из пути точки наката.
func serviceOfMigratorPath(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) >= 2 && parts[0] == "services" {
		return parts[1]
	}
	return rel
}
