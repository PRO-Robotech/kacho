// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт допуска без производителя СПОСОБЕН упасть — и что
// падает он на существе, а не на форме.
//
// Все пробы гоняют ТУ ЖЕ функцию, что и гейт по дереву
// (`auditStatusLaneProducers`): проба, повторяющая логику гейта своей копией,
// доказывала бы свойство копии, а не гейта.
//
// У КАЖДОГО ОТРИЦАНИЯ ЗДЕСЬ ЕСТЬ ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. «Находок ноль»
// само по себе неотличимо от «распознаватель ослеп», поэтому каждая молчащая
// проба дополнительно утверждает, что гейт допуск УВИДЕЛ и промолчал по существу.
//
// ФИКСТУРА ПРИВЯЗАНА К ДЕРЕВУ, А НЕ К ПАМЯТИ АВТОРА. Отдельная проба берёт
// НАСТОЯЩИЙ шаг полосы операций из закоммиченных коллекций, дописывает ему один
// статус в допуск и требует красного. Сменится форма записи допуска в
// генераторах — эта проба скажет об этом сама, вместо того чтобы синтетика
// продолжала доказывать свойство вчерашнего дерева.
package artifactgates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// slpLane — множество статусов «полосы операций» для синтетических проб. Взято
// то же, что даёт перепись на этом дереве, и НЕ вычисляется здесь заново: проба
// проверяет разбор шагов, а не перепись производителей (её проверяет отдельная
// проба ниже, на настоящем дереве).
func slpLane() map[int]bool {
	return map[int]bool{200: true, 400: true, 403: true, 404: true, 500: true, 504: true}
}

func slpAudit(t *testing.T, folders ...nmItem) ([]slpFinding, slpCensus) {
	t.Helper()
	dir := t.TempDir()
	rel := nmWriteCollection(t, dir, "synthetic.postman_collection.json", folders...)
	f, cen, err := auditStatusLaneProducers(dir, []string{rel}, slpLane())
	if err != nil {
		t.Fatal(err)
	}
	return f, cen
}

// ─── подкласс A: допуск шире собственного БЕЗУСЛОВНОГО пина ──────────────────

func TestSLP_A_UnconditionalPinNarrowerThanAllowanceIsAFinding(t *testing.T) {
	step := nmStep("create-badregion", "POST", "{{baseUrl}}/registry/v1/registries",
		"pm.test('rejected 4xx', () => pm.expect(pm.response.code).to.be.oneOf([400, 409]));",
		"pm.test('grpc 9', () => pm.expect(pm.response.json().code).to.eql(9));",
	)
	f, cen := slpAudit(t, nmFolder("CASE", step))
	if len(f) != 1 {
		t.Fatalf("ожидалась 1 находка, получено %d: %v", len(f), f)
	}
	if cen.withUnconditionalPin != 1 {
		t.Fatalf("гейт не увидел безусловного пина: перепись %d", cen.withUnconditionalPin)
	}
	if !strings.Contains(f[0].why, "БЕЗУСЛОВНО пинит `code == 9`") ||
		!strings.Contains(f[0].why, "[409]") {
		t.Fatalf("находка не называет предмет: %s", f[0].why)
	}
	if f[0].step != "create-badregion" {
		t.Fatalf("находка не называет координату: %q", f[0].step)
	}
}

// Законный близнец №1: те же два статуса, но пин В ВЕТКЕ — полос действительно
// две, и допуск законен. Без этой пробы гейт ловил бы форму, а не существо, и
// первый же ложный срабат его отключил бы.
func TestSLP_A_ConditionalPinIsLawfulAndSilent(t *testing.T) {
	step := nmStep("update-immutable", "PATCH", "{{baseUrl}}/iam/v1/accounts/{{id}}",
		"pm.test('rejected', () => pm.expect(pm.response.code).to.be.oneOf([400, 403]));",
		"if (pm.response.code === 400) {",
		"  pm.test('grpc 3', () => pm.expect(pm.response.json().code).to.eql(3));",
		"}",
	)
	f, cen := slpAudit(t, nmFolder("CASE", step))
	if len(f) != 0 {
		t.Fatalf("законный многополосный негатив объявлен находкой: %v", f)
	}
	if cen.withAllowance != 1 {
		t.Fatalf("гейт не увидел допуска — молчание не по существу: перепись %d", cen.withAllowance)
	}
	if cen.withUnconditionalPin != 0 {
		t.Fatalf("условный пин засчитан безусловным: перепись %d", cen.withUnconditionalPin)
	}
}

