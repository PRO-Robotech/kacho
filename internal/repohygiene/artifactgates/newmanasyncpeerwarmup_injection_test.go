// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт прогрева чужого свежего идентификатора СПОСОБЕН
// упасть — и что падает он на существе, а не на форме.
//
// Все пробы гоняют ТУ ЖЕ функцию, что и гейт по дереву (`auditAsyncPeerWarmup`):
// проба, повторяющая логику гейта своей копией, доказывала бы свойство копии.
//
// У КАЖДОГО ОТРИЦАНИЯ ЗДЕСЬ ЕСТЬ ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. «Находок ноль»
// само по себе неотличимо от «разбор ослеп»: молчащая проба обязана дополнительно
// утверждать, что чужая свежая ссылка БЫЛА УВИДЕНА и зачтена в перепись по своей
// причине — прогрета, прикрыта повтором, своя, либо цель проверки прав самого шага.
//
// Синтетика намеренно НЕ повторяет ни одного имени из дерева: гейт обязан ловить
// ФОРМУ, а не помнить `nicId`.
//
// ПОСЛЕДНЯЯ ПРОБА ПРИВЯЗАНА К ДЕРЕВУ: она берёт настоящие контракты, каталог прав
// и коллекции и требует, чтобы разбор нашёл в них все четыре предпосылки. Сменится
// форма — она скажет об этом сама, вместо того чтобы синтетика продолжала
// доказывать свойство вчерашнего дерева.
package artifactgates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ─── синтетическое дерево ────────────────────────────────────────────────────

// nmSynthProto — два домена, три формы глагола: асинхронная мутация с целью
// проверки прав в теле, асинхронная мутация с целью в адресе и СИНХРОННАЯ мутация
// (возвращает ресурс, а не операцию). Последняя нужна как законный близнец: на ней
// отказ владельца виден кодом ответа, и требовать прогрева было бы неверно.
const nmSynthProto = `syntax = "proto3";

package kacho.cloud.synth.v1;

// post: "/decoy/v1/decoys" — координата В КОММЕНТАРИИ. Разбор обязан её не
// увидеть: rpc DecoyService/Decoy тоже назван здесь только словами.
service AlphaService {
  rpc Create (CreateAlphaRequest) returns (operation.Operation) {
    option (google.api.http) = { post: "/alpha/v1/alphas" body: "*" };
  }
  rpc Update (UpdateAlphaRequest) returns (operation.Operation) {
    option (google.api.http) = {
      patch: "/alpha/v1/alphas/{alpha_id}"
      body: "*"
    };
  }
  rpc Rename (RenameAlphaRequest) returns (Alpha) {
    option (google.api.http) = { post: "/alpha/v1/alphas/{alpha_id}:rename" body: "*" };
  }
}

service BetaService {
  rpc Get (GetBetaRequest) returns (Beta) {
    option (google.api.http) = { get: "/beta/v1/betas/{beta_id}" };
  }
  rpc Create (CreateBetaRequest) returns (operation.Operation) {
    option (google.api.http) = { post: "/beta/v1/betas" body: "*" };
  }
}
`

// nmSynthCatalog — записи каталога прав. `AlphaService/Create` гейтится ПРОЕКТОМ:
// именно это делает ссылку на чужой свежий ресурс в его теле полосой ВЛАДЕЛЬЦА, а
// не полосой шлюза.
const nmSynthCatalog = `[
  {"fqn":"kacho.cloud.synth.v1.AlphaService/Create",
   "scope_extractor":{"object_type":"project","from_request_field":"project_id"}},
  {"fqn":"kacho.cloud.synth.v1.AlphaService/Update",
   "scope_extractor":{"object_type":"alpha","from_request_field":"alpha_id"}},
  {"fqn":"kacho.cloud.synth.v1.AlphaService/Rename",
   "scope_extractor":{"object_type":"alpha","from_request_field":"alpha_id"}},
  {"fqn":"kacho.cloud.synth.v1.BetaService/Create",
   "scope_extractor":{"object_type":"project","from_request_field":"project_id"}},
  {"fqn":"kacho.cloud.synth.v1.BetaService/Get",
   "scope_extractor":{"object_type":"beta","from_request_field":"beta_id"}}
]`

