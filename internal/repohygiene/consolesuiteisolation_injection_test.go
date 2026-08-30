// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «проба консоли не пишет в ФС» СПОСОБЕН упасть —
// и что падает он на существе, а не на форме.
//
//	запись через node:fs                → краснеет, называя файл и вызов;
//	чтение дерева через node:fs         → молчит, И ПРИ ЭТОМ счётчик читающих растёт;
//	доменный глагол `rename` без node:fs → молчит (иначе гейт ловит имя, а не предмет);
//	исключение, которому нечего исключать → краснеет само.
//
// Третий случай — тот, ради которого предпосылка «файл импортирует node:fs» вообще
// введена: в консоли `rename`/`truncate` — обычные доменные слова, и гейт по одному
// имени краснел бы на пробах про переименование ресурса. Первый же ложный срабат
// такой гейт отключает.
package repohygiene

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Синтетические исходники. Каждый — настоящая форма из дерева, а не выдумка.
// ─────────────────────────────────────────────────────────────────────────────

// ДЕФЕКТ 1 — запись синхронной формой. Ровно то, чем производитель утечки был
// построен при разборе #461: сосед видит след и падает, если шёл вторым.
const synthProbeWritesSync = `import { writeFileSync } from "node:fs";

it("оставляет след", () => {
  writeFileSync("/tmp/mark", "1");
  expect(true).toBe(true);
});
`

// ДЕФЕКТ 2 — та же запись промисной формой. Без неё побег через `fs/promises`
// оставался бы дырой того же размера.
const synthProbeWritesPromise = `import { mkdir } from "node:fs/promises";

it("готовит каталог", async () => {
  await mkdir("/tmp/out", { recursive: true });
});
`

// ЗАКОННЫЙ БЛИЗНЕЦ 1 — чтение дерева. Оно состояния не несёт и запрещать его нечего;
// таких проб в консоли десятки (сверка с `.proto`, переписи по дереву).
const synthProbeReadsTree = `import { readFileSync } from "node:fs";
import path from "node:path";

const proto = readFileSync(path.resolve("access_binding.proto"), "utf8");

it("контракт несёт поле", () => {
  expect(proto).toContain("scope_type");
});
`

// ЗАКОННЫЙ БЛИЗНЕЦ 3 — имя вызова записи как ХВОСТ чужого идентификатора, в файле,
// который node:fs импортирует законно (ради чтения). Именно на нём гейт краснел:
// сравнение шло подстрокой, и `rm(` находилось внутри `goDeclaredForm(`.
// Предпосылка «файл работает с ФС» этот случай не отсекает by construction.
const synthProbeNameIsATailOfAnIdentifier = `import { readFileSync } from "node:fs";

function goDeclaredForm(relPath: string): string {
  return readFileSync(relPath, "utf8");
}

it("форма имени взята у платформы", () => {
  expect(goDeclaredForm("pkg/validate/nameform/nameform.go")).toContain("a-z0-9");
});
`

// ЗАКОННЫЙ БЛИЗНЕЦ 2 — доменный `rename` без всякого node:fs. Гейт по одному имени
// покраснел бы здесь и был бы снят следующим как мешающий.
const synthProbeDomainRename = `import { render, screen } from "@testing-library/react";
import { rename } from "@shared/api/iam";

it("переименование ресурса не трогает адресацию", async () => {
  await rename("net-1", "новое имя");
  expect(screen.getByText("новое имя")).toBeInTheDocument();
});
`

func TestConsoleFilesystemGateFailsOnWrites(t *testing.T) {
	findings, scanned, fsAware, readers, stale := auditConsoleFilesystemWrites(map[string]string{
		"ui-future/x/src/A.test.ts": synthProbeWritesSync,
		"ui-future/x/src/B.test.ts": synthProbeWritesPromise,
	}, map[string]string{})

	if len(findings) != 2 {
		t.Fatalf("гейт нашёл %d находок вместо 2 — он не краснеет на дефекте, ради которого написан; "+
			"перепись: осмотрено %d, с node:fs %d, читают %d", len(findings), scanned, fsAware, readers)
	}
	if len(stale) != 0 {
		t.Errorf("исключений не подкладывалось, а просроченных насчитано %d", len(stale))
	}
	if findings[0].File != "ui-future/x/src/A.test.ts" || findings[1].File != "ui-future/x/src/B.test.ts" {
		t.Errorf("координаты находок не те и/или не упорядочены: %v", findings)
	}
	if findings[0].Call != "writeFileSync" || findings[1].Call != "mkdir" {
		t.Errorf("находка не называет виновника: %v — по такому сообщению чинить нечего", findings)
	}
}

func TestConsoleFilesystemGateStaysSilentOnReadsAndDomainVerbs(t *testing.T) {
	findings, scanned, fsAware, readers, _ := auditConsoleFilesystemWrites(map[string]string{
		"ui-future/x/src/Contract.test.ts": synthProbeReadsTree,
		"ui-future/x/src/Rename.test.tsx":  synthProbeDomainRename,
		"ui-future/x/src/NameForm.test.ts": synthProbeNameIsATailOfAnIdentifier,
	}, map[string]string{})

	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на законной форме — %v. Первый же ложный срабат его отключит, "+
			"и тогда он не поймает ни настоящей находки", findings)
	}

	// Молчание обязано быть молчанием ПРОЧИТАВШЕГО.
	if scanned != 3 {
		t.Errorf("осмотрено %d файлов вместо 3", scanned)
	}
	if fsAware != 2 {
		t.Errorf("работающими с ФС признаны %d файлов вместо 2 — предпосылка, отсекающая "+
			"доменные глаголы, считает не то", fsAware)
	}
	if readers != 2 {
		t.Errorf("читающими засчитаны %d файлов вместо 2 — дискриминатор не считает законную форму", readers)
	}
}

func TestConsoleFilesystemGateFailsOnAllowanceWithoutSubject(t *testing.T) {
	// Исключение выдано файлу, который ничего не пишет: у послабления не осталось
	// предмета. Оставленное, оно станет слепой зоной для следующей записи.
	findings, _, _, _, stale := auditConsoleFilesystemWrites(map[string]string{
		"ui-future/x/src/Contract.test.ts": synthProbeReadsTree,
	}, map[string]string{
		"ui-future/x/src/Contract.test.ts": "когда-то пересобирал ведомость",
	})

	if len(findings) != 0 {
		t.Errorf("находок быть не должно: файл не пишет — %v", findings)
	}
	if len(stale) != 1 || stale[0] != "ui-future/x/src/Contract.test.ts" {
		t.Fatalf("послабление без предмета не найдено: %v — значит исключение не самоистекает "+
			"и переживёт то, ради чего выдавалось", stale)
	}

	// И обратная сторона: пока предмет есть, послабление молчит.
	findings2, _, _, _, stale2 := auditConsoleFilesystemWrites(map[string]string{
		"ui-future/x/src/A.test.ts": synthProbeWritesSync,
	}, map[string]string{
		"ui-future/x/src/A.test.ts": "пересобирает ведомость под ручкой",
	})
	if len(findings2) != 0 || len(stale2) != 0 {
		t.Errorf("законное послабление не сработало: находок %v, просроченных %v", findings2, stale2)
	}
	if !strings.Contains(synthProbeWritesSync, "writeFileSync") {
		t.Fatal("фикстура перестала нести запись — проба выше проверяет не то, что заявляет")
	}
}
