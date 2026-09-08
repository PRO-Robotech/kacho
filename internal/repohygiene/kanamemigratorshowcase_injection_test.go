// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/repohygiene"
)

// kanamemigratorshowcase_injection_test.go — доказательство способности гейта
// витрины упасть и смолчать.
//
// Инъекция подаёт СИНТЕТИЧЕСКИЙ корпус тем же входом, каким прогон по дереву
// подаёт настоящий: доказывать надо тот код, который исполняется, а не его
// двойник. Каждый случай отличается от своего законного близнеца РОВНО ОДНИМ
// фактом — иначе неизвестно, что именно дало красное.

// showcaseCorpus — минимальный корпус: одна витринная поверхность.
func showcaseCorpus(rel, body string) map[string]string {
	return map[string]string{rel: body}
}

// ── гейт ГОВОРИТ: имя платформы на каждой форме витрины ────────────────────

func TestKanameShowcaseGateSpeaksOnEveryFormOfTheName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, rel, body string
	}{
		{
			name: "команда в спецификации пода — то, что оператор видит в kubectl describe",
			rel:  "services/iam/deploy/templates/deployment.yaml",
			body: "          command: [\"/usr/local/bin/kacho-migrator\", \"up\"]\n",
		},
		{
			name: "шаг клиентского документа установки",
			rel:  "services/iam/INSTALL.md",
			body: "1. `kacho-migrator up` — схема.\n",
		},
		{
			name: "команда на странице сайта документации",
			rel:  "services/iam/docs/content/install/deploy.mdx",
			body: "    bin/kacho-migrator status\n",
		},
		{
			name: "перечень сторонних лицензий называет поставляемый артефакт",
			rel:  "services/iam/THIRD-PARTY-NOTICES.md",
			body: "поставляемые бинари `kaname` и `kacho-migrator`\n",
		},
		{
			name: "текст отказа, который читает оператор",
			rel:  "services/iam/cmd/kaname/main.go",
			body: "\tbootLog.Error(\"migrations live in `kacho-migrator`\")\n",
		},
		{
			name: "путь установки в образе",
			rel:  "services/iam/Dockerfile",
			body: "COPY --from=builder /kacho-migrator /usr/local/bin/kacho-migrator\n",
		},
		{
			name: "подчарт зонта — вторая точка, зовущая тот же путь",
			rel:  "deploy/helm/umbrella/charts/kaname/values.yaml",
			body: "    command: [\"/usr/local/bin/kacho-migrator\", \"up\"]\n",
		},
		{
			name: "накатчик СОСЕДА на витрине Kaname — тоже чужой",
			rel:  "services/iam/INSTALL.md",
			body: "1. `kacho-nlb-migrator up` — схема.\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			findings, census := repohygiene.KanameMigratorShowcaseScan(showcaseCorpus(tc.rel, tc.body))
			if census.FilesRead != 1 {
				t.Fatalf("прочитано файлов %d — инъекция подала корпус, который гейт не читает",
					census.FilesRead)
			}
			if census.TokensJudged == 0 {
				t.Fatalf("токен не признан именем накатчика вовсе — это не находка, а НЕВИДИМОСТЬ: %+v", census)
			}
			if len(findings) == 0 {
				t.Fatalf("чужое имя на витрине принято молча: %+v", census)
			}
			joined := findings[0].String()
			if !strings.Contains(joined, tc.rel) {
				t.Errorf("находка не называет координату: %s", joined)
			}
			if !strings.Contains(joined, "kaname-migrator") {
				t.Errorf("находка не называет, каким имя обязано быть: %s", joined)
			}
		})
	}
}

// ── гейт МОЛЧИТ: законные близнецы ─────────────────────────────────────────
//
// Без этой половины гейт ловил бы форму, а не существо, и первый же ложный
// срабат его отключил бы.

