// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// Инъекция гейта IAM-RV-1-12 «у проекции роли один писатель» — В ОБЕ СТОРОНЫ.
//
// Гейт обходит дерево, поэтому его признак воспроизводится здесь над СИНТЕТИЧЕСКИМ
// входом: доказывается, что он различает ЗАПИСЬ и ЧТЕНИЕ, приписывает находку
// функции и молчит на законных формах. Инъекция правкой настоящего дерева не
// ставится намеренно — она рвала бы чужие прогоны в общей рабочей копии.
//
// Прогонов по каждой оси ТРИ, а не два: контроль · инъекция проверяемого ·
// законный близнец. Без третьего молчание проверки неотличимо от её смерти.

// writeVerbsOf — плоский перечень найденных операторов записи (для утверждений).
func writeVerbsOf(t *testing.T, src string) []string {
	t.Helper()
	writes, _, err := roleVerbWritesIn("zz_injection.go", src)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	out := make([]string, 0, len(writes))
	for _, w := range writes {
		out = append(out, w.Func+" → "+w.Verb)
	}
	return out
}

// TestIAMRV112_InjectionRedOnASecondWriter — второй писатель со своим сырым SQL
// в слое досева → признак КРАСНЕЕТ и НАЗЫВАЕТ функцию.
func TestIAMRV112_InjectionRedOnASecondWriter(t *testing.T) {
	src := `package seed

func replaceRoleVerbsTx(ctx context.Context, tx pgxExecer, roleID string) error {
	if _, err := tx.Exec(ctx, ` + "`" + `DELETE FROM kacho_iam.role_verb WHERE role_id = $1` + "`" + `, roleID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, ` + "`" + `INSERT INTO kacho_iam.role_verb (role_id, object_type, verb)
		 VALUES ($1, $2, $3)` + "`" + `)
	return err
}
`
	got := writeVerbsOf(t, src)
	if len(got) == 0 {
		t.Fatal("признак МОЛЧИТ на записи в проекцию роли — гейт не способен покраснеть, " +
			"и его зелёный на дереве ничего не значит")
	}
	for _, g := range got {
		if !strings.HasPrefix(g, "replaceRoleVerbsTx →") {
			t.Errorf("находка не приписана функции: %q — читатель пойдёт искать координату "+
				"и не найдёт её", g)
		}
	}
	if len(got) != 2 {
		t.Errorf("операторов записи найдено %d, а в синтетике их два (DELETE и INSERT): %v", len(got), got)
	}
}

// TestIAMRV112_InjectionSilentOnBothReaders — ЗАКОННЫЙ БЛИЗНЕЦ, названный ЦЕЛИКОМ.
//
// Читателей проекции в дереве ДВА, и обе формы чтения обязаны молчать: одна
// считает строки таблицы целиком, вторая — по одной роли. Близнец, названный
// одним экземпляром, оставил бы вторую форму непроверенной, и гейт, ловящий
// `SELECT` за запись, покраснел бы на ней при первом же прогоне.
func TestIAMRV112_InjectionSilentOnBothReaders(t *testing.T) {
	whole := `package scalegrid

func (c *census) read() error {
	return scalar(&c.RoleVerbs, ` + "`" + `SELECT count(*)::bigint FROM kacho_iam.role_verb` + "`" + `)
}
`
	perRole := `package scalegrid

func strengthOf(ctx context.Context, roleID string) error {
	return q.QueryRow(ctx,
		` + "`" + `SELECT count(*)::bigint FROM kacho_iam.role_verb WHERE role_id = $1` + "`" + `, roleID).Scan(&n)
}
`
	for name, src := range map[string]string{
		"чтение таблицы целиком": whole,
		"чтение по одной роли":   perRole,
	} {
		if got := writeVerbsOf(t, src); len(got) != 0 {
			t.Errorf("признак краснеет на ЧТЕНИИ проекции (%s): %v — то есть на том, ради чего "+
				"таблица и заведена", name, got)
		}
	}
}

// TestIAMRV112_InjectionSilentOnItsOwnExplanation — ЗАКОННЫЙ БЛИЗНЕЦ второго рода:
// имя таблицы и слово INSERT в КОММЕНТАРИИ, объясняющем эту самую проверку.
//
// Гейт по подстроке над текстом файла краснел бы здесь — то есть на собственном
// объяснении. Признак судит узел-литерал разобранного дерева и обязан молчать.
func TestIAMRV112_InjectionSilentOnItsOwnExplanation(t *testing.T) {
	src := `package repohygiene

// Предмет: INSERT INTO kacho_iam.role_verb из второго места — находка.
// Проверяется оператор DELETE FROM kacho_iam.role_verb тоже.
func explain() {}
`
	if got := writeVerbsOf(t, src); len(got) != 0 {
		t.Errorf("признак краснеет на КОММЕНТАРИИ, объясняющем проверку: %v — гейт, красный "+
			"на собственном объяснении, снимут первым", got)
	}
}

// TestIAMRV112_InjectionSilentOnAnotherTable — запись в чужую таблицу не находка.
func TestIAMRV112_InjectionSilentOnAnotherTable(t *testing.T) {
	src := `package pg

func put(ctx context.Context) error {
	_, err := tx.Exec(ctx, ` + "`" + `INSERT INTO kacho_iam.role_rule_selectors (role_id) VALUES ($1)` + "`" + `)
	return err
}
`
	if got := writeVerbsOf(t, src); len(got) != 0 {
		t.Errorf("признак краснеет на записи в ЧУЖУЮ таблицу: %v", got)
	}
}

// TestIAMRV112_LayerPredicateSeparatesRepoFromSeed — вторая ось гейта: слой.
//
// Она проверяется отдельно от первой, потому что инъекция обязана ронять ТОЛЬКО
// проверяемое: писатель может быть ОДИН и при этом лежать не там.
func TestIAMRV112_LayerPredicateSeparatesRepoFromSeed(t *testing.T) {
	inRepo := "services/iam/internal/repo/kacho/pg/role_repo.go"
	inSeed := "services/iam/internal/apps/kacho/seed/migrate_backfill.go"

	if !strings.Contains("/"+inRepo, roleVerbWriterLayer) {
		t.Errorf("предикат слоя не признаёт законного места писателя (%s) — гейт краснел бы "+
			"на единственно верной раскладке", inRepo)
	}
	if strings.Contains("/"+inSeed, roleVerbWriterLayer) {
		t.Errorf("предикат слоя признаёт своим SQL в слое use-case (%s) — вторая ось гейта "+
			"вакуумна", inSeed)
	}
}
