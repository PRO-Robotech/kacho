// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Посадочный гейт посева не имеет корзины «всё остальное».
//
// # Предмет
//
// `tests/authz-fixtures/setup.sh` решает ОДИН вопрос: у кого харнесс берёт
// Bearer'ы. Ответ ровно один законный — у выдающего (iam), и он выражен
// делегированием в `prodseed_all.py`. Всё, что делегированием не заканчивается,
// обязано заканчиваться отказом с названной причиной.
//
// Отказ у гейта был написан для ДВУХ значений — `dev` и того, что сообщает живой
// край. Разбор же ветвился на трёх: `dev`, `auto` и «forced». Третья ветка ничего
// не проверяла: она печатала «posture: <как есть>» и шла дальше, а сравнение
// с `production` стояло НИЖЕ и было точечным. Значит любое значение, кроме трёх
// узнанных, проваливалось МИМО гейта в ветку симметричной чеканки.
//
// Хуже всего, что среди таких значений — `production-strict`: имя САМОЙ строгой
// посадки, которое автоопределение специально сопоставляет с `production`
// (`case production|production-strict`). Указанное вручную, оно выбирало ровно
// противоположное тому, что называет. Тот же исход у любой опечатки (`prod`,
// `Production`) — и в каждом случае прогон падал бы несколькими шагами позже,
// в месте, которое читается как дефект продукта.
//
// # Как это проверяется
//
// Исполнением НАСТОЯЩЕГО скрипта, а не чтением его текста: греп по строкам
// подтвердил бы наличие слова `refuse`, которое в нём и так есть — оно и было в
// нём, пока дыра существовала. Наблюдаемое — какие подпроцессы скрипт успел
// запустить. Внешние программы подменены заглушками, каждая пишет свой запуск в
// общий журнал:
//
//   - делегирование  ⇒ первым (и единственным) запуском идёт `prodseed_all.py`;
//   - отказ          ⇒ журнал ПУСТ: скрипт вышел до первого обращения наружу.
//
// Две формы не пересекаются, поэтому ни один исход не засчитывается «в обе
// стороны». `kubectl` из журнала исключён намеренно: его скрипт зовёт ВЫШЕ
// гейта (самообеспечение клиентским сертификатом), и его присутствие ничего не
// говорит о посадке.

// stubbedPATH собирает каталог заглушек и возвращает PATH, в котором они
// перекрывают настоящие программы. Заглушка пишет `<имя> <аргументы>` в файл из
// $SEED_STUB_LOG — это единственный наблюдаемый выход пробы.
func stubbedPATH(t *testing.T, dir, logPath string) string {
	t.Helper()
	for _, name := range []string{"python3", "curl", "grpcurl", "kubectl"} {
		body := "#!/usr/bin/env bash\n" +
			"printf '%s %s\\n' " + name + " \"$*\" >> \"$SEED_STUB_LOG\"\n"
		// kubectl обязан ПРОВАЛИТЬСЯ: иначе блок самообеспечения сертификатом
		// выше гейта запишет мусорные файлы и уведёт пробу от предмета.
		if name == "kubectl" {
			body += "exit 1\n"
		} else {
			body += "exit 0\n"
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatalf("write stub %s: %v", p, err)
		}
	}
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		t.Fatalf("create stub log: %v", err)
	}
	return dir + string(os.PathListSeparator) + os.Getenv("PATH")
}

