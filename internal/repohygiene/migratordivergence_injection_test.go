// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratordivergence_injection_test.go — доказательство, что ведомость различий
// СПОСОБНА упасть в обе стороны и СПОСОБНА смолчать.
//
// Инъекция настоящая: исходники точек наката и обёрток взяты по форме с живых
// (`services/geo` — прямая, `services/nlb` — делегирующая с `--config`,
// `services/iam` — обёртка с конкретным типом диалекта и пустым именем).
//
// Прогонов три, а не два (`testing.md` §Гейт на класс, п. 2в): контроль ·
// нарушение живой строки · нарушение снятой. Без третьего молчание половины,
// держащей снятые различия, неотличимо от её отсутствия.
package repohygiene

import (
	"strings"
	"testing"
)

// ── Исходники для разбора: то, что реально лежит в дереве ──────────────────

const (
	// srcEntryShared — прямая форма после #1461: разбор общий, `--target` и
	// `--dsn` приходят разобранными полями.
	srcEntryShared = `package main

import (
	"os"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

func main() {
	opts, _ := migratorcli.Parse("kacho-migrator", os.Args[1:])
	dsn, _ := migratorcli.ResolveDSN(opts.DSN, nil)
	if opts.Target != "" {
		v, _ := migratorcli.ParseTargetVersion(opts.Target)
		_ = v
	}
	_ = dsn
}`

	// srcEntryCobraWithConfig — делегирующая форма nlb: свой флаг --config.
	srcEntryCobraWithConfig = `package main

import "github.com/spf13/cobra"

func newRoot() *cobra.Command {
	var dsn, cfg, target string
	root := &cobra.Command{Use: "kacho-migrator"}
	root.PersistentFlags().StringVar(&dsn, "dsn", "", "database DSN")
	root.PersistentFlags().StringVar(&cfg, "config", "", "path to config.yaml")
	up := &cobra.Command{Use: "up"}
	up.Flags().StringVar(&target, "target", "", "stop at this version")
	root.AddCommand(up)
	return root
}`

	// srcEntryCobraNoConfig — cobra БЕЗ `--config`: так живут vpc и iam. Фикстура
	// заведена затем, чтобы `--config` в дереве был у ОДНОЙ службы, как и в
	// действительности: пока его несли три, снятие флага у одной не лишало строку
	// ведомости предмета, и инъекция «живая строка потеряла предмет» была
	// неисполнима.
	srcEntryCobraNoConfig = `package main

import "github.com/spf13/cobra"

func newRoot() *cobra.Command {
	var dsn, target string
	root := &cobra.Command{Use: "kacho-migrator"}
	root.PersistentFlags().StringVar(&dsn, "dsn", "", "database DSN")
	up := &cobra.Command{Use: "up"}
	up.Flags().StringVar(&target, "target", "", "stop at this version")
	root.AddCommand(up)
	return root
}`

	// srcEntryStdlibFlag — форма ДО #1461: разбор стандартным flag на месте.
	// Именно он терял флаг, написанный после подкоманды.
	//
	// Флаги --dsn и --target объявлены НАМЕРЕННО: инъекция обязана ронять
	// ровно проверяемое (`testing.md` §Гейт на класс, п. 2в). Без них та же
	// подстановка тянула бы за собой ещё две строки ведомости, и молчание
	// каждой из них стало бы недоказуемым.
	srcEntryStdlibFlag = `package main

import "flag"

func main() {
	dsn := flag.String("dsn", "", "database DSN")
	target := flag.String("target", "", "stop at this version")
	flag.Parse()
	_, _ = dsn, target
}`

	// srcEntryNoTarget — накатывает только до головы: --target нет.
	srcEntryNoTarget = `package main

import (
	"os"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

func main() {
	opts, _ := migratorcli.Parse("kacho-migrator", os.Args[1:])
	dsn, _ := migratorcli.ResolveDSN(opts.DSN, nil)
	_ = dsn
}`

	// srcEntryNoDSN — адрес базы задать нечем: ни флага, ни резолвера.
	srcEntryNoDSN = `package main

import (
	"os"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

func main() {
	opts, _ := migratorcli.Parse("kacho-migrator", os.Args[1:])
	if opts.Target != "" {
		v, _ := migratorcli.ParseTargetVersion(opts.Target)
		_ = v
	}
}`

	// srcEntryNamesOnlyInComments — ЗАКОННЫЙ БЛИЗНЕЦ и главная ловушка.
	// `--target`, `--dsn`, `--config` и flag.Parse названы в ПРОЗЕ — ровно так,
	// как они стоят в шапках всех семи живых точек наката. Гейт по подстроке
	// засчитал бы предмет по объяснению; разбор — не засчитывает.
	srcEntryNamesOnlyInComments = `package main

// kacho-migrator [--dsn DSN] {up|down|status} [--target VERSION]
//
// Разбор аргументов общий: собственный flag.Parse МОЛЧА терял флаг, написанный
// после подкоманды. Флага --config у этого сервиса нет и не будет.
import "os"

func main() { _ = os.Args }`

	// srcWrapperIamLike — обёртка iam: Dialect — конкретный тип, фабрика
	// принимает пустое имя.
	srcWrapperIamLike = `package migrator

import "fmt"

type Dialect struct{}

type DialectSpec struct{ Name string }

var SpecPostgres = DialectSpec{Name: "postgres"}

func NewDialect(name string) (*Dialect, error) {
	switch name {
	case "", SpecPostgres.Name:
		return &Dialect{}, nil
	default:
		return nil, fmt.Errorf("unknown dialect %q", name)
	}
}`

	// srcWrapperVpcLike — обёртка vpc/nlb: Dialect — интерфейс, пустое имя не
	// принимается. Законный близнец предыдущего.
	srcWrapperVpcLike = `package migrator

import "fmt"

type Dialect interface{ Spec() DialectSpec }

type DialectSpec struct{ Name string }

var SpecPostgres = DialectSpec{Name: "postgres"}

func NewDialect(name string) (Dialect, error) {
	if name == SpecPostgres.Name {
		return newPostgresDialect(), nil
	}
	return nil, fmt.Errorf("unknown dialect %q", name)
}`
)

