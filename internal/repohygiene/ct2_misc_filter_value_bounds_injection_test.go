// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Инъекция для гейта объявлений о значении фильтра — В ОБЕ СТОРОНЫ.
//
// Гейт, чью способность падать не доказали, неотличим от вечно-зелёного.
// Поэтому здесь: (а) НАСТОЯЩИЙ дефект — контракт, объявляющий отсутствие
// правила, — обязан дать находку с координатой; (б) расхождение ЧИСЛА обязано
// краснеть отдельно; (в) законный близнец обязан молчать; (г) чужая проза о
// длинах в том же файле обязана быть невидима; (д) пустой обход отличим от
// «нарушений нет».

// ct2FilterFixture — тело комментария над полем `filter` в синтетическом
// контракте, плюс необязательная чужая проза в том же файле.
type ct2FilterFixture struct {
	comment   string
	elsewhere string
}

func writeCt2FilterTree(t *testing.T, f ct2FilterFixture) string {
	t.Helper()
	root := t.TempDir()
	rel := "proto/kacho/cloud/vpc/v1/network_service.proto"
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `syntax = "proto3";

message SomethingElse {
  ` + f.elsewhere + `
  string description = 1;
}

message ListNetworksRequest {
  // A filter expression that filters resources listed in the response.
  ` + f.comment + `
  string filter = 4;
}
`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return root
}

func ct2FilterRun(t *testing.T, root string, limit int) (ct2FilterCensus, []string) {
	t.Helper()
	c, err := collectFilterValueClaims(mustSyntheticTree(t, root), limit)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	return c, filterValueClaimFindings(c, limit)
}

const (
	// Дефект, снятый задачей продукта #1654 — дословно то, что стояло в шести
	// контрактах vpc.
	ct2FilterDeniesRule = `// The filter itself applies no
  //    separate length or alphabet rule to the value.`
	// Законный близнец: контракт называет тот предел, который применяет код.
	ct2FilterStatesRight = `// The filter refuses a value longer than 256 characters
  //    with INVALID_ARGUMENT naming the field and the rule.`
	// Разошедшееся число — вторая половина гейта.
	ct2FilterStatesWrong = `// The filter refuses a value longer than 1024 characters.`
	// Контракт вовсе без описания правил — обязан быть невидим.
	ct2FilterSaysNothing = `// Filtering is available on [Network.name].`
)

// (а) НАСТОЯЩИЙ ДЕФЕКТ: контракт объявляет отсутствие правила.
func TestCt2FilterInjection_DeniedRuleIsAFinding(t *testing.T) {
	root := writeCt2FilterTree(t, ct2FilterFixture{comment: ct2FilterDeniesRule})
	c, findings := ct2FilterRun(t, root, 256)

	if len(findings) != 1 {
		t.Fatalf("объявление отсутствия правила обязано дать одну находку, получено: %v", findings)
	}
	for _, want := range []string{"network_service.proto:", "правила длины", "256"} {
		if !strings.Contains(findings[0], want) {
			t.Errorf("находка обязана называть %q, а называет: %s", want, findings[0])
		}
	}
	if c.Denying != 1 || c.Agreeing != 0 {
		t.Errorf("перепись обязана дать 1 отрицающее и 0 совпавших, получено %d/%d",
			c.Denying, c.Agreeing)
	}
}

// (б) РАЗОШЕДШЕЕСЯ ЧИСЛО — отдельная находка; слить её с (а) значило бы
// потерять одну из двух половин.
func TestCt2FilterInjection_DriftedNumberIsItsOwnFinding(t *testing.T) {
	root := writeCt2FilterTree(t, ct2FilterFixture{comment: ct2FilterStatesWrong})
	c, findings := ct2FilterRun(t, root, 256)

	if len(findings) != 1 {
		t.Fatalf("разошедшееся число обязано дать одну находку, получено: %v", findings)
	}
	for _, want := range []string{"предел 1024", "применяет 256"} {
		if !strings.Contains(findings[0], want) {
			t.Errorf("находка обязана называть %q, а называет: %s", want, findings[0])
		}
	}
	if c.Stating != 1 || c.Agreeing != 0 {
		t.Errorf("перепись обязана дать 1 называющее и 0 совпавших, получено %d/%d",
			c.Stating, c.Agreeing)
	}
}

// (в) ЗАКОННЫЙ БЛИЗНЕЦ обязан молчать.
func TestCt2FilterInjection_AgreeingContractIsSilent(t *testing.T) {
	root := writeCt2FilterTree(t, ct2FilterFixture{comment: ct2FilterStatesRight})
	c, findings := ct2FilterRun(t, root, 256)
	if len(findings) != 0 {
		t.Fatalf("совпавший контракт обязан молчать: %v", findings)
	}
	if c.Agreeing != 1 || c.Fields != 1 {
		t.Errorf("перепись обязана дать 1 поле и 1 совпавшее, получено %d/%d",
			c.Fields, c.Agreeing)
	}
}

// (г) ЧУЖАЯ ПРОЗА о длинах в том же файле невидима: обход сужен до ленты
// комментариев над полем. Без сужения гейт краснел бы на соседнем сообщении.
func TestCt2FilterInjection_UnrelatedProseInTheSameFileIsInvisible(t *testing.T) {
	root := writeCt2FilterTree(t, ct2FilterFixture{
		comment:   ct2FilterSaysNothing,
		elsewhere: "// Description is refused when longer than 4096 characters.",
	})
	c, findings := ct2FilterRun(t, root, 256)
	if len(findings) != 0 {
		t.Fatalf("проза о чужом поле находкой не является: %v", findings)
	}
	if len(c.Claims) != 0 {
		t.Fatalf("объявлений о значении фильтра здесь нет, насчитано %d", len(c.Claims))
	}
	if c.Fields != 1 {
		t.Errorf("поле filter обязано быть осмотрено, осмотрено %d", c.Fields)
	}
}

// (д) ПУСТОЙ ОБХОД отличим от «нарушений нет».
func TestCt2FilterInjection_EmptyWalkIsDistinguishable(t *testing.T) {
	c, findings := ct2FilterRun(t, t.TempDir(), 256)
	if c.Files != 0 || c.Fields != 0 || len(c.Claims) != 0 {
		t.Fatalf("на пустом дереве обход обязан быть пуст: файлов %d, полей %d, объявлений %d",
			c.Files, c.Fields, len(c.Claims))
	}
	if len(findings) != 0 {
		t.Fatalf("на пустом дереве находок быть не может — их место занимает "+
			"проверка предпосылки: %v", findings)
	}
}
