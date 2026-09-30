// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package declaredbreak

// ФОРМА СИМВОЛА находки на настоящем выводе buf (kacho#2911).
//
// ПРЕДМЕТ. Какой символ называет разрыв, решает код, а запись перечня пишет человек по
// шапке `proto/declared-breaks.yaml` — либо копирует из отчёта, где символ напечатан у
// каждого необъявленного разрыва. Разойдись три места — и сопоставление рассыплется
// молча: обе стороны останутся синтаксически верными.
//
// Форма: где сообщение называет объемлющий символ (`on message`, `on enum`,
// `on service`), символ — `<контейнер>.<предмет>`, а предмет — ПЕРВОЕ кавычечное
// вхождение до контейнера: номер у поля и значения перечисления, имя у RPC и
// зарезервированного имени, запись диапазона у зарезервированного диапазона. У
// обязательного поля buf пишет контейнер ПЕРВЫМ (`Message "M" had required field "N"`),
// и символ — тот же `M.N`. Где контейнера нет — первое кавычечное вхождение вида имени;
// у снятия файла — путь.
//
// Утверждение — ПОВЕДЕНИЕ, а не внутренний выбор: запись с ожидаемым символом прощает
// ровно эту находку, и отчёт о необъявленной находке печатает этот символ дословно.
//
// ВХОД НАСТОЯЩИЙ: обходятся ВСЕ фикстуры `testdata/*.jsonl` и `testdata/*/*.jsonl`, и
// каждая находка обязана стоять в таблице ниже. Находка вне таблицы — провал (фикстура
// принесла форму, о которой проба не знает), запись таблицы без находки — тоже провал
// (ожидание пережило свою фикстуру).

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// expectedSymbol — сообщение buf → символ находки. Ключ — сообщение ДОСЛОВНО: так
// ожидание не зависит от разбора, который оно проверяет.
var expectedSymbol = map[string]string{
	// testdata/buf-breaking-real.jsonl, testdata/buf-breaking-file-deleted.jsonl
	`Previously present field "10" with name "predefined_target" on message "SecurityGroupRule" was deleted.`: "SecurityGroupRule.10",
	`Previously present RPC "AddRoutes" on service "RouteTableService" was deleted.`:                          "RouteTableService.AddRoutes",
	`Previously present file "kacho/cloud/vpc/v1/internal_dataplane_service.proto" was deleted.`:              "kacho/cloud/vpc/v1/internal_dataplane_service.proto",

	// значения перечисления: имени значения buf не печатает, только номер.
	`Previously present enum value "3" on enum "Status" was deleted.`:                                              "Status.3",
	`Previously present enum value "4" on enum "Status" was deleted.`:                                              "Status.4",
	`Enum value "3" on enum "Status" changed name from "STOPPING" to "STOPPING_RETIRED".`:                          "Status.3",
	`Enum value "4" on enum "Status" changed name from "STOPPED" to "STOPPED_RETIRED".`:                            "Status.4",
	`Previously present reserved name "FAILED" on enum "Status" was deleted.`:                                      "Status.FAILED",
	`Previously present reserved name "DELETING" on enum "Status" was deleted.`:                                    "Status.DELETING",
	`Previously present reserved name "apply_state" on message "NetworkInterface" was deleted.`:                    "NetworkInterface.apply_state",
	`Previously present reserved range "[21]" on message "NetworkInterface" is missing values: [21] were removed.`: "NetworkInterface.[21]",

	// поля: номер, а не имя, — у переименования buf имени в описании поля не печатает.
	`Previously present field "2" with name "page_size" on message "ListNetworksRequest" was deleted.`:                                "ListNetworksRequest.2",
	`Previously present field "2" with name "page_size" on message "ListNetworkOperationsRequest" was deleted.`:                       "ListNetworkOperationsRequest.2",
	`Field "2" on message "ListNetworksRequest" changed name from "page_size" to "page_limit".`:                                       "ListNetworksRequest.2",
	`Field "3" on message "ListNetworksRequest" changed name from "page_token" to "page_cursor".`:                                     "ListNetworksRequest.3",
	`Field "2" with name "page_limit" on message "ListNetworksRequest" changed option "json_name" from "pageSize" to "pageLimit".`:    "ListNetworksRequest.2",
	`Field "3" with name "page_cursor" on message "ListNetworksRequest" changed option "json_name" from "pageToken" to "pageCursor".`: "ListNetworksRequest.3",

	// RPC: имя, а не прежний тип ответа, — тип общий у всех RPC, отвечающих операцией.
	`RPC "Create" on service "NetworkService" changed response type from "corelib.operation.Operation" to "kacho.cloud.vpc.v1.Network".`: "NetworkService.Create",
	`RPC "Update" on service "NetworkService" changed response type from "corelib.operation.Operation" to "kacho.cloud.vpc.v1.Network".`: "NetworkService.Update",

	// обязательное поле: контейнер buf пишет ПЕРВЫМ.
	`Previously present field "1" with name "a" on message "RequiredProbe" was deleted.`:                                                                                                  "RequiredProbe.1",
	`Previously present field "2" with name "b" on message "RequiredProbe" was deleted.`:                                                                                                  "RequiredProbe.2",
	`Message "RequiredProbe" had required field "1" deleted. Required fields must always be sent, so if one side does not know about the required field, this will result in a breakage.`: "RequiredProbe.1",
	`Message "RequiredProbe" had required field "2" deleted. Required fields must always be sent, so if one side does not know about the required field, this will result in a breakage.`: "RequiredProbe.2",

	// контейнера buf не печатает вовсе: различает такие разрывы только расходование записи.
	`Message option "no_standard_descriptor_accessor" changed from "false" to "true".`: "no_standard_descriptor_accessor",
}