// docWithEveryKey — решение, называющее все ключи ведомости. Строится из самой
// ведомости: выписанный литерал был бы третьим местом об одном предмете.
func docWithEveryKey() string {
	var b strings.Builder
	b.WriteString("# Форма мигратора\n")
	for _, d := range migratorDeclaredDivergences {
		b.WriteString("| `" + d.ID + "` | " + d.What + " |\n")
	}
	return b.String()
}

func entryFactsForProbe(t *testing.T, src string) migratorEntryFacts {
	t.Helper()
	f, err := parseMigratorEntryFacts("services/svc/cmd/migrator/main.go", src)
	if err != nil {
		t.Fatalf("разбор точки наката не удался: %v", err)
	}
	return f
}

func wrapperFactsForProbe(t *testing.T, src string) migratorWrapperFacts {
	t.Helper()
	f, err := parseMigratorWrapperFacts(migratorWrapperFacts{},
		"services/svc/internal/apps/migrator/dialect.go", src)
	if err != nil {
		t.Fatalf("разбор обёртки не удался: %v", err)
	}
	return f
}

// factsAsInTree — состояние дерева: семь точек наката, из них одна с `--config`,
// обёрток НОЛЬ.
//
// Фикстура переехала вместе с предметом (#1383). Прежде она несла три обёртки, из
// них одну с конкретным типом диалекта, — то есть описывала дерево ДО сведения.
// Оставь её как была, и гейт объявил бы снятые строки вернувшимися: фикстура
// утверждала бы о дереве то, чего в нём нет, и краснела бы на исправном.
func factsAsInTree(t *testing.T) []migratorServiceFacts {
	t.Helper()
	shared := entryFactsForProbe(t, srcEntryShared)
	cobraCfg := entryFactsForProbe(t, srcEntryCobraWithConfig)
	cobraPlain := entryFactsForProbe(t, srcEntryCobraNoConfig)

	return []migratorServiceFacts{
		{Service: "compute", Entry: shared},
		{Service: "geo", Entry: shared},
		{Service: "iam", Entry: cobraPlain},
		{Service: "nlb", Entry: cobraCfg},
		{Service: "registry", Entry: shared},
		{Service: "storage", Entry: shared},
		{Service: "vpc", Entry: cobraPlain},
	}
}