// Законный близнец №2: ветвление НЕ по коду ответа, а по фикстуре — допуск и пин
// лежат во взаимоисключающих ветках. Ровно эта форма живёт в `load-balancer` и
// дала четыре ложные находки первой редакции предиката.
func TestSLP_A_BranchOnFixtureIsLawfulAndSilent(t *testing.T) {
	step := nmStep("cr-mismatch", "POST", "{{baseUrl}}/nlb/v1/networkLoadBalancers",
		"if (!pm.environment.get('vpcSubnetId')) {",
		"  pm.test('no fixture', () => pm.expect(pm.response.code).to.be.oneOf([400, 404, 503]));",
		"} else {",
		"  pm.test('rejected 400', () => pm.expect(pm.response.code).to.eql(400));",
		"  pm.test('grpc 3', () => pm.expect(pm.response.json().code).to.eql(3));",
		"}",
	)
	f, cen := slpAudit(t, nmFolder("CASE", step))
	if len(f) != 0 {
		t.Fatalf("взаимоисключающие ветки объявлены находкой: %v", f)
	}
	if cen.steps != 1 {
		t.Fatalf("шаг не осмотрен вовсе: перепись %d", cen.steps)
	}
}

// Законный близнец №3: допуск ровно равен образу пина — сужать нечего.
func TestSLP_A_AllowanceEqualToPinImageIsSilent(t *testing.T) {
	step := nmStep("cancel-done", "POST", "{{baseUrl}}/operations/{{opId}}:cancel",
		"pm.test('rejected', () => pm.expect(pm.response.code).to.eql(400));",
		"pm.test('grpc 9', () => pm.expect(pm.response.json().code).to.eql(9));",
	)
	f, cen := slpAudit(t, nmFolder("CASE", step))
	if len(f) != 0 {
		t.Fatalf("допуск, равный образу пина, объявлен находкой: %v", f)
	}
	if cen.withUnconditionalPin != 1 || cen.withAllowance != 1 {
		t.Fatalf("молчание не по существу: пинов %d, допусков %d",
			cen.withUnconditionalPin, cen.withAllowance)
	}
}

// ─── подкласс B: полоса операций ─────────────────────────────────────────────

func TestSLP_B_StatusOutsideTheOperationsLaneIsAFinding(t *testing.T) {
	step := nmStep("cancel-as-B", "POST", "{{baseUrl}}/operations/{{opId}}:cancel",
		"pm.test('rejected', () => pm.expect(pm.response.code).to.be.oneOf([400, 403, 404, 409]));",
	)
	f, cen := slpAudit(t, nmFolder("CASE", step))
	if len(f) != 1 {
		t.Fatalf("ожидалась 1 находка, получено %d: %v", len(f), f)
	}
	if cen.opsLaneSteps != 1 {
		t.Fatalf("шаг не опознан как принадлежащий полосе операций: перепись %d", cen.opsLaneSteps)
	}
	if !strings.Contains(f[0].why, "полосе операций") || !strings.Contains(f[0].why, "[409]") {
		t.Fatalf("находка не называет предмет: %s", f[0].why)
	}
}

// Законный близнец: тот же путь, допуск ровно из производимых полосой статусов.
func TestSLP_B_LawfulOperationsAllowanceIsSilent(t *testing.T) {
	step := nmStep("cancel-as-B", "POST", "{{baseUrl}}/operations/{{opId}}:cancel",
		"pm.test('rejected', () => pm.expect(pm.response.code).to.be.oneOf([400, 403, 404]));",
	)
	f, cen := slpAudit(t, nmFolder("CASE", step))
	if len(f) != 0 {
		t.Fatalf("законный допуск полосы операций объявлен находкой: %v", f)
	}
	if cen.opsLaneStepsWithAllowed != 1 {
		t.Fatalf("молчание не по существу — допуск не увиден: перепись %d", cen.opsLaneStepsWithAllowed)
	}
}