func TestSymbolNamesTheSubjectInsideItsContainer(t *testing.T) {
	var files []string
	for _, pat := range []string{"testdata/*.jsonl", "testdata/*/*.jsonl"} {
		m, err := filepath.Glob(pat)
		if err != nil {
			t.Fatalf("обход фикстур: %v", err)
		}
		files = append(files, m...)
	}
	sort.Strings(files)

	seen := map[string]int{}
	var findings int
	for _, p := range files {
		f, err := os.Open(p) // #nosec G304 -- путь из обхода testdata
		if err != nil {
			t.Fatalf("фикстура %s не открыта: %v", p, err)
		}
		got, err := ParseFindings(f)
		_ = f.Close()
		if err != nil {
			t.Fatalf("фикстура %s не разобрана: %v", p, err)
		}
		for _, fd := range got {
			findings++
			want, ok := expectedSymbol[fd.Message]
			if !ok {
				t.Errorf("%s: форма сообщения вне таблицы ожиданий — проба о ней не знает: %s %q",
					p, fd.Type, fd.Message)
				continue
			}
			seen[fd.Message]++

			// Запись с этим символом прощает ровно эту находку.
			res := Adjudicate([]Finding{fd}, []Declaration{siblingDecl(fd.Type, fd.Path, want)})
			if !res.Clean() || res.Matched != 1 {
				t.Errorf("%s: %s — запись с символом %q не простила свою находку:\n%s",
					p, fd.Type, want, res.Report())
			}
			// Отчёт о необъявленной находке печатает символ дословно: запись берёт его оттуда.
			if rep := Adjudicate([]Finding{fd}, nil).Report(); !strings.Contains(rep, "symbol=\""+want+"\"") {
				t.Errorf("%s: %s — отчёт не называет символ %q, которым разрыв объявляется:\n%s",
					p, fd.Type, want, rep)
			}
		}
	}
	t.Logf("перепись: фикстур %d, находок %d, форм сообщения в таблице %d, из них встречено %d",
		len(files), findings, len(expectedSymbol), len(seen))
	if len(files) == 0 || findings == 0 {
		t.Fatal("проверка НЕ ИСПОЛНЯЛАСЬ: не прочитано ни одной фикстуры настоящего вывода buf")
	}
	for msg := range expectedSymbol {
		if seen[msg] == 0 {
			t.Errorf("ожидание пережило свою фикстуру — ни одна находка его не несёт: %q", msg)
		}
	}
}

// TestFindingWithoutNamedSubjectIsRefused — находка, чей предмет не назван, — отказ
// разбора (код «гейт не смог работать»), а не символ-контейнер и не пустой символ.
// Символом-контейнером запись прощала бы любой разрыв внутри него; пустой символ не
// сопоставим ни с какой записью, и разрыв нельзя было бы объявить при любом входе.
func TestFindingWithoutNamedSubjectIsRefused(t *testing.T) {
	for name, in := range map[string]string{
		"контейнер назван, предмета до него нет": `{"path":"a.proto","type":"FIELD_NO_DELETE",` +
			`"message":"Previously present field on message \"Role\" was deleted."}`,
		"ни одного имени в кавычках": `{"path":"a.proto","type":"RPC_NO_DELETE",` +
			`"message":"Previously present RPC was deleted."}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFindings(strings.NewReader(in + "\n"))
			if err == nil {
				t.Fatal("находка без названного предмета разобрана — символом стал бы контейнер либо пустота")
			}
			if !strings.Contains(err.Error(), "предмет") {
				t.Errorf("причина не названа: %v", err)
			}
		})
	}

	// Положительный контроль: та же форма с предметом разбирается.
	got, err := ParseFindings(strings.NewReader(`{"path":"a.proto","type":"FIELD_NO_DELETE",` +
		`"message":"Previously present field \"1\" with name \"id\" on message \"Role\" was deleted."}` + "\n"))
	if err != nil || len(got) != 1 {
		t.Fatalf("находка с предметом отвергнута: %v %+v", err, got)
	}
}
