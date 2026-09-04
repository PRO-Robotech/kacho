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

// Формы взяты дословно с живых точек наката. Действующая — обращение к общему
// накату (все семь); снятые — прямой goose и per-service обёртка, и обе оставлены
// фикстурами намеренно: их возвращение обязано быть находкой, а не молчанием.
const (
	srcDelegating = `package main
import (
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/PRO-Robotech/kacho/pkg/migratorrun"
)
func main() { _ = migratorrun.New }`

	// srcDirect — снятая форма: goose зовётся прямо из main.go.
	srcDirect = `package main
import (
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)
func main() { _ = goose.Up }`

	// srcWrapper — снятая форма: своя per-service обёртка.
	srcWrapper = `package main
import "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/migrator"
func main() { _ = migrator.New }`

	// srcThirdForm — накат заведён как-то ещё: ни общего, ни goose, ни обёртки.
	srcThirdForm = `package main
import "github.com/PRO-Robotech/kacho/pkg/db"
func main() { _ = db.Open }`

	// srcBothForms — и общий накат, и прямой goose сразу.
	srcBothForms = `package main
import (
	"github.com/pressly/goose/v3"
	"github.com/PRO-Robotech/kacho/pkg/migratorrun"
)
func main() { _, _ = goose.Up, migratorrun.New }`

	// srcGooseOnlyInComment — ЗАКОННЫЙ БЛИЗНЕЦ и главная ловушка: имя goose
	// стоит в прозе (в живом main.go — в длинном разборе про пропущенные
	// миграции), но импорта нет. Гейт по подстроке засчитал бы форму по
	// ОБЪЯСНЕНИЮ; разбор импортов — не засчитывает.
	srcGooseOnlyInComment = `package main
// Накат идёт через общий пакет. Прямой github.com/pressly/goose/v3 здесь НЕ
// импортируется намеренно: форма делегирующая, см. docs/architecture.
import "github.com/PRO-Robotech/kacho/pkg/migratorrun"
func main() { _ = migratorrun.New }`
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
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"вторая форма: прямой goose", srcDirect, "зовёт goose НАПРЯМУЮ"},
		{"вторая форма: своя обёртка", srcWrapper, "импортирует СВОЙ пакет-обёртку"},
		{"накат заведён как-то ещё", srcThirdForm, "не обращается к общему накату"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := classifyForProbe(t, tc.src)
			if f.Recognised() {
				t.Fatalf("снятая форма распознана как действующая — гейт вакуумен")
			}
			got := migratorFormFindings([]migratorForm{f}, migratorWrapperCeiling)
			if len(got) != 1 {
				t.Fatalf("находок %d, ожидалась одна: %v", len(got), got)
			}
			if !strings.Contains(got[0], tc.want) ||
				!strings.Contains(got[0], "services/svc/cmd/migrator/main.go") {
				t.Errorf("находка не называет предмет и координату: %q", got[0])
			}
		})
	}

	t.Run("обе формы сразу", func(t *testing.T) {
		f := classifyForProbe(t, srcBothForms)
		if f.Recognised() {
			t.Fatalf("«и та, и другая» распознано как действующая форма")
		}
		got := migratorFormFindings([]migratorForm{f}, migratorWrapperCeiling)
		if len(got) != 1 || !strings.Contains(got[0], "зовёт goose НАПРЯМУЮ") {
			t.Fatalf("находка про возвращённую вторую форму не выдана: %v", got)
		}
	})

	t.Run("первая копия обёртки", func(t *testing.T) {
		legit := classifyForProbe(t, srcDelegating)
		got := migratorFormFindings([]migratorForm{legit}, migratorWrapperCeiling+1)
		if len(got) != 1 || !strings.Contains(got[0], "копий пакета-обёртки 1") {
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
		{"действующая форма", srcDelegating},
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
		if got := migratorFormFindings([]migratorForm{f}, migratorWrapperCeiling); len(got) != 0 {
			t.Errorf("при %d копиях гейт краснеет, хотя это цель решения: %v",
				migratorWrapperCeiling, got)
		}
	})
}
