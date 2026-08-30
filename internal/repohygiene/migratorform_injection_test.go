// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratorform_injection_test.go — доказательство, что гейт формы мигратора
// СПОСОБЕН упасть и СПОСОБЕН смолчать.
//
// Инъекция настоящая (исходник точки наката), а не выдуманная форма: каждый
// случай — то, что реально может появиться в дереве.
package repohygiene

import (
	"strings"
	"testing"
)

// Формы взяты дословно с живых точек наката: делегирующая — у vpc/iam/nlb,
// прямая — у compute/geo/registry/storage.
const (
	srcDelegating = `package main
import (
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/migrator"
)
func main() { _ = migrator.New }`

	srcDirect = `package main
import (
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)
func main() { _ = goose.Up }`

	// srcThirdForm — накат заведён как-то ещё: ни своей обёртки, ни goose.
	srcThirdForm = `package main
import "github.com/PRO-Robotech/kacho/pkg/db"
func main() { _ = db.Open }`

	// srcBothForms — и обёртка, и прямой goose сразу.
	srcBothForms = `package main
import (
	"github.com/pressly/goose/v3"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/migrator"
)
func main() { _, _ = goose.Up, migrator.New }`

	// srcGooseOnlyInComment — ЗАКОННЫЙ БЛИЗНЕЦ и главная ловушка: имя goose
	// стоит в прозе (в живом compute/main.go — в длинном разборе про
	// пропущенные миграции), но вызова нет. Гейт по подстроке засчитал бы
	// форму по ОБЪЯСНЕНИЮ; разбор импортов — не засчитывает.
	srcGooseOnlyInComment = `package main
// Накат идёт через свою обёртку. Прямой github.com/pressly/goose/v3 здесь НЕ
// импортируется намеренно: форма делегирующая, см. docs/architecture.
import "github.com/PRO-Robotech/kacho/services/iam/internal/apps/migrator"
func main() { _ = migrator.New }`
)

func classifyForProbe(t *testing.T, src string) migratorForm {
	t.Helper()
	f, err := classifyMigratorEntryPoint("svc", "services/svc/cmd/migrator/main.go", src)
	if err != nil {
		t.Fatalf("разбор синтетики не удался: %v", err)
	}
	return f
}

// TestMigratorFormGateSpeaksOnADefect — гейт КРАСНЕЕТ на каждой настоящей форме
// дефекта и НАЗЫВАЕТ координату.
func TestMigratorFormGateSpeaksOnADefect(t *testing.T) {
	t.Run("третья форма", func(t *testing.T) {
		f := classifyForProbe(t, srcThirdForm)
		if f.Recognised() {
			t.Fatalf("третья форма распознана как законная — гейт вакуумен")
		}
		got := migratorFormFindings([]migratorForm{f}, migratorWrapperCeiling)
		if len(got) != 1 {
			t.Fatalf("находок %d, ожидалась одна: %v", len(got), got)
		}
		if !strings.Contains(got[0], "ТРЕТЬЯ форма") ||
			!strings.Contains(got[0], "services/svc/cmd/migrator/main.go") {
			t.Errorf("находка не называет предмет и координату: %q", got[0])
		}
	})

	t.Run("обе формы сразу", func(t *testing.T) {
		f := classifyForProbe(t, srcBothForms)
		if f.Recognised() {
			t.Fatalf("«и та, и другая» распознано как одна форма")
		}
		got := migratorFormFindings([]migratorForm{f}, migratorWrapperCeiling)
		if len(got) != 1 || !strings.Contains(got[0], "обе формы сразу") {
			t.Fatalf("находка про обе формы не выдана: %v", got)
		}
	})

	t.Run("четвёртая копия обёртки", func(t *testing.T) {
		legit := classifyForProbe(t, srcDelegating)
		got := migratorFormFindings([]migratorForm{legit}, migratorWrapperCeiling+1)
		if len(got) != 1 || !strings.Contains(got[0], "копий пакета-обёртки 4") {
			t.Fatalf("рост числа копий не пойман: %v", got)
		}
	})
}

// TestMigratorFormGateStaysSilentOnLegitimateTwins — законные формы гейт НЕ
// трогает. Без этой половины он ловил бы форму записи, а не существо, и первый
// же ложный срабат его отключил бы.
func TestMigratorFormGateStaysSilentOnLegitimateTwins(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"делегирующая форма", srcDelegating},
		{"прямая форма", srcDirect},
		{"goose назван только в комментарии", srcGooseOnlyInComment},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := classifyForProbe(t, tc.src)
			if !f.Recognised() {
				t.Fatalf("законная форма не распознана: delegating=%v direct=%v",
					f.Delegating, f.Direct)
			}
			if got := migratorFormFindings([]migratorForm{f}, migratorWrapperCeiling); len(got) != 0 {
				t.Errorf("законная форма объявлена находкой: %v", got)
			}
		})
	}

	// ГЛАВНОЕ: гейт обязан проходить на ДОСТИЖЕНИИ СВОЕЙ ЦЕЛИ. Сведение копий —
	// то, ради чего решение принято; гейт, краснеющий на сведении, поставил бы
	// исполнителя перед выбором «сделать верно или получить зелёное».
	t.Run("сведение копий не роняет гейт", func(t *testing.T) {
		f := classifyForProbe(t, srcDelegating)
		for _, copies := range []int{migratorWrapperCeiling, 1, 0} {
			if got := migratorFormFindings([]migratorForm{f}, copies); len(got) != 0 {
				t.Errorf("при %d копиях гейт краснеет, хотя это цель решения: %v", copies, got)
			}
		}
	})
}
