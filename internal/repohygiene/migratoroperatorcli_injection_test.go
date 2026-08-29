// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// Доказательство способности гейта поверхности CLI мигратора УПАСТЬ и
// СМОЛЧАТЬ. Инъекция идёт в разбор (чистые функции), а не в дерево: предмет
// проверки — распознавание, и оно обязано быть проверено на каждой законной
// форме записи имени отдельно, иначе форма, о которой распознаватель не знает,
// остаётся не находкой, а НЕВИДИМОСТЬЮ.
//
// Каждая проба ломает РОВНО ОДНО свойство: инъекция, попутно нарушающая
// соседнее требование, доказывала бы работу соседа, а не проверяемого.

// ── имя бинаря: гейт говорит ───────────────────────────────────────────────

func TestMigratorCLINameGateSpeaksOnEveryFormOfTheName(t *testing.T) {
	for _, tc := range []struct {
		name string
		rel  string
		src  string
	}{
		{
			name: "путь установки в манифесте",
			rel:  "services/x/deploy/templates/deployment.yaml",
			src:  "        - name: migrate\n          command: [\"/usr/local/bin/migrator\"]\n",
		},
		{
			name: "выход сборки в Dockerfile",
			rel:  "services/x/Dockerfile",
			src:  "RUN CGO_ENABLED=0 go build -o /out/migrator ./services/x/cmd/migrator\n",
		},
		{
			name: "переменная сборки в Makefile",
			rel:  "services/x/Makefile",
			src:  "BINARY_MIG     := migrator\n",
		},
		{
			name: "константа имени в точке наката",
			rel:  "services/x/cmd/migrator/main.go",
			src:  "package main\n\nconst binaryName = \"migrator\"\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mentions := migratorCLIMentions(tc.rel, tc.src)
			if len(mentions) == 0 {
				t.Fatalf("форма не распознана вовсе — она была бы не находкой, а невидимостью")
			}
			findings := migratorCLINameFindings(mentions)
			if len(findings) == 0 {
				t.Fatalf("чужое имя принято молча: %+v", mentions)
			}
			joined := strings.Join(findings, "\n")
			if !strings.Contains(joined, tc.rel) {
				t.Errorf("находка не называет координату: %s", joined)
			}
			if !strings.Contains(joined, `"migrator"`) {
				t.Errorf("находка не называет само имя: %s", joined)
			}
		})
	}
}

// ── имя бинаря: гейт молчит на законных близнецах ──────────────────────────

func TestMigratorCLINameGateStaysSilentOnLegitimateTwins(t *testing.T) {
	for _, tc := range []struct {
		name        string
		rel         string
		src         string
		wantMention bool // распознано как место, называющее бинарь
	}{
		{
			name:        "то же место с общим именем",
			rel:         "services/x/deploy/templates/deployment.yaml",
			src:         "          command: [\"/usr/local/bin/kacho-migrator\"]\n",
			wantMention: true,
		},
		{
			name:        "выход сборки с общим именем",
			rel:         "services/x/Dockerfile",
			src:         "RUN go build -o /out/kacho-migrator ./services/x/cmd/migrator\n",
			wantMention: true,
		},
		{
			name:        "переменная сборки с общим именем",
			rel:         "services/x/Makefile",
			src:         "MIGRATOR_BIN   := kacho-migrator\n",
			wantMention: true,
		},
		{
			name: "каталог ИСХОДНИКОВ, а не имя бинаря",
			rel:  "services/x/Makefile",
			// Эта строка есть у всех семи. Засчитать её именем значило бы
			// краснеть на каждом сервисе сразу — гейт с такими находками
			// отключают первым.
			src:         "CMD_MIG        := ./cmd/migrator\nMIGRATOR_CMD   := ./cmd/migrator\n",
			wantMention: false,
		},
		{
			name:        "старое имя в ПРОЗЕ о нём",
			rel:         "services/x/Dockerfile",
			src:         "# бинарь звался /usr/local/bin/migrator до #1461\n",
			wantMention: false,
		},
		{
			name:        "посторонний бинарь рядом",
			rel:         "services/x/Dockerfile",
			src:         "COPY --from=builder /out/kacho-loadbalancer /usr/local/bin/kacho-loadbalancer\n",
			wantMention: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mentions := migratorCLIMentions(tc.rel, tc.src)
			if got := len(mentions) > 0; got != tc.wantMention {
				t.Fatalf("распознано мест %d, ожидалось непусто=%v: %+v", len(mentions), tc.wantMention, mentions)
			}
			if f := migratorCLINameFindings(mentions); len(f) != 0 {
				t.Fatalf("законная запись объявлена находкой: %v", f)
			}
		})
	}
}

