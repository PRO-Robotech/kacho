// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «идентификатор из metadata подтверждается
// чтением ресурса» СПОСОБЕН упасть — и что падает он на существе, а не на форме.
//
// Инъекция идёт в обе стороны:
//
//	чтение без подтверждения        → находка с координатой и именем;
//	подтверждение ВЫШЕ чтения       → находка: адрес, прочитанный до мутации,
//	                                  о свежем идентификаторе не говорит ничего;
//	подтверждение в ЧУЖОМ блоке     → находка: за границей области видимости то
//	                                  же имя принадлежит другой переменной;
//	каждый помощник подтверждает
//	свой, имена совпадают           → молчание — иначе гейт ловит совпадение
//	                                  имён, а не отсутствие подтверждения;
//	подтверждение из вложенного
//	блока (`expect.poll`)           → молчание: вложенный блок области не
//	                                  покидает — это живая форма дерева;
//	объявление через `var`          → предпосылка, а не находка кода: область у
//	                                  него шире блока, и перепись это называет;
//	подтверждение чужого имени      → находка, и подстрока чужим именем НЕ
//	                                  считается (`id` внутри `projectId`);
//	подтверждение закомментировано  → находка: рассуждение о проверке проверкой
//	                                  не является;
//	подстановка `${имя}` в адрес    → молчание;
//	построитель `addressOf(имя)`    → молчание (обе живые формы дерева);
//	ключ вычисляемый, `metadata?.[]`→ разбирается: без подтверждения находка,
//	                                  с подтверждением молчание;
//	доступ без `?.`                 → разбирается так же;
//	разбор объекта `const { x } =`  → разбирается так же;
//	слово в комментарии и в строке  → не предмет: перепись его НЕ считает;
//	файл без мутаций вовсе          → молчание при нулевой переписи.
//
// Все случаи гоняют ТУ ЖЕ функцию (`auditConsoleProbeOperationIDs`), что и прогон
// по дереву.
package repohygiene

import "testing"

// ДЕФЕКТ. Каркас взят у пробы потока: идентификатор берётся и немедленно
// уезжает в ожидание события — то есть в шаг, который обвинит невиновного.
const synthProbeUnconfirmedID = `import { expect } from "@playwright/test";
import { test } from "../specs/fixtures";

async function createPlacementGroup(page, projectId, name) {
  const res = await page.request.post("/compute/v1/placementGroups", { data: { projectId, name } });
  const body = JSON.parse(await res.text());
  const id = body.metadata?.placementGroupId ?? "";
  return id;
}

test("страница узнаёт об изменении", async ({ page }) => {
  const created = await createPlacementGroup(page, "prj", "pg");
  await expect.poll(async () => frames(page)).toContain(created);
});
`

// ЗАКОННЫЙ БЛИЗНЕЦ 1 — подстановка имени в адрес: форма `stepup.spec.ts`.
const synthProbeConfirmedByTemplate = `import { expect } from "@playwright/test";

test("группа заводится", async ({ page }) => {
  const created = await page.request.post("/iam/v1/groups", { data: { name: "g" } });
  const op = JSON.parse(await created.text());
  const groupId = op.metadata?.groupId ?? "";
  await expect
    .poll(async () => (await page.request.get(` + "`/iam/v1/groups/${groupId}`" + `)).status())
    .toBe(200);
});
`

// ЗАКОННЫЙ БЛИЗНЕЦ 2 — построитель адреса: форма `fixtures.ts`.
const synthProbeConfirmedByBuilder = `export async function createdResourceId(page, response, metadataField, addressOf, subject) {
  const body = JSON.parse(await response.text());
  const id = body.metadata?.[metadataField] ?? "";
  await expect
    .poll(async () => (await page.request.get(addressOf(id))).status())
    .toBe(200);
  return id;
}
`

// ДЕФЕКТ. Подтверждение есть, но стоит ВЫШЕ чтения: оно относится к прошлому
// состоянию и о свежем идентификаторе не говорит ничего.
const synthProbeConfirmBeforeRead = `test("порядок", async ({ page }) => {
  const stale = await page.request.get(` + "`/compute/v1/placementGroups/${id}`" + `);
  const created = await page.request.post("/compute/v1/placementGroups", { data: {} });
  const body = JSON.parse(await created.text());
  const id = body.metadata?.placementGroupId ?? "";
  await use(id);
});
`

