// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «ребёнок под вложенным потолком в ПОСЕЯННОМ
// родителе объявляет своё снятие» СПОСОБЕН упасть — и что падает он на существе,
// а не на форме.
//
// Инъекция идёт в обе стороны, потому что одного «краснеет» мало (гейт,
// краснеющий на всём, ничего не измеряет), и одного «молчит» тоже мало (молчание
// бывает от того, что читать не стали):
//
//	создание без снятия, родитель посеян     → КРАСНЕЕТ, называя файл, вид, шаг;
//	то же со снятием                         → молчит (законный близнец);
//	родитель заведён самим набором           → молчит, и перепись зовёт его «своим»;
//	создание утверждает ОТКАЗ                → молчит, и перепись зовёт его отказом;
//	снятие адресует базу, а не экземпляр     → КРАСНЕЕТ (это не снятие ребёнка);
//	коллекция без вложенных детей вовсе      → молчит: пустой перечень есть цель.
//
// Обе половины гоняют ТУ ЖЕ функцию (`auditNestedQuotaReclaim`), что и прогон по
// дереву. Каркас коллекций — настоящий выход `scripts/gen.py`, а не выдумка:
// форма шага, событий и тела взята из `services/nlb/tests/newman/collections/`.
package repohygiene_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// synthEndpoints — координаты одного вида, чтобы вердикт зависел от предмета
// пробы, а не от полноты таблицы дерева.
var synthEndpoints = map[string]nestedChildEndpoint{
	"vpc.network.subnet": {Base: "/vpc/v1/subnets", ParentField: "networkId"},
}

// step собирает один шаг коллекции в той же форме, в какой его пишет генератор.
func step(name, method string, path []string, raw string, exec ...string) string {
	var b strings.Builder
	b.WriteString(`{"name":"` + name + `","request":{"method":"` + method + `","url":{"path":[`)
	for i, p := range path {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`"` + p + `"`)
	}
	b.WriteString(`]}`)
	if raw != "" {
		b.WriteString(`,"body":{"mode":"raw","raw":` + jsonQuote(raw) + `}`)
	}
	b.WriteString(`},"event":[{"listen":"test","script":{"exec":[`)
	for i, e := range exec {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(jsonQuote(e))
	}
	b.WriteString(`]}}]}`)
	return b.String()
}

func jsonQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return `"` + r.Replace(s) + `"`
}

func collection(steps ...string) string {
	return `{"item":[{"name":"CASE","item":[` + strings.Join(steps, ",") + `]}]}`
}

const (
	okAssert   = "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));"
	denyAssert = "pm.test('denied', () => pm.expect(pm.response.code).to.be.oneOf([403, 404]));"
	capture    = "if (j.metadata) pm.environment.set('netId', j.metadata.networkId);"
)

var (
	subnetsPath = []string{"vpc", "v1", "subnets"}
	seededBody  = `{"projectId":"{{_suiteProjectId}}","networkId":"{{existingNetworkId}}","name":"s-{{runId}}"}`
	ownBody     = `{"projectId":"{{_suiteProjectId}}","networkId":"{{netId}}","name":"s-{{runId}}"}`
)

// ДЕФЕКТ 1. Ребёнок заведён в посеянном родителе и не снимается — ровно тот
// класс, ради которого гейт написан.
func TestNestedReclaimGateRedsOnSeededParentLeak(t *testing.T) {
	t.Parallel()

	src := collection(
		step("provision-zonal-subnet-setup", "POST", subnetsPath, seededBody, okAssert),
		step("cleanup-del-lb", "DELETE", []string{"nlb", "v1", "networkLoadBalancers", "{{nlbId}}"}, "", okAssert),
	)

	census, findings, err := auditNestedQuotaReclaim(map[string]string{"synth/leak.json": src}, synthEndpoints)
	require.NoError(t, err)

	require.Len(t, findings, 1, "утечка в посеянном родителе обязана быть находкой")
	f := findings[0]
	require.Equal(t, "synth/leak.json", f.File, "находка обязана называть КООРДИНАТУ")
	require.Equal(t, "vpc.network.subnet", f.Kind, "находка обязана называть ВИД")
	require.Equal(t, "existingNetworkId", f.Parent, "находка обязана называть РОДИТЕЛЯ")
	require.Equal(t, 1, f.Created)
	require.Equal(t, 0, f.Deleted)
	require.Contains(t, f.Where, "CASE/provision-zonal-subnet-setup",
		"находка обязана называть КЕЙС и ШАГ: без них координата не приводит к предмету")
	require.Equal(t, 1, census.Creates)
}

