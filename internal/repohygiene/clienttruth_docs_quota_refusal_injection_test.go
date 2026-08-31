// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Инъекция для гейта описанного отказа по исчерпанию предела — В ОБЕ СТОРОНЫ.
//
// Дефекты возвращаются НАСТОЯЩИЕ — те самые, которые закрыл коммит «отказ по
// исчерпанию квоты описан, поверхность чтения названа»: отсутствующая строка
// таблицы кодов, неназванный путь чтения, и обещание отказа на сайте домена,
// который его не производит.
//
// Отдельная ось — ЛОВУШКА, ради которой гейт и написан: ключ остаётся в словаре
// `codes.ts`, но выпадает из массива `codes={[…]}`. Наивный гейт, ищущий литерал
// `RESOURCE_EXHAUSTED` в `.mdx` либо просто ключ в словаре, остаётся зелёным, а
// таблица строку не рендерит. Без этой пробы нельзя утверждать, что гейт не наивен.
//
// Каждая проба меняет ОДНО против контроля (п.2в §«Гейт на класс»): инъекция,
// попутно ломающая соседнюю полосу, дала бы красное от соседа и не доказала бы
// ничего о проверяемой.

// docsQuotaFixture — синтетическое дерево двух владельцев (разные формы подачи)
// и одного домена без учёта.
type docsQuotaFixture struct {
	vpcCodesArray  []string // массив codes={…} на странице обзора vpc
	vpcDictRefusal bool     // словарь vpc объявляет запись с grpc RESOURCE_EXHAUSTED
	vpcPathShown   bool     // страница vpc называет путь чтения пределов
	iamLiteralRow  bool     // таблица iam несёт строку отказа литералом
	geoPromises    bool     // сайт домена БЕЗ учёта обещает отказ
}

// docsQuotaControlFixture — исправное дерево: обе формы подачи целы, пути названы,
// домен без учёта молчит.
func docsQuotaControlFixture() docsQuotaFixture {
	return docsQuotaFixture{
		vpcCodesArray:  []string{"invalidArgument", "notFound", "resourceExhausted", "internal"},
		vpcDictRefusal: true,
		vpcPathShown:   true,
		iamLiteralRow:  true,
	}
}

func writeDocsQuotaTree(t *testing.T, f docsQuotaFixture) string {
	t.Helper()
	root := t.TempDir()
	mk := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	// Контракты. Каталог iam называется `quota`, а не `iam`: соответствие
	// «домен → путь» берётся из ПЕРВОГО СЕГМЕНТА пути, а не из имени каталога,
	// и эта фикстура доказывает, что оно берётся именно так.
	mk("proto/kacho/cloud/vpc/v1/quota_service.proto", `
syntax = "proto3";
service QuotaService {
  rpc List(ListQuotasRequest) returns (ListQuotasResponse) {
    option (google.api.http) = { get: "/vpc/v1/quotas" };
  }
}
`)
	mk("proto/kacho/cloud/quota/v1/identity_quota_service.proto", `
syntax = "proto3";
service IdentityQuotaService {
  rpc List(ListIdentityQuotasRequest) returns (ListIdentityQuotasResponse) {
    option (google.api.http) = { get: "/iam/v1/quotas" };
  }
}
`)

	// vpc — КОМПОНЕНТНАЯ форма подачи таблицы кодов.
	dict := `export const CODES = {
  invalidArgument: {
    grpc: 'INVALID_ARGUMENT',
    http: '400',
  },
`
	if f.vpcDictRefusal {
		dict += `  resourceExhausted: {
    grpc: 'RESOURCE_EXHAUSTED',
    http: '429',
  },
`
	}
	dict += `  internal: {
    grpc: 'INTERNAL',
    http: '500',
  },
} as const
`
	mk("services/vpc/docs/src/constants/codes.ts", dict)

	items := make([]string, 0, len(f.vpcCodesArray))
	for _, k := range f.vpcCodesArray {
		items = append(items, "'"+k+"'")
	}
	mk("services/vpc/docs/content/api/overview.mdx",
		"# Обзор API\n\n## Коды ошибок\n\n<Codes codes={["+strings.Join(items, ", ")+"]} />\n")

	quotasPage := "# Квоты\n\nЧитать пределы:\n\n"
	if f.vpcPathShown {
		quotasPage += "```\nGET /vpc/v1/quotas\n```\n"
	} else {
		quotasPage += "Спросите у владельца вида ресурса.\n"
	}
	mk("services/vpc/docs/content/api/quotas.mdx", quotasPage)

	// iam — ЛИТЕРАЛЬНАЯ форма подачи таблицы кодов.
	iamOverview := "# Обзор API\n\n<table>\n  <tbody>\n" +
		"    <tr><td><code>NOT_FOUND</code></td><td>404</td></tr>\n"
	if f.iamLiteralRow {
		iamOverview += "    <tr><td><code>RESOURCE_EXHAUSTED</code></td><td>429</td>" +
			"<td>Исчерпана квота на число ресурсов вида</td></tr>\n"
	}
	iamOverview += "  </tbody>\n</table>\n"
	mk("services/iam/docs/content/api/overview.mdx", iamOverview)
	mk("services/iam/docs/content/api/quotas.mdx",
		"# Квоты\n\n```\nGET /iam/v1/quotas\n```\n")

	// geo — домен БЕЗ учёта.
	geoOverview := "# Обзор API\n\n<table>\n  <tbody>\n" +
		"    <tr><td><code>NOT_FOUND</code></td><td>404</td></tr>\n"
	if f.geoPromises {
		geoOverview += "    <tr><td><code>RESOURCE_EXHAUSTED</code></td><td>429</td>" +
			"<td>Исчерпана квота</td></tr>\n"
	}
	geoOverview += "  </tbody>\n</table>\n"
	mk("services/geo/docs/content/api/overview.mdx", geoOverview)

	return root
}

