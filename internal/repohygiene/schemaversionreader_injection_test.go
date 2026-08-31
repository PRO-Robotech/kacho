// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// schemaversionreader_injection_test.go — доказательство, что соседний гейт
// СПОСОБЕН упасть и СПОСОБЕН смолчать.
//
// ─────────────────────────────────────────────────────────────────────────────
// ТРИ ПРОГОНА, А НЕ ДВА (testing.md §«Гейт на класс», п. 2в)
//
//	контроль          дерево цело — молчат ОБА: и новый гейт, и существующий;
//	инъекция НОВОГО   у сервиса СНЯТА провязка читателя, набор миграций на месте —
//	                  краснеет ТОЛЬКО новый;
//	инъекция СТАРОГО  у миграции СНЯТО объявление точки невозврата — краснеет
//	                  ТОЛЬКО существующий (`schemarollbackform`).
//
// Третий прогон обязателен: без него молчание существующего контроля на втором
// прогоне неотличимо от молчания МЁРТВОГО контроля.
//
// Инъекция нового снимает НОВОЕ свойство у элемента, чьё СТАРОЕ на месте:
// сервис остаётся сервисом, набор миграций остаётся набором — исчезает ровно
// провязка. Форма «завести ещё один элемент» здесь была бы негодной: новый
// сервис нарушает всё, что от сервисов требуется, и красное пришло бы от соседа.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАКОННЫЙ БЛИЗНЕЦ — БЛИЖНИЙ, А НЕ ПРОИЗВОЛЬНЫЙ
//
// Близнецом служит сервис БЕЗ встроенного набора миграций, но с композиционным
// корнем: он отличается от нарушителя ровно одним признаком — тем самым, по
// которому гейт отбирает субъектов. Произвольный близнец доказывал бы лишь, что
// гейт не краснеет на чём попало.
package repohygiene

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/schemaguard"
)

// wiredRoot — исходник корня, провязавшего читателя. Импорт настоящий: фикстура
// не вправе быть снисходительнее того, что судит дерево.
const wiredRoot = `package main

import (
	"github.com/PRO-Robotech/kacho/pkg/observability/health"
	"github.com/PRO-Robotech/kacho/pkg/schemaguard"
)

func buildReadinessCheckers() []health.Checker { return nil }
`

// unwiredRoot — тот же корень БЕЗ провязки. Отличие ровно одно.
const unwiredRoot = `package main

import "github.com/PRO-Robotech/kacho/pkg/observability/health"

func buildReadinessCheckers() []health.Checker { return nil }
`

