// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"sort"
	"strings"
	"testing"
)

// Инъекция гейта «снятый движок не получает НОВЫХ имён в схеме» — В ОБЕ СТОРОНЫ.
//
// Гейт судится ПРОГОНОМ, а не чтением шапки. Обратная сторона здесь несущая:
// слово движка в дереве осталось законно (файл модели прав, учётка посредника,
// разборы в шапках миграций говорят о нём в прошедшем времени), поэтому гейт,
// краснеющий на прозе, был бы снят первым же обходом.
//
// Инъекция подаёт вход РАЗБОРУ, а не дереву: `FindRetiredEngineDatabaseObjects`
// принимает карту «путь → содержимое», поэтому синтетика не заводит ни файла, ни
// репозитория и не может уронить соседний гейт. Требование «инъекция роняет
// только проверяемое» выполняется by construction, а не аккуратностью.

// baseMigration — законная миграция без единого объекта со следом движка.
//
// Положительный контроль всех случаев ниже: пока он молчит, красное соседних
// проб приходит от инъекции, а не от самой формы входа.
const baseMigration = `-- +goose Up
CREATE TABLE kaname.relation_fact (
    id bigint NOT NULL,
    CONSTRAINT relation_fact_pkey PRIMARY KEY (id)
);
-- +goose Down
DROP TABLE IF EXISTS kaname.relation_fact;
`

func scanOrFail(t *testing.T, sources map[string]string) ([]string, RetiredEngineDatabaseCensus) {
	t.Helper()
	objects, census, err := FindRetiredEngineDatabaseObjects(sources)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	keys := make([]string, 0, len(objects))
	for _, o := range objects {
		keys = append(keys, o.Key())
	}
	sort.Strings(keys)
	return keys, census
}

// TestInjection_ControlCleanTreeIsSilent — контроль: дерево без имён движка даёт
// пустой состав при НЕПУСТОМ обходе.
//
// Без этого случая «ноль находок» ниже было бы неотличимо от «ноль
// прочитанного».
func TestInjection_ControlCleanTreeIsSilent(t *testing.T) {
	keys, census := scanOrFail(t, map[string]string{
		"services/iam/internal/migrations/0001_initial.sql": baseMigration,
	})
	if census.Files != 1 || census.Statements == 0 {
		t.Fatalf("обход не состоялся: файлов %d, операторов %d — контроль беспредметен",
			census.Files, census.Statements)
	}
	if len(keys) != 0 {
		t.Fatalf("на чистом дереве найдено %d объектов (%v) — гейт ловит форму, а не существо", len(keys), keys)
	}
}

// TestInjection_RedOnANewObjectTakingTheRetiredName — новый объект схемы взял
// имя снятого движка → гейт КРАСНЕЕТ и НАЗЫВАЕТ КООРДИНАТУ.
func TestInjection_RedOnANewObjectTakingTheRetiredName(t *testing.T) {
	sources := map[string]string{
		"services/iam/internal/migrations/0001_initial.sql": baseMigration,
		"services/iam/internal/migrations/0102_fga_replay_journal.sql": `-- +goose Up
CREATE TABLE kaname.fga_replay_journal (
    id bigint NOT NULL
);
CREATE INDEX fga_replay_journal_pending_idx ON kaname.fga_replay_journal (id);
-- +goose Down
DROP TABLE IF EXISTS kaname.fga_replay_journal;
`,
	}
	objects, census, err := FindRetiredEngineDatabaseObjects(sources)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if census.Files != 2 {
		t.Fatalf("прочитано файлов %d, ожидалось 2 — инъекция не доехала до разбора", census.Files)
	}

	got := make([]string, 0, len(objects))
	for _, o := range objects {
		got = append(got, o.Key())
	}
	sort.Strings(got)

	added, removed := diffSortedStrings(nil, got)
	if len(added) != 2 {
		t.Fatalf("находок %d, ожидалось 2 (таблица и индекс): %v", len(added), added)
	}
	if len(removed) != 0 {
		t.Fatalf("убыли быть не должно: %v", removed)
	}

	// Координата обязана называться: находка, называющая только симптом,
	// посылает читателя искать не там.
	var namedMigration bool
	for _, o := range objects {
		if o.Name == "fga_replay_journal" {
			if o.Migration != "0102_fga_replay_journal.sql" {
				t.Errorf("координата не названа: миграция %q, ожидалась 0102_fga_replay_journal.sql", o.Migration)
			}
			if o.Service != "iam" {
				t.Errorf("сервис не назван: %q", o.Service)
			}
			namedMigration = true
		}
	}
	if !namedMigration {
		t.Fatalf("новая таблица не найдена вовсе: %v", got)
	}
}

