// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Инъекция для гейта величин отказа учёта — В ОБЕ СТОРОНЫ.
//
// Гейт, чью способность падать не доказали, неотличим от вечно-зелёного: на
// чистом дереве он выглядит точно так же. Поэтому здесь: (а) возвращённый
// НАСТОЯЩИЙ дефект обязан дать находку и назвать координату; (б) законный
// близнец той же формы обязан молчать; (в) пустой обход обязан быть отличим от
// «нарушений нет».
//
// Дефект возвращается тот самый, что стоял в дереве до задачи продукта #1605:
// мост опознаёт `KQ001`, сохраняет текст производителя и НЕ читает `DETAIL`; а
// сборка ответа несёт признак полосы без поля величин.

// quotaMetaFixture — что именно подставляется в синтетическое дерево.
type quotaMetaFixture struct {
	// bridge — как мост обходится с величинами:
	//   "attach"        — зовёт quotadetail.Attach (исправно);
	//   "alias"         — то же, но пакет импортирован под псевдонимом;
	//   "none"          — не зовёт (ДЕФЕКТ, вернувшийся из дерева);
	//   "comment"       — не зовёт, но упоминает вызов в комментарии;
	//   "foreign"       — зовёт Attach ЧУЖОГО одноимённого пакета.
	bridge string
	// outward — как ответ несёт величины:
	//   "literal" — поле Metadata в составном литерале;
	//   "assign"  — вторая законная форма: доставка отдельным присваиванием;
	//   "none"    — не несёт (ДЕФЕКТ).
	outward string
	// twin — положить ли рядом законного близнеца: сборку ErrorInfo ДРУГОЙ
	// полосы, у которой величин быть не должно.
	twin bool
}

func writeQuotaMetaTree(t *testing.T, owner string, f quotaMetaFixture) string {
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

	var imp, call string
	switch f.bridge {
	case "attach":
		imp = `import "github.com/PRO-Robotech/kacho/pkg/quota/quotadetail"`
		call = `return quotadetail.Attach(wrapped, detail)`
	case "alias":
		imp = `import qd "github.com/PRO-Robotech/kacho/pkg/quota/quotadetail"`
		call = `return qd.Attach(wrapped, detail)`
	case "foreign":
		// Одноимённый пакет ЧУЖОГО происхождения: имя в исходнике то же, путь
		// импорта другой — значит это не наш единственный разбор.
		imp = `import "example.com/vendor/quotadetail"`
		call = `return quotadetail.Attach(wrapped, detail)`
	case "comment":
		imp = `import "fmt"`
		call = "// здесь надо бы позвать quotadetail.Attach(wrapped, detail)\n\treturn wrapped"
	default:
		imp = `import "fmt"`
		call = `return wrapped`
	}

	mk("services/"+owner+"/internal/repo/errmap_quota.go", `package pg

`+imp+`

func classifyQuotaErr(code, msg, detail string, wrapped error) error {
	if code == "KQ001" {
		`+call+`
	}
	return nil
}
`)

	var outward string
	switch f.outward {
	case "literal":
		outward = `	return &errdetails.ErrorInfo{
		Reason:   reasonQuotaExceeded,
		Domain:   "` + owner + `.kacho.cloud",
		Metadata: md,
	}`
	case "assign":
		outward = `	info := &errdetails.ErrorInfo{
		Reason: reasonQuotaExceeded,
		Domain: "` + owner + `.kacho.cloud",
	}
	info.Metadata = md
	return info`
	default:
		outward = `	return &errdetails.ErrorInfo{
		Reason: reasonQuotaExceeded,
		Domain: "` + owner + `.kacho.cloud",
	}`
	}

	mk("services/"+owner+"/internal/apps/kaname/shared/serviceerr/quota.go", `package serviceerr

import "google.golang.org/genproto/googleapis/rpc/errdetails"

const reasonQuotaExceeded = "QUOTA_EXCEEDED"

func quotaRefusal(md map[string]string) *errdetails.ErrorInfo {
`+outward+`
}
`)

	if f.twin {
		// ЗАКОННЫЙ БЛИЗНЕЦ: сборка ErrorInfo ДРУГОЙ полосы. Величин у неё нет и
		// быть не должно — требовать их значило бы краснеть на исправном коде.
		mk("services/"+owner+"/internal/apps/kaname/shared/serviceerr/lanes.go", `package serviceerr

import "google.golang.org/genproto/googleapis/rpc/errdetails"

func peerLane() *errdetails.ErrorInfo {
	return &errdetails.ErrorInfo{
		Reason: "PEER_RESOURCE_MISSING",
		Domain: "`+owner+`.kacho.cloud",
	}
}
`)
	}

	return root
}

// runQuotaMeta собирает перепись по синтетическому дереву одного владельца.
func runQuotaMeta(t *testing.T, owner string, f quotaMetaFixture) (quotaMetadataCensus, []string) {
	t.Helper()
	root := writeQuotaMetaTree(t, owner, f)
	tree := mustSyntheticTree(t, root)
	c, err := collectQuotaRefusalMetadata(tree, []string{owner})
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	return c, quotaRefusalMetadataFindings(c)
}

// ── (б) ЗАКОННЫЙ БЛИЗНЕЦ МОЛЧИТ ────────────────────────────────────────────