// ДЕФЕКТ. Подтверждается ЧУЖОЕ имя, и притом такое, внутри которого имя нашего
// идентификатора лежит подстрокой: разбор по подстроке приписал бы подтверждение.
const synthProbeConfirmsAnotherName = `test("чужое имя", async ({ page }) => {
  const created = await page.request.post("/vpc/v1/networks", { data: {} });
  const body = JSON.parse(await created.text());
  const id = body.metadata?.networkId ?? "";
  await page.request.get(` + "`/vpc/v1/networks?projectId=${projectId}`" + `);
  await use(id);
});
`

// ДЕФЕКТ. Подтверждение закомментировано: рассуждение о проверке проверкой не
// является — тот самый класс, ради которого гейт читает исполняемую часть.
const synthProbeConfirmCommentedOut = `test("комментарий вместо проверки", async ({ page }) => {
  const created = await page.request.post("/vpc/v1/networks", { data: {} });
  const body = JSON.parse(await created.text());
  const id = body.metadata?.networkId ?? "";
  // здесь раньше стояло: await page.request.get(` + "`/vpc/v1/networks/${id}`" + `)
  await use(id);
});
`

// ДЕФЕКТ. Доступ без защиты от отсутствия — та же форма записи предмета.
const synthProbeUnprotectedAccess = `test("без вопросительного знака", async ({ page }) => {
  const created = await page.request.post("/vpc/v1/networks", { data: {} });
  const op = JSON.parse(await created.text());
  const netId = op.metadata.networkId;
  await use(netId);
});
`

// ДЕФЕКТ. Разбор объекта: в дереве этой формы сегодня нет, и потому она особенно
// опасна — появится молча.
const synthProbeDestructured = `test("разбор объекта", async ({ page }) => {
  const created = await page.request.post("/vpc/v1/networks", { data: {} });
  const body = JSON.parse(await created.text());
  const { networkId } = body.metadata ?? {};
  await use(networkId);
});
`

// ЗАКОННЫЙ БЛИЗНЕЦ 3 — ни одной мутации: слово «metadata» встречается в
// комментарии и в строке. Перепись обязана показать НОЛЬ чтений, а не находку.
const synthProbeNoMutationAtAll = `test("чтение списка", async ({ page }) => {
  // идентификатор из metadata здесь не берётся: проба ничего не создаёт
  const res = await page.request.get("/vpc/v1/networks");
  expect(await res.text()).not.toContain("metadata?.networkId");
});
`

func TestConsoleProbeOperationIDSeparatesConfirmedFromPhantom(t *testing.T) {
	// ── ДЕФЕКТ: идентификатор не подтверждён ─────────────────────────────────
	census, findings := auditConsoleProbeOperationIDs(map[string]string{
		"specs-awaiting/subscription-stream.spec.ts": synthProbeUnconfirmedID,
	})
	if len(findings) != 1 {
		t.Fatalf("гейт не увидел неподтверждённого идентификатора: находок %d (%v). "+
			"Гейт, молчащий на своём предмете, хуже отсутствующего", len(findings), findings)
	}
	if findings[0].Name != "id" || findings[0].File != "specs-awaiting/subscription-stream.spec.ts" {
		t.Errorf("находка не называет предмет: %v — читатель пойдёт искать не там", findings[0])
	}
	if findings[0].Line != 7 {
		t.Errorf("находка называет строку %d вместо 7: координата обязана вести к самому чтению",
			findings[0].Line)
	}
	if census.Reads != 1 || census.Confirmed != 0 {
		t.Errorf("перепись не сходится: чтений %d, подтверждено %d", census.Reads, census.Confirmed)
	}

	// ── ЗАКОННЫЕ БЛИЗНЕЦЫ: обе живые формы подтверждения ────────────────────
	for name, src := range map[string]string{
		"подстановка имени в адрес": synthProbeConfirmedByTemplate,
		"построитель адреса":        synthProbeConfirmedByBuilder,
	} {
		census, findings := auditConsoleProbeOperationIDs(map[string]string{"specs/x.spec.ts": src})
		if len(findings) != 0 {
			t.Errorf("%s — гейт краснеет на исправном: %v. Гейт, краснеющий на верном коде, "+
				"отключают первым", name, findings)
		}
		if census.Reads != 1 || census.Confirmed != 1 {
			t.Errorf("%s — молчание получено даром: чтений %d, подтверждено %d. "+
				"Гейт мог просто не увидеть предмета", name, census.Reads, census.Confirmed)
		}
	}
}

