// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clientdocscontractdrift_injection_test.go — доказательство способности обоих
// анализаторов упасть И смолчать.
//
// Дерево строится СИНТЕТИЧЕСКОЕ (`t.TempDir()`), потому что доказывать надо
// механизм, а не сегодняшнее состояние продукта: настоящее дерево завтра
// изменится, и проба, опирающаяся на его содержимое, покраснеет от чужой правки.
//
// По каждой оси — ПАРА: дефект обязан находиться, законный близнец той же формы
// обязан молчать. Односторонняя проба зеленела бы на анализаторе, который
// объявляет находкой всё подряд.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clientDocsDriftFixture — синтетическое дерево: контракт двух доменов и два
// сайта документации.
func clientDocsDriftFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("каталог %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("файл %s: %v", rel, err)
		}
	}

	// Общий пакет: конверт операции. Его поля законны в примере ЛЮБОГО домена.
	write("proto/kacho/cloud/operation/v1/operation.proto", `
syntax = "proto3";
package kacho.cloud.operation.v1;
message Operation {
  string id = 1;
  bool done = 2;
  google.protobuf.Any metadata = 3;
}
`)

	// Домен widget: живые поля + два ЗАБРАННЫХ имени + два глагола, один из
	// которых помечен к снятию.
	write("proto/kacho/cloud/widget/v1/widget.proto", `
syntax = "proto3";
package kacho.cloud.widget.v1;
message Widget {
  reserved 7;
  reserved "legacy_knob", "old_shape";
  string id = 1;
  string project_id = 2;
  string display_name = 3;
  // metadata здесь НЕ объявлено: оно приезжает из общего пакета операции.
}
`)
	write("proto/kacho/cloud/widget/v1/widget_service.proto", `
syntax = "proto3";
package kacho.cloud.widget.v1;
service WidgetService {
  // Возвращает виджет по идентификатору.
  rpc Get (GetWidgetRequest) returns (Widget) {
    option (google.api.http) = { get: "/widget/v1/widgets/{widget_id}" };
  }

  // DEPRECATED — use `+"`List`"+` with a filter. Retained for back-compat.
  rpc ListByShape (ListByShapeRequest) returns (ListWidgetsResponse) {
    option (google.api.http) = { get: "/widget/v1/widgets:listByShape" };
  }
}
`)

	// Домен gadget: своё забранное имя. Нужен, чтобы доказать, что забранное у
	// СОСЕДА не судится на чужом сайте.
	write("proto/kacho/cloud/gadget/v1/gadget.proto", `
syntax = "proto3";
package kacho.cloud.gadget.v1;
message Gadget {
  reserved "display_name";
  string id = 1;
  string label = 2;
}
`)

	write("services/widget/docs/docusaurus.config.ts", "export default {}\n")
	write("services/gadget/docs/docusaurus.config.ts", "export default {}\n")
	return root
}

func clientDocsDriftOpts(root string) ClientDocsContractDriftOptions {
	return ClientDocsContractDriftOptions{Root: root, ProtoRoot: "proto"}
}

// clientDocsTracked делает синтетическое дерево ОТСЛЕЖИВАЕМЫМ и возвращает его
// корень.
//
// Анализатор берёт состав контрактов из индекса git, а не обходом диска: чтение
// внутри колбэка обхода подвержено подмене пути символической ссылкой, а сам
// обход захватывал бы игнорируемые каталоги, отчего вердикт стал бы свойством
// рабочего каталога, а не коммита. Значит фикстура обязана быть закоммичена —
// и ПОСЛЕ того, как записаны все её файлы, включая страницы, дописанные самой
// пробой: коммит в конце построения дерева не увидел бы их.
//
// Зовётся в точке вызова анализатора именно поэтому, а не в фикстуре.
func clientDocsTracked(t *testing.T, root string) string {
	t.Helper()
	initTinyRepo(t, root)
	return root
}

func clientDocsWritePage(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("каталог %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("файл %s: %v", rel, err)
	}
}

// TestRetiredFieldGateFallsOnAnExampleShowingARetiredName — ось «забранное имя».
func TestRetiredFieldGateFallsOnAnExampleShowingARetiredName(t *testing.T) {
	root := clientDocsDriftFixture(t)

	// ЛОВИТСЯ: ключ примера — имя, забранное контрактом СВОЕГО домена.
	clientDocsWritePage(t, root, "services/widget/docs/content/api/widget.mdx", `
# Widget

<CodeBlock language="json">
  {dedent`+"`"+`
    {
      "id": "wdg1",
      "legacyKnob": 300,
      "displayName": "acme"
    }
  `+"`"+`}
</CodeBlock>
`)
	findings, census, err := AuditClientDocsRetiredFieldInExample(clientDocsDriftOpts(clientDocsTracked(t, root)), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(findings), findings)
	}
	if findings[0].Key != "legacyKnob" {
		t.Fatalf("находка называет %q, ожидалось legacyKnob", findings[0].Key)
	}
	if !strings.Contains(findings[0].File, "widget.mdx") || findings[0].Line == 0 {
		t.Fatalf("находка не называет координату: %+v", findings[0])
	}
	if census.KeysJudged == 0 || census.RetiredNames == 0 {
		t.Fatalf("перепись пуста: %+v", census)
	}
}