// ЗАКОННЫЙ БЛИЗНЕЦ 1 — та же форма, но снятие объявлено. Без этой половины гейт
// ловил бы форму (наличие создания), а не существо (отсутствие снятия).
func TestNestedReclaimGateSilentWhenReclaimDeclared(t *testing.T) {
	t.Parallel()

	src := collection(
		step("provision-zonal-subnet-setup", "POST", subnetsPath, seededBody, okAssert),
		step("cleanup-setup-subnet", "DELETE", append(append([]string{}, subnetsPath...), "{{vpcSubnetId}}"), "",
			"pm.test('subnet reclaim best-effort (never fails the case)', () => "+
				"pm.expect(pm.response.code).to.be.oneOf([200, 400, 403, 404, 405, 409]));"),
	)

	census, findings, err := auditNestedQuotaReclaim(map[string]string{"synth/ok.json": src}, synthEndpoints)
	require.NoError(t, err)
	require.Empty(t, findings, "объявленное снятие снимает находку")
	require.Equal(t, 1, census.Creates)
	require.Equal(t, 1, census.Deletes)
}

// ЗАКОННЫЙ БЛИЗНЕЦ 2 — родитель заведён САМИМ набором. Ребёнок уходит вместе с
// ним, потолка не переживает, и требовать его отдельного снятия значило бы
// краснеть на исправном (реальный случай — `listener.postman_collection.json`:
// 32 создания против 18 снятий при покейсном родителе).
func TestNestedReclaimGateSilentWhenParentIsOwnedBySuite(t *testing.T) {
	t.Parallel()

	src := collection(
		step("cr-net", "POST", []string{"vpc", "v1", "networks"}, `{"name":"n"}`, okAssert, capture),
		step("cr-subnet", "POST", subnetsPath, ownBody, okAssert),
	)

	census, findings, err := auditNestedQuotaReclaim(map[string]string{"synth/own.json": src}, synthEndpoints)
	require.NoError(t, err)
	require.Empty(t, findings, "родитель, заведённый набором, уносит ребёнка с собой")
	require.Equal(t, 0, census.Creates, "такое создание не считается занявшим потолок")
	require.Equal(t, 1, census.OwnPar, "перепись обязана НАЗВАТЬ его, а не молча выбросить")
}

// ЗАКОННЫЙ БЛИЗНЕЦ 3 — создание, утверждающее ОТКАЗ. Ресурса не появляется,
// потолок не занят. Считать его находкой значило бы требовать снятия того, чего
// нет (реальные случаи дерева — `garbage*`-родители).
func TestNestedReclaimGateSilentWhenCreateAssertsRefusal(t *testing.T) {
	t.Parallel()

	src := collection(
		step("cr-subnet-garbage-parent", "POST", subnetsPath,
			`{"networkId":"{{garbageVpcId}}","name":"s"}`, denyAssert),
	)

	census, findings, err := auditNestedQuotaReclaim(map[string]string{"synth/deny.json": src}, synthEndpoints)
	require.NoError(t, err)
	require.Empty(t, findings, "отвергнутое создание потолка не занимает")
	require.Equal(t, 0, census.Creates)
	require.Equal(t, 1, census.Refusals, "перепись обязана НАЗВАТЬ его, а не молча выбросить")
}

