// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «волна держит запас под потолком аккаунтов»
// СПОСОБЕН упасть — и что падает он на существе, а не на форме.
//
// Инъекция идёт в обе стороны по КАЖДОМУ распознавателю: гейт, краснеющий на всякой
// волне, ничего не измеряет; гейт, молчащий на всякой, — тем более.
//
//	волна с пиком «потолок−1»                       → краснеет, называя координату;
//	та же волна, разведённая по времени (пик −2)     → МОЛЧИТ, и перепись растёт;
//	POST без захвата id и без утверждённого отказа   → краснеет («не отнесено»);
//	POST, чей синхронный отказ утверждён             → молчит, счётчик не двигается;
//	POST, чья ОПЕРАЦИЯ утверждена упавшей            → молчит, счётчик не двигается;
//	удаление, чей отказ утверждён                    → НЕ возврат, счётчик держится;
//	создание без удаления                            → краснеет («не возвращено»);
//	те же слова в КОММЕНТАРИИ                        → не считаются ни за что.
//
// Отдельно проверяется чтение потолка: величина берётся у секции `+goose Up`, а не
// у отката и не у комментария. Без этого гейт стерёг бы запас под числом, которого
// в действующем каталоге нет — и в этом каталоге такой файл уже лежит (`353001`
// несёт посев ИМЕННО в откате).
//
// Обе половины гоняют ТЕ ЖЕ функции (`scanWaveAccountQuota`, `waveLimitsInMigration`),
// что и обход дерева.
package repohygiene

