// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// resources_doc_census_test.go — сверка `docs/architecture/01-resources.md` с деревом.
//
// Документ описывал ДОРЕДИЗАЙНОВЫЙ compute и читался как описание кода. Измерено
// механически: из 28 строк таблицы полей Instance 11 описывали поля, которых в
// контракте нет (`platform_id`, `resources`, `metadata`, `metadata_options`,
// `gpu_settings`, `scheduling_policy`, `service_account_id`, `placement_policy`,
// `host_group_id`/`host_id`, `reserved_instance_pool_id`, `application`), а СЕМЬ
// действующих полей — включая несущие для редизайна `instance_kind`,
// `machine_type_id`, `effective_resources`, `cpu_guarantee_percent` — не упоминались
// вовсе. Плюс два раздела про Region/Zone, которые compute не обслуживает с этапа S7
// (владелец — kacho-geo).
//
// Доредизайновый документ, читающийся как актуальный, — то же ложное утверждение, что
// и прогонщик, печатающий зелёное при красном, только адресованное человеку. Разница
// лишь в том, что человека никто не проверяет автоматически — поэтому проверка здесь.
//
// Гейт спрашивает ТРИ вещи, все механические:
//  1. документ не называет полем Instance то, чего в контракте нет;
//  2. документ называет КАЖДОЕ действующее поле Instance;
//  3. таблица RPC совпадает с `InstanceService` в обе стороны — ни лишнего, ни
//     пропущенного (пропущены были `SetAccessBindings`/`UpdateAccessBindings`).
//
// Прозу (инварианты, cross-links) гейт не читает и не притворяется, что читает: он
// сверяет состав, а состав — это то, что дрейфует молча и чаще всего.
package tools_regression

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
)

// docPath — путь к документу от каталога этого теста (services/compute/tools).
func docPath(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	p := filepath.Join(filepath.Dir(self), "..", "docs", "architecture", "01-resources.md")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("нет документа %s: %v", p, err)
	}
	return p
}

// instanceFields — действующий состав message Instance, из дескриптора, а не из списка
// руками: список руками — ровно то, что здесь и разошлось.
func instanceFields(t *testing.T) map[string]protoreflect.FieldNumber {
	t.Helper()
	md := (&computev1.Instance{}).ProtoReflect().Descriptor()
	out := map[string]protoreflect.FieldNumber{}
	fs := md.Fields()
	for i := 0; i < fs.Len(); i++ {
		f := fs.Get(i)
		out[string(f.Name())] = f.Number()
	}
	if len(out) == 0 {
		t.Fatal("дескриптор Instance не дал ни одного поля — тест ничего не утверждает")
	}
	t.Logf("полей в message Instance: %d", len(out))
	return out
}

// backtickedNames — все `snake_case`-идентификаторы документа: то, что читатель видит
// как имя поля.
var backtickedNames = regexp.MustCompile("`([a-z][a-z0-9_]*)`")

// docFieldTableRows — строки таблицы полей Instance («### proto-поля …» до следующего
// заголовка того же уровня). Читается ИМЕННО таблица: имя поля в прозе может быть
// историческим свидетельством, а строка таблицы — утверждением «поле такое».
func docFieldTableRows(t *testing.T, doc string) []string {
	t.Helper()
	var rows []string
	inside := false
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "### proto-поля") && strings.Contains(line, "Instance") {
			inside = true
			continue
		}
		if inside && strings.HasPrefix(line, "### ") {
			break
		}
		if inside && strings.HasPrefix(line, "| `") {
			rows = append(rows, line)
		}
	}
	if len(rows) == 0 {
		t.Fatal("таблица полей Instance в документе не найдена — сверять нечего, " +
			"а значит гейт молчал бы о любом дрейфе")
	}
	t.Logf("строк в таблице полей документа: %d", len(rows))
	return rows
}

// TestResourcesDoc_NamesNoRetiredInstanceField — ни одна строка таблицы не описывает
// поле, которого в контракте нет.
func TestResourcesDoc_NamesNoRetiredInstanceField(t *testing.T) {
	raw, err := os.ReadFile(docPath(t))
	if err != nil {
		t.Fatalf("read doc: %v", err)
	}
	doc := string(raw)
	fields := instanceFields(t)

	for _, row := range docFieldTableRows(t, doc) {
		cell := strings.SplitN(row, "|", 3)[1]
		names := backtickedNames.FindAllStringSubmatch(cell, -1)
		if len(names) == 0 {
			continue
		}
		alive := false
		var listed []string
		for _, m := range names {
			listed = append(listed, m[1])
			if _, ok := fields[m[1]]; ok {
				alive = true
			}
		}
		if !alive {
			t.Errorf("строка таблицы описывает поле(я) %v, которых в message Instance нет: %s",
				listed, strings.TrimSpace(row))
		}
	}
}

