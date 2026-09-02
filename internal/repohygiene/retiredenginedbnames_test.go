// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// retiredEngineDatabaseLedger — ЖИВОЙ состав объектов схемы, чьё имя досталось
// от снятого движка прав.
//
// Перепись снята по стволу продукта; предикат, которым она повторяется, назван в
// сообщении отказа ниже. Ведомость хранит ТОЧНЫЙ состав, а не потолок: потолок
// не краснеет никогда и потому не истекает.
//
// Три семьи, и различать их надо, потому что предикат снятия у них разный:
//
//   - `fga_outbox*` (iam) — журнал намерений. Дренаж во внешний движок снят
//     вместе с движком; журнал остался и несущий: триггер `relation_fact_follows_journal`
//     складывает из его строк прямой факт, из которого форма считает вердикт.
//     Имя устарело; переименование рассматривалось и отклонено шапкой миграции
//     `20260822160000_journal_without_delivery_columns.sql`, задача #1667.
//
//   - `fga_register_outbox*` (compute, nlb, storage, vpc) — очереди регистрации
//     ресурса у службы прав. Живы и работают; имя такое же историческое.
//     Registry — положительный контроль этой семьи: тот же механизм он назвал
//     `registry_outbox`, по своему домену, и потому в ведомости отсутствует.
//
//   - `fga_model_version*` (iam) — версия модели, ЗАГРУЖЕННОЙ в снятый движок.
//     Читателей и писателей у таблицы не было ни одного вне миграций, поэтому
//     предметом здесь была уборка, а не переименование: таблица снята миграцией
//     `20260831120000_drop_fga_model_version.sql` (задача #1717), и строка
//     ведомости ушла ТЕМ ЖЕ изменением.
//     Три строки семьи остались, и это НЕ упущение: последовательность и оба
//     ключа уходят из базы вместе с таблицей неявно, а разбор миграций видит
//     только ЯВНЫЙ `DROP` в секции `Up`. Снять их из ведомости значило бы
//     объявить прибавкой то, что разбор по-прежнему считает живым.
var retiredEngineDatabaseLedger = []string{
	"compute CONSTRAINT compute_fga_register_outbox_event_type_check",
	"compute FUNCTION compute_fga_register_outbox_notify",
	"compute INDEX compute_fga_register_outbox_claim_order_idx",
	"compute INDEX compute_fga_register_outbox_partition_head_idx",
	"compute TABLE compute_fga_register_outbox",
	"compute TRIGGER compute_fga_register_outbox_notify_trg",
	"iam CONSTRAINT fga_model_version_authorization_model_id_key",
	"iam CONSTRAINT fga_model_version_pkey",
	"iam CONSTRAINT fga_outbox_event_type_check",
	"iam CONSTRAINT fga_outbox_pkey",
	"iam CONSTRAINT fga_outbox_relation_present_check",
	"iam SEQUENCE fga_model_version_id_seq",
	"iam SEQUENCE fga_outbox_id_seq",
	"iam TABLE fga_outbox",
	"nlb CONSTRAINT fga_register_outbox_event_type_check",
	"nlb CONSTRAINT fga_register_outbox_payload_object_ck",
	"nlb FUNCTION fga_register_outbox_notify",
	"nlb INDEX fga_register_outbox_claim_order_idx",
	"nlb INDEX fga_register_outbox_partition_head_idx",
	"nlb SEQUENCE fga_register_outbox_id_seq",
	"nlb TABLE fga_register_outbox",
	"nlb TRIGGER fga_register_outbox_notify_trg",
	"storage CONSTRAINT fga_register_outbox_event_type_check",
	"storage FUNCTION fga_register_outbox_notify",
	"storage INDEX fga_register_outbox_claim_order_idx",
	"storage INDEX fga_register_outbox_partition_head_idx",
	"storage TABLE fga_register_outbox",
	"storage TRIGGER fga_register_outbox_notify_trg",
	"vpc CONSTRAINT fga_register_outbox_event_type_check",
	"vpc FUNCTION fga_register_outbox_notify",
	"vpc INDEX fga_register_outbox_claim_order_idx",
	"vpc INDEX fga_register_outbox_partition_head_idx",
	"vpc TABLE fga_register_outbox",
	"vpc TRIGGER fga_register_outbox_notify_trg",
}

