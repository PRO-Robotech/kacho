// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// nameformdbprobe_injection_test.go — способность гейта упасть и смолчать,
// доказанная настоящим входом обеих сторон.
//
// Инъекция без законного близнеца доказывает только чувствительность к форме:
// гейт, краснеющий на всём, отключат при первом же ложном срабатывании. Поэтому
// каждая проба здесь идёт парой — дефект и та же конструкция без дефекта.
//
// Разбор зовётся ТОТ ЖЕ, что и на настоящем дереве (`analyseNameFormDBCoverage`
// из не-тестового файла): своя копия доказывала бы способность упасть у кода,
// который не исполняется.
package repohygiene

import (
	"testing"
)

// injCanon — форма, которую гейт получает параметром. Здесь она подставная и
// намеренно НЕ совпадает с настоящим каноном: гейт обязан судить по тому, что
// ему дали, а выписанный настоящий канон запрещён соседним гейтом
// TestResourceNameFormIsDeclaredOnce.
const injCanon = `^INJECTED-FORM$`

func injMigration(svc string, carriesForm bool) (string, string) {
	body := "ALTER TABLE t ADD CONSTRAINT t_name_check CHECK (name ~ 'что-то другое');"
	if carriesForm {
		body = "ALTER TABLE t ADD CONSTRAINT t_name_check CHECK (name ~ '" + injCanon + "');"
	}
	return "services/" + svc + "/internal/migrations/715001_resource_name_single_form.sql", body
}

func TestNameFormDBCoverage_FailsOnInjectedDefect(t *testing.T) {
	t.Run("законный близнец: миграция и проба у одного сервиса — находок ноль", func(t *testing.T) {
		mig, body := injMigration("alpha", true)
		files := map[string]string{
			mig: body,
			"services/alpha/internal/repo/name_form_constraint_integration_test.go": "nameformdb.Probe{Schema: \"kacho_alpha\"}",
		}
		cov := analyseNameFormDBCoverage(files, injCanon)
		if got := cov.Unproven(); len(got) != 0 {
			t.Errorf("сервис с пробой объявлен недоказанным: %v", got)
		}
		if cov.MigrationsRead != 1 || cov.TestsRead != 1 {
			t.Errorf("перепись: прочитано миграций %d, проб %d — ожидалось по одному",
				cov.MigrationsRead, cov.TestsRead)
		}
	})

	t.Run("проба ОТСУТСТВУЕТ — находка называет сервис", func(t *testing.T) {
		mig, body := injMigration("beta", true)
		files := map[string]string{
			mig: body,
			// Проба у сервиса есть, но она про другое: марке́ра двигателя в ней нет.
			"services/beta/internal/repo/other_integration_test.go": "func TestSomethingElse(t *testing.T) {}",
		}
		cov := analyseNameFormDBCoverage(files, injCanon)
		got := cov.Unproven()
		if len(got) != 1 || got[0] != "beta" {
			t.Errorf("ожидалась ровно одна находка про beta, получено %v", got)
		}
	})

	t.Run("проба у ЧУЖОГО сервиса не засчитывается", func(t *testing.T) {
		// Двигатель зовут, но из другого сервиса: доказательство принадлежит
		// той схеме, которую оно обходит, и «где-то в дереве есть проба» —
		// не то же самое, что «эта схема доказана».
		migA, bodyA := injMigration("gamma", true)
		migB, bodyB := injMigration("delta", true)
		files := map[string]string{
			migA: bodyA,
			migB: bodyB,
			"services/delta/internal/repo/name_form_constraint_integration_test.go": "nameformdb.Probe{}",
		}
		cov := analyseNameFormDBCoverage(files, injCanon)
		got := cov.Unproven()
		if len(got) != 1 || got[0] != "gamma" {
			t.Errorf("ожидалась ровно одна находка про gamma, получено %v", got)
		}
	})

	t.Run("вызов двигателя в ПРОД-коде доказательством не является", func(t *testing.T) {
		mig, body := injMigration("epsilon", true)
		files := map[string]string{
			mig: body,
			// Не `_test.go`: упоминание в прод-коде ничего не исполняет.
			"services/epsilon/internal/repo/helper.go": "nameformdb.Probe{}",
		}
		cov := analyseNameFormDBCoverage(files, injCanon)
		got := cov.Unproven()
		if len(got) != 1 || got[0] != "epsilon" {
			t.Errorf("ожидалась находка про epsilon, получено %v", got)
		}
	})

	t.Run("миграция БЕЗ формы под гейт не подпадает", func(t *testing.T) {
		// Положительный контроль к самому отбору: сервис, который формы не
		// ставит, не обязан ничего доказывать. Без этого подслучая гейт
		// требовал бы пробу от каждого сервиса дерева.
		mig, body := injMigration("zeta", false)
		cov := analyseNameFormDBCoverage(map[string]string{mig: body}, injCanon)
		if len(cov.Constrained) != 0 {
			t.Errorf("сервис без формы попал под гейт: %v", cov.Services())
		}
		if cov.MigrationsRead != 1 {
			t.Errorf("перепись: миграция не прочитана (%d) — «под гейт не подпадает» стало бы "+
				"неотличимо от «файл не читали»", cov.MigrationsRead)
		}
	})

	t.Run("путь ВНЕ services/ сервисом не считается", func(t *testing.T) {
		// Иначе перепись завела бы фантомный «сервис» с именем каталога, и
		// гейт потребовал бы пробу у того, у кого схемы нет.
		files := map[string]string{
			"pkg/db/migrations/0001_x.sql":      "CHECK (name ~ '" + injCanon + "')",
			"internal/nameformdb/nameformdb.go": "nameformdb.Probe",
		}
		cov := analyseNameFormDBCoverage(files, injCanon)
		if len(cov.Constrained) != 0 || len(cov.Probed) != 0 {
			t.Errorf("путь вне services/ учтён: ставят %v, несут %v", cov.Services(), probedServices(cov))
		}
	})
}