// ДЕФЕКТ 2. Снятие адресует БАЗУ, а не экземпляр. Такой шаг ребёнка не снимает,
// и засчитать его значило бы позволить закрыть находку шагом, который ничего не
// удаляет.
func TestNestedReclaimGateRedsWhenDeleteTargetsTheCollection(t *testing.T) {
	t.Parallel()

	src := collection(
		step("provision-subnet", "POST", subnetsPath, seededBody, okAssert),
		step("delete-base", "DELETE", subnetsPath, "", okAssert),
	)

	_, findings, err := auditNestedQuotaReclaim(map[string]string{"synth/base.json": src}, synthEndpoints)
	require.NoError(t, err)
	require.Len(t, findings, 1,
		"DELETE по базе снятием экземпляра не является и находку снимать не вправе")
	require.Equal(t, 0, findings[0].Deleted)
}

// ЗАКОННЫЙ БЛИЗНЕЦ 4 — коллекция без вложенных детей вовсе. Пустой перечень
// находок есть ЦЕЛЬ, а не поломка: падать на достижении собственной цели гейт не
// вправе, иначе он толкал бы держать утечку ради зелёного.
func TestNestedReclaimGateSilentOnACollectionWithoutNestedChildren(t *testing.T) {
	t.Parallel()

	src := collection(
		step("list-lb", "GET", []string{"nlb", "v1", "networkLoadBalancers"}, "", okAssert),
	)

	census, findings, err := auditNestedQuotaReclaim(map[string]string{"synth/none.json": src}, synthEndpoints)
	require.NoError(t, err)
	require.Empty(t, findings)
	require.Equal(t, 1, census.Files, "перепись обязана заявить объём осмотренного")
	require.Equal(t, 1, census.Steps)
}

// synthPathParentEndpoints — вид, у которого родитель назван ПУТЁМ, а у ребёнка
// есть своя под-коллекция (форма `registry.registries.repositories`).
var synthPathParentEndpoints = map[string]nestedChildEndpoint{
	"registry.registries.repositories": {
		Base:           "/registry/v1/registries/{parent}/repositories",
		SubCollections: []string{"tags"},
	},
}

var repoPath = []string{"registry", "v1", "registries", "{{seedRegId}}", "repositories"}

// ДЕФЕКТ 3. Родитель назван ПУТЁМ и посеян, ребёнок не снимается. Без поддержки
// путевого родителя гейт не нашёл бы по этому виду НИЧЕГО и объявил бы «находок 0» —
// то есть «не смотрели», выданное за чистоту.
func TestNestedReclaimGateRedsOnPathNamedSeededParent(t *testing.T) {
	t.Parallel()

	src := collection(
		step("cr-repo", "POST", repoPath, `{"repository":"team/svc-{{runId}}"}`, okAssert),
	)

	_, findings, err := auditNestedQuotaReclaim(
		map[string]string{"synth/repo.json": src}, synthPathParentEndpoints)
	require.NoError(t, err)
	require.Len(t, findings, 1, "родитель из пути обязан читаться так же, как из тела")
	require.Equal(t, "seedRegId", findings[0].Parent)
}

// ЗАКОННЫЙ БЛИЗНЕЦ 5 — снятие ребёнка, чьё имя СОДЕРЖИТ косую черту. Правило «ровно
// один лишний сегмент» объявило бы такой шаг не-снятием и покраснело бы на исправном.
func TestNestedReclaimGateCountsReclaimOfASlashedChildName(t *testing.T) {
	t.Parallel()

	src := collection(
		step("cr-repo", "POST", repoPath, `{"repository":"team/svc-{{runId}}"}`, okAssert),
		step("cleanup-repo", "DELETE", append(append([]string{}, repoPath...), "team", "svc-{{runId}}"), "", okAssert),
	)

	_, findings, err := auditNestedQuotaReclaim(
		map[string]string{"synth/repo-ok.json": src}, synthPathParentEndpoints)
	require.NoError(t, err)
	require.Empty(t, findings, "имя ребёнка с косой чертой не перестаёт быть именем ребёнка")
}