// TestRetiredEngineNameTakesNoNewDatabaseObject — снятый движок не получает
// НОВЫХ имён в схеме.
//
// Гейт судит РОСТ, а не наличие: имена, уже стоящие в применённых миграциях,
// правке не подлежат (применённую миграцию не редактируют), и ведомость
// описывает их как факт. Прибавка — находка; убыль — тоже находка, потому что
// ведомость, которой больше нечего описывать, переживает свой предмет.
func TestRetiredEngineNameTakesNoNewDatabaseObject(t *testing.T) {
	root := retiredEngineRepoRoot(t)
	sources := readServiceMigrations(t, root)

	objects, census, err := FindRetiredEngineDatabaseObjects(sources)
	if err != nil {
		t.Fatalf("разбор миграций: %v", err)
	}
	t.Logf("объём осмотренного: %s", census)

	// Предпосылка: пустой обход вердикта не выносит.
	if census.Files == 0 || census.Services == 0 {
		t.Fatalf("обход пуст (файлов %d, сервисов %d) — вердикт беспредметен: "+
			"каталоги services/*/internal/migrations не прочитаны", census.Files, census.Services)
	}
	if census.Statements == 0 {
		t.Fatalf("распознано операторов 0 при %d прочитанных файлах — разбор не понимает "+
			"этот диалект, и «находок нет» означало бы «ничего не прочитано»", census.Files)
	}

	got := make([]string, 0, len(objects))
	byKey := make(map[string]RetiredEngineDatabaseObject, len(objects))
	for _, o := range objects {
		got = append(got, o.Key())
		byKey[o.Key()] = o
	}
	sort.Strings(got)

	want := append([]string(nil), retiredEngineDatabaseLedger...)
	sort.Strings(want)

	added, removed := diffSortedStrings(want, got)

	if len(added) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "новых объектов схемы с именем снятого движка: %d.\n", len(added))
		b.WriteString("Движок отношений снят стадией S6 эпика #747; имя, которое он оставил, " +
			"продлевать НОВЫМ объектом нельзя — оно уже стоило заведённой по нему задачи #1667.\n")
		for _, k := range added {
			o := byKey[k]
			fmt.Fprintf(&b, "  + %-11s %-46s services/%s/internal/migrations/%s\n",
				o.Kind, o.Name, o.Service, o.Migration)
		}
		b.WriteString("Исходов два: назвать объект по домену-владельцу (registry так и сделал — " +
			"`registry_outbox` вместо имени движка), либо, если имя взято осознанно, " +
			"внести строку в retiredEngineDatabaseLedger ЭТИМ ЖЕ изменением и сказать в шапке миграции, почему.")
		t.Error(b.String())
	}

	if len(removed) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "ведомость пережила свой предмет: строк, которым больше нечего описывать, %d.\n", len(removed))
		b.WriteString("Объект переименован или снят — значит строка ведомости лишняя, и снимается она " +
			"ТЕМ ЖЕ изменением, которым ушёл объект. Иначе гейт стережёт то, чего нет.\n")
		for _, k := range removed {
			fmt.Fprintf(&b, "  − %s\n", k)
		}
		t.Error(b.String())
	}

	if len(added) == 0 && len(removed) == 0 {
		t.Logf("состав сходится: %d объектов в ведомости, %d в дереве", len(want), len(got))
	}
}

// diffSortedStrings — что появилось в got сверх want и чего в got не хватает.
func diffSortedStrings(want, got []string) (added, removed []string) {
	inWant := make(map[string]bool, len(want))
	for _, w := range want {
		inWant[w] = true
	}
	inGot := make(map[string]bool, len(got))
	for _, g := range got {
		inGot[g] = true
	}
	for _, g := range got {
		if !inWant[g] {
			added = append(added, g)
		}
	}
	for _, w := range want {
		if !inGot[w] {
			removed = append(removed, w)
		}
	}
	return added, removed
}

// readServiceMigrations — содержимое всех .sql каталогов services/*/internal/migrations.
func readServiceMigrations(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	servicesDir := filepath.Join(root, "services")
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		t.Fatalf("чтение %s: %v", servicesDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(servicesDir, e.Name(), "internal", "migrations")
		files, err := os.ReadDir(dir)
		if err != nil {
			continue // сервис без каталога миграций — законно
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".sql") {
				continue
			}
			body, err := os.ReadFile(filepath.Join(dir, f.Name()))
			if err != nil {
				t.Fatalf("чтение %s: %v", filepath.Join(dir, f.Name()), err)
			}
			out[filepath.ToSlash(filepath.Join("services", e.Name(), "internal", "migrations", f.Name()))] = string(body)
		}
	}
	return out
}

// retiredEngineRepoRoot — корень репозитория.
func retiredEngineRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("корень репозитория: %v", err)
	}
	return root
}