func TestSchemaVersionReaderInjection_MissingWiringIsFoundAndTwinIsSilent(t *testing.T) {
	// ── прогон 1: КОНТРОЛЬ ────────────────────────────────────────────────
	//
	// Два сервиса: один с набором миграций и провязкой, второй БЕЗ набора и
	// без провязки — законный близнец.
	withMigrations := []string{"vpc"}
	roots := []schemaReaderSource{
		{Service: "vpc", Rel: "services/vpc/cmd/vpc/main.go", Body: wiredRoot},
		{Service: "gateway-ish", Rel: "services/gateway-ish/cmd/x/main.go", Body: unwiredRoot},
	}
	census, missing := findServicesMissingSchemaReader(withMigrations, roots)
	t.Logf("контроль: %s", census)
	if len(missing) != 0 {
		t.Fatalf("контроль: на целом входе найдено %v — фон непуст, и покраснение инъекции "+
			"не будет доказательством", missing)
	}
	if census.WithMigrations != 1 || census.Wired != 1 {
		t.Fatalf("контроль: перепись не сошлась (%s) — гейт судит не тот вход, что подан", census)
	}

	// ── законный близнец: сервис БЕЗ набора миграций субъектом не является ─
	//
	// Он уже в контроле выше и в находки не попал. Утверждается это отдельно,
	// иначе молчание близнеца неотличимо от того, что гейт вовсе не смотрел.
	for _, m := range missing {
		if m == "gateway-ish" {
			t.Errorf("сервис БЕЗ встроенного набора миграций объявлен нарушителем — гейт " +
				"считает субъектом всякий корень, и первый же ложный срабат его отключит")
		}
	}

	// ── прогон 2: ИНЪЕКЦИЯ НОВОГО СВОЙСТВА ────────────────────────────────
	//
	// Провязка снята. Набор миграций и сам корень на месте: исчезает ровно то,
	// чего гейт требует.
	injected := []schemaReaderSource{
		{Service: "vpc", Rel: "services/vpc/cmd/vpc/main.go", Body: unwiredRoot},
		{Service: "gateway-ish", Rel: "services/gateway-ish/cmd/x/main.go", Body: unwiredRoot},
	}
	censusInj, missingInj := findServicesMissingSchemaReader(withMigrations, injected)
	t.Logf("инъекция нового: %s → %v", censusInj, missingInj)
	if len(missingInj) != 1 || missingInj[0] != "vpc" {
		t.Fatalf("снятие провязки НЕ дало находки по имени сервиса: %v — гейт неспособен упасть "+
			"на предмете, ради которого заведён", missingInj)
	}
	text := schemaReaderFindingText(missingInj, censusInj)
	for _, want := range []string{"vpc", schemaGuardPackage, "ГОТОВЫМ"} {
		if !strings.Contains(text, want) {
			t.Errorf("текст находки не называет %q — читателю некуда идти:\n%s", want, text)
		}
	}

	// ── обход пуст — вердикт беспредметен, а не зелён ─────────────────────
	emptyCensus, emptyMissing := findServicesMissingSchemaReader(nil, roots)
	if len(emptyMissing) != 0 || emptyCensus.WithMigrations != 0 {
		t.Fatalf("на пустом наборе субъектов ядро дало %v (%s) — ожидались ноль находок "+
			"и ноль субъектов, чтобы ЯДРО ГЕЙТА могло отличить их друг от друга",
			emptyMissing, emptyCensus)
	}
}

// TestSchemaVersionReaderInjection_ExistingControlStillReds — ПРОГОН 3.
//
// Существующий контроль — распознаватель точки невозврата (`schemarollbackform`,
// задача #1690), которым новый гейт и питается. Инъекция снимает объявление у
// миграции: старый контроль обязан покраснеть, новый — промолчать.
//
// Прогон обязателен именно здесь: после того как токен и разбор стали ОБЩИМИ
// (`pkg/schemaguard`), молчание старого контроля можно было бы получить не
// только исправностью дерева, но и мёртвым распознавателем.
func TestSchemaVersionReaderInjection_ExistingControlStillReds(t *testing.T) {
	marked := "-- +goose Up\n" + schemaguard.PointOfNoReturnMarker +
		" снимается колонка legacy_id\nALTER TABLE a DROP COLUMN legacy_id;\n"
	unmarked := "-- +goose Up\nALTER TABLE a DROP COLUMN legacy_id;\n"

	// ── контроль: объявление на месте — существующий контроль молчит ──────
	if !hasPointOfNoReturnMarker(marked) {
		t.Fatalf("контроль: объявленная точка невозврата НЕ распознана — существующий контроль " +
			"мёртв, и его молчание ничего не доказывает")
	}

	// ── инъекция СТАРОГО свойства: объявление снято ───────────────────────
	if hasPointOfNoReturnMarker(unmarked) {
		t.Errorf("снятие объявления НЕ замечено распознавателем — существующий контроль " +
			"неспособен упасть")
	}

	// ── и НОВЫЙ гейт на этой инъекции молчит: предметы разведены ──────────
	//
	// Провязка читателя от объявления в миграции не зависит: гейт судит корень.
	_, missing := findServicesMissingSchemaReader([]string{"vpc"}, []schemaReaderSource{
		{Service: "vpc", Rel: "services/vpc/cmd/vpc/main.go", Body: wiredRoot},
	})
	if len(missing) != 0 {
		t.Errorf("новый гейт покраснел на инъекции ЧУЖОГО предмета (%v) — красное приходит "+
			"от соседа, и вердикты перестают быть прослеживаемыми", missing)
	}
	t.Logf("инъекция старого: существующий контроль краснеет, новый молчит — предметы разведены")
}