// runSeedWithPosture исполняет setup.sh с заданным SEED_POSTURE и возвращает
// (запуски внешних программ кроме kubectl, stderr, код возврата).
func runSeedWithPosture(t *testing.T, posture string) (calls []string, stderr string, code int) {
	t.Helper()
	root := repoRoot(t)
	script := filepath.Join(root, "tests", "authz-fixtures", "setup.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("предмет пробы отсутствует (%s): %v", script, err)
	}
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", binDir, err)
	}
	logPath := filepath.Join(tmp, "calls.log")
	path := stubbedPATH(t, binDir, logPath)

	// Срок жизни пробы. Оба ожидаемых исхода — делегирование и отказ — наступают
	// за доли секунды: они лежат ДО первого обращения к стенду. Скрипт, который
	// за это время не закончил, прошёл гейт и ушёл в поллинг посева (свои
	// собственные ожидания там измеряются минутами). Это ТРЕТИЙ исход — «не
	// выполнилось», и он назван отдельно, а не приравнен ни к отказу, ни к успеху.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", script)
	cmd.Dir = tmp
	cmd.Env = append(os.Environ(),
		"PATH="+path,
		"SEED_STUB_LOG="+logPath,
		"SEED_POSTURE="+posture,
		"OUT_DIR="+filepath.Join(tmp, "out"),
		"PATCH_ENV=false",
		"IAM_MTLS_CERT_DIR="+filepath.Join(tmp, "mtls"),
	)
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	cmd.Stdout = &strings.Builder{}
	runErr := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		raw, _ := os.ReadFile(logPath)
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: setup.sh не завершился за 20с. Оба законных исхода "+
			"(делегирование выдающему / отказ) наступают до первого обращения к стенду, "+
			"поэтому такой прогон означает, что скрипт прошёл посадочный гейт и ушёл "+
			"в посев.\nзапуски внешних программ:\n%s\nstderr:\n%s",
			strings.TrimSpace(string(raw)), errBuf.String())
	}
	if runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			code = ee.ExitCode()
		} else {
			t.Fatalf("запуск %s: %v", script, runErr)
		}
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read stub log: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "kubectl ") {
			continue
		}
		calls = append(calls, line)
	}
	return calls, errBuf.String(), code
}

// TestSeedPostureDelegatesOrRefuses — таблица посадок. Положительные случаи
// (`production`, `dev`) держат гейт честным: без них «всё отказывает» читалось бы
// как исправность.
func TestSeedPostureDelegatesOrRefuses(t *testing.T) {
	const (
		delegates = "delegates"
		refuses   = "refuses"
	)
	cases := []struct {
		posture string
		want    string
		why     string
	}{
		{"production", delegates,
			"положительный контроль: узнанная посадка обязана отдавать посев выдающему, " +
				"иначе проба доказывала бы лишь «всё сломано»"},
		{"production-strict", delegates,
			"автоопределение сопоставляет production-strict с production (case production|production-strict); " +
				"указанное вручную то же имя обязано вести туда же, а не в противоположную ветку"},
		{"dev", refuses,
			"положительный контроль отказа: посадки с общим ключом не производит ни один профиль"},
		{"prod", refuses,
			"неузнанное значение — не повод продолжать: корзины «всё остальное» не бывает"},
		{"Production", refuses,
			"регистр не узнан разбором; продолжать по неузнанному значению — тот же дефект"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.posture, func(t *testing.T) {
			calls, stderr, code := runSeedWithPosture(t, tc.posture)
			switch tc.want {
			case delegates:
				if code != 0 {
					t.Fatalf("SEED_POSTURE=%q обязан делегировать выдающему, а вышел с кодом %d.\n%s\nstderr:\n%s",
						tc.posture, code, tc.why, stderr)
				}
				if len(calls) != 1 || !strings.Contains(calls[0], "prodseed_all.py") {
					t.Fatalf("SEED_POSTURE=%q обязан делегировать prodseed_all.py и НИЧЕГО не запускать сам.\n%s\n"+
						"запуски внешних программ: %v\nstderr:\n%s", tc.posture, tc.why, calls, stderr)
				}
			case refuses:
				if code == 0 {
					t.Fatalf("SEED_POSTURE=%q обязан отказать с названной причиной, а вышел успешно.\n%s\n"+
						"запуски внешних программ: %v\nstderr:\n%s", tc.posture, tc.why, calls, stderr)
				}
				if len(calls) != 0 {
					t.Fatalf("SEED_POSTURE=%q дошёл до обращения наружу — значит гейт его пропустил.\n%s\n"+
						"запуски внешних программ: %v\nstderr:\n%s", tc.posture, tc.why, calls, stderr)
				}
				if !strings.Contains(stderr, tc.posture) {
					t.Fatalf("отказ на SEED_POSTURE=%q обязан НАЗВАТЬ значение, иначе оператор не поймёт, "+
						"что именно отвергнуто.\nstderr:\n%s", tc.posture, stderr)
				}
			}
		})
	}
}