// TestRetiredFieldGateStaysSilentOnLawfulTwins — три законных близнеца той же
// формы. Без них гейт ловил бы форму, а не существо: первый ложный срабат его
// отключит, и вместе с ним перестанут читать настоящие находки.
func TestRetiredFieldGateStaysSilentOnLawfulTwins(t *testing.T) {
	root := clientDocsDriftFixture(t)

	// МОЛЧИТ на трёх осях сразу:
	//   displayName — живое поле СВОЕГО домена (и забранное у соседа);
	//   metadata    — живое поле ОБЩЕГО пакета операции, не своего домена;
	//   unknownKey  — в контракте нет вовсе: «неизвестный ключ» намеренно не судится.
	clientDocsWritePage(t, root, "services/widget/docs/content/api/widget.mdx", `
# Widget

<CodeBlock language="json">
  {dedent`+"`"+`
    {
      "id": "wdg1",
      "displayName": "acme",
      "metadata": { "widgetId": "wdg1" },
      "unknownKey": "чужая схема, например google.rpc.Status"
    }
  `+"`"+`}
</CodeBlock>
`)
	findings, census, err := AuditClientDocsRetiredFieldInExample(clientDocsDriftOpts(clientDocsTracked(t, root)), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на законном тексте: %v", findings)
	}
	if census.KeysJudged < 4 {
		t.Fatalf("ключей рассужено %d — молчание получено пустым обходом, а не разбором", census.KeysJudged)
	}
}

// TestRetiredFieldGateReadsBothExampleForms — распознаватель обязан знать ВСЕ
// законные формы записи примера. Форма, о которой он не знает, — не край: всё
// записанное в ней оказывается вне наблюдения.
func TestRetiredFieldGateReadsBothExampleForms(t *testing.T) {
	root := clientDocsDriftFixture(t)
	clientDocsWritePage(t, root, "services/widget/docs/content/api/widget.mdx",
		"# Widget\n\n```json\n{\n  \"oldShape\": \"round\"\n}\n```\n")
	findings, _, err := AuditClientDocsRetiredFieldInExample(clientDocsDriftOpts(clientDocsTracked(t, root)), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 1 || findings[0].Key != "oldShape" {
		t.Fatalf("огороженная тройными кавычками форма примера вне наблюдения: %v", findings)
	}
}

// TestDeprecationGateFallsOnADeprecatedVerbShownAsCurrent — ось «пометка».
func TestDeprecationGateFallsOnADeprecatedVerbShownAsCurrent(t *testing.T) {
	root := clientDocsDriftFixture(t)
	clientDocsWritePage(t, root, "services/widget/docs/content/api/widget.mdx", `
# Widget

<ApiOperation method="GET" endpoint="/widget/v1/widgets:listByShape">

Возвращает виджеты по форме.

</ApiOperation>
`)
	findings, census, err := AuditClientDocsDeprecationParity(clientDocsDriftOpts(clientDocsTracked(t, root)), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(findings), findings)
	}
	if findings[0].RPC != "ListByShape" || findings[0].Line == 0 {
		t.Fatalf("находка не называет глагол и координату: %+v", findings[0])
	}
	if census.DeprecatedPaths != 1 || census.BlocksJudged != 1 {
		t.Fatalf("перепись не сходится: %+v", census)
	}
}

// TestDeprecationGateStaysSilentOnLawfulTwins — два законных близнеца.
func TestDeprecationGateStaysSilentOnLawfulTwins(t *testing.T) {
	root := clientDocsDriftFixture(t)

	// МОЛЧИТ: помеченный путь несёт пометку в СВОЁМ блоке;
	//         непомеченный путь пометки не требует.
	clientDocsWritePage(t, root, "services/widget/docs/content/api/widget.mdx", `
# Widget

<ApiOperation method="GET" endpoint="/widget/v1/widgets/{widget_id}">

Возвращает виджет по идентификатору.

</ApiOperation>

<ApiOperation method="GET" endpoint="/widget/v1/widgets:listByShape">

:::warning Помечено к снятию — используйте `+"`List`"+`
:::

Возвращает виджеты по форме.

</ApiOperation>
`)
	findings, census, err := AuditClientDocsDeprecationParity(clientDocsDriftOpts(clientDocsTracked(t, root)), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на законном тексте: %v", findings)
	}
	if census.Blocks != 2 || census.BlocksJudged != 1 {
		t.Fatalf("перепись не сходится: блоков %d, сверено %d (ожидалось 2 и 1)",
			census.Blocks, census.BlocksJudged)
	}
}

