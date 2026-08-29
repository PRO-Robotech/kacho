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

	cliSrcBothParsers = `package main

import (
	"github.com/spf13/cobra"
	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

func main() { _, _ = cobra.Command{}, migratorcli.Options{} }`

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
	"github.com/pressly/goose/v3"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

func main() { _, _ = goose.Up, migratorcli.Parse }`

	cliSrcCobraDecided = `package main

import "github.com/spf13/cobra"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{Use: "kacho-migrator"}
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
		// Корневая команда исполнения не несёт, поэтому Args с неё не требуется.
		{"корень без исполнения, подкоманда решила Args", cliSrcCobraDecided},
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
