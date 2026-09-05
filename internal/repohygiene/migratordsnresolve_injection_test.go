// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratordsnresolve_injection_test.go — доказательство, что гейт приоритета
// источников DSN СПОСОБЕН упасть, СПОСОБЕН смолчать и роняет ТОЛЬКО своё.
//
// Инъекция настоящая: дефектный исходник взят с живых vpc и iam до сведения
// (#1544), а не выдуман. Законные близнецы — тоже живые: сведённая точка наката
// зовёт общий резолв и передаёт свою конфигурацию замыканием, а имя переменной
// окружения она по-прежнему НАЗЫВАЕТ — в подсказке флага и в прозе. Гейт,
// краснеющий на упоминании, был бы красен на верном коде.
package repohygiene

import (
	"strings"
	"testing"
)

const (
	// relDSNEntry — координата точки наката, под которой судится синтетика.
	relDSNEntry = "services/svc/cmd/migrator/main.go"

	// srcDSNConverged — сведённая форма: общий резолв, своя конфигурация
	// замыканием, имя переменной НАЗВАНО в подсказке флага.
	srcDSNConverged = `package main

import (
	"os"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
	"github.com/PRO-Robotech/kacho/services/svc/internal/apps/kaname/config"
)

const envDSN = migratorcli.EnvDSN

func flags() string { return "database DSN; if empty — read ENV " + envDSN }

func buildRunner(flagDSN string) (string, error) {
	return migratorcli.ResolveDSN(flagDSN, func() (string, error) {
		cfg, cerr := config.Load(os.Getenv("KACHO_SVC_CONFIG_PATH"))
		if cerr != nil {
			return "", cerr
		}
		return cfg.MigrateDSN(), nil
	})
}`

	// srcDSNOwnChainAlias — то, что было у vpc и iam: своя цепочка через местную
	// константу-псевдоним.
	srcDSNOwnChainAlias = `package main

import (
	"os"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
	"github.com/PRO-Robotech/kacho/services/svc/internal/apps/kaname/config"
)

const envDSN = migratorcli.EnvDSN

func buildRunner(flagDSN string) (string, error) {
	dsn := strings.TrimSpace(flagDSN)
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv(envDSN))
	}
	if dsn == "" {
		cfg, _ := config.Load("")
		dsn = cfg.MigrateDSN()
	}
	return dsn, nil
}`

	// srcDSNOwnChainLiteral — та же цепочка, записанная ЛИТЕРАЛОМ. Второе
	// законное написание предмета; распознаватель, знающий только псевдоним,
	// на нём ослеп бы молча.
	srcDSNOwnChainLiteral = `package main

import "os"

func buildRunner(flagDSN string) (string, error) {
	if flagDSN != "" {
		return flagDSN, nil
	}
	return os.Getenv("KACHO_MIGRATOR_DSN"), nil
}`

	// srcDSNOwnChainSelector — третье законное написание: обращение к константе
	// общего пакета напрямую, без местного псевдонима.
	srcDSNOwnChainSelector = `package main

import (
	"os"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

func buildRunner(flagDSN string) (string, error) {
	if flagDSN != "" {
		return flagDSN, nil
	}
	return os.Getenv(migratorcli.EnvDSN), nil
}`

	// srcDSNRenamedImport — сведённая форма с ПЕРЕИМЕНОВАННЫМ импортом. Законная
	// запись Go; распознаватель, предполагающий квалификатор равным последнему
	// сегменту пути, объявил бы её недéлегирующей.
	srcDSNRenamedImport = `package main

import cli "github.com/PRO-Robotech/kacho/pkg/migratorcli"

func buildRunner(flagDSN string) (string, error) {
	return cli.ResolveDSN(flagDSN, nil)
}`

	// srcDSNNamesEnvOnlyInProse — ЗАКОННЫЙ БЛИЗНЕЦ и главная ловушка: имя
	// переменной стоит в комментарии и в чтении ЧУЖОЙ переменной (путь к
	// конфигурации). Гейт по подстроке краснел бы на собственном объяснении.
	srcDSNNamesEnvOnlyInProse = `package main

// Своей цепочки здесь нет: порядок --dsn > KACHO_MIGRATOR_DSN > конфигурация
// объявлен в общем пакете, и os.Getenv(envDSN) отсюда снят.
import (
	"os"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

func buildRunner(flagDSN string) (string, error) {
	_ = os.Getenv("KACHO_SVC_CONFIG_PATH")
	return migratorcli.ResolveDSN(flagDSN, nil)
}`
)