// TestInjection_SilentOnTheRetiredNameInProse — ЗАКОННЫЙ БЛИЗНЕЦ: то же слово,
// и даже тот же оператор, но в РАЗБОРЕ, объясняющем снятый движок.
//
// Ровно та форма, которая стоит в шапках миграций этого дерева. Гейт по слову
// покраснел бы на ней — то есть на исправном дереве.
func TestInjection_SilentOnTheRetiredNameInProse(t *testing.T) {
	sources := map[string]string{
		"services/iam/internal/migrations/0001_initial.sql": baseMigration,
		"services/iam/internal/migrations/0103_history.sql": `-- +goose Up
-- Журнал намерений внешнего движка заводился так:
--     CREATE TABLE kaname.fga_outbox (id bigint NOT NULL);
--     CREATE INDEX fga_outbox_pending_idx ON kaname.fga_outbox (created_at);
-- Движок снят стадией S6; строки выше оставлены как история, а не как описание
-- сегодняшнего тракта.
/* CREATE SEQUENCE kaname.fga_outbox_id_seq; — и это тоже история */
CREATE TABLE kaname.relation_fact_conditioned (id bigint NOT NULL);
-- +goose Down
DROP TABLE IF EXISTS kaname.relation_fact_conditioned;
`,
	}
	keys, census := scanOrFail(t, sources)
	if census.Files != 2 || census.Statements == 0 {
		t.Fatalf("обход не состоялся: файлов %d, операторов %d", census.Files, census.Statements)
	}
	if len(keys) != 0 {
		t.Fatalf("проза о снятом движке принята за объект схемы: %v — гейт краснел бы на исправном дереве", keys)
	}
}

// TestInjection_SilentOnAnObjectDeclaredOnlyInDown — объект, заведённый только в
// откате, живым составом не является.
//
// Down описывает возврат, а не сегодняшнюю схему; читать его значило бы считать
// снятое заведённым.
func TestInjection_SilentOnAnObjectDeclaredOnlyInDown(t *testing.T) {
	keys, _ := scanOrFail(t, map[string]string{
		"services/iam/internal/migrations/0104_retire.sql": `-- +goose Up
DROP TABLE IF EXISTS kaname.relation_fact_conditioned;
-- +goose Down
CREATE TABLE kaname.fga_outbox (id bigint NOT NULL);
`,
	})
	if len(keys) != 0 {
		t.Fatalf("объект из секции Down засчитан живым: %v", keys)
	}
}

// TestInjection_DropRemovesTheObjectFromTheLiveSet — снятый объект из живого
// состава уходит.
//
// Обратная сторона: без неё гейт объявлял бы живым всё, что когда-либо
// заводилось, и ведомость никогда бы не сходилась.
func TestInjection_DropRemovesTheObjectFromTheLiveSet(t *testing.T) {
	keys, _ := scanOrFail(t, map[string]string{
		"services/iam/internal/migrations/0001_initial.sql": `-- +goose Up
CREATE TABLE kaname.fga_outbox (id bigint NOT NULL);
CREATE TRIGGER fga_outbox_notify_trigger AFTER INSERT ON kaname.fga_outbox FOR EACH ROW EXECUTE FUNCTION kaname.noop();
-- +goose Down
`,
		"services/iam/internal/migrations/0105_channel_retires.sql": `-- +goose Up
DROP TRIGGER IF EXISTS fga_outbox_notify_trigger ON kaname.fga_outbox;
-- +goose Down
`,
	})
	want := []string{"iam TABLE fga_outbox"}
	if strings.Join(keys, "|") != strings.Join(want, "|") {
		t.Fatalf("живой состав %v, ожидался %v — снятие не учтено", keys, want)
	}
}