// Законный близнец: 409 на ЧУЖОЙ полосе. Подкласс B судит только полосу операций,
// и это его объявленная граница: у создания ресурса производитель 409 есть
// (UNIQUE → ALREADY_EXISTS), поэтому такой допуск законен.
func TestSLP_B_SameStatusOnAnotherLaneIsSilent(t *testing.T) {
	step := nmStep("seed-pool", "POST", "{{baseUrl}}/vpc/v1/addressPools",
		"pm.test('посев идемпотентен', () => pm.expect(pm.response.code).to.be.oneOf([200, 409]));",
	)
	f, cen := slpAudit(t, nmFolder("CASE", step))
	if len(f) != 0 {
		t.Fatalf("допуск чужой полосы объявлен находкой: %v", f)
	}
	if cen.withAllowance != 1 {
		t.Fatalf("молчание не по существу — допуск не увиден: перепись %d", cen.withAllowance)
	}
	if cen.opsLaneSteps != 0 {
		t.Fatalf("чужой путь опознан как полоса операций: перепись %d", cen.opsLaneSteps)
	}
}

// Законный близнец: статус, который край отдаёт САМ, до бэкенда. Он законен на
// любой полосе, включая операции, — иначе гейт краснел бы на пробе, проверяющей
// нерегистрацию маршрута.
func TestSLP_B_EdgeOwnStatusIsSilentOnTheOperationsLane(t *testing.T) {
	step := nmStep("wrong-method", "GET", "{{baseUrl}}/operations/{{opId}}:cancel",
		"pm.test('маршрут есть, метод не тот', () => pm.expect(pm.response.code).to.be.oneOf([404, 405]));",
	)
	f, cen := slpAudit(t, nmFolder("CASE", step))
	if len(f) != 0 {
		t.Fatalf("собственный статус края объявлен находкой: %v", f)
	}
	if cen.opsLaneStepsWithAllowed != 1 {
		t.Fatalf("молчание не по существу: перепись %d", cen.opsLaneStepsWithAllowed)
	}
}

// ─── распознаватель знает ВСЕ формы записи допуска ───────────────────────────
//
// Форма, о которой распознаватель не знает, — не край и не редкость: всё
// записанное в ней оказывается ВНЕ НАБЛЮДЕНИЯ, и гейт молчит, не давая ни
// красного, ни зелёного (`testing.md` §«Гейт на класс», п.7).
func TestSLP_EveryAllowanceFormIsRead(t *testing.T) {
	forms := map[string]string{
		"oneOf":   "pm.test('t', () => pm.expect(pm.response.code).to.be.oneOf([400, 409]));",
		"include": "pm.test('t', () => pm.expect([400, 409], 'hint').to.include(pm.response.code));",
		"eql":     "pm.test('t', () => pm.expect(pm.response.code).to.eql(409));",
		"equal":   "pm.test('t', () => pm.expect(pm.response.code).to.equal(409));",
	}
	names := make([]string, 0, len(forms))
	for n := range forms {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			step := nmStep("s", "POST", "{{baseUrl}}/operations/{{opId}}:cancel", forms[name])
			f, cen := slpAudit(t, nmFolder("CASE", step))
			if cen.opsLaneStepsWithAllowed != 1 {
				t.Fatalf("форма %q не прочитана как допуск — она вне наблюдения гейта", name)
			}
			if len(f) != 1 {
				t.Fatalf("форма %q прочитана, но 409 не назван находкой: %v", name, f)
			}
		})
	}
}

// Комментарий — не утверждение. Гейт, читающий сырой текст, краснел бы на разборе,
// который сам же просил написать рядом с проверкой.
func TestSLP_CommentIsNotAnAssertion(t *testing.T) {
	step := nmStep("s", "POST", "{{baseUrl}}/operations/{{opId}}:cancel",
		"// 409 снят: производителя у него на этой полосе нет — oneOf([400, 409]) не краснел никогда",
		"pm.test('rejected', () => pm.expect(pm.response.code).to.eql(400));",
	)
	f, _ := slpAudit(t, nmFolder("CASE", step))
	if len(f) != 0 {
		t.Fatalf("гейт нашёл предмет в КОММЕНТАРИИ, объясняющем этот же запрет: %v", f)
	}
}

