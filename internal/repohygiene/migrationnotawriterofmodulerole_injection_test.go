// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migrationnotawriterofmodulerole_injection_test.go — доказательство
// способности Г2 упасть И смолчать (приёмка §7).
//
// Инъекция настоящей формы: миграция, вставляющая роль ровно так, как это
// делают десять живых блоков дерева, плюс манифест того же модуля.
package repohygiene

import (
	"strings"
	"testing"
)

// injMigrationVPCRole — вставка роли модуля в живой форме дерева.
const injMigrationVPCRole = `-- +goose Up
INSERT INTO kaname.roles (id, cluster_id, account_id, name, description, permissions) VALUES
  ('rol' || substr(md5('vpc.network.admin'), 1, 17), 'cluster_root', NULL,
   'vpc.network.admin', 'Admin Network', '["vpc.network.*"]'::jsonb);
`

// injMigrationNoOwnerRole — роль БЕЗ модуля-владельца: точки в имени нет.
const injMigrationNoOwnerRole = `-- +goose Up
INSERT INTO kaname.roles (id, cluster_id, account_id, name, description, permissions) VALUES
  ('rol' || substr(md5('owner'), 1, 17), 'cluster_root', NULL,
   'owner', 'Owner', '["*.*.*.*"]'::jsonb);
`

// injMigrationForeignTable — тот же образец идентификатора во вставке в ЧУЖУЮ
// таблицу. Предикат без привязки к блоку считал бы это ролью.
const injMigrationForeignTable = `-- +goose Up
INSERT INTO kaname.access_bindings (id, subject_id, role_id) VALUES
  ('acb' || substr(md5('module.storage_sa'), 1, 17),
   'sva' || substr(md5('kacho-storage'), 1, 17),
   'rol' || substr(md5('vpc.network.admin'), 1, 17));
`

// injManifestVPC — манифест модуля vpc.
const injManifestVPC = `apiVersion: iam/v1
module: vpc
roles:
  - id: vpc.network.admin
`

// TestMigrationRoleGateRedsWhenTheModuleHasAManifest — инъекция обязана
// краснеть и называть обе стороны: файл миграции и файл манифеста.
func TestMigrationRoleGateRedsWhenTheModuleHasAManifest(t *testing.T) {
	site, ok := ScanModuleManifest("services/vpc/module-manifest.yaml", []byte(injManifestVPC))
	if !ok || site.Module != "vpc" {
		t.Fatalf("манифест не опознан по содержимому: ok=%v site=%+v", ok, site)
	}
	bearing := map[string]string{site.Module: site.File}

	sites, census := ScanMigrationRoleInserts(iamMigrationsDir+"9999_vpc_role.sql", []byte(injMigrationVPCRole))
	if census.Blocks != 1 || census.Names != 1 {
		t.Fatalf("перепись инъекции не та: блоков %d, имён %d", census.Blocks, census.Names)
	}
	findings := migrationRoleFindings(sites, bearing)
	if len(findings) != 1 {
		t.Fatalf("вставка роли модуля с манифестом НЕ стала находкой: находок %d", len(findings))
	}
	for _, want := range []string{"9999_vpc_role.sql", "vpc.network.admin", "module-manifest.yaml"} {
		if !strings.Contains(findings[0], want) {
			t.Errorf("находка не называет %q: %q", want, findings[0])
		}
	}
}

