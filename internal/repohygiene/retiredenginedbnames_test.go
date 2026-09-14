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
// Семья ОДНА — `fga_register_outbox*` (compute, nlb, storage, vpc): очереди
// регистрации ресурса у службы прав. Живы и работают; имя историческое, от
// снятого движка. Registry — положительный контроль семьи: тот же механизм он
// назвал `registry_outbox`, по своему домену, и потому в ведомости отсутствует.
// Без этого контроля «ноль находок» у registry было бы неотличимо от слепоты
// разбора на его миграциях.
//
// ЗДЕСЬ СТОЯЛИ ЕЩЁ ДВЕ СЕМЬИ, обе службы доступа, и обе ушли вместе со своим
// предметом:
//
//   - `fga_model_version*` — версия модели, ЗАГРУЖЕННОЙ в снятый движок; три
//     строки сняты, когда цепочка миграций службы была сведена в одну первичную
//     и разбору стало нечего считать живым;
//
//   - `fga_outbox*` — журнал намерений службы доступа; пять строк (таблица,
//     последовательность и три ограничения) сняты вместе с выносом `services/iam`
//     отдельным продуктом. Миграций службы в этом дереве больше нет, поэтому
//     разбор их не читает, а ведомость про них утверждала бы живое отсутствующее.
//     Двусторонний храповик это и назвал находкой: «строк, которым больше нечего
//     описывать, 5».
//
// Предикат, которым это перемеряется: `git ls-files 'services/*/internal/migrations/*.sql'
// | grep -c '^services/iam/'` → 0. Уборка, которой строки свидетельствовали,
// записана в `dropguard.json` соответствующего сервиса — там она и остаётся;
// второе место о ней здесь завелось бы снова.
var retiredEngineDatabaseLedger = []string{
	"compute CONSTRAINT compute_fga_register_outbox_event_type_check",
	"compute FUNCTION compute_fga_register_outbox_notify",
	"compute INDEX compute_fga_register_outbox_claim_order_idx",
	"compute INDEX compute_fga_register_outbox_partition_head_idx",
	"compute TABLE compute_fga_register_outbox",
	"compute TRIGGER compute_fga_register_outbox_notify_trg",
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
	t.Parallel()
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