func TestQuotaMetadataGate_SilentWhenAmountsFlow(t *testing.T) {
	c, findings := runQuotaMeta(t, "alpha",
		quotaMetaFixture{bridge: "attach", outward: "literal", twin: true})

	if len(findings) != 0 {
		t.Fatalf("исправное дерево обязано молчать, а гейт нашёл: %v", findings)
	}
	if c.BridgesAttaching != 1 || c.OutwardWithMetadata != 1 {
		t.Fatalf("перепись не увидела исправных половин: приклеивают %d, с метаданными %d",
			c.BridgesAttaching, c.OutwardWithMetadata)
	}
	// Близнец другой полосы не обязан нести величин и находкой не является.
	if c.OutwardFound != 1 {
		t.Fatalf("сборкой ответа УЧЁТА засчитана не одна: %d", c.OutwardFound)
	}
}

// Вторая законная форма доставки поля обязана распознаваться наравне с первой:
// распознаватель, знающий одну, всё записанное второй оставил бы ВНЕ наблюдения.
func TestQuotaMetadataGate_SilentOnTheAssignmentForm(t *testing.T) {
	_, findings := runQuotaMeta(t, "alpha",
		quotaMetaFixture{bridge: "attach", outward: "assign"})

	if len(findings) != 0 {
		t.Fatalf("присваивание поля — законная форма, гейт обязан молчать: %v", findings)
	}
}

// Псевдоним импорта на исход не влияет: имя в исходнике задаёт вызывающий.
func TestQuotaMetadataGate_SilentOnAnAliasedImport(t *testing.T) {
	_, findings := runQuotaMeta(t, "alpha",
		quotaMetaFixture{bridge: "alias", outward: "literal"})

	if len(findings) != 0 {
		t.Fatalf("псевдоним импорта — та же функция, гейт обязан молчать: %v", findings)
	}
}

// ── (а) ВЕРНУТЬ ДЕФЕКТ — КРАСНЕЕТ И НАЗЫВАЕТ КООРДИНАТУ ────────────────────

func TestQuotaMetadataGate_RedWhenTheBridgeDropsTheAmounts(t *testing.T) {
	_, findings := runQuotaMeta(t, "beta",
		quotaMetaFixture{bridge: "none", outward: "literal"})

	if len(findings) != 1 {
		t.Fatalf("мост без величин обязан давать РОВНО одну находку, получено %d: %v",
			len(findings), findings)
	}
	if !strings.Contains(findings[0], "beta") ||
		!strings.Contains(findings[0], "services/beta/internal/repo/errmap_quota.go") {
		t.Fatalf("находка обязана называть владельца и файл: %s", findings[0])
	}
}

// Упоминание вызова в комментарии вызовом НЕ является: гейт судит узлы, а не
// текст, — иначе он зеленел бы на собственном объяснении.
func TestQuotaMetadataGate_RedWhenTheCallLivesOnlyInAComment(t *testing.T) {
	_, findings := runQuotaMeta(t, "beta",
		quotaMetaFixture{bridge: "comment", outward: "literal"})

	if len(findings) != 1 {
		t.Fatalf("комментарий не приклеивает величин, ожидалась одна находка, получено %d: %v",
			len(findings), findings)
	}
}

// Одноимённый пакет чужого происхождения — не наш единственный разбор.
func TestQuotaMetadataGate_RedOnAForeignPackageOfTheSameName(t *testing.T) {
	_, findings := runQuotaMeta(t, "beta",
		quotaMetaFixture{bridge: "foreign", outward: "literal"})

	if len(findings) != 1 {
		t.Fatalf("чужой одноимённый пакет не засчитывается, ожидалась одна находка, "+
			"получено %d: %v", len(findings), findings)
	}
}

func TestQuotaMetadataGate_RedWhenTheAnswerHasNowhereToPutThem(t *testing.T) {
	_, findings := runQuotaMeta(t, "beta",
		quotaMetaFixture{bridge: "attach", outward: "none", twin: true})

	if len(findings) != 1 {
		t.Fatalf("ответ без поля величин обязан давать РОВНО одну находку, получено %d: %v",
			len(findings), findings)
	}
	if !strings.Contains(findings[0], "services/beta/internal/apps/kaname/shared/serviceerr/quota.go") {
		t.Fatalf("находка обязана называть файл сборки ответа: %s", findings[0])
	}
}

// Обе половины сломаны — обе находки, а не одна: гейт не останавливается на
// первой, иначе вторая чинилась бы вторым кругом прогона.
func TestQuotaMetadataGate_RedOnBothHalvesAtOnce(t *testing.T) {
	_, findings := runQuotaMeta(t, "beta",
		quotaMetaFixture{bridge: "none", outward: "none"})

	if len(findings) != 2 {
		t.Fatalf("сломаны обе половины — ожидались две находки, получено %d: %v",
			len(findings), findings)
	}
}

// ── (в) ПУСТОЙ ОБХОД ОТЛИЧИМ ОТ «НАРУШЕНИЙ НЕТ» ───────────────────────────

func TestQuotaMetadataGate_EmptyTraversalIsDistinguishableFromClean(t *testing.T) {
	tree := mustSyntheticTree(t, t.TempDir())

	c, err := collectQuotaRefusalMetadata(tree, []string{"alpha"})
	if err != nil {
		t.Fatalf("обход пустого дерева: %v", err)
	}

	if len(quotaRefusalMetadataFindings(c)) != 0 {
		t.Fatal("на пустом дереве находок быть не может — их не из чего вывести")
	}
	// Именно поэтому вердикт гейта опирается на перепись: ноль находок здесь
	// означает «нечего было читать», и проверка предпосылки обязана это поймать.
	if c.Parsed != 0 || c.BridgesFound != 0 || c.OutwardFound != 0 {
		t.Fatalf("пустое дерево обязано давать нулевую перепись: разобрано %d, "+
			"мостов %d, сборок %d", c.Parsed, c.BridgesFound, c.OutwardFound)
	}
}