// TestDeprecationMarkIsNotCountedFromANeighbouringBlock — пометка засчитывается
// ТОЛЬКО из своего блока.
//
// Без этой оси гейт зеленел бы от пометки, стоящей у соседней операции на той же
// странице, — то есть от текста, который вызывающий этого пути не прочтёт.
func TestDeprecationMarkIsNotCountedFromANeighbouringBlock(t *testing.T) {
	root := clientDocsDriftFixture(t)
	clientDocsWritePage(t, root, "services/widget/docs/content/api/widget.mdx", `
# Widget

<ApiOperation method="GET" endpoint="/widget/v1/widgets/{widget_id}">

Это чтение когда-нибудь будет снято — пометка стоит ЗДЕСЬ, у другого пути.

</ApiOperation>

<ApiOperation method="GET" endpoint="/widget/v1/widgets:listByShape">

Возвращает виджеты по форме.

</ApiOperation>
`)
	findings, _, err := AuditClientDocsDeprecationParity(clientDocsDriftOpts(clientDocsTracked(t, root)), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("пометка засчитана из чужого блока: находок %d", len(findings))
	}
}

// TestDeprecationMarkIsReadFromTheDeclarationNotAnyComment — пометка контракта
// читается из блока комментариев ПЕРЕД глаголом, а не откуда попало в файле.
func TestDeprecationMarkIsReadFromTheDeclarationNotAnyComment(t *testing.T) {
	root := clientDocsDriftFixture(t)
	// Пометка стоит в прозе ПОСЛЕ объявления соседнего глагола — то есть
	// относится к тексту, а не к глаголу ниже.
	p := filepath.Join(root, "proto", "kacho", "cloud", "widget", "v1", "widget_service.proto")
	if err := os.WriteFile(p, []byte(`
syntax = "proto3";
package kacho.cloud.widget.v1;
service WidgetService {
  // DEPRECATED — здесь объясняется, почему СОСЕДНЕЕ чтение когда-то сняли.
  // Само это чтение действующее.
  rpc Get (GetWidgetRequest) returns (Widget) {
    option (google.api.http) = { get: "/widget/v1/widgets/{widget_id}" };
  }

  // Действующее чтение без всякой пометки.
  rpc List (ListWidgetsRequest) returns (ListWidgetsResponse) {
    option (google.api.http) = { get: "/widget/v1/widgets" };
  }
}
`), 0o600); err != nil {
		t.Fatalf("фикстура: %v", err)
	}
	paths, _, err := clientDocsDeprecatedPaths(clientDocsDriftOpts(clientDocsTracked(t, root)))
	if err != nil {
		t.Fatalf("разбор контракта: %v", err)
	}
	if _, ok := paths["/widget/v1/widgets/{widget_id}"]; !ok {
		t.Fatalf("пометка перед объявлением не прочитана: %v", paths)
	}
	if _, ok := paths["/widget/v1/widgets"]; ok {
		t.Fatalf("пометка протекла на следующий глагол: %v", paths)
	}
}

// TestBothGatesFallOnAnEmptyWalk — «ноль находок» обязано быть отличимо от «ноль
// прочитанного».
func TestBothGatesFallOnAnEmptyWalk(t *testing.T) {
	root := t.TempDir()
	opts := ClientDocsContractDriftOptions{Root: root, ProtoRoot: "proto"}

	if _, census, _ := AuditClientDocsRetiredFieldInExample(opts, nil); census.Sites != 0 ||
		census.Pages != 0 || census.ProtoFiles != 0 {
		t.Fatalf("на пустом дереве перепись не пуста: %+v", census)
	}
	if _, census, _ := AuditClientDocsDeprecationParity(opts, nil); census.Sites != 0 ||
		census.DeprecatedPaths != 0 {
		t.Fatalf("на пустом дереве перепись не пуста: %+v", census)
	}
	// Сам отказ на пустом обходе выносят пробы о дереве
	// (`clientdocscontractdrift_test.go`): они требуют непустой переписи и падают
	// раньше вердикта. Здесь доказано, что перепись пустоту ПОКАЗЫВАЕТ, — без
	// этого требовать от неё было бы нечего.
}