// ─── фикстура, привязанная к ДЕРЕВУ ──────────────────────────────────────────

// TestSLP_RealTreeStepGoesRedWhenAStatusIsAdded — доказательство на НАСТОЯЩЕМ
// шаге из закоммиченных коллекций, а не на синтетике: берём первый шаг полосы
// операций, несущий допуск, дописываем в него 409 и требуем красного.
//
// Синтетика доказывает свойство синтетики. Эта проба доказывает, что гейт читает
// ту форму, которой генераторы пишут СЕГОДНЯ.
func TestSLP_RealTreeStepGoesRedWhenAStatusIsAdded(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)
	var cols []string
	for rel := range tt.files {
		if strings.Contains(rel, "/tests/newman/collections/") && strings.HasSuffix(rel, ".json") {
			cols = append(cols, rel)
		}
	}
	sort.Strings(cols)

	// Находим НАСТОЯЩИЙ шаг полосы операций с допуском формы oneOf и дописываем
	// ему один статус. Выбор идёт разбором коллекции, а не поиском по тексту:
	// текстовый поиск нашёл бы `oneOf` в соседнем шаге, к полосе операций
	// отношения не имеющем, — и проба доказывала бы совсем другое.
	var chosen string
	var injected []byte
	for _, rel := range cols {
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь получен из индекса git
		if err != nil {
			t.Fatal(err)
		}
		var raw map[string]any
		if err := json.Unmarshal(b, &raw); err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		if !slpInjectStatus(raw["item"]) {
			continue
		}
		out, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		chosen, injected = rel, out
		break
	}
	if chosen == "" {
		t.Fatal("в дереве не нашлось шага полосы операций с допуском формы oneOf — " +
			"фикстура пережила свой предмет: либо форма записи сменилась, либо полоса переехала")
	}

	dir := t.TempDir()
	dst := filepath.Join(dir, chosen)
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, injected, 0o600); err != nil {
		t.Fatal(err)
	}
	// Файл обязан остаться разбираемым — иначе красное придёт от сломанного JSON,
	// а не от предмета гейта.
	var probe nmCollection
	if err := json.Unmarshal(injected, &probe); err != nil {
		t.Fatalf("инъекция сломала разбор коллекции — красное пришло бы от соседа: %v", err)
	}

	f, cen, err := auditStatusLaneProducers(dir, []string{chosen}, slpLane())
	if err != nil {
		t.Fatal(err)
	}
	if cen.opsLaneSteps == 0 {
		t.Fatalf("в %s не опознано ни одного шага полосы операций", chosen)
	}
	if len(f) == 0 {
		t.Fatalf("в НАСТОЯЩИЙ шаг %s дописан 409, а гейт промолчал — он читает не ту форму, "+
			"которой пишут генераторы сегодня", chosen)
	}
	t.Logf("инъекция в дерево: %s, шагов полосы %d, находок %d", chosen, cen.opsLaneSteps, len(f))
}

// ─── предпосылки самого гейта ────────────────────────────────────────────────

// Пустой корпус — ОТКАЗ, а не «находок нет»: иначе переезд коллекций молча
// выключил бы гейт.
func TestSLP_EmptyCorpusIsARefusalNotSilence(t *testing.T) {
	dir := t.TempDir()
	f, cen, err := auditStatusLaneProducers(dir, nil, slpLane())
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 0 || cen.collections != 0 || cen.steps != 0 {
		t.Fatalf("пустой корпус дал непустую перепись: находок %d, коллекций %d, шагов %d",
			len(f), cen.collections, cen.steps)
	}
	// Сам гейт по дереву на такой переписи обязан звать t.Fatal — это его строки
	// `cen.collections == 0` / `cen.steps == 0`. Здесь проверено, что перепись
	// действительно нулевая, то есть отказу есть на что опереться.
}