// TestMigratorDivergenceParserReadsCodeNotProse — предпосылка всей ведомости.
//
// Все семь живых точек наката называют `--target`, `--dsn` и `flag.Parse` в
// комментариях. Если разбор их засчитывает, ведомость измеряет прозу, а не
// дерево, и снятые строки «не возвращаются» просто потому, что их объяснение
// лежит рядом.
func TestMigratorDivergenceParserReadsCodeNotProse(t *testing.T) {
	prose := entryFactsForProbe(t, srcEntryNamesOnlyInComments)
	if prose.HandlesTarget || prose.ResolvesDSN || prose.DeclaresConfigFlag || prose.ParsesWithStdlibFlag {
		t.Fatalf("имена, названные только в прозе, засчитаны за предмет: %+v — "+
			"ведомость мерила бы собственное объяснение", prose)
	}

	// Положительная половина: без неё отрицание выше зеленело бы и на разборе,
	// который не находит вообще ничего.
	code := entryFactsForProbe(t, srcEntryShared)
	if !code.HandlesTarget || !code.ResolvesDSN {
		t.Fatalf("настоящий разбор не распознан: %+v", code)
	}
	if code.DeclaresConfigFlag {
		t.Errorf("прямая форма объявлена несущей --config, хотя его там нет")
	}
	if cfg := entryFactsForProbe(t, srcEntryCobraWithConfig); !cfg.DeclaresConfigFlag {
		t.Errorf("объявление флага --config не распознано")
	}
	if std := entryFactsForProbe(t, srcEntryStdlibFlag); !std.ParsesWithStdlibFlag {
		t.Errorf("разбор стандартным flag не распознан")
	}

	iam := wrapperFactsForProbe(t, srcWrapperIamLike)
	vpc := wrapperFactsForProbe(t, srcWrapperVpcLike)
	if iam.DialectIsInterface || !iam.DialectAcceptsEmptyName {
		t.Errorf("обёртка с конкретным типом и пустым именем разобрана неверно: %+v", iam)
	}
	if !vpc.DialectIsInterface || vpc.DialectAcceptsEmptyName {
		t.Errorf("обёртка-близнец разобрана неверно: %+v", vpc)
	}
}

// TestMigratorDivergenceGateSpeaksOnADefect — гейт КРАСНЕЕТ на каждом настоящем
// нарушении ведомости и НАЗЫВАЕТ ключ.
func TestMigratorDivergenceGateSpeaksOnADefect(t *testing.T) {
	doc := docWithEveryKey()

	t.Run("живая строка потеряла предмет", func(t *testing.T) {
		// `--config` есть только у nlb. Уберём его — единственный предмет
		// единственной живой строки исчезнет. Это и есть тот день, ради которого
		// гейт заведён: строка обязана быть помечена снятой, а не остаться
		// предъявлять снятое как действующее.
		facts := factsAsInTree(t)
		plain := entryFactsForProbe(t, srcEntryCobraNoConfig)
		for i := range facts {
			if facts[i].Service == "nlb" {
				facts[i].Entry = plain
			}
		}
		got := migratorDivergenceFindings(migratorDeclaredDivergences, facts, doc)
		if len(got) != 1 {
			t.Fatalf("находок %d, ожидалась одна (config-flag-only-here): %v", len(got), got)
		}
		if !strings.Contains(got[0], "config-flag-only-here") ||
			!strings.Contains(got[0], "ПОТЕРЯЛО ПРЕДМЕТ") ||
			!strings.Contains(got[0], migratorFormDecisionDoc) {
			t.Errorf("находка не называет ни ключ, ни предмет, ни документ: %v", got)
		}
	})

	// Снятие двух строк про диалект (#1383) проверяется С ОБЕИХ сторон: молчание
	// на сведённом дереве — ниже, в законных близнецах; возвращение — здесь.
	// Односторонняя проверка зеленела бы на ведомости, которая ничего не ловит.
	for _, tc := range []struct {
		name, key, src string
	}{
		{"вернулась обёртка с конкретным типом диалекта", "dialect-not-an-interface", srcWrapperIamLike},
		{"вернулась обёртка с интерфейсом", "dialect-empty-accepted", srcWrapperVpcLike},
	} {
		t.Run("снятое различие вернулось: "+tc.name, func(t *testing.T) {
			facts := factsAsInTree(t)
			wrap := wrapperFactsForProbe(t, tc.src)
			for i := range facts {
				if facts[i].Service == "iam" {
					facts[i].Wrapper = wrap
				}
			}
			got := migratorDivergenceFindings(migratorDeclaredDivergences, facts, doc)
			joined := strings.Join(got, "\n")
			// Обёртка с конкретным типом возвращает ОБЕ строки сразу (она же
			// принимает пустое имя), обёртка с интерфейсом — ни одной: у неё
			// предмета нет. Утверждается поэтому присутствие проверяемого ключа
			// там, где он ожидается, и его отсутствие там, где нет.
			if tc.key == "dialect-not-an-interface" {
				if !strings.Contains(joined, tc.key) || !strings.Contains(joined, "ВЕРНУЛОСЬ") {
					t.Fatalf("возвращение %s не поймано: %v", tc.key, got)
				}
				if !strings.Contains(joined, "#1383") {
					t.Errorf("находка не называет, чем строка была снята: %v", got)
				}
				return
			}
			if len(got) != 0 {
				t.Fatalf("обёртка с интерфейсом и строгим именем предмета снятых строк "+
					"не даёт, а гейт краснеет: %v", got)
			}
		})
	}

	// По одной инъекции на КАЖДУЮ снятую строку, и каждая роняет ровно свою:
	// иначе молчание соседних строк неотличимо от их отсутствия.
	for _, tc := range []struct {
		name, key, src string
	}{
		{"вернулся разбор стандартным flag", "flag-after-subcommand-lost", srcEntryStdlibFlag},
		{"вернулась точка наката без --target", "target-missing", srcEntryNoTarget},
		{"вернулась точка наката без --dsn", "dsn-missing", srcEntryNoDSN},
	} {
		t.Run("снятое различие вернулось: "+tc.name, func(t *testing.T) {
			facts := append(factsAsInTree(t), migratorServiceFacts{
				Service: "newsvc",
				Entry:   entryFactsForProbe(t, tc.src),
			})
			got := migratorDivergenceFindings(migratorDeclaredDivergences, facts, doc)
			if len(got) != 1 {
				t.Fatalf("находок %d, ожидалась ровно одна (%s) — инъекция обязана "+
					"ронять только проверяемое: %v", len(got), tc.key, got)
			}
			for _, want := range []string{"ВЕРНУЛОСЬ", tc.key, "newsvc", "#1461"} {
				if !strings.Contains(got[0], want) {
					t.Errorf("находка не называет %q: %s", want, got[0])
				}
			}
		})
	}

	t.Run("ключ не назван в решении", func(t *testing.T) {
		got := migratorDivergenceFindings(migratorDeclaredDivergences, factsAsInTree(t),
			"# Форма мигратора\nтаблицы различий здесь нет\n")
		if len(got) != len(migratorDeclaredDivergences) {
			t.Fatalf("находок %d, ожидалось %d — по одной на каждую строку ведомости: %v",
				len(got), len(migratorDeclaredDivergences), got)
		}
		if !strings.Contains(strings.Join(got, "\n"), "не названо ключом") {
			t.Errorf("расхождение ведомости и решения не названо: %v", got)
		}
	})
}