// TestInjection_RenameIsUnderstoodInBothDirections — переименование разбирается.
//
// Заведено намеренно и до того, как в дереве появился хоть один RENAME таблицы:
// разбор, не понимающий его, при первом же переименовании оставил бы ведомость
// стеречь имя, которого больше нет, — и МОЛЧА, потому что старое имя просто
// перестало бы встречаться в CREATE.
func TestInjection_RenameIsUnderstoodInBothDirections(t *testing.T) {
	const created = `-- +goose Up
CREATE TABLE kaname.fga_outbox (id bigint NOT NULL);
ALTER TABLE kaname.fga_outbox ADD CONSTRAINT fga_outbox_pkey PRIMARY KEY (id);
-- +goose Down
`
	// Прочь из семьи движка: объект уходит из живого состава.
	keys, _ := scanOrFail(t, map[string]string{
		"services/iam/internal/migrations/0001_initial.sql": created,
		"services/iam/internal/migrations/0106_rename_away.sql": `-- +goose Up
ALTER TABLE kaname.fga_outbox RENAME TO relation_intent_journal;
ALTER TABLE kaname.relation_intent_journal RENAME CONSTRAINT fga_outbox_pkey TO relation_intent_journal_pkey;
-- +goose Down
`,
	})
	if len(keys) != 0 {
		t.Fatalf("после переименования прочь из семьи движка живой состав %v, ожидался пустой", keys)
	}

	// Внутри семьи: имя сменилось, след движка остался — объект остаётся под НОВЫМ именем.
	keys, _ = scanOrFail(t, map[string]string{
		"services/iam/internal/migrations/0001_initial.sql": created,
		"services/iam/internal/migrations/0107_rename_within.sql": `-- +goose Up
ALTER TABLE kaname.fga_outbox RENAME TO fga_intent_outbox;
-- +goose Down
`,
	})
	want := []string{"iam CONSTRAINT fga_outbox_pkey", "iam TABLE fga_intent_outbox"}
	sort.Strings(want)
	if strings.Join(keys, "|") != strings.Join(want, "|") {
		t.Fatalf("после переименования внутри семьи живой состав %v, ожидался %v", keys, want)
	}
}

// TestInjection_RedWhenTheLedgerOutlivesItsSubject — САМОИСТЕЧЕНИЕ: строка
// ведомости, которой больше нечего описывать, — находка.
//
// Без этой стороны ведомость пережила бы свой предмет и стерегла бы имя,
// которого в дереве нет, оставаясь на вид рабочей.
func TestInjection_RedWhenTheLedgerOutlivesItsSubject(t *testing.T) {
	keys, _ := scanOrFail(t, map[string]string{
		"services/iam/internal/migrations/0001_initial.sql": baseMigration,
	})
	ledger := []string{"iam TABLE fga_outbox"}
	added, removed := diffSortedStrings(ledger, keys)
	if len(added) != 0 {
		t.Fatalf("прибавки быть не должно: %v", added)
	}
	if len(removed) != 1 || removed[0] != "iam TABLE fga_outbox" {
		t.Fatalf("убыль не замечена: %v — ведомость стерегла бы то, чего нет", removed)
	}
}

// TestInjection_EmptyWalkIsNotAVerdict — предпосылка гейта проверяется им самим.
//
// Пустой вход обязан давать НУЛЕВОЙ объём осмотренного, на котором гейт роняет
// прогон, а не молчит: иначе «имён движка не прибавилось» означало бы «ничего не
// прочитано».
func TestInjection_EmptyWalkIsNotAVerdict(t *testing.T) {
	keys, census := scanOrFail(t, map[string]string{})
	if len(keys) != 0 {
		t.Fatalf("на пустом входе найдены объекты: %v", keys)
	}
	if census.Files != 0 || census.Services != 0 || census.Statements != 0 {
		t.Fatalf("пустой вход дал непустой объём осмотренного: %s — предпосылка гейта неверна", census)
	}
}

// TestInjection_TokenIsASegmentNotASubstring — след движка ищется сегментом
// имени, а не подстрокой.
//
// Иначе под предикат попало бы всякое имя, внутри которого эти три буквы
// оказались случайно, и гейт краснел бы на своём же дереве.
func TestInjection_TokenIsASegmentNotASubstring(t *testing.T) {
	keys, census := scanOrFail(t, map[string]string{
		"services/vpc/internal/migrations/0001_initial.sql": `-- +goose Up
CREATE TABLE kacho_vpc.config_gateway (id bigint NOT NULL);
CREATE INDEX suffgap_idx ON kacho_vpc.config_gateway (id);
CREATE TABLE kacho_vpc.fga_register_outbox (id bigint NOT NULL);
-- +goose Down
`,
	})
	if census.Statements == 0 {
		t.Fatalf("обход не состоялся")
	}
	want := []string{"vpc TABLE fga_register_outbox"}
	if strings.Join(keys, "|") != strings.Join(want, "|") {
		t.Fatalf("живой состав %v, ожидался %v — предикат считает подстроку, а не сегмент", keys, want)
	}
}
