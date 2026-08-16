// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «подмена модуля добывает замену статически»
// СПОСОБЕН упасть — и что падает он на существе, а не на форме.
//
// Инъекция идёт в обе стороны, потому что одного «краснеет» мало: гейт,
// краснеющий на всякой подмене, ничего не измеряет, а гейт, молчащий на всём,
// не измеряет тем более.
//
//	дефект (динамический импорт в фабрике) → краснеет, называя координату;
//	законный близнец (синхронная фабрика)  → молчит, И ПРИ ЭТОМ перепись растёт.
//
// Второе условие — «перепись растёт» — не украшение: молчание бывает от того,
// что разбор ничего не прочитал. Близнец обязан быть ПОСЧИТАН как годный вызов,
// а не пропущен.
//
// Обе половины гоняют ТУ ЖЕ функцию (`auditModuleMockFactories`), что и обход
// дерева: проба, повторяющая логику гейта своей копией, доказывала бы свойство
// копии.
package repohygiene

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Синтетические исходники. Каждый — настоящая форма из дерева, а не выдумка.
// ─────────────────────────────────────────────────────────────────────────────

// ДЕФЕКТ. Ровно тот образец, что размножился копированием по пяти суитам iam:
// замена добывается динамическим импортом внутри асинхронной фабрики.
const synthMockDynamicFactory = `import { jest } from "@jest/globals";
import { render } from "@testing-library/react";

jest.unstable_mockModule("antd", async () => (await import("@/test/antd-double")).antdDouble);

describe("панель", () => {
  it("рисует строку", async () => {
    const { Panel } = await import("./Panel");
    render(<Panel />);
  });
});
`

// ДЕФЕКТ второго вида — тот же импорт, но записанный в теле фабрики со скобками.
// Разбор обязан досчитать скобки до конца вызова: обрыв по первой закрывающей
// отрезал бы фабрику целиком, и гейт молчал бы на ВСЁМ, ни разу об этом не сказав.
const synthMockDynamicFactoryBlock = `import { jest } from "@jest/globals";

jest.unstable_mockModule("@monaco-editor/react", async () => {
  const real = await import("./editor-double");
  return { __esModule: true, default: real.Editor, loader: { config: () => undefined } };
});
`

// ЗАКОННЫЙ БЛИЗНЕЦ 1 — преобладающая в дереве форма: замена внесена статическим
// импортом, фабрика синхронна. Ровно то, к чему гейт и подталкивает.
const synthMockStaticFactory = `import { jest } from "@jest/globals";
import { antdStub } from "./antd-stub";

jest.unstable_mockModule("antd", () => antdStub());
`

// ЗАКОННЫЙ БЛИЗНЕЦ 2 — подмена, объявленная объектом на месте. Скобок в ней
// столько же, а импорта нет: гейт обязан её пропустить.
const synthMockInlineObject = `import { jest } from "@jest/globals";

jest.unstable_mockModule("@shared/api/iam", () => ({
  IAM: { users: "/iam/v1/users" },
  iamApi: { listUsers: jest.fn(() => Promise.resolve({ users: [] })) },
}));
`

// ЗАКОННЫЙ БЛИЗНЕЦ 3 — слово `import(` стоит В КОММЕНТАРИИ, объясняющем запрет.
// Гейт, читающий текст вместо исполняемой части, покраснел бы на разборе
// собственного правила и был бы снят следующим как непонятный.
const synthMockCommentMentionsImport = `import { jest } from "@jest/globals";
import { antdDouble } from "@/test/antd-double";

// Раньше здесь стояло async () => (await import("@/test/antd-double")).antdDouble —
// фабрика добывала замену динамическим импортом, и суита падала через раз.
jest.unstable_mockModule("antd", () => antdDouble);
`

func TestModuleMockFactoryGateFailsOnDynamicResolution(t *testing.T) {
	findings, calls, static := auditModuleMockFactories(map[string]string{
		"ui-future/x/src/Panel.test.tsx":  synthMockDynamicFactory,
		"ui-future/x/src/Editor.test.tsx": synthMockDynamicFactoryBlock,
	})

	if len(findings) != 2 {
		t.Fatalf("гейт нашёл %d находок вместо 2 — он не краснеет на дефекте, ради которого написан; "+
			"перепись: вызовов %d, статических %d", len(findings), calls, static)
	}
	if calls != 2 || static != 0 {
		t.Errorf("перепись разошлась с подложенным: вызовов %d (ждём 2), статических %d (ждём 0)", calls, static)
	}

	var files []string
	for _, f := range findings {
		files = append(files, f.File)
		if !strings.Contains(f.Call, "import(") {
			t.Errorf("находка %s не называет виновника: в тексте вызова нет `import(` — "+
				"по такому сообщению чинить нечего", f.File)
		}
	}
	if files[0] != "ui-future/x/src/Editor.test.tsx" || files[1] != "ui-future/x/src/Panel.test.tsx" {
		t.Errorf("координаты находок не те и/или не упорядочены: %v", files)
	}
}

func TestModuleMockFactoryGateStaysSilentOnStaticResolution(t *testing.T) {
	findings, calls, static := auditModuleMockFactories(map[string]string{
		"ui-future/x/src/test/setup.ts":    synthMockStaticFactory,
		"ui-future/x/src/Users.test.tsx":   synthMockInlineObject,
		"ui-future/x/src/Comment.test.tsx": synthMockCommentMentionsImport,
	})

	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на законной форме — %v. Первый же ложный срабат его отключит, "+
			"и тогда он не поймает ни настоящей находки", findings)
	}

	// Молчание обязано быть молчанием ПРОЧИТАВШЕГО. Без этой проверки гейт со
	// сломанным разбором выглядел бы точно так же.
	if calls != 3 {
		t.Errorf("осмотрено %d вызовов подмены вместо 3 — молчание выше означает «не прочитал», "+
			"а не «чисто»", calls)
	}
	if static != 3 {
		t.Errorf("годными засчитаны %d вызовов вместо 3 — дискриминатор не считает законную форму", static)
	}
}