// TestMigratorDivergenceGateStaysSilentOnLegitimateTwins — законные состояния
// гейт НЕ трогает. Без этой половины он ловил бы форму, а не существо.
func TestMigratorDivergenceGateStaysSilentOnLegitimateTwins(t *testing.T) {
	doc := docWithEveryKey()

	t.Run("дерево как есть", func(t *testing.T) {
		if got := migratorDivergenceFindings(migratorDeclaredDivergences, factsAsInTree(t), doc); len(got) != 0 {
			t.Errorf("на состоянии дерева гейт краснеет: %v", got)
		}
	})

	// ГЛАВНОЕ: гейт обязан проходить на ДОСТИЖЕНИИ СВОЕЙ ЦЕЛИ. Пустая ведомость
	// означает, что различий не осталось вовсе, — это цель решения, а не
	// поломка. Гейт, краснеющий на пустой ведомости, толкал бы держать строку
	// ради зелёного.
	t.Run("пустая ведомость не роняет гейт", func(t *testing.T) {
		if got := migratorDivergenceFindings(nil, factsAsInTree(t), doc); len(got) != 0 {
			t.Errorf("пустая ведомость объявлена находкой: %v", got)
		}
	})

	t.Run("снятое различие остаётся снятым", func(t *testing.T) {
		// Все семь несут общий разбор — ни одна снятая строка не возвращается,
		// и ни одна живая не теряет предмета.
		var closed []migratorDivergence
		for _, d := range migratorDeclaredDivergences {
			if d.Closed != "" {
				closed = append(closed, d)
			}
		}
		if len(closed) == 0 {
			t.Fatalf("снятых строк в ведомости нет — эта половина проверяет воздух")
		}
		if got := migratorDivergenceFindings(closed, factsAsInTree(t), doc); len(got) != 0 {
			t.Errorf("снятые строки объявлены находкой на чистом дереве: %v", got)
		}
	})
}
