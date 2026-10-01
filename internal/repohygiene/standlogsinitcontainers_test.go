// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// standlogsinitcontainers_test.go — ОТКАЗ INIT-КОНТЕЙНЕРА ДОЕЗЖАЕТ ДО АРТЕФАКТА
// (kacho#2915).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Сбор журналов стенда читал каждый носитель ОДНИМ обращением
// `kubectl logs <носитель> --all-containers`. Когда init-контейнер уходит в
// CrashLoopBackOff, основной контейнер пода стоит в PodInitializing, и чтение
// его журнала отказывает («is waiting to start: PodInitializing»). Отказ
// одного контейнера ронял всё обращение — и файл, в который уже лёг журнал
// init-контейнера, ПЕРЕЗАПИСЫВАЛСЯ строкой «журнал недоступен». Текст отказа
// мигратора терялся ровно тогда, когда он единственный называл причину:
// прогон 36904979893, все четыре шарда, в артефакте — одна строка про
// PodInitializing.
//
// Утверждается исходом исполнения скрипта против подменного `kubectl`, а не
// текстом скрипта:
//
//   - журнал прошлого запуска init-контейнера с рестартами лежит отдельным
//     файлом и несёт текст отказа;
//   - отказ чтения носителя целиком не стирает уже прочитанное;
//   - близнец: init-контейнер без рестартов даёт журнал текущего запуска и НЕ
//     даёт файла прошлого (его нет и читать нечего).
package repohygiene

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeKubectl — подменный kubectl: один под с init-контейнером `migrate` и
// основным `kaname`, число рестартов init-контейнера — параметр.
const fakeKubectl = `#!/usr/bin/env bash
args="$*"
case "$args" in
  *"get deploy,statefulset -o name"*)
    echo "deployment.apps/kaname" ;;
  *"get pods -o jsonpath="*)
    printf 'kaname-abc\tinit:migrate:%s main:kaname:0 \n' "$FAKE_RESTARTS" ;;
  *"logs deployment.apps/kaname --all-containers"*)
    echo "[pod/kaname-abc/migrate] current-run-line"
    echo 'Error from server (BadRequest): container "kaname" in pod "kaname-abc" is waiting to start: PodInitializing' >&2
    exit 1 ;;
  *"logs pod/kaname-abc -c migrate --previous"*)
    echo 'Error: dsn unset and service config load failed: unknown configuration key ` + "`jobs.catalog-snapshot.refresh-interval`" + `' ;;
  *"logs pod/kaname-abc -c migrate"*)
    echo "current-run-line" ;;
  *"get events"*)
    echo "events" ;;
  *)
    echo "fake kubectl: unexpected call: $args" >&2
    exit 3 ;;
esac
`

func runStandLogCollector(t *testing.T, restarts string) (string, string) {
	t.Helper()
	root := repoRoot(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(fakeKubectl), 0o755); err != nil { // #nosec G306 -- исполняемая подмена в каталоге пробы
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "stand-logs")
	cmd := exec.Command("bash", filepath.Join(root, ".github", "scripts", standLogsCollector), "kacho", out, "500") // #nosec G204 -- скрипт дерева
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "FAKE_RESTARTS="+restarts)
	stdout, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("сбор журналов обязан выходить успехом (он диагностика): %v\n%s", err, stdout)
	}
	if strings.Contains(string(stdout), "unexpected call") {
		t.Fatalf("скрипт звал kubectl формой, которой подмена не знает:\n%s", stdout)
	}
	return out, string(stdout)
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path) // #nosec G304 -- файл пробы
	if err != nil {
		t.Fatalf("журнал %s не лёг в артефакт: %v", filepath.Base(path), err)
	}
	return string(body)
}

// TestInitContainerRefusalReachesTheArtifact — init-контейнер с рестартами.
func TestInitContainerRefusalReachesTheArtifact(t *testing.T) {
	t.Parallel()
	out, stdout := runStandLogCollector(t, "4")

	prev := readLog(t, filepath.Join(out, "init-kaname-abc-migrate.previous.log"))
	if !strings.Contains(prev, "unknown configuration key `jobs.catalog-snapshot.refresh-interval`") {
		t.Errorf("журнал прошлого запуска init-контейнера обязан нести текст отказа, получено:\n%s", prev)
	}
	whole := readLog(t, filepath.Join(out, "deployment-kaname.log"))
	if !strings.Contains(whole, "current-run-line") {
		t.Errorf("отказ чтения носителя целиком стёр уже прочитанное:\n%s", whole)
	}
	if !strings.Contains(whole, "PodInitializing") {
		t.Errorf("отказ чтения носителя обязан называться в его файле:\n%s", whole)
	}
	if !strings.Contains(stdout, "init-контейнеров") {
		t.Errorf("перепись обязана называть init-контейнеры:\n%s", stdout)
	}
}

// TestInitContainerWithoutRestartsHasNoPreviousLog — близнец: тот же под,
// рестартов ноль.
func TestInitContainerWithoutRestartsHasNoPreviousLog(t *testing.T) {
	t.Parallel()
	out, _ := runStandLogCollector(t, "0")

	if cur := readLog(t, filepath.Join(out, "init-kaname-abc-migrate.log")); !strings.Contains(cur, "current-run-line") {
		t.Errorf("журнал текущего запуска init-контейнера обязан лечь в артефакт:\n%s", cur)
	}
	if _, err := os.Stat(filepath.Join(out, "init-kaname-abc-migrate.previous.log")); err == nil {
		t.Error("без рестартов прошлого запуска нет — файла прошлого журнала быть не должно")
	}
}
