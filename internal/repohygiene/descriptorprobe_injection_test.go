// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// descriptorprobe_injection_test.go — доказательство того, что гейт
// `TestEveryDescriptorHasAProbe` СПОСОБЕН упасть, и падает он на существе.
//
// Инъекция гоняет ТУ ЖЕ функцию обхода (`auditDescriptorProbes`), что и гейт, на
// синтетическом дереве: проба, повторяющая логику гейта своей копией, доказывала
// бы свойство копии.
//
// Пара обязательна в ОБЕ стороны:
//
//   - (а) корень БЕЗ вызывающего в пробах → находка, называющая функцию и файл;
//   - (б) тот же корень С вызывающим → молчание.
//
// Без (б) гейт ловил бы форму, а не существо: «корень найден» краснело бы на
// любом дереве, и первый же ложный срабат его отключил бы.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// syntheticTree раскладывает крошечное дерево и делает его ОТСЛЕЖИВАЕМЫМ:
// обходчик берёт состав из индекса git, а не с диска, поэтому неотслеживаемый
// файл ему невиден — и инъекция молча ничего бы не проверила.
func syntheticTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("каталог %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("файл %s: %v", rel, err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
	} {
		cmd := gitenv.Command(root, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return root
}

// rootSource — композиционный корень: функция, зовущая конструктор дескриптора.
const rootSource = `package main

import "github.com/PRO-Robotech/kacho/pkg/servicecontract"

// describe собирает объявление сервиса о себе.
func describe() (servicecontract.Descriptor, error) {
	return servicecontract.New(servicecontract.Spec{})
}
`

// decoySource — ЗАКОННЫЙ БЛИЗНЕЦ формы: тот же пакет, тот же вид вызова, но
// конструктор ЧУЖОЙ. Гейт обязан о нём молчать, иначе он ловит слово «New», а не
// сборку дескриптора.
const decoySource = `package main

import "net/http"

func newClient() *http.Client {
	return http.DefaultClient
}
`

func TestGateFindsARootWithoutAProbe(t *testing.T) {
	root := syntheticTree(t, map[string]string{
		"cmd/svc/serve.go": rootSource,
		// Тест в пакете ЕСТЬ, и он зелёный — но корень он не зовёт. Ровно то
		// состояние, в котором дерево и находилось: три пробы у geo, ни одна не
		// звала describe.
		"cmd/svc/other_test.go": "package main\n\nimport \"testing\"\n\nfunc TestSomethingElse(t *testing.T) {}\n",
	})
	res := auditDescriptorProbes(t, root)
	if len(res.roots) != 1 {
		t.Fatalf("корень не распознан (%d) — инъекция ничего не проверяет: %s", len(res.roots), res.summary())
	}
	if got := res.callers[res.roots[0].key()]; len(got) != 0 {
		t.Fatalf("у корня без вызывающего нашлись вызывающие %v — гейт зеленел бы на дефекте", got)
	}
	if res.tests == 0 {
		t.Fatal("тестовые файлы не прочитаны вовсе — «вызывающих нет» означало бы «не смотрели», " +
			"и находка была бы верной по неверной причине")
	}
}

func TestGateStaysSilentWhenTheProbeCallsTheRoot(t *testing.T) {
	root := syntheticTree(t, map[string]string{
		"cmd/svc/serve.go": rootSource,
		"cmd/svc/describe_test.go": "package main\n\nimport \"testing\"\n\n" +
			"func TestDescribeIsAccepted(t *testing.T) {\n\tif _, err := describe(); err != nil {\n\t\tt.Fatal(err)\n\t}\n}\n",
	})
	res := auditDescriptorProbes(t, root)
	if len(res.roots) != 1 {
		t.Fatalf("корень не распознан (%d): %s", len(res.roots), res.summary())
	}
	if got := res.callers[res.roots[0].key()]; len(got) == 0 {
		t.Fatal("вызов корня из пробы не засчитан — гейт краснел бы на исправном дереве, " +
			"и первый же ложный срабат его отключил бы")
	}
}

// TestGateIgnoresACallOfSomebodyElsesNew — вторая половина законного близнеца:
// вызов чужого `New` корнем не является.
func TestGateIgnoresACallOfSomebodyElsesNew(t *testing.T) {
	root := syntheticTree(t, map[string]string{
		"cmd/svc/client.go": decoySource,
	})
	res := auditDescriptorProbes(t, root)
	if len(res.roots) != 0 {
		t.Fatalf("чужой конструктор засчитан композиционным корнем (%v): гейт ловит имя метода, "+
			"а не сборку дескриптора", res.roots)
	}
	if res.files == 0 {
		t.Fatalf("файл не прочитан — «корней нет» означало бы «не смотрели»: %s", res.summary())
	}
}

// TestGateReadsCodeNotComments — предикат обязан отличать исполняемую часть от
// комментария: имя конструктора стоит в шапках чаще, чем в коде.
func TestGateReadsCodeNotComments(t *testing.T) {
	root := syntheticTree(t, map[string]string{
		"cmd/svc/doc.go": "package main\n\n" +
			"// describe зовёт servicecontract.New(spec) и отдаёт дескриптор.\n" +
			"// Здесь этого вызова НЕТ — только рассказ о нём.\n" +
			"func describe() string { return \"servicecontract.New\" }\n",
	})
	res := auditDescriptorProbes(t, root)
	if len(res.roots) != 0 {
		t.Fatalf("гейт засчитал корнем комментарий либо строковый литерал (%v) — текстовый предикат "+
			"зеленел бы на собственной документации и молчал на снятой сборке", res.roots)
	}
	if !strings.Contains(res.summary(), "не-тестовых файлов 1") {
		t.Fatalf("файл не прочитан: %s", res.summary())
	}
}
