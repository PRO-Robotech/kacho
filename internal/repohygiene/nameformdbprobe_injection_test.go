// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// nameformdbprobe_injection_test.go — способность гейта упасть и смолчать,
// доказанная настоящим входом обеих сторон.
//
// Инъекция без законного близнеца доказывает только чувствительность к форме:
// гейт, краснеющий на всём, отключат при первом же ложном срабатывании. Поэтому
// каждая проба здесь идёт парой — дефект и НЕ ЕГО КОПИЯ, а другая конструкция
// той же формы, в которой дефекта нет.
//
// Разбор зовётся ТОТ ЖЕ, что и на настоящем дереве (`analyseNameFormDBCoverage`
// из не-тестового файла): своя копия доказывала бы способность упасть у кода,
// который не исполняется.
//
// Вход — настоящий Go: гейт разбирает синтаксис, и на выдуманном тексте разбор
// отказал бы, а инъекция доказала бы отказ вместо свойства.
package repohygiene

import (
	"strings"
	"testing"
)

// injCanon — форма, которую гейт получает параметром. Здесь она подставная и
// намеренно НЕ совпадает с настоящим каноном: гейт обязан судить по тому, что
// ему дали, а выписанный настоящий канон запрещён соседним гейтом
// TestResourceNameFormIsDeclaredOnce.
const injCanon = `^INJECTED-FORM$`

// injEntry — входные методы двигателя, которые инъекция подаёт вместо
// прочитанных из дерева: гейт обязан судить по тому, что ему дали.
var injEntry = map[string]bool{"Run": true, "Check": true}

func injMigration(svc string, carriesForm bool) (string, string) {
	body := "ALTER TABLE t ADD CONSTRAINT t_name_check CHECK (name ~ 'что-то другое');"
	if carriesForm {
		body = "ALTER TABLE t ADD CONSTRAINT t_name_check CHECK (name ~ '" + injCanon + "');"
	}
	return "services/" + svc + "/internal/migrations/715001_resource_name_single_form.sql", body
}

// injProbeSource — файл пробы в законной форме дерева: импорт двигателя и вызов
// входного метода на составном литерале прямо в теле теста.
func injProbeSource(pkg string) string {
	return `package ` + pkg + `

import (
	"context"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/nameformdb"
)

func TestIntegration_NameForm(t *testing.T) {
	ctx := context.Background()
	nameformdb.Probe{Schema: "kacho_x"}.Run(ctx, t, nil)
}
`
}