// TestResourcesDoc_NamesEveryLiveInstanceField — каждое действующее поле имеет СТРОКУ В
// ТАБЛИЦЕ.
//
// Обратное направление обязательно: документ, из которого просто вычеркнули устаревшее,
// полон и молчалив одновременно — молчание о несущих полях редизайна (`instance_kind`,
// `machine_type_id`, `effective_resources`) было половиной дефекта.
//
// Проверяется ТАБЛИЦА, а не весь текст, и это исправление самого гейта: первая редакция
// искала имя по всему документу и на инъекции «убрать строку `instance_kind`» осталась
// ЗЕЛЁНОЙ — имя нашлось в прозе строки Create. Проверка, которую удовлетворяет
// упоминание в чужом абзаце, не проверяет состав.
func TestResourcesDoc_NamesEveryLiveInstanceField(t *testing.T) {
	raw, err := os.ReadFile(docPath(t))
	if err != nil {
		t.Fatalf("read doc: %v", err)
	}
	doc := string(raw)

	inTable := map[string]bool{}
	for _, row := range docFieldTableRows(t, doc) {
		cell := strings.SplitN(row, "|", 3)[1]
		for _, m := range backtickedNames.FindAllStringSubmatch(cell, -1) {
			inTable[m[1]] = true
		}
	}
	for name, num := range instanceFields(t) {
		if !inTable[name] {
			t.Errorf("действующее поле Instance %q (номер %d) не имеет строки в таблице "+
				"полей: упоминание в прозе состав не описывает", name, num)
		}
	}
}

// TestResourcesDoc_RpcTableMatchesTheService — таблица RPC совпадает с сервисом в обе
// стороны.
//
// Пропуск и лишнее — разные дефекты с одним следствием: читатель считает, что видит
// поверхность. Пропущенными были `SetAccessBindings`/`UpdateAccessBindings` — а они
// вдобавок объявлены без обработчика, то есть отвечают `Unimplemented`; именно про
// такие строки документ и обязан говорить, иначе их «наличие» узнают вызовом.
func TestResourcesDoc_RpcTableMatchesTheService(t *testing.T) {
	raw, err := os.ReadFile(docPath(t))
	if err != nil {
		t.Fatalf("read doc: %v", err)
	}
	doc := string(raw)

	sd := (&computev1.Instance{}).ProtoReflect().Descriptor().ParentFile().Services()
	var svc protoreflect.ServiceDescriptor
	for i := 0; i < sd.Len(); i++ {
		if sd.Get(i).Name() == "InstanceService" {
			svc = sd.Get(i)
		}
	}
	// Instance живёт в instance.proto, а сервис — в instance_service.proto; если файл
	// сменится, молчать нельзя.
	if svc == nil {
		for _, fd := range []protoreflect.FileDescriptor{
			(&computev1.CreateInstanceRequest{}).ProtoReflect().Descriptor().ParentFile(),
		} {
			ss := fd.Services()
			for i := 0; i < ss.Len(); i++ {
				if ss.Get(i).Name() == "InstanceService" {
					svc = ss.Get(i)
				}
			}
		}
	}
	if svc == nil {
		t.Fatal("дескриптор InstanceService не найден — тест ничего не утверждает")
	}

	actual := map[string]bool{}
	ms := svc.Methods()
	for i := 0; i < ms.Len(); i++ {
		actual[string(ms.Get(i).Name())] = true
	}
	t.Logf("RPC в InstanceService: %d", len(actual))

	// Строки таблицы RPC: «### RPC (`instance_service.proto` …» до следующего заголовка.
	rowRe := regexp.MustCompile("^\\| `([A-Za-z]+)` \\|")
	listed := map[string]bool{}
	inside := false
	rows := 0
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "### RPC (`instance_service.proto`") {
			inside = true
			continue
		}
		if inside && strings.HasPrefix(line, "### ") {
			break
		}
		if !inside {
			continue
		}
		if m := rowRe.FindStringSubmatch(line); m != nil {
			rows++
			listed[m[1]] = true
		}
	}
	if rows == 0 {
		t.Fatal("таблица RPC в документе не найдена — сверять нечего")
	}
	t.Logf("строк в таблице RPC документа: %d", rows)

	for name := range actual {
		if !listed[name] {
			t.Errorf("RPC %q объявлен в InstanceService, но в таблице документа его нет: "+
				"читатель не узнает о нём из описания поверхности", name)
		}
	}
	for name := range listed {
		if !actual[name] {
			t.Errorf("таблица документа называет RPC %q, которого в InstanceService нет", name)
		}
	}
}