// ── разбор аргументов: гейт говорит ────────────────────────────────────────

const (
	cliSrcThirdParser = `package main

import "flag"

func main() { flag.Parse() }`

	// Оба разбора ИСПОЛНЯЮТСЯ в одной точке: общий зовётся, и рядом строится
	// дерево cobra. Импорта мало — разбирает тот, кто зовёт.
	cliSrcBothParsers = `package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

func main() {
	_, _ = migratorcli.Parse("kacho-migrator", os.Args[1:])
	_ = (&cobra.Command{Use: "kacho-migrator"}).Execute()
}`

	cliSrcCobraWithoutArgs = `package main

import "github.com/spf13/cobra"

func newUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:  "up",
		RunE: func(cmd *cobra.Command, args []string) error { return nil },
	}
}`
)

func TestMigratorCLIParserGateSpeaksOnADefect(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"третий разбор аргументов", cliSrcThirdParser, "третий разбор"},
		{"оба разбора сразу", cliSrcBothParsers, "по коду не сказать"},
		{"команда cobra не решила Args", cliSrcCobraWithoutArgs, "поле Args"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := classifyMigratorCLIParser("services/x/cmd/migrator/main.go", tc.src)
			if err != nil {
				t.Fatalf("разбор не удался: %v", err)
			}
			findings := migratorCLIParserFindings([]migratorCLIParser{parsed})
			if len(findings) == 0 {
				t.Fatalf("дефект принят молча: %+v", parsed)
			}
			joined := strings.Join(findings, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Errorf("находка не называет предмет %q: %s", tc.want, joined)
			}
			if !strings.Contains(joined, "services/x/cmd/migrator/main.go") {
				t.Errorf("находка не называет координату: %s", joined)
			}
		})
	}
}

// ── разбор аргументов: гейт молчит на законных близнецах ───────────────────

const (
	cliSrcSharedParser = `package main

import (
	"os"

	"github.com/pressly/goose/v3"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

func main() {
	_, _ = migratorcli.Parse("kacho-migrator", os.Args[1:])
	_ = goose.Up
}`

	cliSrcCobraDecided = `package main

import (
	"errors"

	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:  "kacho-migrator",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return errors.New("no command given") },
	}
	root.AddCommand(&cobra.Command{
		Use:  "up",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return nil },
	})
	return root
}`

	cliSrcCobraDecidedOtherwise = `package main

import "github.com/spf13/cobra"

func newUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:  "up",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return nil },
	}
}`
)

func TestMigratorCLIParserGateStaysSilentOnLegitimateTwins(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		// Прямая форма и сегодня зовёт goose сама — импорт goose рядом с общим
		// разбором законен и третьей формой НЕ является.
		{"общий разбор рядом с goose", cliSrcSharedParser},
		// Корень несёт исполнение и решил Args; подкоманда — тоже.
		{"корень отвечает на пустую строку, подкоманда решила Args", cliSrcCobraDecided},
		// Гейт судит, РЕШЁН ли вопрос, а не как именно.
		{"Args решён не через NoArgs", cliSrcCobraDecidedOtherwise},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := classifyMigratorCLIParser("services/x/cmd/migrator/main.go", tc.src)
			if err != nil {
				t.Fatalf("разбор не удался: %v", err)
			}
			if !parsed.Recognised() {
				t.Fatalf("законная форма не распознана: %+v", parsed)
			}
			if f := migratorCLIParserFindings([]migratorCLIParser{parsed}); len(f) != 0 {
				t.Fatalf("законная форма объявлена находкой: %v", f)
			}
		})
	}
}