const nmSynthCatalogRel = "gateway/internal/middleware/embed/permission_catalog.json"
const nmSynthProtoRel = "proto/kacho/cloud/synth/v1/synth_service.proto"

// nmStepBody — шаг с телом. Ссылка на чужой ресурс приезжает и адресом, и телом;
// половина дерева пишет её именно телом.
func nmStepBody(name, method, url, body string, script ...string) nmItem {
	return nmItem{
		Name: name,
		Request: &nmRequest{
			Method: method,
			URL:    json.RawMessage(`{"raw":` + mustJSON(url) + `}`),
			Body:   &nmBody{Raw: body},
		},
		Event: []nmEvent{{Listen: "test", Script: nmScript{Exec: script}}},
	}
}

// nmPublish — скрипт захвата идентификатора в той форме, какую эмитят генераторы.
func nmPublish(v string) []string {
	return []string{
		"const j = pm.response.json();",
		"if (j.id) pm.environment.set('opId', j.id);",
		"if (j.metadata) pm.environment.set('" + v + "', j.metadata.someId);",
	}
}

// nmPoll — опрос операции; при redriveTo — с повтором по ИСХОДУ операции.
func nmPoll(name, redriveTo string) nmItem {
	script := []string{
		"const j = pm.response.json();",
		"pm.test('operation done', () => pm.expect(j.done).to.eql(true));",
	}
	if redriveTo != "" {
		script = append(script,
			"if (pm.environment.get('"+nmRedriveMark+"') !== pm.info.requestName) {",
			"  pm.environment.set('_opRedriveCount', '0');",
			"  pm.environment.set('"+nmRedriveMark+"', pm.info.requestName);",
			"}",
			"let _opTransient = false;",
			"try { _opTransient = !!j.error && /not found/i.test(j.error.message || ''); } catch (e) {}",
			"if (_opTransient) {",
			"  pm.execution.setNextRequest('"+redriveTo+"');",
			"  return;",
			"}",
		)
	}
	return nmStep(name, "GET", "{{opsBase}}/operations/{{opId}}", script...)
}