func TestNameFormDBCoverage_FailsOnInjectedDefect(t *testing.T) {
	t.Run("законный близнец: миграция и вызов у одного сервиса — находок ноль", func(t *testing.T) {
		mig, body := injMigration("alpha", true)
		files := map[string]string{
			mig: body,
			"services/alpha/internal/repo/name_form_constraint_integration_test.go": injProbeSource("repo_test"),
		}
		cov := analyseNameFormDBCoverage(files, injCanon, injEntry)
		if got := cov.Unproven(); len(got) != 0 {
			t.Errorf("сервис с исполняемым вызовом объявлен недоказанным: %v", got)
		}
		if cov.MigrationsRead != 1 || cov.TestsRead != 1 || cov.TestsParsed != 1 {
			t.Errorf("перепись: миграций %d, проб %d, разобрано %d — ожидалось по одному",
				cov.MigrationsRead, cov.TestsRead, cov.TestsParsed)
		}
		if len(cov.Unparsed) != 0 {
			t.Errorf("законный файл не разобрался: %v", cov.Unparsed)
		}
	})

	// Дефект, которым рецензент опроверг прежнюю редакцию гейта (2026-08-19):
	// от пробы остался комментарий, гейт остался зелёным. Подслучая ровно этой
	// формы в наборе не было — были только «вызова нет вовсе» и «вызов у чужого
	// сервиса», а выпотрошенная проба ОТ ОБОИХ отличается тем, что имя двигателя
	// в файле есть.
	t.Run("проба ВЫПОТРОШЕНА до комментария — находка называет сервис", func(t *testing.T) {
		mig, body := injMigration("beta", true)
		files := map[string]string{
			mig: body,
			"services/beta/internal/repo/name_form_constraint_integration_test.go": `package repo_test

// Здесь когда-то звался nameformdb.Probe
`,
		}
		cov := analyseNameFormDBCoverage(files, injCanon, injEntry)
		got := cov.Unproven()
		if len(got) != 1 || got[0] != "beta" {
			t.Fatalf("ожидалась ровно одна находка про beta, получено %v", got)
		}
		// Упоминание обязано попасть в диагностику: без него находка посылала бы
		// заводить пробу заново там, где её надо восстановить.
		if len(cov.Mentioned["beta"]) != 1 {
			t.Errorf("текстовое упоминание двигателя не отмечено: %v", cov.Mentioned["beta"])
		}
	})

	t.Run("имя двигателя в СТРОКЕ вызовом не является", func(t *testing.T) {
		// Второй вид того же класса: подстрока найдётся и в литерале. Разбор
		// синтаксиса оставляет её `*ast.BasicLit`, а не вызовом.
		mig, body := injMigration("gamma", true)
		files := map[string]string{
			mig: body,
			"services/gamma/internal/repo/name_form_constraint_integration_test.go": `package repo_test

import "testing"

func TestSomething(t *testing.T) {
	t.Log("проба зовётся так: nameformdb.Probe{}.Run(ctx, t, pool)")
}
`,
		}
		cov := analyseNameFormDBCoverage(files, injCanon, injEntry)
		got := cov.Unproven()
		if len(got) != 1 || got[0] != "gamma" {
			t.Errorf("ожидалась находка про gamma, получено %v", got)
		}
	})

	t.Run("вызов в МЁРТВОМ помощнике не доказывает ничего", func(t *testing.T) {
		// Достижимость — то, чем «исполняемый» отличается от «написанный».
		// Помощник, которого не зовёт ни одна точка входа, не исполняется, и
		// его вызов доказывает ровно столько же, сколько комментарий.
		mig, body := injMigration("delta", true)
		files := map[string]string{
			mig: body,
			"services/delta/internal/repo/name_form_constraint_integration_test.go": `package repo_test

import (
	"context"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/nameformdb"
)

func deadHelper(ctx context.Context, t *testing.T) {
	nameformdb.Probe{Schema: "kacho_delta"}.Run(ctx, t, nil)
}

func TestSomethingElse(t *testing.T) {
	t.Log("помощника выше не зовёт никто")
}
`,
		}
		cov := analyseNameFormDBCoverage(files, injCanon, injEntry)
		got := cov.Unproven()
		if len(got) != 1 || got[0] != "delta" {
			t.Errorf("ожидалась находка про delta, получено %v", got)
		}
	})

	t.Run("законный близнец: тот же помощник, но ПОЗВАННЫЙ из теста — молчание", func(t *testing.T) {
		// Близнец к предыдущему подслучаю отличается ОДНОЙ строкой — вызовом
		// помощника. Без него гейт ловил бы форму «вызов не в теле теста», а не
		// исполняемость, и запретил бы законную раскладку с помощником.
		mig, body := injMigration("epsilon", true)
		files := map[string]string{
			mig: body,
			"services/epsilon/internal/repo/name_form_constraint_integration_test.go": `package repo_test

import (
	"context"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/nameformdb"
)

func liveHelper(ctx context.Context, t *testing.T) {
	nameformdb.Probe{Schema: "kacho_epsilon"}.Run(ctx, t, nil)
}

func TestIntegration_NameForm(t *testing.T) {
	liveHelper(context.Background(), t)
}
`,
		}
		cov := analyseNameFormDBCoverage(files, injCanon, injEntry)
		if got := cov.Unproven(); len(got) != 0 {
			t.Errorf("законная раскладка с помощником объявлена недоказанной: %v", got)
		}
	})

	t.Run("законный близнец: помощник ВОЗВРАЩАЕТ пробу, метод зовут у результата", func(t *testing.T) {
		// Форма, уже применённая в дереве (инъекция geo): проба собирается
		// помощником, а входной метод зовётся у возвращённого значения. Вход
		// намеренно другой, а не копия предыдущего близнеца: там доказывалась
		// достижимость, здесь — опознание значения, полученного не литералом.
		mig, body := injMigration("zeta", true)
		files := map[string]string{
			mig: body,
			"services/zeta/internal/repo/probe_fixture_test.go": `package repo_test

import "github.com/PRO-Robotech/kacho/pkg/nameformdb"

func zetaProbe() nameformdb.Probe {
	return nameformdb.Probe{Schema: "kacho_zeta"}
}
`,
			// Второй файл того же пакета ДВИГАТЕЛЯ НЕ ИМПОРТИРУЕТ — и это
			// законно: тип приезжает возвратом помощника. Значит достижимость и
			// опознание обязаны считаться по пакету, а не по файлу.
			"services/zeta/internal/repo/name_form_constraint_integration_test.go": `package repo_test

import (
	"context"
	"testing"
)

func TestIntegration_NameForm(t *testing.T) {
	if _, err := zetaProbe().Check(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}
`,
		}
		cov := analyseNameFormDBCoverage(files, injCanon, injEntry)
		if got := cov.Unproven(); len(got) != 0 {
			t.Errorf("проба через помощника объявлена недоказанной: %v", got)
		}
		if cov.TestsParsed != 2 {
			t.Errorf("разобрано файлов %d — оба файла пакета обязаны попасть в разбор, "+
				"иначе помощник в соседнем файле невидим", cov.TestsParsed)
		}
	})

	t.Run("законный близнец: импорт под ПСЕВДОНИМОМ — молчание", func(t *testing.T) {
		// Гейт судит по типу, а не по написанию: требование одного написания
		// мерило бы стиль импорта, а не наличие доказательства.
		mig, body := injMigration("eta", true)
		files := map[string]string{
			mig: body,
			"services/eta/internal/repo/name_form_constraint_integration_test.go": `package repo_test

import (
	"context"
	"testing"

	nf "github.com/PRO-Robotech/kacho/pkg/nameformdb"
)

func TestIntegration_NameForm(t *testing.T) {
	nf.Probe{Schema: "kacho_eta"}.Run(context.Background(), t, nil)
}
`,
		}
		cov := analyseNameFormDBCoverage(files, injCanon, injEntry)
		if got := cov.Unproven(); len(got) != 0 {
			t.Errorf("вызов через псевдоним импорта не засчитан: %v", got)
		}
		// Текстовое упоминание `nameformdb.Probe` здесь отсутствует — то есть
		// ПРЕЖНЯЯ, подстрочная редакция гейта на этом законном входе краснела бы.
		if len(cov.Mentioned["eta"]) != 0 {
			t.Errorf("вход подобран неверно: он обязан НЕ содержать текстового упоминания, "+
				"иначе не отличает разбор синтаксиса от поиска подстроки; получено %v",
				cov.Mentioned["eta"])
		}
	})

	t.Run("метод НЕ из входного набора доказательством не является", func(t *testing.T) {
		// Перечень входных методов приезжает параметром из двигателя. Вызов
		// чего-то ещё на пробе её не исполняет.
		mig, body := injMigration("theta", true)
		files := map[string]string{
			mig: body,
			"services/theta/internal/repo/name_form_constraint_integration_test.go": `package repo_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/nameformdb"
)

func TestIntegration_NameForm(t *testing.T) {
	p := nameformdb.Probe{Schema: "kacho_theta"}
	t.Log(p.String())
}
`,
		}
		cov := analyseNameFormDBCoverage(files, injCanon, injEntry)
		got := cov.Unproven()
		if len(got) != 1 || got[0] != "theta" {
			t.Errorf("ожидалась находка про theta, получено %v", got)
		}
	})

	t.Run("проба у ЧУЖОГО сервиса не засчитывается", func(t *testing.T) {
		// Двигатель зовут, но из другого сервиса: доказательство принадлежит
		// той схеме, которую оно обходит, и «где-то в дереве есть проба» —
		// не то же самое, что «эта схема доказана».
		migA, bodyA := injMigration("iota", true)
		migB, bodyB := injMigration("kappa", true)
		files := map[string]string{
			migA: bodyA,
			migB: bodyB,
			"services/kappa/internal/repo/name_form_constraint_integration_test.go": injProbeSource("repo_test"),
		}
		cov := analyseNameFormDBCoverage(files, injCanon, injEntry)
		got := cov.Unproven()
		if len(got) != 1 || got[0] != "iota" {
			t.Errorf("ожидалась ровно одна находка про iota, получено %v", got)
		}
	})

	t.Run("вызов двигателя в ПРОД-коде доказательством не является", func(t *testing.T) {
		mig, body := injMigration("lambda", true)
		files := map[string]string{
			mig: body,
			// Не `_test.go`: прогон проб этот файл не исполняет.
			"services/lambda/internal/repo/helper.go": injProbeSource("repo"),
		}
		cov := analyseNameFormDBCoverage(files, injCanon, injEntry)
		got := cov.Unproven()
		if len(got) != 1 || got[0] != "lambda" {
			t.Errorf("ожидалась находка про lambda, получено %v", got)
		}
	})

	t.Run("миграция БЕЗ формы под гейт не подпадает", func(t *testing.T) {
		// Положительный контроль к самому отбору: сервис, который формы не
		// ставит, не обязан ничего доказывать. Без этого подслучая гейт
		// требовал бы пробу от каждого сервиса дерева.
		mig, body := injMigration("mu", false)
		cov := analyseNameFormDBCoverage(map[string]string{mig: body}, injCanon, injEntry)
		if len(cov.Constrained) != 0 {
			t.Errorf("сервис без формы попал под гейт: %v", cov.Services())
		}
		if cov.MigrationsRead != 1 {
			t.Errorf("перепись: миграция не прочитана (%d) — «под гейт не подпадает» стало бы "+
				"неотличимо от «файл не читали»", cov.MigrationsRead)
		}
	})

	// --- снятие формы (#716) ------------------------------------------------
	//
	// Форму можно не только поставить, но и снять вместе с её колонкой. Текст
	// поставившей миграции при этом не меняется никогда (применённую не правят),
	// поэтому «канон встречается в дереве» перестаёт означать «форма стоит».
	// Подслучаи ниже проверяют ИСХОД применения по порядку, и каждый идёт с
	// законным близнецом ДРУГОЙ конструкции, а не с копией предыдущего.

	t.Run("форма ПОСТАВЛЕНА и СНЯТА позже — доказывать нечего", func(t *testing.T) {
		mig, body := injMigration("xi", true)
		files := map[string]string{
			mig: body,
			"services/xi/internal/migrations/716001_drop.sql": "-- +goose Up\nALTER TABLE t DROP COLUMN name;\n",
		}
		cov := analyseNameFormDBCoverage(files, injCanon, injEntry)
		if len(cov.Constrained) != 0 {
			t.Errorf("сервис со снятой формой остался под гейтом: %v", cov.Services())
		}
		if cov.MigrationsRead != 2 {
			t.Errorf("перепись: прочитано миграций %d, ожидалось 2 — «формы нет» стало бы "+
				"неотличимо от «файлы не читали»", cov.MigrationsRead)
		}
	})

	t.Run("законный близнец: снятие РАНЬШЕ постановки — форма стоит", func(t *testing.T) {
		// Другая конструкция, а не копия: те же два оператора в обратном
		// порядке версий. Без этого подслучая гейт ловил бы «в дереве есть
		// DROP COLUMN name», а не исход применения, и объявлял бы форму снятой
		// у compute, чья ранняя миграция снимает name у своей таблицы зон.
		mig, body := injMigration("omicron", true)
		files := map[string]string{
			"services/omicron/internal/migrations/0003_drop.sql": "-- +goose Up\nALTER TABLE t DROP COLUMN name;\n",
			mig: body,
			"services/omicron/internal/repo/name_form_constraint_integration_test.go": injProbeSource("repo_test"),
		}
		cov := analyseNameFormDBCoverage(files, injCanon, injEntry)
		if len(cov.Constrained) != 1 {
			t.Fatalf("форма, поставленная ПОСЛЕ снятия, не учтена: %v", cov.Services())
		}
		if got := cov.Unproven(); len(got) != 0 {
			t.Errorf("сервис с доказательством объявлен недоказанным: %v", got)
		}
	})

	t.Run("снятие в КОММЕНТАРИИ снятием не является", func(t *testing.T) {
		// Тот же класс, что «имя двигателя в комментарии»: гейт обязан читать
		// оператор, а не объяснение. Иначе миграция, ОБЪЯСНЯЮЩАЯ форму словами
		// «здесь мог бы стоять DROP COLUMN name», выключала бы требование.
		mig, body := injMigration("pi", true)
		files := map[string]string{
			mig: body,
			"services/pi/internal/migrations/716001_note.sql": "-- +goose Up\n-- ALTER TABLE t DROP COLUMN name; -- так делать не стали\nSELECT 1;\n",
		}
		cov := analyseNameFormDBCoverage(files, injCanon, injEntry)
		if len(cov.Constrained) != 1 {
			t.Errorf("комментарий принят за снятие: под гейтом %v", cov.Services())
		}
	})

	t.Run("снятие в ОТКАТЕ снятием не является", func(t *testing.T) {
		// Секция Down описывает возвращение прежнего состояния, а не текущее.
		// Считать её объявлением значило бы принимать откат за применение.
		mig, body := injMigration("rho", true)
		files := map[string]string{
			mig: body,
			"services/rho/internal/migrations/716001_other.sql": "-- +goose Up\nSELECT 1;\n-- +goose Down\nALTER TABLE t DROP COLUMN name;\n",
		}
		cov := analyseNameFormDBCoverage(files, injCanon, injEntry)
		if len(cov.Constrained) != 1 {
			t.Errorf("откат принят за снятие: под гейтом %v", cov.Services())
		}
	})

	t.Run("форма ВЕРНУЛАСЬ новой миграцией — требование включается само", func(t *testing.T) {
		// Самоистечение послабления: перечня исключений нет, и снятие не
		// становится вечным. Третья миграция снова ставит форму — гейт снова
		// требует доказательства, без правки своего кода.
		mig, body := injMigration("sigma", true)
		back := strings.Replace(mig, "715001_resource_name_single_form.sql", "717001_back.sql", 1)
		files := map[string]string{
			mig: body,
			"services/sigma/internal/migrations/716001_drop.sql": "-- +goose Up\nALTER TABLE t DROP COLUMN name;\n",
			back: "-- +goose Up\n" + body,
		}
		cov := analyseNameFormDBCoverage(files, injCanon, injEntry)
		got := cov.Unproven()
		if len(got) != 1 || got[0] != "sigma" {
			t.Fatalf("вернувшаяся форма не потребовала доказательства: %v (под гейтом %v)", got, cov.Services())
		}
		if fs := cov.Constrained["sigma"]; len(fs) != 1 || !strings.HasSuffix(fs[0], "717001_back.sql") {
			t.Errorf("находка обязана называть ДЕЙСТВУЮЩУЮ миграцию формы, а не снятую: %v", fs)
		}
	})

	t.Run("порядок миграций ЧИСЛОВОЙ, а не лексикографический", func(t *testing.T) {
		// `1000001` меньше `715001` как строка и больше как число. При
		// лексикографическом порядке снятие оказалось бы «раньше» постановки, и
		// гейт объявил бы форму действующей там, где её сняли.
		mig, body := injMigration("tau", true)
		files := map[string]string{
			mig: body,
			"services/tau/internal/migrations/1000001_drop.sql": "-- +goose Up\nALTER TABLE t DROP COLUMN name;\n",
		}
		cov := analyseNameFormDBCoverage(files, injCanon, injEntry)
		if len(cov.Constrained) != 0 {
			t.Errorf("снятие с бо́льшим номером не учтено как более позднее: под гейтом %v", cov.Services())
		}
	})

	t.Run("путь ВНЕ services/ сервисом не считается", func(t *testing.T) {
		// Иначе перепись завела бы фантомный «сервис» с именем каталога, и
		// гейт потребовал бы пробу у того, у кого схемы нет.
		files := map[string]string{
			"pkg/db/migrations/0001_x.sql":      "CHECK (name ~ '" + injCanon + "')",
			"pkg/nameformdb/nameformdb_test.go": injProbeSource("nameformdb_test"),
		}
		cov := analyseNameFormDBCoverage(files, injCanon, injEntry)
		if len(cov.Constrained) != 0 || len(cov.Probed) != 0 {
			t.Errorf("путь вне services/ учтён: ставят %v, несут %v", cov.Services(), probedServices(cov))
		}
	})

	t.Run("НЕРАЗБИРАЕМЫЙ файл проб попадает в перепись, а не в «вызова нет»", func(t *testing.T) {
		// «Не разобрали» и «вызова нет» — разные утверждения, и гейт обязан их
		// различать: иначе сломанный файл читался бы как отсутствие пробы, а
		// починка пошла бы не туда.
		mig, body := injMigration("nu", true)
		files := map[string]string{
			mig: body,
			"services/nu/internal/repo/name_form_constraint_integration_test.go": `package repo_test
это не Go, но путь пакета-двигателя тут есть: pkg/nameformdb
`,
		}
		cov := analyseNameFormDBCoverage(files, injCanon, injEntry)
		if len(cov.Unparsed) != 1 ||
			!strings.HasSuffix(cov.Unparsed[0], "name_form_constraint_integration_test.go") {
			t.Fatalf("неразбираемый файл не назван переписью: %v", cov.Unparsed)
		}
		if got := cov.Unproven(); len(got) != 1 || got[0] != "nu" {
			t.Errorf("ожидалась находка про nu, получено %v", got)
		}
	})
}
