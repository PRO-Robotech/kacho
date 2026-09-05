// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratorform.go — форма наката миграций в дереве ОДНА, и она объявлена.
//
// # Предмет: различие, которое НАКОПИЛОСЬ, а не было решено
//
// Точек наката семь (`services/*/cmd/migrator`), и жили они в ДВУХ формах: три
// делегировали собственному пакету-обёртке `internal/apps/migrator`, четыре
// звали goose прямо из `main.go`. Различие не решал никто — оно образовалось от
// того, что службы заводились в разное время, и каждая следующая копировала
// ближайшего соседа.
//
// Формы сведены (#1383): накат живёт в одном общем пакете
// ([sharedApplyImport]), к которому обращаются все семь, а per-service обёрток
// не осталось ни одной. Гейт держит достигнутое: вторая форма не заводится молча.
//
// # Что требуется
//
//  1. Каждая точка наката импортирует ОБЩИЙ накат. Не импортирующая — находка:
//     она означает, что накат заведён как-то ещё.
//  2. Ни одна не зовёт goose НАПРЯМУЮ. Прямой вызов — возвращение второй формы,
//     и он кладёт живой счёт строк перед сносом обратно в строку, которую надо
//     не забыть написать.
//  3. Копий пакета-обёртки не больше [migratorWrapperCeiling]. Это ПОТОЛОК,
//     КОТОРЫЙ МОЖЕТ ТОЛЬКО УБЫВАТЬ: он прошёл путь 3 → 0 вместе со сведением, и
//     расти не вправе.
//  4. Решение существует и называет действующую форму. Документ, потерявший
//     утверждение, ради которого на него ссылаются, — тот же класс, что и
//     отсутствие решения.
//
// # Почему потолок числом, а не «копий ровно ноль»
//
// Форма записи та же, и довод тот же, каким потолок держался на трёх:
// равенство краснело бы на сведении — то есть ровно тогда, когда работа сделана
// правильно. Потолок самоистекает и на нуле остаётся осмысленным: он падает на
// первой заведённой копии. Гейт, краснеющий на достижении своей цели, ставит
// исполнителя перед выбором «сделать верно или получить зелёное», и его снимают.
//
// # Чего гейт НЕ утверждает, названо честно
//
// Что накат ВЕДЁТ СЕБЯ верно: это предмет доказательства наката
// (`internal/migratorapply`), которое собирает бинарь каждой точки и гоняет его
// против живой базы в той форме, которой её зовёт манифест. Что CLI у семи
// одинаков: это ОТДЕЛЬНЫЙ предмет со своим решением
// (`docs/architecture/migrator-cli.md`) и своими гейтами
// ([TestMigratorBinaryIsNamedTheSameEverywhere] и соседние). Здесь судится ровно
// одно: не появилось ли ТРЕТЬЕЙ формы и ЧЕТВЁРТОЙ копии — молча.
//
// Здесь стояло «CLI НЕ одинаков (`--target` есть у трёх из семи)» — утверждение
// о дереве в шапке гейта, и оно пережило свой предмет: #1461 дал `--target`
// всем семи, а строка осталась и читалась как действующая. Утверждения о
// состоянии соседнего предмета в шапке больше нет — есть ссылка на того, кто
// это состояние держит. Живость строк ведомости различий держит
// [TestEveryDeclaredMigratorDivergenceStillHasASubject].
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
	// Убывает вместе со сведением; расти не вправе. Прошёл 3 → 0 (#1383).
	migratorWrapperCeiling = 0

	// sharedApplyImport — признак действующей формы: точка наката обращается к
	// ОБЩЕМУ накату. Импорт, а не имя каталога: имя встречается и в прозе.
	sharedApplyImport = "github.com/PRO-Robotech/kacho/pkg/migratorrun"

	// wrapperImportSuffix — признак снятой формы: точка наката импортирует
	// СВОЙ пакет-обёртку. Признак оставлен намеренно — им ловится возвращение.
	wrapperImportSuffix = "/internal/apps/migrator"

	// directDriverImport — признак прямой формы: goose зовётся из main.go.
	directDriverImport = "github.com/pressly/goose/v3"
)

// migratorForm — распознанная форма одной точки наката.
type migratorForm struct {
	Service    string
	Rel        string
	Delegating bool
	Direct     bool
	// Wrapper — точка наката импортирует СВОЙ пакет-обёртку. Признак снятой
	// формы; оставлен, чтобы её возвращение было находкой, а не молчанием.
	Wrapper bool
}

// Recognised — точка наката несёт действующую форму и только её.
func (f migratorForm) Recognised() bool { return f.Delegating && !f.Direct }

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
		if strings.HasSuffix(path, wrapperImportSuffix) {
			f.Wrapper = true
		}
		if path == directDriverImport {
			f.Direct = true
		}
		if path == sharedApplyImport {
			f.Delegating = true
		}
	}
	return f, nil
}

// migratorFormFindings формулирует находки по распознанным формам и числу копий.
func migratorFormFindings(forms []migratorForm, wrapperCopies int) []string {
	var out []string
	for _, f := range forms {
		switch {
		case f.Wrapper:
			out = append(out, fmt.Sprintf(
				"%s: импортирует СВОЙ пакет-обёртку (%s) — форма, снятая #1383. "+
					"Накат живёт в одном общем пакете; per-service обёртка возвращает "+
					"расхождение, которое сведением и убрали (%s)",
				f.Rel, wrapperImportSuffix, migratorFormDecisionDoc))
		case f.Direct:
			out = append(out, fmt.Sprintf(
				"%s: зовёт goose НАПРЯМУЮ — это вторая форма наката, снятая #1383. "+
					"Она кладёт живой счёт строк перед сносом обратно в строку, которую "+
					"надо не забыть написать; форма в дереве одна и объявлена в %s",
				f.Rel, migratorFormDecisionDoc))
		case !f.Delegating:
			out = append(out, fmt.Sprintf(
				"%s: не обращается к общему накату (%s) — значит накат заведён как-то ещё. "+
					"Форма в дереве одна и объявлена в %s; вторая заводится решением, а не молча",
				f.Rel, sharedApplyImport, migratorFormDecisionDoc))
		}
	}
	if wrapperCopies > migratorWrapperCeiling {
		out = append(out, fmt.Sprintf(
			"копий пакета-обёртки %d при потолке %d: per-service копия запрещена — "+
				"накат делегирующий, но пакет ОДИН общий (%s)",
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
