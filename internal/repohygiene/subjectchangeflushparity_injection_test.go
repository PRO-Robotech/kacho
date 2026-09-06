// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/contractroot"
)

// flushParityTree раскладывает синтетическое дерево: use-case владельца и файл
// края с набором самосброса.
func flushParityTree(t *testing.T, producers []string, flushKeys []string) SubjectChangeFlushParityOptions {
	t.Helper()
	root := t.TempDir()
	for i, body := range producers {
		p := filepath.Join(root, "api", "uc", "p"+string(rune('a'+i))+".go")
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("каталог: %v", err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("файл: %v", err)
		}
	}
	var keys strings.Builder
	for _, k := range flushKeys {
		keys.WriteString("\t\"" + k + "\": {},\n")
	}
	edge := "package mw\n\nvar " + selfFlushSetName + " = map[string]struct{}{\n" + keys.String() + "}\n"
	p := filepath.Join(root, "edge", "authz.go")
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatalf("каталог края: %v", err)
	}
	if err := os.WriteFile(p, []byte(edge), 0o600); err != nil {
		t.Fatalf("файл края: %v", err)
	}
	return SubjectChangeFlushParityOptions{
		Root: root, ProducerRoot: "api", SelfFlushFile: "edge/authz.go",
		MethodExists: func(service, method string) bool {
			// Подделка обязана быть НЕ СНИСХОДИТЕЛЬНЕЕ и НЕ СТРОЖЕ продукта:
			// имя службы признаётся по объявленному множеству корней, а не по
			// одному литералу. Литерал отверг бы имя из второго корня, и
			// инъекция краснела бы на собственной строгости.
			for _, root := range contractroot.Roots {
				if strings.HasPrefix(service, root+".") {
					return method != "NoSuchMethod"
				}
			}
			return false
		},
	}
}

const producerBody = `package uc

func run(w writer) error { return w.AccessBindingsW().EmitSubjectChangeEvent(nil, evt{}) }
`

// producerValueBody — ВТОРАЯ законная форма обращения: метод порта передан
// ЗНАЧЕНИЕМ общему развёртывателю, который и делает вызов. Производителем
// остаётся тот, кто метод отдал.
const producerValueBody = `package uc

func run(w writer) error {
	return fanout(nil, w.AccessBindingsW().EmitSubjectChangeEvent, "binding_revoke")
}
`

// законный близнец производителя: имя стоит ПРОЗОЙ, вызова нет.
const producerTwin = `package uc

// Строка очереди здесь не пишется: EmitSubjectChangeEvent зовёт соседний
// use-case, а этот только читает.
func read() {}
`

func auditParity(t *testing.T, opts SubjectChangeFlushParityOptions) []SubjectChangeFlushParityFinding {
	t.Helper()
	findings, census, err := AuditSubjectChangeFlushParity(opts, nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if census.GoFiles == 0 {
		t.Fatal("обход пуст — инъекция ничего не доказывает")
	}
	return findings
}

// TestFlushParityGateFallsWhenAProducerHasNoFlush — производитель добавлен, полоса
// самосброса не пополнена.
func TestFlushParityGateFallsWhenAProducerHasNoFlush(t *testing.T) {
	opts := flushParityTree(t,
		[]string{producerBody, producerBody, producerTwin},
		[]string{"kaname.cloud.iam.v1.GroupService/AddMember"})
	findings := auditParity(t, opts)
	if len(findings) != 1 || !strings.Contains(findings[0].What, "производителей очереди 2, самосброс покрывает 1") {
		t.Fatalf("расхождение полос не названо числами обеих: %v", findings)
	}
}

// TestFlushParityGateSeesTheValueForm — обращение, переданное ЗНАЧЕНИЕМ, есть
// производитель наравне с вызовом на месте.
//
// Распознаватель, знавший только вызов, объявлял эту форму отсутствующей — не
// нарушением, а НЕВИДИМОСТЬЮ: полоса пополнялась, перепись не менялась, и гейт
// краснел ровно наоборот — на дереве, где полосы сошлись.
func TestFlushParityGateSeesTheValueForm(t *testing.T) {
	opts := flushParityTree(t,
		[]string{producerValueBody, producerValueBody, producerTwin},
		[]string{"kaname.cloud.iam.v1.GroupService/AddMember"})
	findings := auditParity(t, opts)
	if len(findings) != 1 || !strings.Contains(findings[0].What, "производителей очереди 2, самосброс покрывает 1") {
		t.Fatalf("обращение значением не сочтено производителем: %v", findings)
	}
}

// TestFlushParityGateCountsMixedFormsOnce — обе формы вперемешку, и ни одна не
// сочтена дважды: вызов СОДЕРЖИТ узел обращения, поэтому наивное расширение дало
// бы двойной счёт именно на первой форме.
func TestFlushParityGateCountsMixedFormsOnce(t *testing.T) {
	opts := flushParityTree(t,
		[]string{producerBody, producerValueBody, producerTwin},
		[]string{
			"kaname.cloud.iam.v1.AccessBindingService/Create",
			"kaname.cloud.iam.v1.AccessBindingService/Delete",
		})
	if findings := auditParity(t, opts); len(findings) != 0 {
		t.Fatalf("две формы, два имени — полосы сошлись, а гейт нашёл: %v", findings)
	}
}

// TestFlushParityGateFallsOnAnUnresolvableName — имя, которого нет в контракте:
// счёт сошёлся бы, а самосброс не сработал бы ни разу.
func TestFlushParityGateFallsOnAnUnresolvableName(t *testing.T) {
	opts := flushParityTree(t,
		[]string{producerBody},
		[]string{"kaname.cloud.iam.v1.GroupService/NoSuchMethod"})
	findings := auditParity(t, opts)
	if len(findings) != 1 || !strings.Contains(findings[0].What, "не разрешается в контракте") {
		t.Fatalf("неразрешимое имя пропущено при сошедшемся счёте: %v", findings)
	}
}

// TestFlushParityGateStaysSilentWhenLanesAgree — контроль в обратную сторону.
// Без него гейт был бы зелен только на пустом дереве.
func TestFlushParityGateStaysSilentWhenLanesAgree(t *testing.T) {
	opts := flushParityTree(t,
		[]string{producerBody, producerBody, producerTwin},
		[]string{
			"kaname.cloud.iam.v1.AccessBindingService/Create",
			"kaname.cloud.iam.v1.AccessBindingService/Delete",
		})
	if findings := auditParity(t, opts); len(findings) != 0 {
		t.Fatalf("сошедшиеся полосы объявлены находкой: %v", findings)
	}
}

// TestFlushParityGateRefusesWhenItsOwnPremiseIsGone — предпосылка гейта: набор
// самосброса существует. Пропав, он сделал бы вердикт беспредметным, а гейт —
// вечно зелёным.
func TestFlushParityGateRefusesWhenItsOwnPremiseIsGone(t *testing.T) {
	opts := flushParityTree(t, []string{producerBody}, nil)
	p := filepath.Join(opts.Root, "edge", "authz.go")
	if err := os.WriteFile(p, []byte("package mw\n"), 0o600); err != nil {
		t.Fatalf("файл края: %v", err)
	}
	if _, _, err := AuditSubjectChangeFlushParity(opts, nil); err == nil {
		t.Fatal("пропавший набор самосброса не остановил анализатор — вердикт был бы беспредметен")
	}
}