// auditDSNSource — одна инъекция через ТУ ЖЕ функцию, которую зовёт гейт.
// Возвращает делегирование и тексты находок в той же редакции, что и гейт.
func auditDSNSource(t *testing.T, rel, src string) (bool, []string) {
	t.Helper()
	facts, err := migratorDSNFactsOf(rel, src)
	if err != nil {
		t.Fatalf("разбор синтетики не удался: %v", err)
	}
	findings := make([]migratorDSNFinding, 0, len(facts.OwnEnvReads))
	for _, read := range facts.OwnEnvReads {
		findings = append(findings, migratorDSNFinding{Rel: rel, What: read})
	}
	return facts.CallsSharedResolve, sortedMigratorDSNTexts(findings)
}

// TestDSNResolveInjectionRunOne_Control — ПРОГОН 1 из трёх: всё цело, молчат
// ОБА гейта. Без него молчание существующего контроля в прогоне 2 неотличимо от
// молчания мёртвого (testing.md §«Гейт на класс», п. 2в).
func TestDSNResolveInjectionRunOne_Control(t *testing.T) {
	delegates, got := auditDSNSource(t, relDSNEntry, srcDSNConverged)
	if !delegates {
		t.Error("сведённая точка наката не опознана делегирующей — гейт краснел бы на верном коде")
	}
	if len(got) != 0 {
		t.Errorf("новый гейт краснеет на сведённой точке наката: %v", got)
	}
	if sibling := auditSource(t, relDSNEntry, srcDSNConverged); len(sibling) != 0 {
		t.Errorf("существующий гейт краснеет на законной форме — контроль недействителен: %v", sibling)
	}
}

// TestDSNResolveInjectionRunTwo_NewPropertyOnly — ПРОГОН 2: снято НОВОЕ
// свойство, старое цело. Краснеет только новый гейт, и по каждому из ТРЁХ
// законных написаний предмета порознь (testing.md §«Гейт на класс», п. 7).
func TestDSNResolveInjectionRunTwo_NewPropertyOnly(t *testing.T) {
	cases := []struct {
		name string
		src  string
		// naming — чем находка обязана назвать написание, которым она найдена.
		naming string
		// delegates — зовёт ли эта форма общий резолв. Своя цепочка РЯДОМ с
		// общим вызовом тоже нарушение, поэтому положительная половина здесь
		// молчать не обязана.
		delegates bool
	}{
		{
			name:      "местная константа-псевдоним",
			src:       srcDSNOwnChainAlias,
			naming:    "envDSN (местная константа-псевдоним)",
			delegates: false,
		},
		{
			name:      "литерал",
			src:       srcDSNOwnChainLiteral,
			naming:    `"KACHO_MIGRATOR_DSN" (литерал)`,
			delegates: false,
		},
		{
			name:      "константа общего пакета",
			src:       srcDSNOwnChainSelector,
			naming:    "migratorcli.EnvDSN (константа общего пакета)",
			delegates: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			delegates, got := auditDSNSource(t, relDSNEntry, tc.src)
			if delegates != tc.delegates {
				t.Errorf("делегирование: получено %t, ожидалось %t", delegates, tc.delegates)
			}
			if len(got) != 1 {
				t.Fatalf("находок %d, ожидалась одна: %v", len(got), got)
			}
			for _, want := range []string{relDSNEntry, tc.naming, "pkg/migratorcli", "ResolveDSN"} {
				if !strings.Contains(got[0], want) {
					t.Errorf("находка не называет %q: %s", want, got[0])
				}
			}
			// Существующий гейт этих инъекций не видит вовсе — он судит тексты
			// отказа и разбор цели. Утверждается именно это: моя инъекция не
			// трогает его предмет, значит прогон 3 говорит о нём, а не о ней.
			if sibling := auditSource(t, relDSNEntry, tc.src); len(sibling) != 0 {
				t.Errorf("существующий гейт покраснел от чужой инъекции — доказательство "+
					"недействительно: %v", sibling)
			}
		})
	}
}

