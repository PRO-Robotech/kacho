// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// seed_secret_has_a_consumer_test.go — каждый секрет, который заводит посев
// стенда, обязан иметь ПОТРЕБИТЕЛЯ в объявлениях зонта.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ (kacho#1276, часть 3)
//
// Посев (`scripts/dev-prod-secrets.sh`) заводит ключевой материал стенда ДО
// первого прогона helm. Секрет, на который не ссылается ни одно объявление
// зонта, посев всё равно чеканит — на каждом новом стенде, — а предполёт стенда
// (`scripts/stack-secrets.sh`, источник (б)) требует из посева ровно то, на что
// ссылается рендер, и о лишнем молчит by construction. Такой секрет — не запас,
// а объявление, пережившее своего потребителя: оно утверждает в шапке посева
// «требуется», и это ложь.
//
// Наблюдалось: общий секрет обратных вызовов (`kaname-hook-token`) заводился
// посевом после того, как его потребители — под издателя поставщика личности
// (снят первой частью kacho#1276) и слушатель хуков службы (снят kacho#2818) —
// ушли из зонта. Ссылок на него не осталось ни в одном шаблоне и ни в одном
// профиле; шапка посева по-прежнему называла его предусловием.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЧИТАЕТСЯ И ЧЕГО ПРОБА НЕ УТВЕРЖДАЕТ
//
// Читаются ОБЪЯВЛЕНИЯ — отслеживаемые файлы зонта и его подчартов формы
// `.yaml`/`.yml`/`.tpl` (профили, умолчания, шаблоны) по индексу git, со
// снятыми комментариями: имя, названное прозой, потребителем не является.
// Потребитель — имя секрета отдельным словом в исполняемой части объявления
// (значение ручки имени секрета, `secretKeyRef.name`, `secretName`).
//
//   - проба НЕ судит, что объявление, назвавшее имя, дочитывается шаблоном до
//     пода: это предмет гейтов мёртвых ручек и рендера. Здесь предмет — посев,
//     чеканящий то, чего не называет никто;
//   - проба НЕ судит обратную сторону (ссылка без производителя): её держат
//     deploy/tests/helm/prerequisite-secrets-test.sh и предполёт стенда.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// seedConsumerSuffixes — формы файлов, в которых объявление может назвать
// секрет. Архивы подчартов (`.tgz`) и проза (`.md`) в предмет не входят.
var seedConsumerSuffixes = []string{".yaml", ".yml", ".tpl"}

// seedTemplateComment — комментарий шаблона `{{/* … */}}` (с обрезкой пробелов
// или без), в том числе многострочный.
var seedTemplateComment = regexp.MustCompile(`(?s)\{\{-?\s*/\*.*?\*/\s*-?\}\}`)

// seedTrailingComment — комментарий YAML в конце строки: знак `#` после
// пробела. Внутри значения без пробела перед ним (`a#b`) он комментарием не
// является.
var seedTrailingComment = regexp.MustCompile(`\s#.*$`)

// seedExecutablePart — исполняемая часть объявления: без строк-комментариев
// YAML, без хвостовых комментариев и без комментариев шаблона обеих форм
// (`{{/* … */}}` и `{{- /* … */ -}}`). Соседний stripDeclarationComments
// (iam_module_manifest_delivery_test.go) хвостовых комментариев не снимает и
// форму с пробелом после дефиса не узнаёт; его предмету — три файла подчарта —
// этого хватает, здешнему — всему зонту — нет.
func seedExecutablePart(body string) string {
	body = seedTemplateComment.ReplaceAllString(body, "")
	lines := strings.Split(body, "\n")
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimLeft(ln, " \t"), "#") {
			lines[i] = ""
			continue
		}
		lines[i] = seedTrailingComment.ReplaceAllString(ln, "")
	}
	return strings.Join(lines, "\n")
}

// seedNameBounded — имя секрета отдельным словом: соседний знак не буква, не
// цифра и не дефис. Без границы `kaname-hook-token` находился бы внутри
// `kaname-hook-token-v2`, и потребитель другого секрета засчитался бы этому.
func seedNameBounded(text, name string) bool {
	re := regexp.MustCompile(`(^|[^A-Za-z0-9-])` + regexp.QuoteMeta(name) + `($|[^A-Za-z0-9-])`)
	return re.MatchString(text)
}

// seedConsumerFacts — посев и объявления, прочитанные из дерева.
type seedConsumerFacts struct {
	seeded    []string            // секреты, которые заводит посев
	consumers map[string][]string // секрет → объявления, называющие его
	read      int                 // объявлений прочитано
}

// judgeSeedConsumers — ядро: чистая функция над фактами.
func judgeSeedConsumers(f seedConsumerFacts) []string {
	var out []string
	for _, n := range f.seeded {
		if len(f.consumers[n]) == 0 {
			out = append(out, fmt.Sprintf(
				"посев %s заводит секрет %s, а ни одно объявление зонта его не называет "+
					"(прочитано объявлений %d) — посев пережил своего потребителя: секрет "+
					"чеканится на каждом новом стенде и никем не читается, а шапка посева "+
					"называет его предусловием", seedScript, n, f.read))
		}
	}
	return out
}