// Перепись производителей полосы — утверждение о ДЕРЕВЕ, и оно проверяется здесь,
// а не объявляется в шапке.
func TestSLP_OperationsLaneProducersAreCountedFromTheTree(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)
	lane, producers, err := slpOpsLaneStatuses(root, slpGoFilesOf(tt))
	if err != nil {
		t.Fatal(err)
	}
	if len(producers) == 0 {
		t.Fatal("перепись производителей полосы пуста — гейт судил бы по памяти автора")
	}
	// Предмет подкласса B: 409 полосой не производится. Если это перестанет быть
	// верным, гейт обязан сказать об этом, а не тихо продолжить.
	if lane[409] {
		t.Errorf("полоса операций теперь производит 409 — предмет подкласса B исчез, "+
			"его пора снимать вместе с ним (коды: %v)", producers)
	}
	for _, want := range []string{"NotFound", "FailedPrecondition", "InvalidArgument"} {
		if producers[want] == 0 {
			t.Errorf("в производителях полосы нет %s — распознаватель кодов ослеп "+
				"либо полоса переехала; чинить надо гейт, а не выходить успехом", want)
		}
	}
	t.Logf("перепись производителей полосы операций: %d различных кодов, статусы %v",
		len(producers), slpSorted(lane))
}

// Имена кодов, читаемые из исходников, обязаны совпадать с идентификаторами
// пакета `codes`. Разойдутся — перепись производителей начнёт пропускать коды
// молча, и «409 не производится» станет свойством слепоты, а не дерева.
func TestSLP_CodeNameTableAgreesWithThePackage(t *testing.T) {
	byName := slpGrpcCodeByName()
	for _, want := range []string{"OK", "InvalidArgument", "NotFound", "AlreadyExists",
		"PermissionDenied", "FailedPrecondition", "Aborted", "Internal", "Unavailable", "Unauthenticated"} {
		if _, ok := byName[want]; !ok {
			t.Errorf("имя кода %q не выводится из codes.Code.String() — таблица разошлась "+
				"с библиотекой, перепись производителей будет пропускать его молча", want)
		}
	}
}

// slpInjectStatus — дописывает 409 в допуск ПЕРВОГО шага полосы операций,
// найденного обходом. Возвращает true, если такой шаг нашёлся.
//
// Обход идёт по нетипизированной карте намеренно: проба обязана вернуть в дерево
// файл, отличающийся от исходного РОВНО одним статусом, а перекладывание через
// типизированную структуру гейта потеряло бы всё, чего та не читает, — и красное
// могло бы прийти от потери, а не от предмета.
func slpInjectStatus(items any) bool {
	arr, ok := items.([]any)
	if !ok {
		return false
	}
	for _, raw := range arr {
		it, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if inner, ok := it["item"]; ok {
			if slpInjectStatus(inner) {
				return true
			}
			continue
		}
		req, ok := it["request"].(map[string]any)
		if !ok {
			continue
		}
		url := ""
		switch u := req["url"].(type) {
		case string:
			url = u
		case map[string]any:
			if s, ok := u["raw"].(string); ok {
				url = s
			}
		}
		if !strings.Contains(url, "/operations/") {
			continue
		}
		evs, ok := it["event"].([]any)
		if !ok {
			continue
		}
		for _, ev := range evs {
			e, ok := ev.(map[string]any)
			if !ok || e["listen"] != "test" {
				continue
			}
			sc, ok := e["script"].(map[string]any)
			if !ok {
				continue
			}
			lines, ok := sc["exec"].([]any)
			if !ok {
				continue
			}
			for i, ln := range lines {
				s, ok := ln.(string)
				// Допуск ИМЕННО ПО КОДУ ОТВЕТА. Признак по одной лишь `oneOf` привёл бы
				// в `pm.expect(j.error).to.be.oneOf([undefined, null])` — форму, к коду
				// ответа отношения не имеющую, — и проба доказывала бы, что гейт молчит
				// на том, о чём и не должен говорить.
				if !ok || !strings.Contains(s, ".to.be.oneOf([") || !strings.Contains(s, "pm.response.code") {
					continue
				}
				// Допуск внутри ветвления гейт не судит намеренно; инъекция в такую
				// строку не доказала бы ничего.
				if strings.Contains(s, "if ") || strings.Contains(s, "else") {
					continue
				}
				lines[i] = strings.Replace(s, ".to.be.oneOf([", ".to.be.oneOf([409, ", 1)
				sc["exec"] = lines
				return true
			}
		}
	}
	return false
}