// TestMigrationRoleGateStaysSilentOnLegalTwins — законные близнецы, каждый
// своей осью.
func TestMigrationRoleGateStaysSilentOnLegalTwins(t *testing.T) {
	bearing := map[string]string{"vpc": "services/vpc/module-manifest.yaml"}

	t.Run("модуль БЕЗ манифеста", func(t *testing.T) {
		sites, census := ScanMigrationRoleInserts(iamMigrationsDir+"9999_x.sql",
			[]byte(strings.ReplaceAll(injMigrationVPCRole, "vpc.", "compute.")))
		if census.Names != 1 {
			t.Fatalf("близнец беспредметен: имён извлечено %d", census.Names)
		}
		if f := migrationRoleFindings(sites, bearing); len(f) != 0 {
			t.Fatalf("роль модуля БЕЗ манифеста объявлена находкой: %v\n"+
				"Пока манифеста нет, миграция остаётся законным её писателем", f)
		}
	})

	t.Run("роль без модуля-владельца", func(t *testing.T) {
		sites, census := ScanMigrationRoleInserts(iamMigrationsDir+"9999_owner.sql",
			[]byte(injMigrationNoOwnerRole))
		if census.Names != 1 {
			t.Fatalf("близнец беспредметен: имён извлечено %d", census.Names)
		}
		if sites[0].Owner != "" {
			t.Errorf("у роли `owner` определён владелец %q — признак считает не то", sites[0].Owner)
		}
		if f := migrationRoleFindings(sites, map[string]string{"owner": "x.yaml"}); len(f) != 0 {
			t.Fatalf("роль без модуля-владельца объявлена находкой: %v", f)
		}
	})

	t.Run("тот же образец во вставке в ЧУЖУЮ таблицу", func(t *testing.T) {
		sites, census := ScanMigrationRoleInserts(iamMigrationsDir+"9999_bind.sql",
			[]byte(injMigrationForeignTable))
		if census.Blocks != 0 || census.Names != 0 || len(sites) != 0 {
			t.Fatalf("предикат не привязан к блоку вставки роли: блоков %d, имён %d — "+
				"он мерил бы УПОМИНАНИЯ идентификатора, а не строки роли",
				census.Blocks, census.Names)
		}
	})

	t.Run("фикстура пробы манифестом не считается", func(t *testing.T) {
		// Отбор проверяется тем же предикатом, каким его делает гейт.
		for _, rel := range []string{
			"services/iam/internal/manifest/testdata/vpc.resources-fixture.yaml",
			"services/iam/internal/manifest/roles_test.yaml",
		} {
			if !strings.Contains(rel, "/testdata/") && !strings.Contains(rel, "_test") {
				t.Errorf("отбор гейта взял бы фикстуру %s — тогда модуль vpc числился бы "+
					"несущим манифест, и КАЖДАЯ его миграция стала бы находкой", rel)
			}
		}
		// Обратная сторона: настоящий путь продукта отбор пропускать не должен.
		if strings.Contains("services/vpc/module-manifest.yaml", "/testdata/") {
			t.Errorf("отбор гейта отверг бы настоящий манифест — популяция осталась бы пустой навсегда")
		}
	})
}

// TestModuleManifestIsRecognisedByContentNotByPath — распознаватель знает
// формы: манифест опознаётся ключами оболочки, а файл без них — нет.
func TestModuleManifestIsRecognisedByContentNotByPath(t *testing.T) {
	if _, ok := ScanModuleManifest("a/b/anything.yaml", []byte(injManifestVPC)); !ok {
		t.Errorf("манифест на произвольном пути не опознан — гейт был бы привязан к пути, " +
			"который выбирает не он")
	}
	for name, src := range map[string]string{
		"чужая оболочка":  "apiVersion: apps/v1\nkind: Deployment\nmodule: vpc\n",
		"без модуля":      "apiVersion: iam/v1\nroles: []\n",
		"ключ не с корня": "spec:\n  apiVersion: iam/v1\n  module: vpc\n",
	} {
		if site, ok := ScanModuleManifest("x.yaml", []byte(src)); ok {
			t.Errorf("%s опознано манифестом: %+v", name, site)
		}
	}
}

// injMigrationAddedRel / injMigrationAppliedRel — ДВА пути одной и той же
// вставки: добавленная относительно ствола миграция и применённая.
const (
	injMigrationAddedRel   = iamMigrationsDir + "20260902000000_vpc_role.sql"
	injMigrationAppliedRel = iamMigrationsDir + "0001_initial.sql"
)