// docsQuotaInjectionOwners — владельцы учёта синтетического дерева.
var docsQuotaInjectionOwners = []string{"iam", "vpc"}

func docsQuotaInjectionFindings(t *testing.T, f docsQuotaFixture) (docsQuotaCensus, []string) {
	t.Helper()
	root := writeDocsQuotaTree(t, f)
	c, err := collectDocsQuotaRefusal(mustSyntheticTree(t, root), docsQuotaInjectionOwners)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	// Инъекция беспредметна, если дерево не прочитано: молчание непрочитанного
	// гейта неотличимо от молчания исправного.
	if c.ProtoFiles != 2 || c.Owners != 2 || c.NonOwners != 1 {
		t.Fatalf("фикстура не прочитана: контрактов %d, владельцев %d, без учёта %d",
			c.ProtoFiles, c.Owners, c.NonOwners)
	}
	return c, docsQuotaRefusalFindings(c)
}

func TestDocsQuotaRefusalGateInjection(t *testing.T) {
	t.Run("КОНТРОЛЬ: обе формы подачи целы — гейт молчит", func(t *testing.T) {
		c, f := docsQuotaInjectionFindings(t, docsQuotaControlFixture())
		if c.FormLiteral != 1 || c.FormComponent != 1 {
			t.Fatalf("прочитаны не обе формы подачи: литералом %d, компонентом %d",
				c.FormLiteral, c.FormComponent)
		}
		if c.Described != 2 {
			t.Fatalf("строку отказа несут %d владельцев из 2 — контроль не исправен", c.Described)
		}
		if len(f) != 0 {
			t.Errorf("гейт краснеет на исправном дереве: %v", f)
		}
	})

	t.Run("ЛОВУШКА: ключ остался в словаре, но выпал из массива codes={…}",
		func(t *testing.T) {
			// Ровно тот случай, который наивный гейт пропускает: литерала
			// RESOURCE_EXHAUSTED на странице нет и не было, ключ в словаре
			// находится поиском, а таблица строки НЕ рендерит.
			fx := docsQuotaControlFixture()
			fx.vpcCodesArray = []string{"invalidArgument", "notFound", "internal"}
			c, f := docsQuotaInjectionFindings(t, fx)
			if c.DictFiles != 1 {
				t.Fatalf("словарь кодов не прочитан: %d", c.DictFiles)
			}
			if len(f) != 1 {
				t.Fatalf("ожидалась одна находка, получено %d: %v", len(f), f)
			}
			t.Logf("находка: %s", f[0])
			if !strings.Contains(f[0], "resourceExhausted") {
				t.Errorf("находка не называет ключ: %s", f[0])
			}
			if !strings.Contains(f[0], "codes.ts") ||
				!strings.Contains(f[0], "overview.mdx") {
				t.Errorf("находка не называет обе координаты — словарь и страницу: %s", f[0])
			}
		})

	t.Run("ЛОВУШКА, вторая половина: ключ в массиве, а словарь кода не объявляет",
		func(t *testing.T) {
			fx := docsQuotaControlFixture()
			fx.vpcDictRefusal = false
			_, f := docsQuotaInjectionFindings(t, fx)
			if len(f) != 1 {
				t.Fatalf("ожидалась одна находка, получено %d: %v", len(f), f)
			}
			t.Logf("находка: %s", f[0])
			if !strings.Contains(f[0], "codes.ts") ||
				!strings.Contains(f[0], docsQuotaRefusalCode) {
				t.Errorf("находка не называет ни словаря, ни кода: %s", f[0])
			}
		})

	t.Run("ДЕФЕКТ: у владельца с ЛИТЕРАЛЬНОЙ таблицей строки отказа нет",
		func(t *testing.T) {
			fx := docsQuotaControlFixture()
			fx.iamLiteralRow = false
			_, f := docsQuotaInjectionFindings(t, fx)
			if len(f) != 1 {
				t.Fatalf("ожидалась одна находка, получено %d: %v", len(f), f)
			}
			t.Logf("находка: %s", f[0])
			if !strings.HasPrefix(f[0], "iam:") {
				t.Errorf("находка не называет домен: %s", f[0])
			}
			if !strings.Contains(f[0], "overview.mdx") {
				t.Errorf("находка не называет координату: %s", f[0])
			}
		})

	t.Run("ДЕФЕКТ: поверхность чтения пределов не названа", func(t *testing.T) {
		fx := docsQuotaControlFixture()
		fx.vpcPathShown = false
		_, f := docsQuotaInjectionFindings(t, fx)
		if len(f) != 1 {
			t.Fatalf("ожидалась одна находка, получено %d: %v", len(f), f)
		}
		t.Logf("находка: %s", f[0])
		if !strings.Contains(f[0], "/vpc/v1/quotas") {
			t.Errorf("находка не называет ненайденный путь: %s", f[0])
		}
	})

	t.Run("ЗЕРКАЛО: домен БЕЗ учёта обещает отказ, которого не производит",
		func(t *testing.T) {
			fx := docsQuotaControlFixture()
			fx.geoPromises = true
			_, f := docsQuotaInjectionFindings(t, fx)
			if len(f) != 1 {
				t.Fatalf("ожидалась одна находка, получено %d: %v", len(f), f)
			}
			t.Logf("находка: %s", f[0])
			if !strings.HasPrefix(f[0], "geo:") {
				t.Errorf("находка не называет домен: %s", f[0])
			}
			if !strings.Contains(f[0], "overview.mdx") {
				t.Errorf("находка не называет координату: %s", f[0])
			}
		})

	t.Run("ЗАКОННЫЙ БЛИЗНЕЦ: путь с подстановкой в требование не идёт",
		func(t *testing.T) {
			// Путь `/vpc/v1/quotas/{id}` дословно на странице не стоит и стоять не
			// обязан: как пишется подстановка — решение автора страницы, и гейт об
			// этом не судит. Близнец обязан быть ПРОЧИТАН, иначе его молчание
			// ничего не доказывает.
			root := writeDocsQuotaTree(t, docsQuotaControlFixture())
			extra := filepath.Join(root, "proto/kacho/cloud/vpc/v1/quota_item_service.proto")
			if err := os.WriteFile(extra, []byte(`
syntax = "proto3";
service QuotaItemService {
  rpc Get(GetQuotaRequest) returns (Quota) {
    option (google.api.http) = { get: "/vpc/v1/quotas/{quota_id}" };
  }
}
`), 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}
			c, err := collectDocsQuotaRefusal(mustSyntheticTree(t, root), docsQuotaInjectionOwners)
			if err != nil {
				t.Fatalf("обход: %v", err)
			}
			var param int
			for _, s := range c.Sites {
				param += len(s.ParamPaths)
			}
			if param != 1 {
				t.Fatalf("путь с подстановкой не прочитан: %d", param)
			}
			if f := docsQuotaRefusalFindings(c); len(f) != 0 {
				t.Errorf("путь с подстановкой объявлен находкой: %v", f)
			}
		})

	t.Run("ПРЕДПОСЫЛКА: у владельца нет пути чтения — находка, а не молчание",
		func(t *testing.T) {
			// Соответствие «домен → путь» берётся из первого сегмента пути.
			// Домен, чьего сегмента в контрактах нет, обязан быть НАЗВАН: иначе
			// смена префикса адреса тихо вывела бы его из-под наблюдения.
			c, err := collectDocsQuotaRefusal(
				mustSyntheticTree(t, writeDocsQuotaTree(t, docsQuotaControlFixture())),
				[]string{"iam", "vpc", "storage"})
			if err != nil {
				t.Fatalf("обход: %v", err)
			}
			f := docsQuotaRefusalFindings(c)
			var named bool
			for _, x := range f {
				if strings.HasPrefix(x, "storage:") && strings.Contains(x, "предпосылка") {
					named = true
				}
			}
			if !named {
				t.Errorf("владелец без пути чтения не назван находкой предпосылки: %v", f)
			}
			t.Logf("находки: %v", f)
		})

	t.Run("ПУСТОЙ ОБХОД отличим от «нарушений нет»", func(t *testing.T) {
		c, err := collectDocsQuotaRefusal(mustSyntheticTree(t, t.TempDir()), nil)
		if err != nil {
			t.Fatalf("обход: %v", err)
		}
		if c.ProtoFiles != 0 || c.DocFiles != 0 || len(c.Sites) != 0 {
			t.Fatalf("пустое дерево дало непустую перепись: %+v", c)
		}
	})
}
