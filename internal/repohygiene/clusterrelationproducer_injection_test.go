// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// clusterrelationproducer_injection_test.go — доказательство, что гейт
// TestClusterScopedCatalogEntryNamesARelationSomeoneProduces СПОСОБЕН упасть на
// дефекте и СМОЛЧАТЬ на законном близнеце. Без обеих сторон гейт ловил бы форму,
// а не существо: односторонняя проба зеленеет и на разборе, отвергающем всё.

import (
	"os"
	"path/filepath"
	"testing"
)

const injCatalog = `[
  {"fqn":"svc/Legit","permission":"x.legit","required_relation":"system_admin",
   "scope_extractor":{"object_type":"cluster","from_request_field":"*"}},
  {"fqn":"svc/Exempt","permission":"<exempt>","required_relation":"",
   "scope_extractor":{"object_type":"","from_request_field":""}},
  {"fqn":"svc/ProjectScoped","permission":"x.proj","required_relation":"v_list",
   "scope_extractor":{"object_type":"vpc_network","from_request_field":"network_id"}},
  {"fqn":"svc/Broken","permission":"x.broken","required_relation":"ghost_relation",
   "scope_extractor":{"object_type":"cluster","from_request_field":"*"}}
]`

// injSeed — производитель отношений в обеих формах, которыми сеет дерево: SQL
// (jsonb_build_object) и JSON в коде.
const injSeedSQL = `INSERT INTO kacho_iam.fga_outbox (event_type, payload) VALUES
  ('fga.tuple.write', jsonb_build_object(
     'user','service_account:svaX','relation','system_admin','object','cluster:cluster_kacho_root'));`

const injSeedGo = `package seed
const payload = ` + "`" + `{"user": "user:u1", "relation": "system_viewer", "object": "cluster:cluster_kacho_root"}` + "`"

func writeInj(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("фикстура %s не записана: %v", name, err)
	}
	return p
}

func TestClusterRelationProducerGate_FailsOnAnUnproducedRelation(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		writeInj(t, dir, "0001_seed.sql", injSeedSQL),
		writeInj(t, dir, "seed.go", injSeedGo),
	}

	required, total, err := clusterScopedCatalogRelations([]byte(injCatalog))
	if err != nil {
		t.Fatalf("каталог фикстуры не разобран: %v", err)
	}
	if total != 4 {
		t.Fatalf("фикстура каталога прочитана не целиком: записей %d, ожидалось 4", total)
	}

	// Область отбора: кластерные записи с отношением — и только они. Запись
	// `<exempt>` и запись с областью проекта в отбор попасть не вправе, иначе
	// гейт судил бы то, о чём не говорит.
	if _, ok := required["v_list"]; ok {
		t.Error("в отбор попала запись с областью ПРОЕКТА — гейт вышел за свой предмет")
	}
	if len(required) != 2 {
		t.Fatalf("кластерных отношений отобрано %d, ожидалось 2 (system_admin, ghost_relation): %v",
			len(required), required)
	}

	produced, read, err := relationsProducedOnCluster(files)
	if err != nil {
		t.Fatalf("производители не прочитаны: %v", err)
	}
	if read != 2 {
		t.Fatalf("прочитано файлов %d, ожидалось 2", read)
	}

	// ДЕФЕКТ: отношения, которого никто не производит, гейт обязан НЕ найти.
	if produced["ghost_relation"] != 0 {
		t.Errorf("выдуманное отношение объявлено производимым (%d) — гейт не упал бы на дефекте",
			produced["ghost_relation"])
	}
	// ЗАКОННЫЙ БЛИЗНЕЦ: отношение, которое посев заводит, обязано найтись — в
	// ОБЕИХ формах, иначе гейт красил бы законный посев.
	if produced["system_admin"] == 0 {
		t.Error("отношение из SQL-посева не распознано — гейт дал бы ложную находку на законном дереве")
	}
	if produced["system_viewer"] == 0 {
		t.Error("отношение из посева в коде не распознано — гейт дал бы ложную находку на законном дереве")
	}
}

func TestClusterRelationProducerGate_IgnoresANonClusterObject(t *testing.T) {
	dir := t.TempDir()
	// Тот же посев, но объект НЕ кластерный: гейт не вправе считать это
	// производством кластерного отношения.
	files := []string{writeInj(t, dir, "0002_project.sql",
		`INSERT INTO x VALUES (jsonb_build_object('relation','ghost_relation','object','project:prj1'));`)}

	produced, read, err := relationsProducedOnCluster(files)
	if err != nil {
		t.Fatalf("производители не прочитаны: %v", err)
	}
	if read != 1 {
		t.Fatalf("прочитано файлов %d, ожидалось 1", read)
	}
	if len(produced) != 0 {
		t.Errorf("отношение на НЕкластерном объекте засчитано производителем: %v — "+
			"тогда гейт молчал бы на дефекте, у которого производитель есть, но не тот", produced)
	}
}

func TestClusterRelationProducerGate_TestFilesAreNotProducers(t *testing.T) {
	dir := t.TempDir()
	// Посев в ПРОБЕ производителем не является: иначе фикстура соседнего теста
	// маскировала бы отсутствие настоящего посева.
	files := []string{writeInj(t, dir, "seed_test.go", injSeedGo)}

	produced, read, err := relationsProducedOnCluster(files)
	if err != nil {
		t.Fatalf("производители не прочитаны: %v", err)
	}
	if read != 0 || len(produced) != 0 {
		t.Errorf("проба засчитана производителем (файлов %d, отношений %v) — "+
			"фикстура пробы маскировала бы отсутствие посева", read, produced)
	}
}