// TestMigrationRoleGateJudgesOnlyAddedMigrations — ось «ДОБАВЛЕННОЕ», введённая
// приведением гейта к его же приёмке (§6 Г2, §7 — «добавленная миграция»).
//
// Инъекция трогает ТОЛЬКО отбор по составу добавленного: вход у обеих сторон
// побайтово один и тот же, различается лишь перечень файлов, поданный отбору.
// Поэтому красное не может прийти от соседней оси — ни манифест, ни имя роли,
// ни привязка к блоку здесь не меняются.
func TestMigrationRoleGateJudgesOnlyAddedMigrations(t *testing.T) {
	bearing := map[string]string{"vpc": "services/vpc/manifest.yaml"}
	added := map[string]bool{injMigrationAddedRel: true}

	addedSites, addedCensus := ScanMigrationRoleInserts(injMigrationAddedRel, []byte(injMigrationVPCRole))
	appliedSites, appliedCensus := ScanMigrationRoleInserts(injMigrationAppliedRel, []byte(injMigrationVPCRole))
	if addedCensus.Names != 1 || appliedCensus.Names != 1 {
		t.Fatalf("инъекция беспредметна: имён извлечено %d и %d — разбор не увидел вставки, "+
			"и обе стороны ниже утверждали бы о пустом входе",
			addedCensus.Names, appliedCensus.Names)
	}

	t.Run("ДОБАВЛЕННАЯ миграция — находка, названы обе стороны", func(t *testing.T) {
		f := migrationRoleFindings(sitesInFiles(addedSites, added), bearing)
		if len(f) != 1 {
			t.Fatalf("вставка роли модуля с манифестом в ДОБАВЛЕННОЙ миграции находкой "+
				"не стала: находок %d — гейт вооружён ни на что", len(f))
		}
		for _, want := range []string{injMigrationAddedRel, "vpc.network.admin", "manifest.yaml"} {
			if !strings.Contains(f[0], want) {
				t.Errorf("находка не называет %q: %q — покрасневший молча гейт "+
					"посылает читателя искать не там", want, f[0])
			}
		}
	})

	t.Run("ПРИМЕНЁННАЯ миграция с той же вставкой — молчание", func(t *testing.T) {
		f := migrationRoleFindings(sitesInFiles(appliedSites, added), bearing)
		if len(f) != 0 {
			t.Fatalf("применённая миграция объявлена находкой: %v\n"+
				"Править её нельзя (ban #5), значит гейт требовал бы неисполнимого; "+
				"переезд её строк к применителю — задача продукта #1891", f)
		}
	})

	t.Run("перепись видит ОБЕ — сужены находки, а не разбор", func(t *testing.T) {
		// Порог переписи стережёт РАЗБОР. Сузь его вместе с находками — и ветка,
		// не добавившая ни одной миграции, роняла бы гейт на достижении его цели.
		if appliedCensus.Blocks != 1 {
			t.Errorf("перепись применённой миграции сузилась вместе с находками: блоков %d",
				appliedCensus.Blocks)
		}
	})

	t.Run("пустой перечень добавленного — молчание, а не отказ", func(t *testing.T) {
		// Ветка без единой добавленной миграции — обычный случай, а не поломка.
		if f := migrationRoleFindings(sitesInFiles(addedSites, map[string]bool{}), bearing); len(f) != 0 {
			t.Fatalf("на пустом перечне добавленного гейт нашёл %d — он судил бы дерево, "+
				"а не изменение", len(f))
		}
	})
}

// injMigrationDumpForm — вставка роли в форме `pg_dump`: идентификатор уже
// вычислен, колонки перечислены явно, `NULL` и вложенные структуры на местах.
//
// Значение колонки `rules` несёт запятые ВНУТРИ литерала и внутри скобок — без
// счёта уровней разбор разорвал бы кортеж посередине и сдвинул бы все
// последующие колонки, то есть прочитал бы имя не той роли.
const injMigrationDumpForm = `-- +goose Up
INSERT INTO kaname.roles (id, account_id, name, description, permissions, created_at, cluster_id, project_id, rules, labels, owner_module, live) VALUES ('rol6307d201bf18e6763', NULL, 'vpc.network.admin', 'Admin Network', '["vpc.network.*.*"]', now(), 'cluster_root', NULL, '[{"verbs": ["*"], "module": "vpc", "resources": ["network"]}]', '{}', NULL, true);
`

// injMigrationDumpFormNoColumnList — вставка БЕЗ перечня колонок.
//
// Имени такая вставка не называет ничем, что разбор вправе прочесть: позиция
// колонки `name` держится только перечнем, а угадывать её по порядку колонок
// таблицы — значит завести второе место об одном предмете, которое разойдётся
// с первой же миграцией, меняющей порядок.
const injMigrationDumpFormNoColumnList = `-- +goose Up
INSERT INTO kaname.roles VALUES ('rol6307d201bf18e6763', NULL, 'vpc.network.admin');
`