// TestMigratorCLIGatesAreSilentOnAHealthyCorpus — контроль: на исправном входе
// молчат ОБА разбора сразу. Без него каждая проба выше зеленела бы на гейте,
// объявляющем находкой всё подряд.
func TestMigratorCLIGatesAreSilentOnAHealthyCorpus(t *testing.T) {
	mentions := migratorCLIMentions("services/x/Dockerfile",
		"RUN go build -o /out/kacho-migrator ./services/x/cmd/migrator\n"+
			"COPY --from=builder /out/kacho-migrator /usr/local/bin/kacho-migrator\n")
	if len(mentions) == 0 {
		t.Fatal("исправный вход не распознан вовсе — молчание неотличимо от невидимости")
	}
	if f := migratorCLINameFindings(mentions); len(f) != 0 {
		t.Fatalf("исправный вход объявлен находкой: %v", f)
	}

	parsed, err := classifyMigratorCLIParser("services/x/cmd/migrator/main.go", cliSrcSharedParser)
	if err != nil {
		t.Fatalf("разбор не удался: %v", err)
	}
	if f := migratorCLIParserFindings([]migratorCLIParser{parsed}); len(f) != 0 {
		t.Fatalf("исправный вход объявлен находкой: %v", f)
	}
	t.Logf("перепись контроля: мест с именем %d, находок 0", len(mentions))
}

// ── имя, которым инструмент представляется САМ (формы 5 и 6) ───────────────
//
// Инъекция 4 приёмщика: подмена `Use:` корневой команды на своё имя оставляла
// гейт ЗЕЛЁНЫМ, а перепись печатала «различных имён 1». Распознаватель знал
// четыре формы (путь установки, выход сборки, переменная сборки, константа) и
// не знал ту, в которой расхождение было живо: имя, которое инструмент печатает
// о себе — в форме вызова и в тексте `unknown command "X" for "ИМЯ"`.
//
// Форма, о которой распознаватель не знает, не даёт ни красного, ни зелёного —
// она даёт НЕВИДИМОСТЬ, и молчание гейта читается как «имя одно».

func TestMigratorCLINameGateSpeaksOnTheNameTheToolPrintsOfItself(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{
			name: "Use корневой команды",
			src: `package main

import "github.com/spf13/cobra"

func newRootCmd() *cobra.Command {
	return &cobra.Command{Use: "kacho-nlb-migrator"}
}`,
		},
		{
			name: "имя в справке, собранной склейкой",
			src: `package main

import "github.com/spf13/cobra"

func newRootCmd() *cobra.Command {
	return &cobra.Command{
		Use:  "kacho-migrator",
		Long: "kacho-nlb-migrator — отдельный CLI управления миграциями.\n" +
			"Вторая строка.",
	}
}`,
		},
		{
			name: "имя в краткой справке",
			src: `package main

import "github.com/spf13/cobra"

func newRootCmd() *cobra.Command {
	return &cobra.Command{Use: "kacho-migrator", Short: "runner kacho-nlb-migrator"}
}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const rel = "services/x/cmd/migrator/main.go"
			mentions, err := migratorCLIGoMentions(rel, tc.src)
			if err != nil {
				t.Fatalf("разбор не удался: %v", err)
			}
			if len(mentions) == 0 {
				t.Fatalf("форма не распознана вовсе — она была бы не находкой, а НЕВИДИМОСТЬЮ")
			}
			findings := migratorCLINameFindings(mentions)
			if len(findings) == 0 {
				t.Fatalf("чужое имя принято молча: %+v", mentions)
			}
			joined := strings.Join(findings, "\n")
			if !strings.Contains(joined, rel) {
				t.Errorf("находка не называет координату: %s", joined)
			}
			if !strings.Contains(joined, `"kacho-nlb-migrator"`) {
				t.Errorf("находка не называет само имя: %s", joined)
			}
		})
	}
}

func TestMigratorCLINameGateStaysSilentOnLegitimateSelfNames(t *testing.T) {
	for _, tc := range []struct {
		name        string
		src         string
		wantMention bool
	}{
		{
			name: "общее имя в Use и в справке",
			src: `package main

import "github.com/spf13/cobra"

func newRootCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kacho-migrator",
		Short: "Управление миграциями БД сервиса kacho-vpc",
		Long:  "kacho-migrator — отдельный CLI управления миграциями.",
	}
}`,
			wantMention: true,
		},
		{
			name: "имя подкоманды именем бинаря не является",
			src: `package main