// ДЕФЕКТ 4. Снят ВНУК, а не ребёнок. Засчитать его значило бы закрывать находку
// шагом, который ребёнка не трогает.
func TestNestedReclaimGateRedsWhenOnlyTheGrandchildIsReclaimed(t *testing.T) {
	t.Parallel()

	src := collection(
		step("cr-repo", "POST", repoPath, `{"repository":"app-{{runId}}"}`, okAssert),
		step("cleanup-tag", "DELETE",
			append(append([]string{}, repoPath...), "app-{{runId}}", "tags", "v1"), "", okAssert),
	)

	_, findings, err := auditNestedQuotaReclaim(
		map[string]string{"synth/tag.json": src}, synthPathParentEndpoints)
	require.NoError(t, err)
	require.Len(t, findings, 1, "снятие внука снятием ребёнка не является")
	require.Equal(t, 0, findings[0].Deleted)
}

// ЗАКОННЫЙ БЛИЗНЕЦ 6 — снятие, утверждающее ТОЛЬКО отказ, ничего не удаляет и
// находку не закрывает; а уборка `oneOf([200, …])` — закрывает. Обе половины в
// одной пробе: порознь любая из них зеленела бы на неверном правиле.
func TestNestedReclaimGateCountsBestEffortReclaimButNotARefusedDelete(t *testing.T) {
	t.Parallel()

	refused := collection(
		step("provision-subnet", "POST", subnetsPath, seededBody, okAssert),
		step("del-absent", "DELETE", append(append([]string{}, subnetsPath...), "{{garbageId}}"), "",
			"pm.test('absent', () => pm.expect(pm.response.code).to.eql(404));"),
	)
	_, findings, err := auditNestedQuotaReclaim(map[string]string{"synth/r.json": refused}, synthEndpoints)
	require.NoError(t, err)
	require.Len(t, findings, 1, "снятие, утверждающее только отказ, ничего не удаляет")

	bestEffort := collection(
		step("provision-subnet", "POST", subnetsPath, seededBody, okAssert),
		step("cleanup-setup-subnet", "DELETE", append(append([]string{}, subnetsPath...), "{{vpcSubnetId}}"), "",
			"pm.test('subnet reclaim best-effort (never fails the case)', () => "+
				"pm.expect(pm.response.code).to.be.oneOf([200, 400, 403, 404, 405, 409]));"),
	)
	_, findings, err = auditNestedQuotaReclaim(map[string]string{"synth/b.json": bestEffort}, synthEndpoints)
	require.NoError(t, err)
	require.Empty(t, findings, "уборка best-effort — объявленное снятие: 200 в её перечне есть")
}

// Координата обязана ПРИВОДИТЬ К ПРЕДМЕТУ: генератор зовёт шаги одинаково («post»),
// поэтому имя шага достраивается именем папки-кейса. Без этого перечень
// «post, post, post» не ведёт читателя никуда.
func TestNestedReclaimFindingNamesTheCaseNotJustTheStep(t *testing.T) {
	t.Parallel()

	src := `{"item":[{"name":"VPC-AZD-SUBNET-DENY","item":[` +
		step("post", "POST", subnetsPath, seededBody, okAssert) + `]}]}`

	_, findings, err := auditNestedQuotaReclaim(map[string]string{"synth/case.json": src}, synthEndpoints)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Contains(t, findings[0].Where, "VPC-AZD-SUBNET-DENY/post",
		"координата обязана называть КЕЙС, иначе «post, post, post» не приводит к предмету")
}

// Разбор обязан ОТКАЗАТЬ на негодной коллекции, а не молча счесть её пустой:
// нечитаемый вход, принятый за чистый, — это «ноль прочитанного», выданное за
// «ноль находок».
func TestNestedReclaimGateRefusesUnparsableCollection(t *testing.T) {
	t.Parallel()

	_, _, err := auditNestedQuotaReclaim(map[string]string{"synth/broken.json": "{не json"}, synthEndpoints)
	require.Error(t, err, "негодная коллекция обязана быть отказом, а не пустотой")
	require.Contains(t, err.Error(), "synth/broken.json", "отказ обязан называть координату")
}