func nmPeerWarmAudit(t *testing.T, catalog string, folders ...nmItem) ([]nmPeerWarmFinding, nmPeerWarmCensus) {
	t.Helper()
	dir := t.TempDir()
	writeSynth := func(rel, content string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeSynth(nmSynthProtoRel, nmSynthProto)
	writeSynth(nmSynthCatalogRel, catalog)
	rel := nmWriteCollection(t, dir, "synthetic.postman_collection.json", folders...)

	f, cen, err := auditAsyncPeerWarmup(dir, []string{rel}, []string{nmSynthProtoRel}, nmSynthCatalogRel)
	if err != nil {
		t.Fatal(err)
	}
	return f, cen
}

// nmSeedBeta — фикстура: чужой домен создаёт ресурс и публикует его идентификатор.
func nmSeedBeta(varName string) []nmItem {
	return []nmItem{
		nmStepBody("seed-beta", "POST", "{{baseUrl}}/beta/v1/betas",
			`{"projectId": "{{projId}}", "name": "b"}`, nmPublish(varName)...),
		nmPoll("poll-beta", ""),
	}
}

// ─── инъекция: настоящий дефект ──────────────────────────────────────────────

// Чужой свежий идентификатор уезжает в асинхронную мутацию первым обращением.
func TestAsyncPeerWarmupGate_FailsWhenPeerIdGoesStraightIntoAnAsyncMutation(t *testing.T) {
	steps := append(nmSeedBeta("betaRef"),
		nmStepBody("create-alpha", "POST", "{{baseUrl}}/alpha/v1/alphas",
			`{"projectId": "{{projId}}", "betaId": "{{betaRef}}"}`),
		nmPoll("poll-alpha", ""))

	findings, cen := nmPeerWarmAudit(t, nmSynthCatalog, nmFolder("SYNTH-1", steps...))

	if len(findings) != 1 {
		t.Fatalf("гейт обязан найти чужой непрогретый идентификатор; найдено %d: %v", len(findings), findings)
	}
	got := findings[0].String()
	for _, want := range []string{"SYNTH-1", "create-alpha", "betaRef", "beta", "alpha"} {
		if !strings.Contains(got, want) {
			t.Errorf("находка обязана называть координату (%q отсутствует): %s", want, got)
		}
	}
	if cen.peerRefs != 1 || cen.peerWarmed != 0 || cen.peerRedriven != 0 {
		t.Errorf("перепись: peerRefs=%d warmed=%d redriven=%d — ожидалось 1/0/0",
			cen.peerRefs, cen.peerWarmed, cen.peerRedriven)
	}
	if cen.asyncMutations != 2 {
		t.Errorf("асинхронных мутаций ожидалось 2 (seed-beta + create-alpha), увидено %d", cen.asyncMutations)
	}
}

// Тот же дефект, но ссылка стоит в АДРЕСЕ, а не в теле. Разбор, читающий только
// тело, промолчал бы на половине дерева.
func TestAsyncPeerWarmupGate_FailsWhenPeerIdIsInThePathOfAnAsyncMutation(t *testing.T) {
	steps := append(nmSeedBeta("betaRef"),
		nmStepBody("update-alpha", "PATCH", "{{baseUrl}}/alpha/v1/alphas/{{alphaId}}?ref={{betaRef}}",
			`{"name": "x"}`),
		nmPoll("poll-alpha", ""))
	steps = append([]nmItem{
		nmStepBody("seed-alpha", "POST", "{{baseUrl}}/alpha/v1/alphas",
			`{"projectId": "{{projId}}"}`, nmPublish("alphaId")...),
		nmPoll("poll-seed-alpha", ""),
	}, steps...)

	findings, cen := nmPeerWarmAudit(t, nmSynthCatalog, nmFolder("SYNTH-2", steps...))

	if len(findings) != 1 {
		t.Fatalf("ссылка в адресе обязана быть найдена; найдено %d: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0].String(), "betaRef") {
		t.Errorf("находка обязана называть переменную: %s", findings[0])
	}
	// {{alphaId}} — СВОЙ свежий и он же цель проверки прав шага: находкой не является.
	if cen.peerRefs != 1 {
		t.Errorf("чужих свежих ссылок ожидалась 1 (свой alphaId не в счёт), увидено %d", cen.peerRefs)
	}
}

// ─── законные близнецы: гейт обязан МОЛЧАТЬ ──────────────────────────────────

// Прогрев чтением — форма, посаженная PR #350.
func TestAsyncPeerWarmupGate_SilentWhenPeerIdIsWarmedByAPriorRead(t *testing.T) {
	steps := append(nmSeedBeta("betaRef"),
		nmStep("warm-beta", "GET", "{{baseUrl}}/beta/v1/betas/{{betaRef}}",
			"pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));"),
		nmStepBody("create-alpha", "POST", "{{baseUrl}}/alpha/v1/alphas",
			`{"projectId": "{{projId}}", "betaId": "{{betaRef}}"}`),
		nmPoll("poll-alpha", ""))

	findings, cen := nmPeerWarmAudit(t, nmSynthCatalog, nmFolder("SYNTH-3", steps...))

	if len(findings) != 0 {
		t.Errorf("на прогретой форме гейт обязан молчать, получено: %v", findings)
	}
	// Положительный контроль: молчание по существу, а не по слепоте.
	if cen.peerRefs != 1 || cen.peerWarmed != 1 {
		t.Errorf("гейт обязан был УВИДЕТЬ чужую свежую ссылку и зачесть её прогретой: "+
			"peerRefs=%d warmed=%d", cen.peerRefs, cen.peerWarmed)
	}
}

// Повтор по ИСХОДУ операции — второй законный исход, названный в issue.
func TestAsyncPeerWarmupGate_SilentWhenThePollRedrivesTheMutation(t *testing.T) {
	steps := append(nmSeedBeta("betaRef"),
		nmStepBody("create-alpha", "POST", "{{baseUrl}}/alpha/v1/alphas",
			`{"projectId": "{{projId}}", "betaId": "{{betaRef}}"}`),
		nmPoll("poll-alpha", "create-alpha"))

	findings, cen := nmPeerWarmAudit(t, nmSynthCatalog, nmFolder("SYNTH-4", steps...))

	if len(findings) != 0 {
		t.Errorf("повтор по исходу операции закрывает полосу, получено: %v", findings)
	}
	if cen.peerRefs != 1 || cen.peerRedriven != 1 {
		t.Errorf("гейт обязан был УВИДЕТЬ ссылку и зачесть повтор: peerRefs=%d redriven=%d",
			cen.peerRefs, cen.peerRedriven)
	}
}

// Повтор по исходу, названный ЧУЖИМ именем, этот шаг не прикрывает. Иначе один
// повтор в кейсе выключал бы гейт на всех его мутациях.
func TestAsyncPeerWarmupGate_FailsWhenTheRedriveNamesAnotherStep(t *testing.T) {
	steps := append(nmSeedBeta("betaRef"),
		nmStepBody("create-alpha", "POST", "{{baseUrl}}/alpha/v1/alphas",
			`{"projectId": "{{projId}}", "betaId": "{{betaRef}}"}`),
		nmPoll("poll-alpha", "seed-beta"))

	findings, cen := nmPeerWarmAudit(t, nmSynthCatalog, nmFolder("SYNTH-5", steps...))

	if len(findings) != 1 {
		t.Fatalf("повтор чужого шага не прикрывает эту мутацию; найдено %d", len(findings))
	}
	if cen.peerRedriven != 0 {
		t.Errorf("повтор не должен был зачесться этому шагу: redriven=%d", cen.peerRedriven)
	}
}

// СВОЙ свежий идентификатор (тот же домен) — не находка: владелец резолвит его в
// собственной БД, отдельной проверки прав нет. Ровно то, что issue называет
// «создал сеть → создал в ней подсеть».
func TestAsyncPeerWarmupGate_SilentWhenTheFreshIdBelongsToTheSameDomain(t *testing.T) {
	steps := []nmItem{
		nmStepBody("seed-alpha", "POST", "{{baseUrl}}/alpha/v1/alphas",
			`{"projectId": "{{projId}}"}`, nmPublish("parentAlphaId")...),
		nmPoll("poll-seed", ""),
		nmStepBody("create-child", "POST", "{{baseUrl}}/alpha/v1/alphas",
			`{"projectId": "{{projId}}", "parentId": "{{parentAlphaId}}"}`),
		nmPoll("poll-child", ""),
	}

	findings, cen := nmPeerWarmAudit(t, nmSynthCatalog, nmFolder("SYNTH-6", steps...))

	if len(findings) != 0 {
		t.Errorf("своя свежая ссылка находкой не является, получено: %v", findings)
	}
	// Положительный контроль: шаги увидены, просто ссылка своя.
	if cen.asyncMutations != 2 || cen.peerRefs != 0 {
		t.Errorf("гейт обязан был увидеть обе мутации и ни одной ЧУЖОЙ ссылки: "+
			"async=%d peerRefs=%d", cen.asyncMutations, cen.peerRefs)
	}
}

// Чужая свежая ссылка, которая ЕСТЬ цель проверки прав самого шага: полосу
// закрывает синхронный 403, и обёртка на ней работает.
func TestAsyncPeerWarmupGate_SilentWhenTheFreshIdIsTheStepsOwnScopeTarget(t *testing.T) {
	steps := append(nmSeedBeta("scopeRef"),
		nmStepBody("create-alpha", "POST", "{{baseUrl}}/alpha/v1/alphas",
			`{"projectId": "{{scopeRef}}", "name": "a"}`),
		nmPoll("poll-alpha", ""))

	findings, cen := nmPeerWarmAudit(t, nmSynthCatalog, nmFolder("SYNTH-7", steps...))

	if len(findings) != 0 {
		t.Errorf("цель проверки прав самого шага находкой не является, получено: %v", findings)
	}
	// Положительный контроль: цель проверки прав ВЗЯТА ИЗ КАТАЛОГА у обеих мутаций
	// кейса (у обеих `projectId` в теле). Ноль означал бы, что послабление сработало
	// по слепоте — разбор просто не нашёл цели и промолчал бы на любой форме.
	if cen.asyncWithVarTarget != 2 {
		t.Errorf("гейт обязан был РАЗРЕШИТЬ цель проверки прав из каталога у обеих "+
			"мутаций: asyncWithVarTarget=%d", cen.asyncWithVarTarget)
	}
}

// ...и то же самое ПРИ ПОТЕРЕ основания: если каталог перестал объявлять цель для
// этого RPC, послабление предмета не имеет и обязано истечь само — та же форма
// становится находкой. Это и есть самоистечение, а не вечный список исключений.
func TestAsyncPeerWarmupGate_ScopeTargetExemptionExpiresWithItsCatalogEntry(t *testing.T) {
	catalogWithoutAlphaCreate := `[
  {"fqn":"kacho.cloud.synth.v1.BetaService/Create",
   "scope_extractor":{"object_type":"project","from_request_field":"project_id"}}
]`
	steps := append(nmSeedBeta("scopeRef"),
		nmStepBody("create-alpha", "POST", "{{baseUrl}}/alpha/v1/alphas",
			`{"projectId": "{{scopeRef}}", "name": "a"}`),
		nmPoll("poll-alpha", ""))

	findings, cen := nmPeerWarmAudit(t, catalogWithoutAlphaCreate, nmFolder("SYNTH-8", steps...))

	if len(findings) != 1 {
		t.Fatalf("без записи каталога послаблению нечем обосноваться — форма обязана стать "+
			"находкой; найдено %d", len(findings))
	}
	// Запись `BetaService/Create` в урезанном каталоге осталась — её цель по-прежнему
	// разрешается. Пропала ровно та, на которой держалось послабление предыдущей пробы.
	if cen.asyncWithVarTarget != 1 {
		t.Errorf("цель обязана разрешаться только у той мутации, чья запись в каталоге "+
			"осталась: asyncWithVarTarget=%d", cen.asyncWithVarTarget)
	}
}

// СИНХРОННАЯ мутация (возвращает ресурс, а не операцию): отказ владельца виден
// кодом ответа, обёртка окна видимости на ней работает. Требовать прогрева здесь
// значило бы чинить закрытое.
func TestAsyncPeerWarmupGate_SilentOnASynchronousMutation(t *testing.T) {
	steps := append(nmSeedBeta("betaRef"),
		nmStepBody("rename-alpha", "POST", "{{baseUrl}}/alpha/v1/alphas/{{alphaId}}:rename",
			`{"name": "n", "betaId": "{{betaRef}}"}`))
	steps = append([]nmItem{
		nmStepBody("seed-alpha", "POST", "{{baseUrl}}/alpha/v1/alphas",
			`{"projectId": "{{projId}}"}`, nmPublish("alphaId")...),
		nmPoll("poll-seed-alpha", ""),
	}, steps...)

	findings, cen := nmPeerWarmAudit(t, nmSynthCatalog, nmFolder("SYNTH-9", steps...))

	if len(findings) != 0 {
		t.Errorf("синхронная мутация — не предмет этого гейта, получено: %v", findings)
	}
	// Положительный контроль: в кейсе три не-опросных шага, и РЕЗОЛВЛЕНЫ все три —
	// включая адрес с суффиксом `:глагол`. Значит молчание про `:rename` вызвано его
	// синхронным ответом, а не тем, что гейт его не узнал.
	if cen.resolvedSteps != 3 {
		t.Errorf("разбор обязан резолвить все три не-опросных шага, включая `:rename`: "+
			"resolvedSteps=%d", cen.resolvedSteps)
	}
	if cen.asyncMutations != 2 {
		t.Errorf("асинхронных ожидалось 2 (seed-alpha и seed-beta; `:rename` синхронна), "+
			"увидено %d", cen.asyncMutations)
	}
}

// Идентификатор, ОПУБЛИКОВАННЫЙ чтением (каталог зон/регионов, discover-шаг), уже
// прочитан by construction — окна у него нет.
func TestAsyncPeerWarmupGate_SilentWhenThePeerIdWasPublishedByARead(t *testing.T) {
	steps := []nmItem{
		nmStep("discover-beta", "GET", "{{baseUrl}}/beta/v1/betas/{{seedBetaId}}",
			"pm.environment.set('betaRef', pm.response.json().id);"),
		nmStepBody("create-alpha", "POST", "{{baseUrl}}/alpha/v1/alphas",
			`{"projectId": "{{projId}}", "betaId": "{{betaRef}}"}`),
		nmPoll("poll-alpha", ""),
	}

	findings, cen := nmPeerWarmAudit(t, nmSynthCatalog, nmFolder("SYNTH-10", steps...))

	if len(findings) != 0 {
		t.Errorf("идентификатор, полученный чтением, прогревать нечем и незачем: %v", findings)
	}
	if cen.asyncMutations != 1 {
		t.Errorf("положительный контроль: мутация обязана быть увидена, async=%d", cen.asyncMutations)
	}
}

// Комментарий с координатой не является объявлением маршрута. Иначе первая же
// синтетическая фикстура в соседнем файле сломала бы гейт или замаскировала дефект.
func TestAsyncPeerWarmupGate_CommentedRouteIsNotARoute(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, nmSynthProtoRel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(nmSynthProto), 0o600); err != nil {
		t.Fatal(err)
	}
	routes, files, err := nmBuildRoutes(dir, []string{nmSynthProtoRel})
	if err != nil {
		t.Fatal(err)
	}
	if files != 1 {
		t.Fatalf("прочитан должен быть ровно один контракт, прочитано %d", files)
	}
	if _, ok := routes["POST /decoy/v1/decoys"]; ok {
		t.Error("маршрут из КОММЕНТАРИЯ попал в карту — разбор читает текст, а не объявления")
	}
	// Положительный контроль: настоящие объявления того же файла разобраны, значит
	// молчание выше — по существу.
	for _, want := range []string{"POST /alpha/v1/alphas", "PATCH /alpha/v1/alphas/*",
		"POST /alpha/v1/alphas/*:rename", "GET /beta/v1/betas/*", "POST /beta/v1/betas"} {
		if _, ok := routes[want]; !ok {
			t.Errorf("маршрут %q не разобран — контроль положительной стороны провален", want)
		}
	}
	if !routes["POST /alpha/v1/alphas"].returnsOp || routes["POST /alpha/v1/alphas/*:rename"].returnsOp {
		t.Error("разбор обязан отличать конверт операции от синхронного ответа")
	}
}

// ─── предпосылка, привязанная к ДЕРЕВУ ───────────────────────────────────────

// Все четыре предпосылки, на которых стоит гейт, обязаны существовать в НАСТОЯЩЕМ
// дереве. Пропадёт любая — синтетика выше продолжит зеленеть, доказывая свойство
// вчерашнего дерева; эта проба скажет об этом прямо.
func TestAsyncPeerWarmupGate_PremisesPresentInTheRealTree(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)
	var cols, protos []string
	for rel := range tt.files {
		switch {
		case strings.Contains(rel, "/tests/newman/collections/") && strings.HasSuffix(rel, ".json"):
			cols = append(cols, rel)
		case strings.HasPrefix(rel, "proto/") && strings.HasSuffix(rel, ".proto"):
			protos = append(protos, rel)
		}
	}
	sort.Strings(cols)
	sort.Strings(protos)

	_, cen, err := auditAsyncPeerWarmup(root, cols, protos, nmCatalogPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		got  int
	}{
		{"маршрутов из контрактов", cen.httpRoutes},
		{"записей каталога прав", cen.catalogRows},
		{"асинхронных мутаций", cen.asyncMutations},
		{"ссылок на чужой свежий ресурс", cen.peerRefs},
		{"повторов по исходу операции", cen.peerRedriven},
	} {
		if c.got == 0 {
			t.Errorf("в дереве не нашлось ни одного: %s — форма сменилась, гейт ослеп", c.name)
		}
	}
	t.Logf("предпосылка на дереве: маршрутов %d, записей каталога %d, асинхронных мутаций %d, "+
		"чужих свежих ссылок %d (прогрето %d, прикрыто повтором %d)",
		cen.httpRoutes, cen.catalogRows, cen.asyncMutations, cen.peerRefs,
		cen.peerWarmed, cen.peerRedriven)
}