import "github.com/spf13/cobra"

func newUpCmd() *cobra.Command {
	return &cobra.Command{Use: "up", Short: "Apply migrations up to latest"}
}`,
			wantMention: false,
		},
		{
			name: "каталог ИСХОДНИКОВ в справке — не имя бинаря",
			src: `package main

import "github.com/spf13/cobra"

func newRootCmd() *cobra.Command {
	return &cobra.Command{
		Use:  "kacho-migrator",
		Long: "Построено по образцу services/vpc/cmd/migrator; см. docs/architecture/migrator-cli.md",
	}
}`,
			wantMention: true, // ровно одно упоминание — само `Use`
		},
		{
			name: "старое имя в ПРОЗЕ о нём, а не в справке",
			src: `package main

import "github.com/spf13/cobra"

// До #1461 инструмент звался kacho-nlb-migrator.
func newRootCmd() *cobra.Command {
	return &cobra.Command{Use: "kacho-migrator"}
}`,
			wantMention: true, // только `Use`; комментарий не судится
		},
		{
			name: "посторонний бинарь рядом",
			src: `package main

import "github.com/spf13/cobra"

func newServeCmd() *cobra.Command {
	return &cobra.Command{Use: "kacho-loadbalancer", Short: "serve"}
}`,
			wantMention: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mentions, err := migratorCLIGoMentions("services/x/cmd/migrator/main.go", tc.src)
			if err != nil {
				t.Fatalf("разбор не удался: %v", err)
			}
			if got := len(mentions) > 0; got != tc.wantMention {
				t.Fatalf("распознано мест %d, ожидалось непусто=%v: %+v", len(mentions), tc.wantMention, mentions)
			}
			if f := migratorCLINameFindings(mentions); len(f) != 0 {
				t.Fatalf("законная запись объявлена находкой: %v", f)
			}
		})
	}
}

// ── разбор определяется ВЫЗОВОМ, а не импортом ─────────────────────────────
//
// Импорт общего пакета ради ИМЕНИ или ради ТЕКСТА отказа вторым разбором не
// является: разбирает тот, кто зовёт `migratorcli.Parse`. Пока классификация
// шла по импорту, делегирующая форма не могла взять из общего пакета ни одной
// величины, не став в глазах гейта «двумя разборами сразу», — то есть гейт
// запрещал ровно то сведение, ради которого он заведён.

func TestMigratorCLIParserGateJudgesTheCallNotTheImport(t *testing.T) {
	const cobraBorrowingSharedConstants = `package main

