// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// Инъекция гейта «условие каталога несёт КАЖДЫЙ писатель зеркала» — В ОБЕ СТОРОНЫ.
//
// Гейт обходит дерево, поэтому его признак воспроизводится здесь над СИНТЕТИЧЕСКИМ
// входом: правкой настоящего дерева инъекция не ставится намеренно — она рвала бы
// чужие прогоны в общей рабочей копии.
//
// Осей ВОСЕМЬ, и по каждой прогон не один: контроль · инъекция проверяемого ·
// законный близнец. Без третьего молчание проверки неотличимо от её смерти.

// injWrites — плоский перечень находок распознавателя (для утверждений).
func injWrites(t *testing.T, src string) []mirrorWrite {
	t.Helper()
	writes, _, err := mirrorWritesIn("zz_injection.go", src)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	return writes
}

// srcReferenceBare — эталонная полоса БЕЗ условия каталога (состояние дерева на
// сегодня: условие ещё не написано, его пишет своя задача).
const srcReferenceBare = `package resource_mirror

func UpsertTx(ctx context.Context, tx pgx.Tx, row Row) error {
	_, err := tx.Exec(ctx, ` + "`" + `INSERT INTO kaname.resource_mirror
	   (object_type, object_id) VALUES ($1, $2)` + "`" + `, row.ObjectType, row.ObjectID)
	return err
}
`

// srcReferenceWithCondition — та же полоса ПОСЛЕ того, как условие каталога
// приехало: вставка спрашивает каталог в том же операторе.
const srcReferenceWithCondition = `package resource_mirror

func UpsertTx(ctx context.Context, tx pgx.Tx, row Row) error {
	_, err := tx.Exec(ctx, ` + "`" + `INSERT INTO kaname.resource_mirror
	   (object_type, object_id)
	 SELECT $1, $2
	  WHERE EXISTS (SELECT 1 FROM kaname.catalog_resource c
	                 WHERE c.dotted = $1 AND c.live)` + "`" + `, row.ObjectType, row.ObjectID)
	return err
}
`

// srcSecondWriterBare — второй писатель без условия (посевщик / страж).
const srcSecondWriterBare = `package pg

func (a *BackfillAdapter) SeedSmokeMirrorObject(ctx context.Context, objectType, objectID string) error {
	_, err := a.pool.Exec(ctx, ` + "`" + `INSERT INTO kaname.resource_mirror
	   (object_type, object_id) VALUES ($1, $2)` + "`" + `, objectType, objectID)
	return err
}
`

// srcSecondWriterWithCondition — тот же писатель, условие доехало.
const srcSecondWriterWithCondition = `package pg

func (a *BackfillAdapter) SeedSmokeMirrorObject(ctx context.Context, objectType, objectID string) error {
	_, err := a.pool.Exec(ctx, ` + "`" + `INSERT INTO kaname.resource_mirror
	   (object_type, object_id)
	 SELECT $1, $2
	  WHERE EXISTS (SELECT 1 FROM kaname.catalog_resource c
	                 WHERE c.dotted = $1 AND c.live)` + "`" + `, objectType, objectID)
	return err
}
`

const (
	refPath    = "services/iam/internal/repo/kaname/pg/resource_mirror/emitter.go"
	secondPath = "services/iam/internal/repo/kaname/pg/backfill_adapter.go"
)

