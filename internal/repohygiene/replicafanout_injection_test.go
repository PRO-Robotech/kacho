// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// replicafanout_injection_test.go — доказательство способности гейта упасть И
// смолчать.
//
// Гейт, который никогда не краснеет, неотличим от гейта, у которого нет предмета;
// гейт, который краснеет на всём, отключают после первого ложного срабатывания.
// Поэтому каждое утверждение здесь идёт ПАРОЙ: дефект — и его законный близнец.
//
// Разбор берётся ТОТ ЖЕ, что у гейта (scanBackgroundLoops), а не его копия: копия
// разошлась бы молча и доказывала бы способность упасть у кода, который не
// исполняется.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// injectTree кладёт файлы в синтетическое дерево и возвращает его корень.
func injectTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("создать каталог для %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("записать %s: %v", rel, err)
		}
	}
	return root
}

// loopSource собирает файл с фоновой петлёй и заданной шапкой функции.
func loopSource(doc, body string) string {
	if body == "" {
		body = `	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			work(ctx)
		}
	}`
	}
	return "package svc\n\nimport (\n\t\"context\"\n\t\"time\"\n)\n\n" +
		"func work(context.Context) {}\n\n" + doc + "func Run(ctx context.Context) {\n" + body + "\n}\n"
}

func scanFor(t *testing.T, files map[string]string) bgCensus {
	t.Helper()
	c, err := scanBackgroundLoops(injectTree(t, files))
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	return c
}

// A. Фоновая петля БЕЗ записи — находка, и находка НАЗЫВАЕТ координату.
func TestInjection_LoopWithoutFanoutRecordIsAFinding(t *testing.T) {
	c := scanFor(t, map[string]string{
		"services/x/internal/jobs/runner.go": loopSource("// Run крутит работу.\n", ""),
	})
	if c.FilesRead != 1 {
		t.Fatalf("предпосылка: прочитан 1 файл, а не %d", c.FilesRead)
	}
	f := c.Findings()
	if len(f) != 1 {
		t.Fatalf("петля без записи обязана быть находкой, найдено %d", len(f))
	}
	if !strings.Contains(f[0].File, "services/x/internal/jobs/runner.go") || f[0].Func != "Run" {
		t.Fatalf("находка обязана называть координату, получено %s:%d %s", f[0].File, f[0].Line, f[0].Func)
	}
}

// A'. ЗАКОННЫЙ БЛИЗНЕЦ: та же петля с записью — гейт молчит.
func TestInjection_LoopWithFanoutRecordIsSilent(t *testing.T) {
	c := scanFor(t, map[string]string{
		"services/x/internal/jobs/runner.go": loopSource(
			"// Run крутит работу.\n//\n// РЕПЛИКИ: клейм — строки берутся клеймом с пропуском занятых,\n"+
				"// поэтому репликам достаются непересекающиеся партии.\n", ""),
	})
	if got := len(c.Loops); got != 1 {
		t.Fatalf("предпосылка: петля обязана быть найдена, найдено %d", got)
	}
	if f := c.Findings(); len(f) != 0 {
		t.Fatalf("петля с годной записью находкой быть не должна: %+v", f)
	}
	if c.ByKind()["клейм"] != 1 {
		t.Fatalf("вид записи обязан попасть в перепись: %v", c.ByKind())
	}
}

// B. Вид ВНЕ закрытого словаря — находка. Иначе словарь пополнялся бы по ходу
// дела, и «прочее» вернулось бы под другим именем.
func TestInjection_UnknownFanoutKindIsAFinding(t *testing.T) {
	c := scanFor(t, map[string]string{
		"services/x/internal/jobs/runner.go": loopSource(
			"// РЕПЛИКИ: как-нибудь — разберёмся потом, сейчас и так работает нормально.\n", ""),
	})
	f := c.Findings()
	if len(f) != 1 || !strings.Contains(f[0].Bad, "закрытого словаря") {
		t.Fatalf("неизвестный вид обязан быть находкой с объяснением, получено %+v", f)
	}
}

// C. Причина-отписка — находка. Проверка НАЛИЧИЯ маркера без проверки содержания
// есть ровно та форма без содержания, которую гейт и ловит.
func TestInjection_EmptyReasonIsAFinding(t *testing.T) {
	c := scanFor(t, map[string]string{
		"services/x/internal/jobs/runner.go": loopSource("// РЕПЛИКИ: на-реплику — ок\n", ""),
	})
	f := c.Findings()
	if len(f) != 1 || !strings.Contains(f[0].Bad, "отписка") {
		t.Fatalf("короткая причина обязана быть находкой, получено %+v", f)
	}
}

// C'. ЗАКОННЫЙ БЛИЗНЕЦ к C: тот же вид с настоящей причиной — молчание. Без него
// проба C зеленела бы и на гейте, отвергающем вид «на-реплику» целиком.
func TestInjection_PerReplicaWithRealReasonIsSilent(t *testing.T) {
	c := scanFor(t, map[string]string{
		"services/x/internal/jobs/runner.go": loopSource(
			"// РЕПЛИКИ: на-реплику — петля обновляет кэш СВОЕГО процесса и общего\n"+
				"// состояния не трогает; каждая реплика обязана делать это сама.\n", ""),
	})
	if f := c.Findings(); len(f) != 0 {
		t.Fatalf("вид «на-реплику» с настоящей причиной находкой быть не должен: %+v", f)
	}
}

