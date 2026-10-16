// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «группа concurrency процесса запроса различает
// ветку; production-posture без отмены» СПОСОБЕН упасть — в обе стороны:
//
//	общая группа у процесса запроса (отображение)  → краснеет, называя координату;
//	то же с `${{ github.ref }}`                     → молчит, группа засчитана;
//	общая группа строкой (вторая форма записи)      → краснеет;
//	общая группа уровня задания                     → краснеет;
//	`github.ref` только в комментарии               → краснеет: комментарий не различитель;
//	общая группа без триггера запроса               → молчит (законная сериализация);
//	номер запроса и github.head_ref                 → молчат (законные различители);
//	cancel-in-progress: true у posture              → краснеет (уборка);
//	cancel-in-progress выражением у posture         → краснеет;
//	НАСТОЯЩИЙ файл дерева с вывернутой группой      → краснеет;
//	НАСТОЯЩИЙ файл дерева как есть                  → молчит.
//
// Обе половины гоняют ТУ ЖЕ функцию `auditWorkflowConcurrency`, что и прогон по
// дереву.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const synthPRWorkflowHead = `name: синтетика
on:
  pull_request:
    branches: [main]
`

func synthConcurrency(group, cancel string) string {
	return synthPRWorkflowHead + "concurrency:\n  group: " + group + "\n  cancel-in-progress: " + cancel + `
jobs:
  stand:
    runs-on: ubuntu-latest
    steps:
      - run: make dev-prod-up
`
}

func mustFind(t *testing.T, path, raw, want string) {
	t.Helper()
	findings, _ := auditWorkflowConcurrency(path, raw)
	for _, f := range findings {
		if strings.Contains(f, want) {
			return
		}
	}
	t.Fatalf("ожидалась находка с %q, получено %d: %v", want, len(findings), findings)
}

func mustBeSilent(t *testing.T, path, raw string) workflowConcurrencyCensus {
	t.Helper()
	findings, census := auditWorkflowConcurrency(path, raw)
	if len(findings) != 0 {
		t.Fatalf("законный близнец дал находки: %v", findings)
	}
	return census
}

func TestWorkflowConcurrencyInjection_SharedGroupOnPullRequestIsFound(t *testing.T) {
	t.Parallel()
	mustFind(t, "x.yml", synthConcurrency("production-posture", "false"), "x.yml:6: группа concurrency процесса `production-posture`")
}

func TestWorkflowConcurrencyInjection_RefGroupIsSilentAndCounted(t *testing.T) {
	t.Parallel()
	c := mustBeSilent(t, "x.yml", synthConcurrency("posture-${{ github.ref }}", "false"))
	if c.prFiles != 1 || c.groups != 1 {
		t.Fatalf("близнец не засчитан переписью: %+v", c)
	}
}

func TestWorkflowConcurrencyInjection_ScalarFormIsJudged(t *testing.T) {
	t.Parallel()
	raw := synthPRWorkflowHead + "concurrency: shared-queue\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n"
	mustFind(t, "x.yml", raw, "`shared-queue`")
	twin := synthPRWorkflowHead + "concurrency: q-${{ github.head_ref }}\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n"
	mustBeSilent(t, "x.yml", twin)
}

func TestWorkflowConcurrencyInjection_JobLevelGroupIsJudged(t *testing.T) {
	t.Parallel()
	raw := synthPRWorkflowHead + "jobs:\n  stand:\n    runs-on: ubuntu-latest\n    concurrency:\n      group: stand\n    steps:\n      - run: true\n"
	mustFind(t, "x.yml", raw, "задания `stand`")
	twin := synthPRWorkflowHead + "jobs:\n  stand:\n    runs-on: ubuntu-latest\n    concurrency:\n      group: stand-${{ github.event.pull_request.number }}\n    steps:\n      - run: true\n"
	mustBeSilent(t, "x.yml", twin)
}

func TestWorkflowConcurrencyInjection_RefInCommentIsNotADiscriminator(t *testing.T) {
	t.Parallel()
	raw := synthPRWorkflowHead + "concurrency:\n  group: shared  # github.ref здесь только в комментарии\n  cancel-in-progress: false\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n"
	mustFind(t, "x.yml", raw, "`shared`")
	// Слово без выражения провайдера тоже не различитель: группа — литерал.
	mustFind(t, "x.yml", synthConcurrency("posture-github.ref", "false"), "`posture-github.ref`")
	// Похожее имя — не различитель.
	mustFind(t, "x.yml", synthConcurrency("p-${{ github.refx }}", "false"), "github.refx")
}

func TestWorkflowConcurrencyInjection_NoPullRequestTriggerMaySerialize(t *testing.T) {
	t.Parallel()
	raw := "name: выпуск\non:\n  push:\n    tags: ['v*']\nconcurrency:\n  group: release-publisher\n  cancel-in-progress: false\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n"
	c := mustBeSilent(t, "x.yml", raw)
	if c.prFiles != 0 {
		t.Fatalf("процесс без триггера запроса засчитан как процесс запроса: %+v", c)
	}
}

func TestWorkflowConcurrencyInjection_PostureCancelTrueIsFound(t *testing.T) {
	t.Parallel()
	mustFind(t, postureWorkflowPath, synthConcurrency("posture-${{ github.ref }}", "true"), "cancel-in-progress `true`")
	mustFind(t, postureWorkflowPath, synthConcurrency("posture-${{ github.ref }}", "${{ github.event_name == 'pull_request' }}"), "cancel-in-progress `${{")
	// Тот же true у ДРУГОГО процесса — законен (ci.yaml, e2e-newman отменяют).
	mustBeSilent(t, "other.yml", synthConcurrency("o-${{ github.ref }}", "true"))
	// У posture `false` литералом — законен.
	mustBeSilent(t, postureWorkflowPath, synthConcurrency("posture-${{ github.ref }}", "false"))
	// Без группы — предпосылка не выполняется, а не тишина.
	mustFind(t, postureWorkflowPath, synthPRWorkflowHead+"jobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n", "предпосылка гейта не выполняется")
}

// Настоящий вход: файл дерева, у которого вывернута ровно группа.
func TestWorkflowConcurrencyInjection_RealPostureFileWithSharedGroupIsFound(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), postureWorkflowPath))
	if err != nil {
		t.Fatalf("предмет не прочитан: %v", err)
	}
	mustBeSilent(t, postureWorkflowPath, string(raw))

	groupLine := regexp.MustCompile(`(?m)^  group: .*$`)
	if n := len(groupLine.FindAllString(string(raw), -1)); n != 1 {
		t.Fatalf("строк группы уровня процесса в предмете %d, ожидалась 1 — инъекция не адресуется", n)
	}
	shared := groupLine.ReplaceAllString(string(raw), "  group: production-posture")
	mustFind(t, postureWorkflowPath, shared, "`production-posture` у процесса, запускаемого запросом слияния")

	cancelLine := regexp.MustCompile(`(?m)^  cancel-in-progress: false$`)
	if !cancelLine.MatchString(string(raw)) {
		t.Fatalf("в предмете нет `cancel-in-progress: false` уровня процесса — инъекция не адресуется")
	}
	mustFind(t, postureWorkflowPath, cancelLine.ReplaceAllString(string(raw), "  cancel-in-progress: true"), "cancel-in-progress `true`")
}