func TestConsoleProbeOperationIDJudgesSubstanceNotShape(t *testing.T) {
	cases := map[string]struct {
		src  string
		want int
	}{
		"подтверждение выше чтения":      {synthProbeConfirmBeforeRead, 1},
		"подтверждается чужое имя":       {synthProbeConfirmsAnotherName, 1},
		"подтверждение закомментировано": {synthProbeConfirmCommentedOut, 1},
		"доступ без вопросительного":     {synthProbeUnprotectedAccess, 1},
		"разбор объекта":                 {synthProbeDestructured, 1},
	}
	for name, c := range cases {
		census, findings := auditConsoleProbeOperationIDs(map[string]string{"specs/x.spec.ts": c.src})
		if len(findings) != c.want {
			t.Errorf("%s — находок %d, ожидалось %d (%v); перепись: чтений %d, подтверждено %d",
				name, len(findings), c.want, findings, census.Reads, census.Confirmed)
		}
	}
}

func TestConsoleProbeOperationIDCountsOnlyWhatCodeDoes(t *testing.T) {
	census, findings := auditConsoleProbeOperationIDs(map[string]string{
		"specs/listing.spec.ts": synthProbeNoMutationAtAll,
	})
	if len(findings) != 0 {
		t.Errorf("слово в комментарии и в строке принято за чтение идентификатора: %v. "+
			"Гейт обязан судить по тому, что код ДЕЛАЕТ, а не по тому, о чём он говорит", findings)
	}
	if census.Reads != 0 {
		t.Errorf("перепись насчитала %d чтений там, где мутации нет вовсе: "+
			"число, полученное из поломки разбора, подтверждало бы его работу", census.Reads)
	}
	if census.Files != 1 {
		t.Errorf("перепись прочитанного не ведётся: файлов %d", census.Files)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ОБЛАСТЬ ВИДИМОСТИ. Сопоставление «чтение ↔ подтверждение» по одному имени в
// пределах ФАЙЛА маскирует предмет: два помощника, каждый со своей переменной
// `id`, называют одно и то же имя, и подтверждение второго закрывает
// неподтверждённое чтение первого. Имена совпадают, значения — разные.
// ─────────────────────────────────────────────────────────────────────────────

// ДЕФЕКТ. Первый помощник идентификатор не подтверждает; второй подтверждает
// СВОЙ — переменной с тем же именем и НИЖЕ по файлу.
const synthProbeMaskedByAnotherScope = `import { expect } from "@playwright/test";

async function createPlacementGroup(page, projectId, name) {
  const res = await page.request.post("/compute/v1/placementGroups", { data: { projectId, name } });
  const body = JSON.parse(await res.text());
  const id = body.metadata?.placementGroupId ?? "";
  return id;
}

async function createSomethingElse(page, projectId) {
  const res = await page.request.post("/vpc/v1/networks", { data: { projectId } });
  const body = JSON.parse(await res.text());
  const id = body.metadata?.networkId ?? "";
  await expect.poll(async () => (await page.request.get(` + "`/vpc/v1/networks/${id}`" + `)).status()).toBe(200);
  return id;
}
`

// ЗАКОННЫЙ БЛИЗНЕЦ. Тот же файл с двумя помощниками и тем же именем `id` в
// обоих — но КАЖДЫЙ подтверждает свой. Гейт обязан молчать: иначе он ловит
// совпадение имён, а не отсутствие подтверждения.
const synthProbeTwoScopesBothConfirmed = `import { expect } from "@playwright/test";

async function createPlacementGroup(page, projectId, name) {
  const res = await page.request.post("/compute/v1/placementGroups", { data: { projectId, name } });
  const body = JSON.parse(await res.text());
  const id = body.metadata?.placementGroupId ?? "";
  await expect.poll(async () => (await page.request.get(` + "`/compute/v1/placementGroups/${id}`" + `)).status()).toBe(200);
  return id;
}

async function createSomethingElse(page, projectId) {
  const res = await page.request.post("/vpc/v1/networks", { data: { projectId } });
  const body = JSON.parse(await res.text());
  const id = body.metadata?.networkId ?? "";
  await expect.poll(async () => (await page.request.get(` + "`/vpc/v1/networks/${id}`" + `)).status()).toBe(200);
  return id;
}
`

// ДЕФЕКТ. Тот же класс на канонической форме дерева: неподтверждённое чтение
// стоит ВЫШЕ помощника, который подтверждает свой идентификатор построителем
// адреса и называет переменную тем же именем.
const synthProbeMaskedByCanonicalHelper = `async function unconfirmedThing(page, response) {
  const body = JSON.parse(await response.text());
  const id = body.metadata?.thingId ?? "";
  return id;
}

export async function createdResourceId(page, response, metadataField, addressOf, subject) {
  const body = JSON.parse(await response.text());
  const id = body.metadata?.[metadataField] ?? "";
  await expect.poll(async () => (await page.request.get(addressOf(id))).status()).toBe(200);
  return id;
}
`

// ЗАКОННЫЙ БЛИЗНЕЦ. Подтверждение стоит ГЛУБЖЕ объявления — внутри стрелочной
// функции, вложенной в тот же блок. Это живая форма дерева (`expect.poll`), и
// оценка по блоку обязана её принимать: вложенный блок области не покидает.
const synthProbeConfirmedFromNestedBlock = `export async function createdResourceId(page, response, metadataField, addressOf) {
  const body = JSON.parse(await response.text());
  const id = body.metadata?.[metadataField] ?? "";
  if (true) {
    await expect.poll(async () => (await page.request.get(addressOf(id))).status()).toBe(200);
  }
  return id;
}
`

// ПРЕДПОСЫЛКА. Оценка по блоку верна для `const`/`let`; у `var` область
// видимости шире блока, и та же оценка была бы УЖЕ языка — то есть давала бы
// находку на исправном коде. В пакете `var` сегодня нет (перепись — ноль), и
// гейт обязан объявить это сам, а не молча судить по более узкому правилу.
const synthProbeVarDeclaration = `async function outer(page, response) {
  {
    var id = (JSON.parse(await response.text())).metadata?.thingId ?? "";
  }
  await expect.poll(async () => (await page.request.get(` + "`/vpc/v1/networks/${id}`" + `)).status()).toBe(200);
  return id;
}
`

func TestConsoleProbeOperationIDRespectsScopeNotJustFileOrder(t *testing.T) {
	// ── ДЕФЕКТ: подтверждение живёт в ЧУЖОЙ области видимости ───────────────
	for name, src := range map[string]string{
		"чужой помощник ниже по файлу": synthProbeMaskedByAnotherScope,
		"канонический помощник ниже":   synthProbeMaskedByCanonicalHelper,
	} {
		census, findings := auditConsoleProbeOperationIDs(map[string]string{"specs/x.spec.ts": src})
		if len(findings) != 1 {
			t.Errorf("%s — находок %d, ожидалась 1 (%v). Совпадение ИМЕНИ переменной в чужой "+
				"области видимости подтверждением не является: значения разные, "+
				"а неподтверждённое чтение объявлено подтверждённым; перепись: чтений %d, подтверждено %d",
				name, len(findings), findings, census.Reads, census.Confirmed)
			continue
		}
		if findings[0].Name != "id" {
			t.Errorf("%s — находка называет %q вместо \"id\": читатель пойдёт искать не там",
				name, findings[0].Name)
		}
	}

	// ── ЗАКОННЫЕ БЛИЗНЕЦЫ: молчание обязано быть заработанным ───────────────
	for name, c := range map[string]struct {
		src   string
		reads int
	}{
		"каждый помощник подтверждает свой": {synthProbeTwoScopesBothConfirmed, 2},
		"подтверждение из вложенного блока": {synthProbeConfirmedFromNestedBlock, 1},
	} {
		census, findings := auditConsoleProbeOperationIDs(map[string]string{"specs/x.spec.ts": c.src})
		if len(findings) != 0 {
			t.Errorf("%s — гейт краснеет на исправном: %v. Гейт, краснеющий на верном коде, "+
				"отключают первым", name, findings)
		}
		if census.Reads != c.reads || census.Confirmed != c.reads {
			t.Errorf("%s — молчание получено даром: чтений %d (ожидалось %d), подтверждено %d",
				name, census.Reads, c.reads, census.Confirmed)
		}
	}
}

func TestConsoleProbeOperationIDDeclaresItsOwnPremiseAboutVar(t *testing.T) {
	census, findings := auditConsoleProbeOperationIDs(map[string]string{"specs/x.spec.ts": synthProbeVarDeclaration})
	if census.WiderThanBlock != 1 {
		t.Errorf("объявление через `var` не отмечено: WiderThanBlock=%d. Оценка по блоку для него "+
			"УЖЕ языка, и молча судить по ней значит давать находку на исправном коде",
			census.WiderThanBlock)
	}
	if len(findings) != 0 {
		t.Errorf("`var` выдан за находку кода: %v. Предпосылка гейта — не дефект пробы, "+
			"и путать их нельзя", findings)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СЧИТАЕТСЯ ЧТЕНИЕМ РЕСУРСА. Идентификатор в СТРОКЕ ЗАПРОСА адреса
// существования ресурса не доказывает: список, отфильтрованный по
// несуществующему владельцу, отвечает `200` и пустым массивом. Подтверждает
// только чтение по СОБСТВЕННОМУ адресу — то есть упоминание в ПУТИ.
// ─────────────────────────────────────────────────────────────────────────────

// ДЕФЕКТ. Подтверждения нет: идентификатор стоит фильтром списка.
const synthProbeConfirmedByQueryFilterOnly = `test("список вместо ресурса", async ({ page }) => {
  const created = await page.request.post("/iam/v1/accounts", { data: {} });
  const body = JSON.parse(await created.text());
  const projectId = body.metadata?.defaultProjectId ?? "";
  const res = await page.request.get(` + "`/vpc/v1/networks?projectId=${projectId}`" + `);
  expect(res.status()).toBe(200);
});
`

// ЗАКОННЫЙ БЛИЗНЕЦ. Тот же идентификатор, тот же вопросительный знак в адресе —
// но упомянут он в ПУТИ, а строка запроса несёт постороннее. Гейт обязан
// молчать: иначе он ловит наличие `?`, а не место упоминания.
const synthProbeConfirmedByPathWithQuery = `test("путь с параметрами", async ({ page }) => {
  const created = await page.request.post("/iam/v1/accounts", { data: {} });
  const body = JSON.parse(await created.text());
  const projectId = body.metadata?.defaultProjectId ?? "";
  const res = await page.request.get(` + "`/iam/v1/projects/${projectId}?view=FULL`" + `);
  expect(res.status()).toBe(200);
});
`

// ЗАКОННЫЙ БЛИЗНЕЦ. Построитель адреса: литерала в аргументе нет вовсе, значит
// нет и строки запроса, по которой можно было бы судить. Живая форма дерева.
const synthProbeConfirmedByBuilderWithQuestionMarkNearby = `export async function createdResourceId(page, response, metadataField, addressOf) {
  const body = JSON.parse(await response.text());
  const id = body.metadata?.[metadataField] ?? "";
  await expect.poll(async () => (await page.request.get(addressOf(id))).status()).toBe(200);
  return id;
}
`

// ДЕФЕКТ. Чужой ресурс читается по своему адресу, а наш идентификатор стоит в
// строке запроса того же чтения: подтверждён подсеть, а не проект.
const synthProbeQueryAlongsideForeignPath = `test("чужой путь, наш фильтр", async ({ page }) => {
  const created = await page.request.post("/iam/v1/accounts", { data: {} });
  const body = JSON.parse(await created.text());
  const projectId = body.metadata?.defaultProjectId ?? "";
  const res = await page.request.get(` + "`/vpc/v1/subnets/${subnetId}?projectId=${projectId}`" + `);
  expect(res.status()).toBe(200);
});
`

func TestConsoleProbeOperationIDDoesNotCountAListFilterAsAResourceRead(t *testing.T) {
	// ── ДЕФЕКТ: идентификатор стоит фильтром, а не адресом ──────────────────
	for name, src := range map[string]string{
		"фильтр списка":            synthProbeConfirmedByQueryFilterOnly,
		"фильтр при чужом ресурсе": synthProbeQueryAlongsideForeignPath,
	} {
		census, findings := auditConsoleProbeOperationIDs(map[string]string{"specs/x.spec.ts": src})
		if len(findings) != 1 {
			t.Errorf("%s — находок %d, ожидалась 1 (%v). Список, отфильтрованный по "+
				"несуществующему идентификатору, отвечает `200` и пустым массивом: "+
				"существования ресурса такое чтение не доказывает; перепись: чтений %d, подтверждено %d",
				name, len(findings), findings, census.Reads, census.Confirmed)
			continue
		}
		if findings[0].Name != "projectId" {
			t.Errorf("%s — находка называет %q вместо \"projectId\"", name, findings[0].Name)
		}
	}

	// ── ЗАКОННЫЕ БЛИЗНЕЦЫ: упоминание в ПУТИ остаётся подтверждением ───────
	for name, src := range map[string]string{
		"путь, а за ним строка запроса": synthProbeConfirmedByPathWithQuery,
		"построитель адреса":            synthProbeConfirmedByBuilderWithQuestionMarkNearby,
	} {
		census, findings := auditConsoleProbeOperationIDs(map[string]string{"specs/x.spec.ts": src})
		if len(findings) != 0 {
			t.Errorf("%s — гейт краснеет на исправном: %v. Гейт, краснеющий на верном коде, "+
				"отключают первым", name, findings)
		}
		if census.Reads != 1 || census.Confirmed != 1 {
			t.Errorf("%s — молчание получено даром: чтений %d, подтверждено %d",
				name, census.Reads, census.Confirmed)
		}
	}
}
