// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «проба консоли не пишет в ФС» СПОСОБЕН упасть —
// и что падает он на существе, а не на форме.
//
//	запись через node:fs                  → краснеет, называя файл и вызов;
//	запись через объект модуля (`fs.rm(`) → краснеет (точка имена разделяет);
//	чтение дерева через node:fs           → молчит, И ПРИ ЭТОМ счётчик читающих растёт;
//	доменный глагол `rename` без node:fs  → молчит (иначе гейт ловит имя, а не предмет);
//	ХВОСТ чужого имени при живом node:fs  → молчит (`goDeclaredForm(` кончается на `rm(`);
//	исключение, которому нечего исключать → краснеет само.
//
// Четвёртый и пятый случаи — ДВА РАЗНЫХ способа получить ложную находку по одному
// имени, и одного заслона от них не хватает.
//
// Четвёртый закрывает ПРЕДПОСЫЛКА («файл импортирует node:fs»): в консоли
// `rename`/`truncate` — обычные доменные слова, и гейт по одному имени краснел бы
// на пробах про переименование ресурса.
//
// Пятый предпосылкой НЕ закрывается — файл `node:fs` импортирует, только ради
// чтения, — и закрывает его ЦЕЛОСТЬ ИДЕНТИФИКАТОРА. Это и был живой дефект:
// `console-name-form-tracks-platform.test.ts` объявлялся пишущим за хвост
// `goDeclaredForm(`. Первый же ложный срабат такой гейт отключает.
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

// ДЕФЕКТ 3 — запись через объект модуля. В дереве такой формы сегодня нет (замер:
// namespace-импортов `node:fs` среди 442 файлов охвата ноль), и стоит она здесь не
// как образец, а как СТРАЖ ОТ СУЖЕНИЯ: прежний подстрочный предикат её ловил, и
// починка ложной находки не вправе её потерять. Точка имена РАЗДЕЛЯЕТ, поэтому
// `fs.rm(` — это вызов `rm`, а `goDeclaredForm(` — другое имя.
const synthProbeWritesViaNamespace = `import fs from "node:fs";

it("подчищает за собой", () => {
  fs.rm("/tmp/mark", { force: true });
});
`

// ЗАКОННЫЙ БЛИЗНЕЦ 3 — ХВОСТ чужого имени в файле, который `node:fs` импортирует
// (ради чтения) и ничего не пишет. Взято дословно с живого дефекта: имя
// `goDeclaredForm(` кончается на `rm(`, и подстрочный предикат объявлял файл
// пишущим. Предпосылка «файл импортирует node:fs» здесь НЕ спасает — файл её
// удовлетворяет.
const synthProbeIdentifierTail = `import { readFileSync } from "node:fs";

function goDeclaredForm(src: string): string {
  return readFileSync(src, "utf8");
}

it("форма имени совпадает с платформенной", () => {
  expect(goDeclaredForm("pkg/validate/name.go")).toContain("a-z0-9");
});
`

func TestConsoleFilesystemGateFailsOnWrites(t *testing.T) {
	findings, scanned, fsAware, readers, tailRejected, stale := auditConsoleFilesystemWrites(map[string]string{
		"ui-future/x/src/A.test.ts": synthProbeWritesSync,
		"ui-future/x/src/B.test.ts": synthProbeWritesPromise,
	}, map[string]string{})

	// Контроль соседнего утверждения: на этих фикстурах хвостов чужих имён нет
	// вовсе, значит краснота ниже приходит от записи, а не от различителя.
	if tailRejected != 0 {
		t.Errorf("отвергнуто хвостов %d вместо 0 — фикстура несёт не то, что заявляет", tailRejected)
	}

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
	findings, scanned, fsAware, readers, _, _ := auditConsoleFilesystemWrites(map[string]string{
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
	findings, _, _, _, _, stale := auditConsoleFilesystemWrites(map[string]string{
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
	findings2, _, _, _, _, stale2 := auditConsoleFilesystemWrites(map[string]string{
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

// TestConsoleFilesystemGateSeparatesIdentifierTailFromCall — ось, которой гейту
// не хватало: имя записи, совпавшее ХВОСТОМ чужого идентификатора, при живом
// импорте `node:fs`.
//
// Пара неделима. Одно отрицание («хвост молчит») зеленело бы и на гейте, который
// не находит вообще ничего, поэтому рядом стоит положительная половина на том же
// имени `rm` — и обе в одном прогоне.
func TestConsoleFilesystemGateSeparatesIdentifierTailFromCall(t *testing.T) {
	// (а) ОТРИЦАНИЕ — хвост чужого имени. Файл `node:fs` импортирует, значит
	// предпосылка его не отсекает: молчание обязано прийти от целости имени.
	findings, _, fsAware, readers, tailRejected, _ := auditConsoleFilesystemWrites(map[string]string{
		"ui-future/shared/src/test/tail.test.ts": synthProbeIdentifierTail,
	}, map[string]string{})

	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на ХВОСТЕ чужого имени — %v. Это живой дефект: "+
			"`goDeclaredForm(` кончается на `rm(`, файл читает дерево и не пишет ничего. "+
			"Ложная находка отключает гейт, а с ним и защиту настоящего свойства", findings)
	}
	if fsAware != 1 {
		t.Errorf("файл признан работающим с ФС %d раз вместо 1 — предпосылка не выполнена, "+
			"и молчание выше пришло бы от неё, а не от целости имени", fsAware)
	}
	if readers != 1 {
		t.Errorf("читающих засчитано %d вместо 1 — молчание выше неотличимо от «не прочитал»", readers)
	}
	if tailRejected != 1 {
		t.Errorf("хвостов отвергнуто %d вместо 1 — различитель не сработал ни разу, "+
			"значит молчание выше получено даром", tailRejected)
	}

	// (б) ПОЛОЖИТЕЛЬНАЯ ПОЛОВИНА на том же имени: доступ через объект модуля —
	// законная форма записи, и она обязана краснеть. Точка имена разделяет.
	findings, _, _, _, tailRejected, _ = auditConsoleFilesystemWrites(map[string]string{
		"ui-future/x/src/Ns.test.ts": synthProbeWritesViaNamespace,
	}, map[string]string{})

	if len(findings) != 1 || findings[0].Call != "rm" {
		t.Fatalf("запись `fs.rm(` не найдена: %v — починка ложной находки сузила гейт "+
			"вместо того, чтобы его исправить", findings)
	}
	if tailRejected != 0 {
		t.Errorf("отвергнуто хвостов %d вместо 0 — настоящий вызов принят за хвост", tailRejected)
	}

	// (в) ОБЕ ФОРМЫ В ОДНОМ ПРОГОНЕ: находка обязана указать на пишущий файл, а
	// не на читающий. Иначе координата отправит чинить не туда.
	findings, _, _, _, tailRejected, _ = auditConsoleFilesystemWrites(map[string]string{
		"ui-future/shared/src/test/tail.test.ts": synthProbeIdentifierTail,
		"ui-future/x/src/Ns.test.ts":             synthProbeWritesViaNamespace,
	}, map[string]string{})

	if len(findings) != 1 {
		t.Fatalf("на смеси хвоста и настоящего вызова находок %d вместо 1: %v", len(findings), findings)
	}
	if findings[0].File != "ui-future/x/src/Ns.test.ts" {
		t.Errorf("находка называет %s — это файл, который ничего не пишет; "+
			"по такой координате чинят не то", findings[0].File)
	}
	if tailRejected != 1 {
		t.Errorf("хвостов отвергнуто %d вместо 1", tailRejected)
	}
}