import (
	"github.com/spf13/cobra"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

func newRootCmd() *cobra.Command {
	return &cobra.Command{
		Use:  migratorcli.BinaryName,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return nil },
	}
}`
	parsed, err := classifyMigratorCLIParser("services/x/cmd/migrator/main.go", cobraBorrowingSharedConstants)
	if err != nil {
		t.Fatalf("разбор не удался: %v", err)
	}
	if parsed.Shared {
		t.Error("заимствование величины принято за второй разбор: разбирает тот, кто зовёт Parse")
	}
	if !parsed.Cobra {
		t.Error("делегирующая форма не распознана")
	}
	if f := migratorCLIParserFindings([]migratorCLIParser{parsed}); len(f) != 0 {
		t.Fatalf("законная запись объявлена находкой: %v", f)
	}
}

func TestMigratorCLIParserGateStillSpeaksOnTwoRealParsers(t *testing.T) {
	// Законный близнец предыдущей пробы: тот же импорт, но общий разбор ЗОВЁТСЯ
	// рядом с деревом cobra. Вот это — действительно два разбора в одной точке,
	// и по коду не сказать, какой исполняется.
	const bothActuallyParse = `package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

func main() {
	_, _ = migratorcli.Parse("kacho-migrator", os.Args[1:])
	_ = (&cobra.Command{Use: "kacho-migrator"}).Execute()
}`
	parsed, err := classifyMigratorCLIParser("services/x/cmd/migrator/main.go", bothActuallyParse)
	if err != nil {
		t.Fatalf("разбор не удался: %v", err)
	}
	findings := migratorCLIParserFindings([]migratorCLIParser{parsed})
	if len(findings) == 0 {
		t.Fatalf("два настоящих разбора приняты молча: %+v", parsed)
	}
	if !strings.Contains(strings.Join(findings, "\n"), "по коду не сказать") {
		t.Errorf("находка не называет предмет: %v", findings)
	}
}

// ── пустая командная строка: гейт говорит и молчит ─────────────────────────

func TestMigratorCLIGateSpeaksOnARootThatSucceedsOnAnEmptyCommandLine(t *testing.T) {
	// Инъекция ломает РОВНО ОДНО свойство: у корня снято исполнение, всё
	// остальное (имя, Args подкоманды, разбор) на месте.
	const rootWithoutRun = `package main

import "github.com/spf13/cobra"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{Use: "kacho-migrator", SilenceUsage: true}
	root.AddCommand(&cobra.Command{
		Use:  "up",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return nil },
	})
	return root
}`
	parsed, err := classifyMigratorCLIParser("services/x/cmd/migrator/main.go", rootWithoutRun)
	if err != nil {
		t.Fatalf("разбор не удался: %v", err)
	}
	if parsed.Roots != 1 {
		t.Fatalf("корневая команда не распознана: %+v", parsed)
	}
	findings := migratorCLIParserFindings([]migratorCLIParser{parsed})
	if len(findings) == 0 {
		t.Fatalf("корень, выходящий успехом на пустой строке, принят молча: %+v", parsed)
	}
	joined := strings.Join(findings, "\n")
	if !strings.Contains(joined, "services/x/cmd/migrator/main.go") {
		t.Errorf("находка не называет координату: %s", joined)
	}
	if !strings.Contains(joined, "УСПЕХОМ") {
		t.Errorf("находка не называет предмет: %s", joined)
	}
}

func TestMigratorCLIGateDoesNotMistakeASubcommandForTheRoot(t *testing.T) {
	// Законный близнец: подкоманда без исполнения (её `Use` бинаря не называет)
	// корнем не является и требования не наследует. Без этой пробы гейт ловил бы
	// форму «команда без Run», а не существо «корень выходит успехом».
	const subcommandWithoutRun = `package main

import "github.com/spf13/cobra"

func newHelpersCmd() *cobra.Command {
	return &cobra.Command{Use: "tools", Short: "grouping only"}
}`
	parsed, err := classifyMigratorCLIParser("services/x/cmd/migrator/main.go", subcommandWithoutRun)
	if err != nil {
		t.Fatalf("разбор не удался: %v", err)
	}
	if parsed.Roots != 0 {
		t.Fatalf("подкоманда принята за корень: %+v", parsed)
	}
	if len(parsed.RootsWithoutRun) != 0 {
		t.Fatalf("подкоманда объявлена находкой: %+v", parsed.RootsWithoutRun)
	}
}
