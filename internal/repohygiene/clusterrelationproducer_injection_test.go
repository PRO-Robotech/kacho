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
const injSeedSQL = `INSERT INTO kaname.fga_outbox (event_type, payload) VALUES
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

// ── Формы СВОДА: инъекция по каждой названной форме отдельно ────────────────
//
// Расширение распознавателя доказывается не тем, что настоящее дерево зелено, а
// тем, что на СИНТЕТИКЕ каждая новая форма и находится, и, будучи убранной,
// перестаёт находиться. Без этого «гейт зелен» и «гейт ослеп на настоящем
// дереве и зелен поэтому» неразличимы.

// injSeedDumpJSON — форма сведённой миграции: готовый объект JSON, где ключи
// стоят в порядке jsonb (по длине), то есть ОТНОШЕНИЕ ПОСЛЕ ОБЪЕКТА.
const injSeedDumpJSON = `INSERT INTO kaname.fga_outbox (id, event_type, payload, created_at) VALUES ` +
	`(1, 'fga.tuple.write', '{"user": "service_account:svaX", "object": "cluster:cluster_kacho_root", "relation": "system_admin"}', now());`

// injSeedDumpRow — форма сведённой миграции: столбцовая строка журнала отношений.
const injSeedDumpRow = `INSERT INTO kaname.relation_fact (object_type, object_id, relation, subject) VALUES ` +
	`('cluster', 'cluster_kacho_root', 'system_admin', 'group:grpX#member');`

func TestClusterRelationProducerGate_ReadsTheDumpJSONOrder(t *testing.T) {
	dir := t.TempDir()
	produced, read, err := relationsProducedOnCluster([]string{
		writeInj(t, dir, "0001_initial.sql", injSeedDumpJSON),
	})
	if err != nil {
		t.Fatalf("обход фикстуры: %v", err)
	}
	if read != 1 {
		t.Fatalf("прочитано файлов %d, ожидался 1 — обход беспредметен", read)
	}
	if produced["system_admin"] == 0 {
		t.Fatalf("отношение, записанное ПОСЛЕ объекта (порядок ключей jsonb), не найдено: %v.\n"+
			"Так выглядит распознаватель, смотрящий только назад: на сведённой миграции он "+
			"объявляет непроизводимым КАЖДОЕ отношение, при живой выдаче", produced)
	}
}

func TestClusterRelationProducerGate_ReadsTheColumnarRelationRow(t *testing.T) {
	dir := t.TempDir()
	produced, _, err := relationsProducedOnCluster([]string{
		writeInj(t, dir, "0001_initial.sql", injSeedDumpRow),
	})
	if err != nil {
		t.Fatalf("обход фикстуры: %v", err)
	}
	if produced["system_admin"] == 0 {
		t.Fatalf("выдача, записанная столбцами, не найдена: %v", produced)
	}
}

// TestClusterRelationProducerGate_DumpFormsStillMissWhatIsAbsent — контроль в
// обратную сторону по КАЖДОЙ новой форме.
//
// Без него расширение неотличимо от предиката, который засчитывает что угодно:
// обе пробы выше зеленели бы и на разборе, объявляющем произведённым всякое
// отношение, какое встретит.
func TestClusterRelationProducerGate_DumpFormsStillMissWhatIsAbsent(t *testing.T) {
	dir := t.TempDir()
	produced, _, err := relationsProducedOnCluster([]string{
		writeInj(t, dir, "0001_initial.sql", injSeedDumpJSON+"\n"+injSeedDumpRow),
	})
	if err != nil {
		t.Fatalf("обход фикстуры: %v", err)
	}
	if produced["ghost_relation"] != 0 {
		t.Fatalf("отношение, которого в фикстуре НЕТ, объявлено произведённым: %v", produced)
	}
}

// TestClusterRelationProducerGate_JSONWindowStopsAtTheObjectEnd — окно вперёд не
// перетекает в СЛЕДУЮЩИЙ кортеж той же вставки.
//
// Проба существует потому, что без границы по концу объекта отношение соседа
// зачлось бы объекту, у которого своего отношения нет вовсе, — и гейт молчал бы
// ровно там, где обязан находить.
func TestClusterRelationProducerGate_JSONWindowStopsAtTheObjectEnd(t *testing.T) {
	dir := t.TempDir()
	// Первый кортеж называет кластерный объект и НЕ несёт отношения; второй
	// несёт отношение, но объект у него другой.
	body := `INSERT INTO kaname.fga_outbox (payload) VALUES ` +
		`('{"user": "user:u1", "object": "cluster:cluster_kacho_root"}'), ` +
		`('{"user": "user:u2", "object": "group:grpX", "relation": "member"}');`
	produced, _, err := relationsProducedOnCluster([]string{
		writeInj(t, dir, "0001_initial.sql", body),
	})
	if err != nil {
		t.Fatalf("обход фикстуры: %v", err)
	}
	if produced["member"] != 0 {
		t.Fatalf("отношение соседнего кортежа зачлось кластерному объекту: %v.\n"+
			"Окно вперёд обязано кончаться на закрывающей скобке объекта", produced)
	}
}