import (
	"encoding/json"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Синтетические шаги. Каждый — настоящая форма, произведённая генератором набора
// (`services/iam/tests/newman/scripts/gen.py`), а не выдумка: инъекция обязана
// доказывать что-то про предмет, а не про свою фикстуру.
// ─────────────────────────────────────────────────────────────────────────────

// waveAuthPre — пре-скрипт шага под предъявителем. Строка `const __t = …` —
// исполняемая; комментарий над ней генератор ставит тоже, и он здесь намеренно
// сохранён: гейт обязан читать код, а не его объяснение.
func waveAuthPre(env string) []string {
	return []string{
		"// per-step auth: bearer from env '" + env + "'",
		"const __t = pm.environment.get('" + env + "') || pm.variables.get('" + env + "') || '';",
		"if (__t) {",
		"  pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __t});",
		"}",
	}
}

// waveCaptureTest — тело `save_from_response('j.metadata && j.metadata.accountId', V)`.
func waveCaptureTest(v string) []string {
	return []string{
		"pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
		"pm.environment.unset('opId');",
		"try {",
		"  const j = pm.response.json();",
		"  const v = (j.id);",
		"  if (v !== undefined && v !== null) pm.environment.set('opId', String(v));",
		"} catch (e) {}",
		"try {",
		"  const j = pm.response.json();",
		"  const v = (j.metadata && j.metadata.accountId);",
		"  if (v !== undefined && v !== null) pm.environment.set('" + v + "', String(v));",
		"} catch (e) {}",
	}
}

func waveStepJSON(name, method, url string, pre, test []string) string {
	enc := func(ss []string) string {
		b, _ := json.Marshal(ss)
		return string(b)
	}
	nameJSON, _ := json.Marshal(name)
	return `{"name":` + string(nameJSON) +
		`,"request":{"method":"` + method + `","url":{"raw":"{{baseUrl}}` + url + `"}}` +
		`,"event":[{"listen":"prerequest","script":{"exec":` + enc(pre) + `}},` +
		`{"listen":"test","script":{"exec":` + enc(test) + `}}]}`
}

func waveCollectionJSON(stem string, steps ...string) waveCollection {
	return waveCollection{
		Stem: stem,
		Rel:  "services/iam/tests/newman/collections/" + stem + ".postman_collection.json",
		Body: []byte(`{"item":[{"name":"SYNTH — case","item":[` + strings.Join(steps, ",") + `]}]}`),
	}
}

// ─── готовые шаги ────────────────────────────────────────────────────────────

func waveCreateStep(name, v, env string) string {
	return waveStepJSON(name, "POST", "/iam/v1/accounts", waveAuthPre(env), waveCaptureTest(v))
}

func waveDeleteStep(name, v, env string) string {
	return waveStepJSON(name, "DELETE", "/iam/v1/accounts/{{"+v+"}}", waveAuthPre(env),
		[]string{"pm.test('teardown: removed or already gone', () => pm.expect([200,404]).to.include(pm.response.code));"})
}

// waveRefusedCreateStep — создание, чей СИНХРОННЫЙ отказ утверждён (400).
func waveRefusedCreateStep(name, env string) string {
	return waveStepJSON(name, "POST", "/iam/v1/accounts", waveAuthPre(env), []string{
		"pm.test('status 400', () => pm.expect(pm.response.code).to.eql(400));",
		"pm.test('grpc code 3 (INVALID_ARGUMENT)', () => pm.expect(pm.response.json().code).to.eql(3));",
	})
}

// waveOpErrorCreateSteps — создание, чья ОПЕРАЦИЯ утверждена упавшей: край отвечает
// 200, строки не остаётся. Форма из `IAM-ACC-CR-NEG-NAME-DUP`.
func waveOpErrorCreateSteps(name, env string) []string {
	return []string{
		waveStepJSON(name, "POST", "/iam/v1/accounts", waveAuthPre(env), []string{
			"pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
			"pm.environment.unset('opId');",
			"try {",
			"  const j = pm.response.json();",
			"  const v = (j.id);",
			"  if (v !== undefined && v !== null) pm.environment.set('opId', String(v));",
			"} catch (e) {}",
		}),
		waveStepJSON(name+" :: assert-op-error", "GET", "/operations/{{opId}}", waveAuthPre(env), []string{
			"const j = pm.response.json();",
			"pm.test('operation done', () => pm.expect(j.done).to.eql(true));",
			"pm.test('error code 6 (ALREADY_EXISTS)', () => pm.expect(j.error && j.error.code).to.eql(6));",
		}),
	}
}

// waveRefusedDeleteStep — удаление, чей отказ утверждён: аккаунт ОСТАЁТСЯ жить.
func waveRefusedDeleteStep(name, v, env string) string {
	return waveStepJSON(name, "DELETE", "/iam/v1/accounts/{{"+v+"}}", waveAuthPre(env), []string{
		"pm.test('status 400', () => pm.expect(pm.response.code).to.eql(400));",
	})
}

// waveUnattributedCreateStep — создание без захвата id и без утверждённого отказа.
// Со стороны выглядит безобидно: шаг «просто создал». Аккаунт при этом занимает
// место под потолком и не возвращается никогда, потому что адресовать его нечем.
func waveUnattributedCreateStep(name, env string) string {
	return waveStepJSON(name, "POST", "/iam/v1/accounts", waveAuthPre(env), []string{
		"pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
	})
}

// waveCommentOnlyCaptureStep — создание, у которого ЗАХВАТ стоит только в
// комментарии, а исполняемая часть его не делает. Шаг успешен (200), поэтому
// отказом он не прикрыт: гейт, читающий сырой текст, засчитает объяснение за код и
// объявит аккаунт учтённым — тогда как адресовать его нечем и убрать некому.
// Правильный исход — «не отнесено».
func waveCommentOnlyCaptureStep(name, env string) string {
	return waveStepJSON(name, "POST", "/iam/v1/accounts", waveAuthPre(env), []string{
		"// прежде здесь стоял захват:",
		"//   const v = (j.metadata && j.metadata.accountId);",
		"//   if (v !== undefined && v !== null) pm.environment.set('ghostAccId', String(v));",
		"pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
	})
}

// waveCommentOnlyOpErrorSteps — создание, чья «утверждённая ошибка операции» тоже
// живёт только в комментарии поллера. Тот же класс с другой стороны: гейт по сырому
// тексту решит, что строки не осталось, и не спросит про неё вовсе.
func waveCommentOnlyOpErrorSteps(name, env string) []string {
	return []string{
		waveStepJSON(name, "POST", "/iam/v1/accounts", waveAuthPre(env), []string{
			"pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
			"pm.environment.unset('opId');",
			"try {",
			"  const j = pm.response.json();",
			"  const v = (j.id);",
			"  if (v !== undefined && v !== null) pm.environment.set('opId', String(v));",
			"} catch (e) {}",
		}),
		waveStepJSON(name+" :: poll", "GET", "/operations/{{opId}}", waveAuthPre(env), []string{
			"const j = pm.response.json();",
			"// раньше тут утверждалось: pm.expect(j.error && j.error.code).to.eql(6)",
			"pm.test('operation done', () => pm.expect(j.done).to.eql(true));",
		}),
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. Пик: краснеет на «потолок−1», молчит на «потолок−2»
// ─────────────────────────────────────────────────────────────────────────────

const (
	synthCeiling  = 5
	synthBaseline = 2
)

func TestWaveAccountCeilingGateCutsBothWaysOnThePeak(t *testing.T) {
	// ДЕФЕКТ — второй аккаунт заводится, пока жив первый: пик 4 при потолке 5.
	overlapping := []waveCollection{
		waveCollectionJSON("synth-account",
			waveCreateStep("SYNTH-CR-LONG :: create", "longAccId", "jwtHumanCeremony"),
			waveCreateStep("SYNTH-BVA-MIN :: create", "bvaMinAccId", "jwtHumanCeremony"),
			waveDeleteStep("SYNTH-BVA-MIN :: teardown", "bvaMinAccId", "jwtHumanCeremonyStepUp"),
			waveDeleteStep("SYNTH-CR-LONG :: teardown", "longAccId", "jwtHumanCeremonyStepUp"),
		),
	}
	got, err := scanWaveAccountQuota(overlapping, synthBaseline)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if got.Peak != 4 {
		t.Fatalf("пик перекрывающейся волны = %d, ожидался 4 — распознаватель списания "+
			"или возврата не сработал на настоящей форме шага", got.Peak)
	}
	if got.Peak < synthCeiling-1 {
		t.Fatalf("пик %d не достигает порога %d — инъекция не воспроизводит дефект, и "+
			"её молчание ничего не доказывает", got.Peak, synthCeiling-1)
	}
	if !strings.Contains(got.PeakAt, "SYNTH-BVA-MIN :: create") ||
		!strings.Contains(got.PeakAt, "collections/synth-account") {
		t.Errorf("гейт нашёл пик, но НЕ НАЗВАЛ координату: %q — находка без координаты "+
			"не чинится", got.PeakAt)
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ — те же четыре шага, разведённые по времени: пик 3.
	sequential := []waveCollection{
		waveCollectionJSON("synth-account",
			waveCreateStep("SYNTH-CR-LONG :: create", "longAccId", "jwtHumanCeremony"),
			waveDeleteStep("SYNTH-CR-LONG :: teardown", "longAccId", "jwtHumanCeremonyStepUp"),
			waveCreateStep("SYNTH-BVA-MIN :: create", "bvaMinAccId", "jwtHumanCeremony"),
			waveDeleteStep("SYNTH-BVA-MIN :: teardown", "bvaMinAccId", "jwtHumanCeremonyStepUp"),
		),
	}
	ok, err := scanWaveAccountQuota(sequential, synthBaseline)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if ok.Peak >= synthCeiling-1 {
		t.Errorf("разведённая по времени волна даёт пик %d при потолке %d — гейт ловит "+
			"ФОРМУ (сколько создано), а не свойство (сколько живо одновременно)",
			ok.Peak, synthCeiling)
	}
	// Перепись обязана РАСТИ: молчание при нулевой переписи неотличимо от «не смотрел».
	if ok.Charges != 2 || ok.Releases != 2 || !ok.SawCharge {
		t.Errorf("законный близнец молчит, но перепись не выросла: списаний %d, "+
			"возвратов %d, распознаватель подтверждён=%v — такое молчание значит "+
			"«не прочитано»", ok.Charges, ok.Releases, ok.SawCharge)
	}
	if len(ok.LiveAtEnd) != 0 || len(ok.Unattributed) != 0 {
		t.Errorf("законный близнец даёт ложные находки: не возвращено %v, не отнесено %v",
			ok.LiveAtEnd, ok.Unattributed)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Отвергнутое создание счётчика НЕ двигает — иначе гейт краснел бы на негативах
// ─────────────────────────────────────────────────────────────────────────────

func TestWaveAccountCeilingGateDoesNotChargeRefusedCreates(t *testing.T) {
	steps := []string{
		waveCreateStep("SYNTH-CR :: create", "accId", "jwtHumanCeremony"),
		waveRefusedCreateStep("SYNTH-NEG-SYNC :: create-invalid", "jwtHumanCeremony"),
	}
	steps = append(steps, waveOpErrorCreateSteps("SYNTH-NEG-DUP :: create-dup", "jwtHumanCeremony")...)
	steps = append(steps,
		waveDeleteStep("SYNTH-CR :: teardown", "accId", "jwtHumanCeremonyStepUp"),
	)

	got, err := scanWaveAccountQuota([]waveCollection{waveCollectionJSON("synth-neg", steps...)}, synthBaseline)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if got.Charges != 1 {
		t.Errorf("списаний %d, ожидалось 1: отвергнутое создание (синхронно и через "+
			"ошибку операции) и комментарий не имеют права двигать счётчик — иначе "+
			"гейт краснеет на негативных кейсах, и его отключат первым же ложным "+
			"срабатыванием.\nход счётчика:\n%s", got.Charges, strings.Join(got.Ledger, "\n"))
	}
	if len(got.Unattributed) != 0 {
		t.Errorf("гейт не отнёс шаги, чей отказ утверждён: %v", got.Unattributed)
	}
	if !got.SawRefusal {
		t.Errorf("распознаватель утверждённого отказа не сработал на настоящей форме — " +
			"без него всякий негатив уезжает в «не отнесено»")
	}
	if got.Peak != synthBaseline+1 {
		t.Errorf("пик %d, ожидался %d", got.Peak, synthBaseline+1)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Не отнесённое создание — НАХОДКА, а не ноль
// ─────────────────────────────────────────────────────────────────────────────

func TestWaveAccountCeilingGateNamesUnattributedCreates(t *testing.T) {
	got, err := scanWaveAccountQuota([]waveCollection{
		waveCollectionJSON("synth-unattr",
			waveCreateStep("SYNTH-CR :: create", "accId", "jwtHumanCeremony"),
			waveUnattributedCreateStep("SYNTH-GHOST :: create", "jwtHumanCeremony"),
			waveDeleteStep("SYNTH-CR :: teardown", "accId", "jwtHumanCeremonyStepUp"),
		),
	}, synthBaseline)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(got.Unattributed) != 1 {
		t.Fatalf("не отнесённых создан: %d, ожидалось 1 — шаг, создающий аккаунт и не "+
			"захватывающий его id, невидим счётчику, и пик занижается ровно там, где "+
			"занижение опасно", len(got.Unattributed))
	}
	if !strings.Contains(got.Unattributed[0], "SYNTH-GHOST :: create") ||
		!strings.Contains(got.Unattributed[0], "collections/synth-unattr") {
		t.Errorf("находка без координаты: %q", got.Unattributed[0])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 3б. Гейт читает КОД, а не объяснение рядом с ним
// ─────────────────────────────────────────────────────────────────────────────

func TestWaveAccountCeilingGateReadsCodeNotComments(t *testing.T) {
	steps := []string{waveCommentOnlyCaptureStep("SYNTH-CMT-CAP :: create", "jwtHumanCeremony")}
	steps = append(steps, waveCommentOnlyOpErrorSteps("SYNTH-CMT-OPERR :: create", "jwtHumanCeremony")...)

	got, err := scanWaveAccountQuota([]waveCollection{waveCollectionJSON("synth-comments", steps...)}, synthBaseline)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if got.Charges != 0 {
		t.Errorf("списаний %d, ожидалось 0: захват, стоящий ТОЛЬКО в комментарии, "+
			"аккаунта не захватывает — засчитывать его значит удостоверять уборку, "+
			"которой в коде нет\n%s", got.Charges, strings.Join(got.Ledger, "\n"))
	}
	if len(got.Unattributed) != 2 {
		t.Fatalf("не отнесённых создан: %d, ожидалось 2 — оба шага создают аккаунт, и "+
			"ни у одного нет ни захвата, ни утверждённого отказа В КОДЕ; молчание "+
			"гейта здесь означало бы, что он прочитал комментарий вместо кода: %v",
			len(got.Unattributed), got.Unattributed)
	}
	for _, h := range got.Unattributed {
		if !strings.Contains(h, "collections/synth-comments") {
			t.Errorf("находка без координаты: %q", h)
		}
	}
}

// waveStaleAuthCommentStep — код называет ОДНОГО предъявителя, комментарий над ним
// — другого (так и выглядит шаг, у которого предъявителя поменяли, а объяснение
// оставили). Гейт обязан приписать списание тому, под кем шаг ИДЁТ.
func waveStaleAuthCommentStep(name, v, codeEnv, commentEnv string) string {
	pre := []string{
		"// per-step auth: bearer from env '" + commentEnv + "'",
		"const __t = pm.environment.get('" + codeEnv + "') || pm.variables.get('" + codeEnv + "') || '';",
		"if (__t) {",
		"  pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __t});",
		"}",
	}
	return waveStepJSON(name, "POST", "/iam/v1/accounts", pre, waveCaptureTest(v))
}

// ─────────────────────────────────────────────────────────────────────────────
// 3в. Плательщик — тот, под кем шаг ИДЁТ, а не тот, кого называет комментарий
// ─────────────────────────────────────────────────────────────────────────────

func TestWaveAccountCeilingGateAttributesTheChargeToTheRunningPrincipal(t *testing.T) {
	got, err := scanWaveAccountQuota([]waveCollection{
		waveCollectionJSON("synth-auth",
			waveStaleAuthCommentStep("SYNTH-AUTH :: create", "accId",
				"jwtHumanCeremony", "jwtAccountAdminA"),
			waveDeleteStep("SYNTH-AUTH :: teardown", "accId", "jwtHumanCeremonyStepUp"),
		),
	}, synthBaseline)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if got.Payers["jwtHumanCeremony"] != 1 || got.Payers["jwtAccountAdminA"] != 0 {
		t.Errorf("списание приписано %v — гейт прочитал комментарий вместо кода. "+
			"Комментарий переживает смену предъявителя, и тогда перепись плательщиков "+
			"называет того, под кем шаг больше не ходит", got.Payers)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 3г. Второй плательщик ВИДЕН переписи — на неё опирается проверка премиссы
// ─────────────────────────────────────────────────────────────────────────────

func TestWaveAccountCeilingScanSeesASecondPayer(t *testing.T) {
	got, err := scanWaveAccountQuota([]waveCollection{
		waveCollectionJSON("synth-two-payers",
			waveCreateStep("SYNTH-A :: create", "aAccId", "jwtHumanCeremony"),
			waveDeleteStep("SYNTH-A :: teardown", "aAccId", "jwtHumanCeremonyStepUp"),
			waveCreateStep("SYNTH-B :: create", "bAccId", "jwtSecondHuman"),
			waveDeleteStep("SYNTH-B :: teardown", "bAccId", "jwtSecondHuman"),
		),
	}, synthBaseline)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(got.Payers) != 2 {
		t.Fatalf("плательщиков распознано %d (%v), ожидалось 2 — гейт по дереву "+
			"проверяет свою премиссу («волна идёт под одним человеком») именно по "+
			"этой переписи; не увидев второго, он молча сложил бы чужие счётчики в "+
			"один и назвал бы завышенное число верным", len(got.Payers), got.Payers)
	}
	// И перепись обязана быть ВЕРНОЙ, а не просто непустой: возврат относится к
	// плательщику СПИСАНИЯ, поэтому уборка под поднятым уровнем плательщиком не
	// становится.
	if got.Payers["jwtHumanCeremony"] != 1 || got.Payers["jwtSecondHuman"] != 1 {
		t.Errorf("перепись плательщиков %v — уборка не имеет права попадать в "+
			"плательщики: место под потолком занимает тот, кто аккаунт СОЗДАЛ",
			got.Payers)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. Удаление, чей отказ утверждён, возвратом НЕ является
// ─────────────────────────────────────────────────────────────────────────────

func TestWaveAccountCeilingGateDoesNotReleaseOnRefusedDelete(t *testing.T) {
	got, err := scanWaveAccountQuota([]waveCollection{
		waveCollectionJSON("synth-restrict",
			waveCreateStep("SYNTH-RST :: seed", "rstAccId", "jwtHumanCeremony"),
			waveRefusedDeleteStep("SYNTH-RST :: delete-nonempty", "rstAccId", "jwtHumanCeremonyStepUp"),
			waveCreateStep("SYNTH-OTHER :: create", "otherAccId", "jwtHumanCeremony"),
			waveDeleteStep("SYNTH-OTHER :: teardown", "otherAccId", "jwtHumanCeremonyStepUp"),
			waveDeleteStep("SYNTH-RST :: teardown", "rstAccId", "jwtHumanCeremonyStepUp"),
		),
	}, synthBaseline)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if got.Releases != 2 {
		t.Errorf("возвратов %d, ожидалось 2: удаление, которому ОТКАЗАНО, места под "+
			"потолком не освобождает, и засчитывать его значит занижать пик\n%s",
			got.Releases, strings.Join(got.Ledger, "\n"))
	}
	if got.Peak != synthBaseline+2 {
		t.Errorf("пик %d, ожидался %d — отвергнутое удаление обязано ОСТАВИТЬ аккаунт "+
			"живым", got.Peak, synthBaseline+2)
	}
	if len(got.LiveAtEnd) != 0 {
		t.Errorf("уборка прошла, а гейт считает аккаунт живым: %v", got.LiveAtEnd)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. Созданное и не удалённое — НАХОДКА
// ─────────────────────────────────────────────────────────────────────────────

func TestWaveAccountCeilingGateNamesAccountsLeftAlive(t *testing.T) {
	got, err := scanWaveAccountQuota([]waveCollection{
		waveCollectionJSON("synth-leak",
			waveCreateStep("SYNTH-LEAK :: create", "leakAccId", "jwtHumanCeremony"),
		),
	}, synthBaseline)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(got.LiveAtEnd) != 1 || !strings.Contains(got.LiveAtEnd[0], "leakAccId") {
		t.Fatalf("не возвращённых к концу волны: %v, ожидался leakAccId — аккаунт, "+
			"который волна не убрала, есть единица под потолком, которую она больше "+
			"не вернёт", got.LiveAtEnd)
	}
	if !strings.Contains(got.LiveAtEnd[0], "jwtHumanCeremony") {
		t.Errorf("не назван плательщик: %q — без него непонятно, чей счётчик занят",
			got.LiveAtEnd[0])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 6. Пустой вход — не «ноль находок»
// ─────────────────────────────────────────────────────────────────────────────

func TestWaveAccountCeilingScanReportsEmptyInputAsUnread(t *testing.T) {
	got, err := scanWaveAccountQuota(nil, synthBaseline)
	if err != nil {
		t.Fatalf("разбор пустого входа: %v", err)
	}
	if got.Collections != 0 || got.Steps != 0 {
		t.Fatalf("пустой вход дал коллекций %d, шагов %d", got.Collections, got.Steps)
	}
	if got.SawCharge || got.SawRefusal {
		t.Errorf("на пустом входе распознаватели объявлены подтверждёнными "+
			"(списание=%v, отказ=%v) — тогда гейт не отличил бы «ноль находок» от "+
			"«ноль прочитанного»", got.SawCharge, got.SawRefusal)
	}
	if got.Peak != synthBaseline {
		t.Errorf("пик пустой волны %d, ожидалась база %d", got.Peak, synthBaseline)
	}

	// И на неразбираемой коллекции — ОТКАЗ, а не молчание.
	if _, err := scanWaveAccountQuota([]waveCollection{{
		Stem: "broken",
		Rel:  "services/iam/tests/newman/collections/broken.postman_collection.json",
		Body: []byte("{ это не json"),
	}}, synthBaseline); err == nil {
		t.Errorf("неразбираемая коллекция прошла молча — «файл не читается» не имеет " +
			"права стать «нулём находок»")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 7. Потолок берётся у секции Up, а не у отката и не у комментария
// ─────────────────────────────────────────────────────────────────────────────

func TestWaveAccountCeilingReadsTheUpSectionOnly(t *testing.T) {
	const kind = "iam.account"

	cases := []struct {
		name    string
		sql     string
		want    []waveLimitSeed
		mutates bool
	}{
		{
			name: "посев в Up — читается",
			sql: `-- +goose Up
INSERT INTO kacho_iam.limits (id, scope, scope_id, kind, limit_value) VALUES
    ('lim-00000000000000032', 'DEFAULT', '', 'iam.account', 5)
ON CONFLICT (id) DO NOTHING;
-- +goose Down
DELETE FROM kacho_iam.limits WHERE id = 'lim-00000000000000032';
`,
			want: []waveLimitSeed{{Scope: "DEFAULT", Value: 5}},
		},
		{
			// Форма из `353001`: величина живёт ИМЕННО в откате. Файл, прочитанный
			// целиком, объявил бы её действующей.
			name: "посев ТОЛЬКО в откате — не читается",
			sql: `-- +goose Up
DELETE FROM kacho_iam.limits WHERE kind = 'vpc.subnet.networkInterface';
-- +goose Down
INSERT INTO kacho_iam.limits (id, scope, scope_id, kind, limit_value) VALUES
    ('lim-00000000000000032', 'DEFAULT', '', 'iam.account', 64)
ON CONFLICT (id) DO NOTHING;
`,
			want: nil,
		},
		{
			name: "посев ТОЛЬКО в комментарии — не читается",
			sql: `-- +goose Up
-- Прежде здесь стояло:
--   INSERT INTO kacho_iam.limits (id, scope, scope_id, kind, limit_value) VALUES
--       ('lim-00000000000000032', 'DEFAULT', '', 'iam.account', 99);
SELECT 1;
`,
			want: nil,
		},
		{
			name: "правка каталога помимо посева — названа",
			sql: `-- +goose Up
UPDATE kacho_iam.limits SET limit_value = 12 WHERE kind = 'iam.account';
`,
			want:    nil,
			mutates: true,
		},
		{
			name: "чужой вид не засчитывается",
			sql: `-- +goose Up
INSERT INTO kacho_iam.limits (id, scope, scope_id, kind, limit_value) VALUES
    ('lim-00000000000000009', 'DEFAULT', '', 'iam.project', 16);
`,
			want: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, mutates, err := waveLimitsInMigration(c.sql, kind)
			if err != nil {
				t.Fatalf("разбор: %v", err)
			}
			if mutates != c.mutates {
				t.Errorf("правка каталога: получено %v, ожидалось %v", mutates, c.mutates)
			}
			if len(got) != len(c.want) {
				t.Fatalf("посевных строк %d (%v), ожидалось %d (%v)",
					len(got), got, len(c.want), c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("строка %d: получено %+v, ожидалось %+v", i, got[i], c.want[i])
				}
			}
		})
	}
}