// TestMigrationRoleScannerReadsBothFormsOfTheName — РАСПОЗНАВАТЕЛЬ ЗНАЕТ ОБЕ
// ЗАКОННЫЕ ФОРМЫ записи имени роли.
//
// # Зачем отдельная проба
//
// Имя роли записывают двояко: рукописная миграция ВЫВОДИТ идентификатор из
// имени (`md5('vpc.network.admin')`), а `pg_dump` подставляет уже вычисленный
// идентификатор и перечисляет колонки явно. Вторая форма пришла со сводом
// миграций iam 2026-09-04 и стала в этом сервисе ЕДИНСТВЕННОЙ: деривации в своде
// нет ни одной.
//
// Разбор, знавший одну форму, извлёк ноль имён при сорока восьми блоках — это не
// находка и не молчание, а НЕВИДИМОСТЬ: каждая вставленная роль оказалась вне
// наблюдения, при том что обход, перепись и диагностика были целы.
func TestMigrationRoleScannerReadsBothFormsOfTheName(t *testing.T) {
	for _, tc := range []struct {
		name, body string
	}{
		{"рукописная — идентификатор выводится из имени", injMigrationVPCRole},
		{"форма pg_dump — идентификатор вычислен, колонки перечислены", injMigrationDumpForm},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sites, census := ScanMigrationRoleInserts("synthetic.sql", []byte(tc.body))
			if census.Blocks != 1 {
				t.Fatalf("блоков прочитано %d, ожидался 1", census.Blocks)
			}
			if census.Unreadable != 0 {
				t.Fatalf("блок объявлен непрочитанным (%d) — форма записи имени не узнана",
					census.Unreadable)
			}
			if census.Names != 1 || len(sites) != 1 {
				t.Fatalf("имён извлечено %d (мест %d), ожидалось 1: %+v",
					census.Names, len(sites), sites)
			}
			if sites[0].Name != "vpc.network.admin" {
				t.Fatalf("имя прочитано как %q — вероятно, кортеж разорван по запятой "+
					"внутри литерала и колонки сдвинулись", sites[0].Name)
			}
			if sites[0].Owner != "vpc" {
				t.Fatalf("владелец выведен как %q, ожидался vpc", sites[0].Owner)
			}
		})
	}
}

// TestMigrationRoleScannerCountsABlockItCannotRead — блок, имя которого прочесть
// не удалось, считается ОТДЕЛЬНО и молча не пропускается.
//
// # Зачем отдельная проба
//
// Прежде слепоту ловила предпосылка «имён извлечено ноль», и поймала она только
// потому, что свод сменил форму РАЗОМ у всех сорока восьми блоков. Смени форму
// один блок — сорок семь имён скрыли бы сорок восьмое, и гейт остался бы зелёным
// при роли, о которой он не судил.
//
// Пара обязательна: непрочитанный блок обязан считаться, прочитанный — нет.
func TestMigrationRoleScannerCountsABlockItCannotRead(t *testing.T) {
	sites, census := ScanMigrationRoleInserts(
		"synthetic.sql", []byte(injMigrationDumpFormNoColumnList))
	if census.Blocks != 1 {
		t.Fatalf("блоков прочитано %d, ожидался 1", census.Blocks)
	}
	if census.Unreadable != 1 {
		t.Fatalf("непрочитанных блоков %d, ожидался 1 — вставка без перечня колонок имени "+
			"не называет, и пропустить её молча значит не судить о вставленной роли",
			census.Unreadable)
	}
	if census.Names != 0 || len(sites) != 0 {
		t.Fatalf("имён извлечено %d — позиция колонки УГАДАНА, а угадывать её нельзя: "+
			"порядок колонок держится только перечнем: %+v", census.Names, sites)
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: та же форма, но перечень колонок на месте.
	_, ok := ScanMigrationRoleInserts("synthetic.sql", []byte(injMigrationDumpForm))
	if ok.Unreadable != 0 || ok.Names != 1 {
		t.Fatalf("вставка С перечнем колонок обязана читаться: непрочитанных %d, имён %d",
			ok.Unreadable, ok.Names)
	}
}