// taggedWrites — записи распознавателя, приписанные пути.
func taggedWrites(t *testing.T, path, src string) []mirrorWrite {
	t.Helper()
	out := injWrites(t, src)
	for i := range out {
		out[i].File = path
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// ОСЬ A. РАСПОЗНАВАТЕЛЬ: запись против чтения, узел против текста.

// TestMirrorCondition_InjectionRecognisesTheWrite — оператор вставки в зеркало
// НАХОДИТСЯ и приписывается функции. Без этого гейт вакуумен.
func TestMirrorCondition_InjectionRecognisesTheWrite(t *testing.T) {
	got := injWrites(t, srcReferenceBare)
	if len(got) != 1 {
		t.Fatalf("признак нашёл %d операторов вставки, ожидалась 1 — распознаватель "+
			"не способен увидеть предмет, и его зелёный на дереве ничего не значит: %+v", len(got), got)
	}
	if got[0].Func != "UpsertTx" {
		t.Errorf("находка не приписана функции: %q — читатель пойдёт искать координату и не найдёт", got[0].Func)
	}
	if !got[0].Introduces {
		t.Errorf("вставка не помечена как ВВОДЯЩАЯ строку — предмет условия у неё есть")
	}
	if got[0].Verb != "INSERT INTO "+resourceMirrorTable {
		t.Errorf("глагол записи назван неверно: %q — перепись по глаголам печатала бы неправду", got[0].Verb)
	}
	if len(got[0].Catalog) != 0 {
		t.Errorf("у оператора без условия найден каталог %v — распознаватель выдумывает условие", got[0].Catalog)
	}
}

// TestMirrorCondition_InjectionSeesTheCondition — условие каталога В ТОМ ЖЕ
// операторе распознаётся и называется таблицей.
func TestMirrorCondition_InjectionSeesTheCondition(t *testing.T) {
	got := injWrites(t, srcReferenceWithCondition)
	if len(got) != 1 {
		t.Fatalf("операторов вставки %d, ожидалась 1: %+v", len(got), got)
	}
	if strings.Join(got[0].Catalog, ",") != "kaname.catalog_resource" {
		t.Fatalf("условие каталога НЕ распознано (%v) — гейт не отличит писателя с условием "+
			"от писателя без него, то есть не сможет покраснеть никогда", got[0].Catalog)
	}
}

// TestMirrorCondition_LegitimateTwin_ReadIsNotAWrite — чтение зеркала записью НЕ
// является: читателей у таблицы много, и все обязаны остаться законными.
func TestMirrorCondition_LegitimateTwin_ReadIsNotAWrite(t *testing.T) {
	src := `package relverdict

func listObjects(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Query(ctx, ` + "`" + `SELECT object_type, object_id FROM kaname.resource_mirror
	  WHERE parent_project_id = $1` + "`" + `)
	return err
}
`
	if got := injWrites(t, src); len(got) != 0 {
		t.Errorf("чтение зачтено записью: %+v — гейт краснел бы на каждом читателе и был бы снят первым же", got)
	}
}

// TestMirrorCondition_LegitimateTwin_CommentIsNotAStatement — имя таблицы в
// КОММЕНТАРИИ (в том числе в комментарии, объясняющем эту самую защиту) записью
// не является. Гейт по подстроке краснел бы на собственном объяснении.
func TestMirrorCondition_LegitimateTwin_CommentIsNotAStatement(t *testing.T) {
	src := `package pg

// Здесь НЕ делается INSERT INTO kaname.resource_mirror — строку заводит
// производитель, и условие по kaname.catalog_resource стоит у него.
func onlyProse(ctx context.Context) error { return nil }
`
	if got := injWrites(t, src); len(got) != 0 {
		t.Errorf("комментарий зачтён оператором: %+v — гейт краснеет на собственном объяснении", got)
	}
}

// TestMirrorCondition_CommentedCatalogIsNotACondition — упоминание каталога в
// комментарии РЯДОМ с оператором условием не является: судится литерал, а не файл.
func TestMirrorCondition_CommentedCatalogIsNotACondition(t *testing.T) {
	src := `package pg

func sneaky(ctx context.Context, tx pgx.Tx) error {
	// условие по kaname.catalog_resource здесь ПОДРАЗУМЕВАЕТСЯ вызывающим
	_, err := tx.Exec(ctx, ` + "`" + `INSERT INTO kaname.resource_mirror (object_type, object_id)
	 VALUES ($1, $2)` + "`" + `)
	return err
}
`
	got := injWrites(t, src)
	if len(got) != 1 {
		t.Fatalf("операторов %d, ожидался 1: %+v", len(got), got)
	}
	if len(got[0].Catalog) != 0 {
		t.Errorf("условие зачтено по КОММЕНТАРИЮ (%v) — писателю довольно было бы пообещать "+
			"сверку словами, и гейт молчал бы", got[0].Catalog)
	}
}

// TestMirrorCondition_UpdateDoesNotIntroduceARow — правка существующей строки
// нового типа не вводит, и предмета у условия там нет. Это законный близнец:
// пометить её нарушением значило бы требовать сверки там, где сверять нечего.
func TestMirrorCondition_UpdateDoesNotIntroduceARow(t *testing.T) {
	src := `package pg

func bump(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, ` + "`" + `UPDATE kaname.resource_mirror SET source_version = $1
	  WHERE object_type = $2` + "`" + `)
	return err
}
`
	got := injWrites(t, src)
	if len(got) != 1 {
		t.Fatalf("операторов %d, ожидался 1 (правка — тоже запись, её надо ВИДЕТЬ): %+v", len(got), got)
	}
	if got[0].Introduces {
		t.Errorf("правка помечена как вводящая строку — гейт потребовал бы условия там, где нового типа не появляется")
	}
	if got[0].Verb != "UPDATE "+resourceMirrorTable {
		t.Errorf("глагол записи назван неверно: %q", got[0].Verb)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ОСЬ B. СВЕРКА ПОЛОС МЕЖДУ СОБОЙ.

// TestMirrorCondition_ControlNoConditionAnywhere — состояние дерева на сегодня:
// условия нет НИ У КОГО. Находок ноль — и это не послабление, а сверка полос:
// гейт спрашивает «решал ли кто-нибудь, что они различаются», а не «каким
// свойство должно быть».
func TestMirrorCondition_ControlNoConditionAnywhere(t *testing.T) {
	writes := append(taggedWrites(t, refPath, srcReferenceBare),
		taggedWrites(t, secondPath, srcSecondWriterBare)...)
	rep := mirrorConditionReport(writes, nil)
	if len(rep.Findings) != 0 {
		t.Fatalf("находки на дереве, где условия нет ни у кого: %v — гейт краснеет на "+
			"исправном, и его отключат первым же", rep.Findings)
	}
	if rep.Lanes != 2 {
		t.Errorf("полос насчитано %d, ожидалось 2 — перепись обязана печатать ОБА числа", rep.Lanes)
	}
	if len(rep.Required) != 0 {
		t.Errorf("требование выведено из ниоткуда: %v", rep.Required)
	}
}

// TestMirrorCondition_InjectionRedWhenReferenceGainsTheCondition — НЕСУЩАЯ ось.
// Условие приезжает в эталонную полосу — и гейт немедленно краснеет на втором
// писателе, называя его файл, функцию и недостающую таблицу. Ровно этого не
// случилось бы, останься инвариант свойством одной полосы.
func TestMirrorCondition_InjectionRedWhenReferenceGainsTheCondition(t *testing.T) {
	writes := append(taggedWrites(t, refPath, srcReferenceWithCondition),
		taggedWrites(t, secondPath, srcSecondWriterBare)...)
	rep := mirrorConditionReport(writes, nil)
	if len(rep.Findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1 — гейт НЕ ловит писателя, обошедшего условие: %v",
			len(rep.Findings), rep.Findings)
	}
	f := rep.Findings[0]
	for _, want := range []string{secondPath, "SeedSmokeMirrorObject", "kaname.catalog_resource"} {
		if !strings.Contains(f, want) {
			t.Errorf("находка не называет %q: %q — читатель пойдёт искать координату и не найдёт", want, f)
		}
	}
	if rep.Carriers != 1 {
		t.Errorf("несут условие %d, ожидался 1 — одно число скрывает ровно тот случай, ради которого гейт заведён", rep.Carriers)
	}
}

// TestMirrorCondition_LegitimateTwin_BothCarryTheCondition — оба писателя несут
// условие → МОЛЧАНИЕ. Без этого прогона гейт ловил бы форму, а не существо.
func TestMirrorCondition_LegitimateTwin_BothCarryTheCondition(t *testing.T) {
	writes := append(taggedWrites(t, refPath, srcReferenceWithCondition),
		taggedWrites(t, secondPath, srcSecondWriterWithCondition)...)
	rep := mirrorConditionReport(writes, nil)
	if len(rep.Findings) != 0 {
		t.Fatalf("находки при обоих несущих условие: %v — гейт не отличает существо от формы", rep.Findings)
	}
	if rep.Carriers != 2 || rep.Lanes != 2 {
		t.Errorf("перепись врёт: полос %d, несут %d, ожидалось 2 и 2", rep.Lanes, rep.Carriers)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ОСЬ C. ВЕДОМОСТЬ ИСКЛЮЧЕНИЙ ИСТЕКАЕТ САМА.

// TestMirrorCondition_ExemptionSilencesItsOwnWriter — названное исключение
// гасит находку по СВОЕМУ писателю и ни по какому другому.
func TestMirrorCondition_ExemptionSilencesItsOwnWriter(t *testing.T) {
	writes := append(taggedWrites(t, refPath, srcReferenceWithCondition),
		taggedWrites(t, secondPath, srcSecondWriterBare)...)
	led := map[string]string{
		secondPath + "::BackfillAdapter.SeedSmokeMirrorObject": "причина: синтетический объект дымовой пробы",
	}
	rep := mirrorConditionReport(writes, led)
	if len(rep.Findings) != 0 {
		t.Fatalf("исключение не сработало: %v", rep.Findings)
	}
	if rep.Exempt != 1 {
		t.Errorf("исключений зачтено %d, ожидалось 1", rep.Exempt)
	}
}

// TestMirrorCondition_StaleExemptionIsAFinding — запись, которой больше нечего
// исключать (писатель условие получил), — НАХОДКА. Послабление обязано истекать
// само, иначе оно переживает свой предмет и остаётся слепой зоной.
func TestMirrorCondition_StaleExemptionIsAFinding(t *testing.T) {
	writes := append(taggedWrites(t, refPath, srcReferenceWithCondition),
		taggedWrites(t, secondPath, srcSecondWriterWithCondition)...)
	led := map[string]string{
		secondPath + "::BackfillAdapter.SeedSmokeMirrorObject": "причина: синтетический объект дымовой пробы",
	}
	rep := mirrorConditionReport(writes, led)
	if len(rep.Stale) != 1 {
		t.Fatalf("протухшее исключение НЕ найдено (%v) — ведомость не истекает сама и переживёт свой предмет", rep.Stale)
	}
	if !strings.Contains(rep.Stale[0], "SeedSmokeMirrorObject") {
		t.Errorf("находка не называет запись: %q", rep.Stale[0])
	}
}

// TestMirrorCondition_ExemptionForAVanishedWriterIsAFinding — писателя больше
// нет в дереве, а запись о нём осталась: тоже находка.
func TestMirrorCondition_ExemptionForAVanishedWriterIsAFinding(t *testing.T) {
	writes := taggedWrites(t, refPath, srcReferenceWithCondition)
	led := map[string]string{
		"services/iam/internal/repo/kaname/pg/gone.go::Gone.Write": "причина: писатель, которого сняли",
	}
	rep := mirrorConditionReport(writes, led)
	if len(rep.Stale) != 1 {
		t.Fatalf("запись об исчезнувшем писателе НЕ найдена (%v)", rep.Stale)
	}
}

// TestMirrorCondition_EmptyLedgerIsTheGoal — ПУСТАЯ ведомость проходит. Пустая
// ведомость есть цель, ради которой ведомость заведена; отказ на ней толкал бы
// держать запись ради зелёного.
func TestMirrorCondition_EmptyLedgerIsTheGoal(t *testing.T) {
	writes := append(taggedWrites(t, refPath, srcReferenceWithCondition),
		taggedWrites(t, secondPath, srcSecondWriterWithCondition)...)
	rep := mirrorConditionReport(writes, map[string]string{})
	if len(rep.Findings) != 0 || len(rep.Stale) != 0 {
		t.Fatalf("пустая ведомость объявлена поломкой: %v / %v", rep.Findings, rep.Stale)
	}
}

// TestMirrorCondition_ExemptionWithoutAReasonIsAFinding — исключение без причины
// исключением не является: следующий читатель снимет его как непонятное либо
// оставит навсегда, не зная предмета.
func TestMirrorCondition_ExemptionWithoutAReasonIsAFinding(t *testing.T) {
	writes := append(taggedWrites(t, refPath, srcReferenceWithCondition),
		taggedWrites(t, secondPath, srcSecondWriterBare)...)
	led := map[string]string{secondPath + "::BackfillAdapter.SeedSmokeMirrorObject": "  "}
	rep := mirrorConditionReport(writes, led)
	if len(rep.Stale) != 1 {
		t.Fatalf("исключение без причины принято молча: %v", rep.Stale)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ОСЬ D. ПРЕДПОСЫЛКИ ГЕЙТА.

// TestMirrorCondition_NoReferenceLaneIsARefusal — эталонной полосы в наборе нет
// (каталог переехал, константу не поправили) → ОТКАЗ, а не «нарушений нет».
// Сверять «тем же условием» не с чем, и молчание здесь означало бы, что гейт
// умер вместе с координатой.
func TestMirrorCondition_NoReferenceLaneIsARefusal(t *testing.T) {
	writes := taggedWrites(t, secondPath, srcSecondWriterBare)
	rep := mirrorConditionReport(writes, nil)
	if !rep.ReferenceMissing {
		t.Fatal("отсутствие эталонной полосы не объявлено отказом — гейт молчит после переезда каталога")
	}
}

// TestMirrorCondition_WriterOutsideIAMIsAFinding — вводящий писатель ВНЕ
// services/iam: зеркало — таблица iam, и запись в неё из чужого сервиса означает
// общую БД (ban #8). Ось вооружена СЕГОДНЯ, до всякого условия.
func TestMirrorCondition_WriterOutsideIAMIsAFinding(t *testing.T) {
	writes := append(taggedWrites(t, refPath, srcReferenceBare),
		taggedWrites(t, "services/vpc/internal/repo/mirror.go", srcSecondWriterBare)...)
	rep := mirrorConditionReport(writes, nil)
	if len(rep.Findings) != 1 || !strings.Contains(rep.Findings[0], "services/vpc/") {
		t.Fatalf("чужой сервис, пишущий зеркало iam, не найден: %v", rep.Findings)
	}
}

// TestMirrorCondition_MentionsCountedForThePremise — перепись предпосылки:
// исходник без единого упоминания таблицы даёт ноль. «Ноль находок» обязано быть
// отличимо от «ноль прочитанного».
func TestMirrorCondition_MentionsCountedForThePremise(t *testing.T) {
	_, mentions, err := mirrorWritesIn("zz.go", "package p\n\nfunc f() {}\n")
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if mentions != 0 {
		t.Errorf("упоминаний %d на исходнике без таблицы", mentions)
	}
	_, mentions, err = mirrorWritesIn("zz.go", srcReferenceBare)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if mentions != 1 {
		t.Errorf("упоминаний %d, ожидалось 1 — счётчик предпосылки не работает", mentions)
	}
}