// TestDSNResolveInjectionRunThree_ExistingPropertyOnly — ПРОГОН 3: снято
// СУЩЕСТВУЮЩЕЕ свойство (свой текст отказа предусловий). Краснеет только
// соседний гейт; новый молчит и по-прежнему считает точку делегирующей.
func TestDSNResolveInjectionRunThree_ExistingPropertyOnly(t *testing.T) {
	const src = `package main

import (
	"errors"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

func buildRunner(flagDSN string) (string, error) {
	dsn, err := migratorcli.ResolveDSN(flagDSN, nil)
	if err != nil {
		return "", errors.New("dsn is empty (set --dsn or KACHO_MIGRATOR_DSN)")
	}
	return dsn, nil
}`

	sibling := auditSource(t, relDSNEntry, src)
	if len(sibling) == 0 {
		t.Fatal("существующий гейт не увидел своего текста отказа — он мёртв, " +
			"и молчание в прогонах 1 и 2 ничего не доказывало")
	}
	delegates, got := auditDSNSource(t, relDSNEntry, src)
	if !delegates {
		t.Error("новый гейт объявил делегирующую точку недéлегирующей на чужой инъекции")
	}
	if len(got) != 0 {
		t.Errorf("новый гейт краснеет на чужом предмете: %v", got)
	}
}

// TestDSNResolveGateIsSilentOnLegalTwins — гейт СПОСОБЕН смолчать. Без этого он
// ловил бы форму, а не существо, и первый же ложный срабат его отключил бы.
func TestDSNResolveGateIsSilentOnLegalTwins(t *testing.T) {
	t.Run("имя переменной только в прозе и в подсказке флага", func(t *testing.T) {
		delegates, got := auditDSNSource(t, relDSNEntry, srcDSNNamesEnvOnlyInProse)
		if !delegates {
			t.Error("точка наката, зовущая общий резолв, не опознана делегирующей")
		}
		if len(got) != 0 {
			t.Errorf("гейт краснеет на упоминании имени переменной и на чтении ЧУЖОЙ "+
				"переменной (путь к конфигурации): %v", got)
		}
	})

	t.Run("переименованный импорт общего пакета", func(t *testing.T) {
		delegates, got := auditDSNSource(t, relDSNEntry, srcDSNRenamedImport)
		if !delegates {
			t.Error("делегирование через переименованный импорт не опознано — " +
				"распознаватель предполагает квалификатор равным сегменту пути")
		}
		if len(got) != 0 {
			t.Errorf("гейт краснеет на переименованном импорте: %v", got)
		}
	})

	t.Run("каталог точки наката выводится из пути", func(t *testing.T) {
		if got := migratorDSNEntryDir("services/vpc/cmd/migrator/main.go"); got != "services/vpc/cmd/migrator" {
			t.Errorf("каталог точки наката: получено %q", got)
		}
		if got := migratorDSNEntryDir("services/vpc/internal/apps/migrator/runner.go"); got != "" {
			t.Errorf("слой наката принят за точку наката: %q — положительная половина "+
				"потребовала бы делегирования от того, кто DSN не выбирает", got)
		}
	})

	t.Run("предпосылка читается у общего пакета", func(t *testing.T) {
		facts, err := migratorDSNFactsOf("pkg/migratorcli/parse.go", `package migratorcli

const EnvDSN = "KACHO_MIGRATOR_DSN"

func ResolveDSN(flagDSN string, fromConfig func() (string, error)) (string, error) {
	return flagDSN, nil
}`)
		if err != nil {
			t.Fatalf("разбор синтетики не удался: %v", err)
		}
		if !facts.DeclaresResolve {
			t.Error("объявление общего резолва не опознано — премиса гейта не проверяется")
		}
		if !facts.DeclaresEnvName || facts.DeclaredEnvValue != migratorDSNEnvName {
			t.Errorf("объявление имени переменной не опознано: %t %q",
				facts.DeclaresEnvName, facts.DeclaredEnvValue)
		}
	})
}
