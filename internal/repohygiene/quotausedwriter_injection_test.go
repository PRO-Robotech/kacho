// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// quotausedwriter_injection_test.go — доказательство, что гейт потребления квоты
// СПОСОБЕН упасть, и что он падает на существе, а не на форме.
//
// Гейт, чья способность краснеть не доказана, неотличим от гейта, который ничего
// не проверяет: оба молчат на чистом дереве. Здесь пара — внесённый дефект обязан
// быть НАЗВАН, законный близнец той же формы обязан остаться незамеченным. Без
// второй половины гейт ловил бы «в файле упомянута таблица учёта», и первый же
// ложный срабат его отключил бы.
//
// Живая инъекция в дерево тоже прогонялась при заведении гейта: временный файл с
// обходом дал красное с координатой, он же, переписанный в законную форму
// материализации, — зелёное. Здесь та же пара закреплена постоянно, чтобы
// доказательство не осталось в чужой памяти.
package repohygiene

import "testing"

func TestQuotaUsedWriterGate_FailsOnBypassAndIsSilentOnItsLegalTwin(t *testing.T) {
	cases := []struct {
		name  string
		stmt  string
		found bool
	}{
		{
			name:  "материализация заводит строку с нулевым потреблением — законно",
			stmt:  `INSERT INTO project_resource_quotas (carrier_type, carrier_id, kind, used, limit_value) VALUES ('project', $1, $2, 0, $3)`,
			found: false,
		},
		{
			name:  "вставка сразу с потреблением — обход предиката со стороны вставки",
			stmt:  `INSERT INTO project_resource_quotas (carrier_type, carrier_id, kind, used, limit_value) VALUES ('project', $1, $2, 7, $3)`,
			found: true,
		},
		{
			name:  "потребление приезжает параметром — статически не судится",
			stmt:  `INSERT INTO project_resource_quotas (carrier_type, carrier_id, kind, used, limit_value) VALUES ('project', $1, $2, $3, $4)`,
			found: false,
		},
		{
			name:  "used вообще не назван — умолчание схемы, ноль",
			stmt:  `INSERT INTO project_resource_quotas (carrier_type, carrier_id, kind, limit_value) VALUES ('project', $1, $2, $3)`,
			found: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := quotaInsertSetsNonZeroUsed(tc.stmt); got != tc.found {
				t.Fatalf("quotaInsertSetsNonZeroUsed = %v, ожидалось %v\nоператор: %s",
					got, tc.found, tc.stmt)
			}
		})
	}
}

// TestQuotaUsedWriterGate_SelfExpiryHasTeeth — перечень послаблений самоистекает.
//
// Проверяется НЕ то, что список непуст, а то, что каждая его запись указывает на
// существующий файл: запись, которой больше нечего исключать, — находка, потому
// что её унаследует следующая слепая зона. Сам гейт это утверждает на живом
// дереве; здесь закреплено, что предикат существования вообще спрашивается — без
// этого «самоистечение» было бы словом в комментарии.
func TestQuotaUsedWriterGate_SelfExpiryHasTeeth(t *testing.T) {
	if len(quotaTriggerDefiningFiles) == 0 {
		t.Fatal("перечень определяющих файлов пуст: гейт разрешал бы писать used отовсюду")
	}
	for table, files := range quotaTriggerDefiningFiles {
		if len(files) == 0 {
			t.Errorf("у таблицы %q нет ни одного определяющего файла — тогда её нечем "+
				"списывать, и запись в перечне таблиц лишняя", table)
		}
	}
	for _, table := range quotaUsageTables {
		if _, ok := quotaTriggerDefiningFiles[table]; !ok {
			t.Errorf("таблица учёта %q объявлена, но её писатель не назван: гейт нашёл бы "+
				"находку в самом механизме списания", table)
		}
	}
}