// D. Петля БЕЗ тика/уведомления/паузы фоновой не считается — иначе гейт требовал
// бы записи от каждого `for` в дереве и был бы снят первым же читателем.
func TestInjection_PlainLoopIsNotBackgroundWork(t *testing.T) {
	c := scanFor(t, map[string]string{
		"services/x/internal/jobs/plain.go": "package svc\n\nfunc Sum(xs []int) int {\n" +
			"\ttotal := 0\n\tfor _, x := range xs {\n\t\ttotal += x\n\t}\n\treturn total\n}\n",
	})
	if c.FilesRead != 1 {
		t.Fatalf("предпосылка: файл обязан быть прочитан, прочитано %d", c.FilesRead)
	}
	if len(c.Loops) != 0 {
		t.Fatalf("обычная петля фоновой не является, найдено %d", len(c.Loops))
	}
}

// E. Пауза и ожидание уведомления — тоже фоновая работа. Без этого утверждения
// признак сузился бы до тикера, и петля на `time.Sleep` уехала бы мимо гейта.
func TestInjection_SleepAndNotifyLoopsAreBackgroundWork(t *testing.T) {
	sleepLoop := "package svc\n\nimport (\n\t\"context\"\n\t\"time\"\n)\n\n" +
		"func Run(ctx context.Context) {\n\tfor {\n\t\tif ctx.Err() != nil {\n\t\t\treturn\n\t\t}\n" +
		"\t\ttime.Sleep(time.Second)\n\t}\n}\n"
	notifyLoop := "package svc\n\nimport \"context\"\n\n" +
		"type conn interface{ WaitForNotification(context.Context) error }\n\n" +
		"func Listen(ctx context.Context, c conn) {\n\tfor {\n\t\tif err := c.WaitForNotification(ctx); err != nil {\n" +
		"\t\t\treturn\n\t\t}\n\t}\n}\n"
	c := scanFor(t, map[string]string{
		"services/x/internal/jobs/sleep.go":  sleepLoop,
		"services/x/internal/jobs/notify.go": notifyLoop,
	})
	if len(c.Loops) != 2 {
		t.Fatalf("пауза и ожидание уведомления обязаны считаться фоновой работой, найдено %d: %+v",
			len(c.Loops), c.Loops)
	}
	drivers := map[string]bool{}
	for _, l := range c.Loops {
		drivers[l.Driver] = true
	}
	if !drivers["пауза"] || !drivers["уведомление"] {
		t.Fatalf("обе движущие силы обязаны быть названы, получено %v", drivers)
	}
}

// F. Область осмотра: дублёр порта и клиентская библиотека выведены НАЗВАННО.
// Проба существует ради того, чтобы исключение не расползлось: если оно однажды
// начнёт покрывать прод-код, это утверждение покраснеет вместе с ним.
func TestInjection_MocksAndClientSDKAreOutOfScope(t *testing.T) {
	body := loopSource("// Run крутит работу.\n", "")
	c := scanFor(t, map[string]string{
		"services/x/internal/ports/portmock/portmock.go": body,
		"services/x/pkg/sdk/x/waiter.go":                 body,
		"services/x/internal/jobs/runner.go":             body,
	})
	if len(c.Loops) != 1 {
		t.Fatalf("осмотру подлежит только прод-петля, найдено %d: %+v", len(c.Loops), c.Loops)
	}
	if !strings.Contains(c.Loops[0].File, "internal/jobs/runner.go") {
		t.Fatalf("осмотрен не тот файл: %s", c.Loops[0].File)
	}
}

// G. Проба и каталог вне развёрнутого процесса не осматриваются.
func TestInjection_TestFilesAndOutOfTreeDirsAreNotScanned(t *testing.T) {
	body := loopSource("// Run крутит работу.\n", "")
	c := scanFor(t, map[string]string{
		"services/x/internal/jobs/runner_test.go": body,
		"terraform/internal/provider/wait.go":     body,
		"tools/bench/wait.go":                     body,
	})
	if c.FilesRead != 0 || len(c.Loops) != 0 {
		t.Fatalf("ни проба, ни каталоги вне развёрнутого процесса не осматриваются, "+
			"прочитано %d, найдено %d", c.FilesRead, len(c.Loops))
	}
}

// H. Одна функция — одна запись, даже если петель в ней две. Иначе гейт требовал
// бы копию маркера, а копия расходится с оригиналом молча.
func TestInjection_TwoLoopsInOneFunctionNeedOneRecord(t *testing.T) {
	two := "package svc\n\nimport (\n\t\"context\"\n\t\"time\"\n)\n\n" +
		"// РЕПЛИКИ: одиночка — проход берёт одна реплика замком прохода в базе.\n" +
		"func Run(ctx context.Context) {\n" +
		"\tt := time.NewTicker(time.Second)\n\tdefer t.Stop()\n" +
		"\tfor {\n\t\tselect {\n\t\tcase <-ctx.Done():\n\t\t\treturn\n\t\tcase <-t.C:\n\t\t}\n\t}\n" +
		"}\n\n" +
		"func Second(ctx context.Context) {\n" +
		"\tfor {\n\t\tselect {\n\t\tcase <-ctx.Done():\n\t\t\treturn\n\t\tcase <-time.After(time.Second):\n\t\t}\n\t}\n}\n"
	c := scanFor(t, map[string]string{"services/x/internal/jobs/two.go": two})
	if len(c.Loops) != 2 {
		t.Fatalf("предпосылка: в файле две функции с петлями, найдено %d", len(c.Loops))
	}
	f := c.Findings()
	if len(f) != 1 || f[0].Func != "Second" {
		t.Fatalf("находкой обязана быть ровно вторая функция, получено %+v", f)
	}
}
