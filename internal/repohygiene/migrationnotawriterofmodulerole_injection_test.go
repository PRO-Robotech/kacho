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
INSERT INTO kacho_iam.roles (id, cluster_id, account_id, name, description, permissions) VALUES
  ('rol' || substr(md5('vpc.network.admin'), 1, 17), 'cluster_kacho_root', NULL,
   'vpc.network.admin', 'Admin Network', '["vpc.network.*"]'::jsonb);
`

// injMigrationNoOwnerRole — роль БЕЗ модуля-владельца: точки в имени нет.
const injMigrationNoOwnerRole = `-- +goose Up
INSERT INTO kacho_iam.roles (id, cluster_id, account_id, name, description, permissions) VALUES
  ('rol' || substr(md5('owner'), 1, 17), 'cluster_kacho_root', NULL,
   'owner', 'Owner', '["*.*.*.*"]'::jsonb);
`

// injMigrationForeignTable — тот же образец идентификатора во вставке в ЧУЖУЮ
// таблицу. Предикат без привязки к блоку считал бы это ролью.
const injMigrationForeignTable = `-- +goose Up
INSERT INTO kacho_iam.access_bindings (id, subject_id, role_id) VALUES
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