func TestKanameShowcaseGateStaysSilentOnLegitimateTwins(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, rel, body string
		wantJudged      int
	}{
		{
			name:       "то же место со СВОИМ именем — отличие ровно в имени",
			rel:        "services/iam/deploy/templates/deployment.yaml",
			body:       "          command: [\"/usr/local/bin/kaname-migrator\", \"up\"]\n",
			wantJudged: 1,
		},
		{
			name:       "цель сборки: `build-migrator` имени продукта не несёт",
			rel:        "services/iam/CONTRIBUTING.md",
			body:       "make build-migrator # bin/kaname-migrator\n",
			wantJudged: 1,
		},
		{
			name:       "каталог ИСХОДНИКОВ, а не имя двоичного файла",
			rel:        "services/iam/Makefile",
			body:       "MIGRATOR_CMD   := ./cmd/migrator\n",
			wantJudged: 0,
		},
		{
			name:       "пакет разбора: `migratorcli` именем бинаря не является",
			rel:        "services/iam/cmd/migrator/main.go",
			body:       "\t\"github.com/PRO-Robotech/kacho/pkg/migratorcli\"\n",
			wantJudged: 0,
		},
		{
			name:       "ручка окружения — предмет другой полосы, не эта",
			rel:        "services/iam/INSTALL.md",
			body:       "| `KACHO_MIGRATOR_DSN` | адрес базы |\n",
			wantJudged: 0,
		},
		{
			name: "ПРОБА называет имя платформы как предмет своей проверки",
			rel:  "services/iam/deploy/schema_mechanism_precedes_the_service_test.go",
			body: "const migratorBinaryPath = \"/usr/local/bin/kacho-migrator\"\n",
		},
		{
			name: "ОДОБРЕННАЯ ПРИЁМКА: вердикт назван вместе со своей ревизией",
			rel:  "services/iam/docs/engineering/acceptance/roles-come-as-data.md",
			body: "  → бинарь kacho-migrator\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			findings, census := repohygiene.KanameMigratorShowcaseScan(showcaseCorpus(tc.rel, tc.body))
			if len(findings) != 0 {
				t.Fatalf("законная запись объявлена находкой: %v", findings)
			}
			if census.FilesRead == 1 && census.TokensJudged != tc.wantJudged {
				t.Fatalf("признано именем накатчика %d, ожидалось %d — молчание пришло "+
					"не оттуда, откуда должно: %+v", census.TokensJudged, tc.wantJudged, census)
			}
		})
	}
}

// TestKanameShowcaseExemptionsAreNamedNotSilent — изъятие названо ПРИЧИНОЙ и
// сосчитано, а не проглочено.
//
// Изъятие, которого не видно в переписи, неотличимо от слепой зоны: «ноль
// находок» читалось бы как «имени нет», тогда как файл вовсе не читали.
func TestKanameShowcaseExemptionsAreNamedNotSilent(t *testing.T) {
	t.Parallel()
	corpus := map[string]string{
		"services/iam/INSTALL.md":                                   "`kaname-migrator up`\n",
		"services/iam/deploy/schema_test.go":                        "\"kacho-migrator\"\n",
		"services/iam/docs/engineering/acceptance/roles-as-data.md": "бинарь kacho-migrator\n",
	}
	findings, census := repohygiene.KanameMigratorShowcaseScan(corpus)
	if len(findings) != 0 {
		t.Fatalf("изъятые поверхности объявлены находками: %v", findings)
	}
	if census.FilesTracked != 3 || census.FilesExempt != 2 || census.FilesRead != 1 {
		t.Fatalf("перепись не разводит прочитанное и изъятое: %+v", census)
	}
	if len(census.ExemptReasons) != 2 {
		t.Fatalf("причин изъятия названо %d, а изъятий 2 — изъятие без причины "+
			"неотличимо от слепой зоны: %+v", len(census.ExemptReasons), census.ExemptReasons)
	}
	for reason, n := range census.ExemptReasons {
		if strings.TrimSpace(reason) == "" || n == 0 {
			t.Errorf("причина изъятия пуста либо ничего не покрывает: %q → %d", reason, n)
		}
	}
}

// TestKanameShowcaseEmptyCorpusIsNotAPass — пустой обход зелёным не бывает.
//
// Проверяется здесь, а не только в прогоне по дереву: «ноль находок» на пустом
// корпусе обязано быть отличимо от «ноль находок» на прочитанном.
func TestKanameShowcaseEmptyCorpusIsNotAPass(t *testing.T) {
	t.Parallel()
	findings, census := repohygiene.KanameMigratorShowcaseScan(map[string]string{})
	if len(findings) != 0 {
		t.Fatalf("на пустом корпусе появились находки: %v", findings)
	}
	if census.FilesRead != 0 || census.TokensSeen != 0 {
		t.Fatalf("пустой корпус дал непустую перепись: %+v", census)
	}
	// Величины на нуле — то, по чему прогон по дереву обязан ОТКАЗАТЬ. Что он
	// это делает, утверждает TestKanameShowcaseNamesItsOwnMigrator: здесь
	// доказано лишь, что различить эти два случая ЕСТЬ по чему.
}
