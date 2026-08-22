// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clientexpiryimmutable_test.go — срок клиента, способного к утверждению,
// НЕИЗМЕНЯЕМ после создания (приёмка F2, §9.4, решение §2.10).
//
// # Предмет
//
// На неизменяемости срока стоит структурная гарантия: срок выданного токена не
// превышает остатка срока клиента, и проверять это на пути запроса не нужно
// ровно потому, что срок не двигается. Сдвинь его — и третья строка таблицы
// механизма возвращается к читателю на путь запроса, а гарантия, заменившая
// рантаймовый контроль, держится ничем.
//
// # Почему гейт дерева, а не ограничение схемы
//
// Ограничением схемы это выражается новой миграцией; применённые не правятся
// (ban #5). Приёмка называет допустимую замену прямо — гейт дерева с переписью
// и инъекцией, — и цену её тоже: гейт слабее ограничения, потому что правка,
// собранная во время выполнения, до базы доедет. Слепые зоны названы в шапке
// разбора, а не спрятаны.
//
// # Что здесь считается деревом
//
// Индекс git — то же множество, которое увидит свежий клон и CI.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	// clientExpiryColumn — столбец срока.
	clientExpiryColumn = "expires_at"
	// clientExpiryMigrations — каталог миграций владельца таблиц.
	clientExpiryMigrations = "services/iam/internal/migrations/"
	// clientExpiryCensusFloor — порог переписи.
	clientExpiryCensusFloor = 1000
)

// clientExpiryTables — таблицы клиентов, способных к утверждению.
//
// Третья таблица клиентов сюда не входит намеренно: ключевого материала у неё
// нет ни одной колонкой (приёмка §1.1), и утверждением такой клиент себя
// аутентифицировать не может by construction.
var clientExpiryTables = []string{"user_oauth_clients", "service_account_oauth_clients"}

// TestClientExpiryIsNeverUpdated — сам гейт.
func TestClientExpiryIsNeverUpdated(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	// (1) Предпосылка: столбец срока у обеих таблиц ЕСТЬ. Гейт, стерегущий
	// неизменяемость столбца, которого нет, молчит по построению — и молчит он
	// одинаково и тогда, когда предмет исчез, и тогда, когда сломался он сам.
	declared := map[string]string{}
	migrations := 0
	for rel := range tt.files {
		if !strings.HasPrefix(rel, clientExpiryMigrations) || !strings.HasSuffix(rel, ".sql") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		migrations++
		for _, table := range clientExpiryTables {
			if _, ok := declared[table]; ok {
				continue
			}
			if b := SQLCreateTableBody(string(body), table); b != "" {
				declared[table] = rel
				if !strings.Contains(b, clientExpiryColumn) {
					t.Errorf("таблица %s объявлена в %s и столбца %q НЕ несёт. Предпосылка "+
						"решения §2.10 отпала: неизменяемость стерегут у столбца, которого "+
						"нет, и гейт молчал бы по построению.", table, rel, clientExpiryColumn)
				}
			}
		}
	}
	for _, table := range clientExpiryTables {
		if declared[table] == "" {
			t.Fatalf("объявления таблицы %s в %s (прочитано файлов %d) не найдено — гейт "+
				"стережёт координату, которой больше не существует",
				table, clientExpiryMigrations, migrations)
		}
	}

	var rels []string
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") || skipPath(rel) {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var (
		parsed  int
		census  SQLUpdateCensus
		updates []SQLUpdate
	)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		us, c, err := ScanSQLUpdates(rel, src, clientExpiryTables)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		parsed++
		census.StringLiterals += c.StringLiterals
		census.SQLLiterals += c.SQLLiterals
		census.Updates += c.Updates
		census.UpdatesWithoutColumns += c.UpdatesWithoutColumns
		updates = append(updates, us...)
	}

	var touched []string
	for _, u := range updates {
		touched = append(touched, fmt.Sprintf("%s:%d %s → %s SET %s",
			u.File, u.Line, u.Func, u.Table, strings.Join(u.Columns, ", ")))
	}
	sort.Strings(touched)

	t.Logf("перепись: файлов миграций прочитано %d, объявления таблиц найдены (%s); "+
		"не-тестовых файлов Go разобрано %d, строковых литералов осмотрено %d, из них "+
		"операторов SQL %d, из них правок стережённых таблиц %d (без разобранных столбцов %d)",
		migrations, strings.Join(clientExpiryTables, ", "), parsed,
		census.StringLiterals, census.SQLLiterals, census.Updates, census.UpdatesWithoutColumns)

	if parsed < clientExpiryCensusFloor {
		t.Fatalf("перепись обвалилась: разобрано %d файлов при пороге %d", parsed, clientExpiryCensusFloor)
	}
	if census.SQLLiterals == 0 {
		t.Fatalf("на %d файлах не найдено НИ ОДНОГО литерала SQL — разбор перестал видеть "+
			"предмет, и его молчание сказано ни о чём", parsed)
	}
	// (2) Предпосылка различения: правки стережённых таблиц в дереве ЕСТЬ. Ноль
	// правок означает, что признак «правка столбца» не производится ничем, и
	// молчание гейта верно и для гейта, не умеющего читать SET.
	if census.Updates == 0 {
		t.Fatalf("правок таблиц %v в дереве НОЛЬ при %d литералах SQL. Разбор не производит "+
			"признака, который стережёт: он молчал бы и на правке срока. Законный близнец — "+
			"правка ДРУГОГО столбца этих же таблиц — обязан существовать, иначе различение "+
			"ничего не различает.", clientExpiryTables, census.SQLLiterals)
	}

	// (3) Находка: срок назван в списке правки.
	var findings []string
	for _, u := range updates {
		for _, col := range u.Columns {
			if col != clientExpiryColumn {
				continue
			}
			findings = append(findings, fmt.Sprintf("%s:%d  %s — %s SET %s",
				u.File, u.Line, u.Func, u.Table, strings.Join(u.Columns, ", ")))
		}
	}
	sort.Strings(findings)
	if len(findings) > 0 {
		t.Fatalf("срок клиента правится после создания — %d место(а):\n  %s\n\n"+
			"На неизменяемости срока стоит структурная гарантия: срок выданного токена не "+
			"превышает остатка срока клиента, и на пути запроса это не проверяется РОВНО "+
			"потому, что срок не двигается. Сдвинутый срок делает уже выданный токен длиннее "+
			"клиента, которым он выдан, — и заметить это будет нечем: контроль, который "+
			"структурная гарантия заменила, на пути запроса отсутствует.\n"+
			"Исходов два: не двигать срок (создать нового клиента) либо вернуть проверку "+
			"остатка на путь запроса ТЕМ ЖЕ изменением.",
			len(findings), strings.Join(findings, "\n  "))
	}

	t.Logf("законные правки этих таблиц (столбец срока среди них не назван), %d:\n  %s",
		len(touched), strings.Join(touched, "\n  "))
}