// seedConsumerFactsFromTree — посев и объявления по индексу git.
func seedConsumerFactsFromTree(t *testing.T) seedConsumerFacts {
	t.Helper()
	seed, err := os.ReadFile(seedScript)
	if err != nil {
		t.Fatalf("посев %s не читается (%v) — судить нечего", seedScript, err)
	}
	f := seedConsumerFacts{consumers: map[string][]string{}}
	seen := map[string]bool{}
	for _, m := range seedCreates.FindAllStringSubmatch(stripShellComments(string(seed)), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			f.seeded = append(f.seeded, m[1])
		}
	}
	sort.Strings(f.seeded)

	files, err := treecorpus.UnderWithSuffix(umbrellaDir, seedConsumerSuffixes...)
	if err != nil {
		t.Fatalf("объявления зонта %s не перечислены: %v", umbrellaDir, err)
	}
	abs, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("абсолютный путь каталога deploy: %v", err)
	}
	for _, p := range files {
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Fatalf("чтение %s: %v", p, rerr)
		}
		f.read++
		code := seedExecutablePart(string(b))
		rel, _ := filepath.Rel(abs, p)
		for _, n := range f.seeded {
			if seedNameBounded(code, n) {
				f.consumers[n] = append(f.consumers[n], filepath.ToSlash(rel))
			}
		}
	}
	return f
}

// ─────────────────────────────────────────────────────────────────────────────
// ПРОВЕРКА ПО ДЕРЕВУ

func TestEverySeededSecretHasAConsumer(t *testing.T) {
	f := seedConsumerFactsFromTree(t)

	parts := make([]string, 0, len(f.seeded))
	for _, n := range f.seeded {
		parts = append(parts, fmt.Sprintf("%s: %d", n, len(f.consumers[n])))
	}
	t.Logf("осмотрено: объявлений зонта %d; посев заводит секретов %d; потребителей по секрету — %s",
		f.read, len(f.seeded), strings.Join(parts, ", "))

	if f.read == 0 {
		t.Fatalf("объявлений зонта не прочитано ни одного — «ноль находок» здесь " +
			"неотличимо от «ноль прочитанного»")
	}
	if len(f.seeded) == 0 {
		t.Fatalf("из посева %s не прочитано ни одного секрета — либо посев снят, либо "+
			"форма заведения изменилась и разбор ослеп", seedScript)
	}

	for _, msg := range judgeSeedConsumers(f) {
		t.Errorf("%s", msg)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в обе стороны, по одному факту на ось.

func TestJudgeSeedConsumers_SelfTest(t *testing.T) {
	base := seedConsumerFacts{
		seeded:    []string{"секрет-а", "секрет-б"},
		consumers: map[string][]string{"секрет-а": {"values.yaml"}, "секрет-б": {"templates/x.yaml"}},
		read:      10,
	}

	// (0) КОНТРОЛЬ: у каждого посеянного есть потребитель — молчание.
	if got := judgeSeedConsumers(base); len(got) != 0 {
		t.Errorf("(0) посев с потребителями обязан молчать: %v", got)
	}

	// (A) ИНЪЕКЦИЯ — ровно наблюдавшийся дефект: посев заводит секрет без
	//     потребителя.
	orphan := base
	orphan.seeded = []string{"секрет-а", "секрет-б", "секрет-сирота"}
	got := judgeSeedConsumers(orphan)
	if len(got) != 1 || !strings.Contains(got[0], "секрет-сирота") {
		t.Errorf("(A) посеянный секрет без потребителя ПРОПУЩЕН или назван не тот: %v", got)
	}

	// (B) БЛИЗНЕЦ (A): тот же секрет, но потребитель назван — молчание.
	twin := orphan
	twin.consumers = map[string][]string{"секрет-а": {"values.yaml"}, "секрет-б": {"templates/x.yaml"},
		"секрет-сирота": {"values.own.yaml"}}
	if got := judgeSeedConsumers(twin); len(got) != 0 {
		t.Errorf("(B) посеянный секрет с потребителем обязан молчать: %v", got)
	}
}

// TestSeedConsumerRecognisers_RealForms — распознаватели узнают формы
// настоящего дерева: значение ручки имени секрета — потребитель, то же имя в
// комментарии YAML, в хвостовом комментарии и в комментарии шаблона — нет;
// имя внутри более длинного имени — нет.
func TestSeedConsumerRecognisers_RealForms(t *testing.T) {
	const name = "секрет-посева"
	cases := []struct {
		label string
		body  string
		want  bool
	}{
		{"значение ручки имени секрета", "      encKeySecretName: " + name + "\n", true},
		{"имя в secretKeyRef", "          secretKeyRef: {name: " + name + ", key: k}\n", true},
		{"строка-комментарий YAML", "#   - " + name + " (key: token)\n", false},
		{"хвостовой комментарий", "      enabled: true  # " + name + "\n", false},
		{"комментарий шаблона", "{{/* " + name + " */}}\nkind: ConfigMap\n", false},
		{"комментарий шаблона с обрезкой, многострочный", "{{- /*\n  " + name + "\n*/ -}}\n", false},
		{"имя внутри более длинного", "      secretName: " + name + "-v2\n", false},
	}
	for _, c := range cases {
		if got := seedNameBounded(seedExecutablePart(c.body), name); got != c.want {
			t.Errorf("%s: потребитель %v, ожидалось %v", c.label, got, c.want)
		}
	}
}