// TestSLP_OperationsLaneHasExactlyTheProducersTheGateReads — предпосылка
// подкласса B названа координатами (`slpOpsLaneProducerDirs`), и она обязана
// быть ПОЛНОЙ: заведись третья реализация полосы вне этих двух каталогов —
// перепись производителей станет неполной МОЛЧА, и «409 не производится»
// превратится из замера в свойство слепоты.
//
// Проверяется по исполняемой части: реализация полосы — это метод с сигнатурой
// отмены, а не упоминание её имени. Регистрация общего обработчика в композиционном
// корне каждого сервиса реализацией НЕ является и здесь не считается.
func TestSLP_OperationsLaneHasExactlyTheProducersTheGateReads(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	const marker = "CancelOperationRequest) (*operationpb.Operation"
	var impls []string
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		if strings.HasPrefix(rel, filepath.Join("pkg", "api")+string(filepath.Separator)) {
			continue // сгенерированные стабы — не реализация полосы
		}
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь получен из индекса git
		if err != nil {
			t.Fatal(err)
		}
		for _, raw := range strings.Split(string(b), "\n") {
			if strings.Contains(slpStripGoComment(raw), marker) {
				impls = append(impls, rel)
				break
			}
		}
	}
	sort.Strings(impls)

	if len(impls) == 0 {
		t.Fatal("не найдено НИ ОДНОЙ реализации отмены операции — распознаватель ослеп " +
			"либо сигнатура сменилась; чинить надо пробу, а не выходить успехом")
	}
	for _, rel := range impls {
		covered := false
		for _, dir := range slpOpsLaneProducerDirs {
			if strings.HasPrefix(rel, dir+string(filepath.Separator)) {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("реализация полосы операций %s лежит ВНЕ каталогов, которые читает "+
				"перепись производителей (%v): множество кодов полосы неполно, и подкласс B "+
				"судит по части дерева. Либо добавь каталог в slpOpsLaneProducerDirs, либо "+
				"сведи реализацию к общему слою", rel, slpOpsLaneProducerDirs)
		}
	}
	t.Logf("перепись реализаций полосы операций: %d — %v (читаются каталоги %v)",
		len(impls), impls, slpOpsLaneProducerDirs)
}

// TestSLP_RetryPredicateFormIsNotAnAllowance — форма `[…].includes(pm.response.code)`
// в присваивании признака повтора допуском НЕ считается.
//
// Это объявленная граница гейта, а не слепая зона, и разница проверяется здесь:
// форма ничего не утверждает (её значение уходит в условие повтора), поэтому
// упасть не может — и требовать от неё состава исходов значило бы краснеть на
// коде, который никаких исходов не обещает.
func TestSLP_RetryPredicateFormIsNotAnAllowance(t *testing.T) {
	step := nmStep("poll", "GET", "{{baseUrl}}/operations/{{opId}}",
		"const _p200retryCode = [403, 409].includes(pm.response.code);",
		"if (_p200retryCode) { pm.execution.setNextRequest(pm.info.requestName); return; }",
		"pm.test('poll status 200', () => pm.expect(pm.response.code).to.eql(200));",
	)
	f, cen := slpAudit(t, nmFolder("CASE", step))
	if len(f) != 0 {
		t.Fatalf("признак повтора принят за допуск: %v", f)
	}
	// Молчание по существу: шаг полосы операций осмотрен, и допуск в нём УВИДЕН —
	// тот, что утверждает (200), а не тот, что вычисляет повтор.
	if cen.opsLaneStepsWithAllowed != 1 {
		t.Fatalf("шаг не осмотрен как несущий допуск: перепись %d", cen.opsLaneStepsWithAllowed)
	}
}
