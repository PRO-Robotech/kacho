// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Посадочный гейт посева устанавливает посадку по ДОЛГОВЕЧНОМУ свидетельству,
// а его отказ называет НАСТОЯЩУЮ причину.
//
// # Предмет
//
// `tests/authz-fixtures/setup.sh` решает ОДИН вопрос: у кого харнесс берёт
// Bearer'ы. Ответ ровно один законный — у выдающего (iam), и он выражен
// делегированием в `prodseed_all.py`. Всё, что делегированием не заканчивается,
// обязано заканчиваться отказом с названной причиной.
//
// # Почему предмет пробы сменился
//
// Прежде посадка выяснялась чтением строки запуска из ЖУРНАЛА КОНТЕЙНЕРА края.
// Журнал контейнера — эфемерный артефакт: kubelet ротирует его по достижении
// предела, а `kubectl logs` без `--previous` отдаёт только ТЕКУЩИЙ файл. На
// стенде, живущем дольше одного оборота журнала, строка запуска недостижима —
// и гейт, у которого свидетельство исчезло, докладывал «посадка не опознана»,
// приписывал это отсутствию доступа к кластеру (доступ при этом был) и
// предлагал оператору продавить непроверенную классификацию переменной
// окружения. Три дефекта в одной точке: свидетельство эфемерное, диагноз
// ложный, обход молчаливый.
//
// Свидетельство теперь — ИСХОД, а не запись: край сам отвечает на запрос без
// удостоверения. Ответ не ротируется, не зависит от возраста пода и не
// подделывается настройкой: край либо требует удостоверение, либо нет.
//
// # Как это проверяется
//
// Исполнением НАСТОЯЩЕГО скрипта на СИНТЕТИЧЕСКОМ наблюдении. Внешние программы
// подменены заглушками; заглушка `curl` отдаёт заданный код ответа, поэтому
// классификатор можно провести по всем исходам БЕЗ кластера. Наблюдаемое —
// какие подпроцессы скрипт успел запустить, с каким кодом он вышел и что
// сказал оператору:
//
//   - делегирование  ⇒ единственный запуск после проб — `prodseed_all.py`;
//   - отказ          ⇒ до `prodseed_all.py` дело не дошло.
//
// Пробы считаются отдельно от делегирования: «делегировал» без единой пробы
// означало бы, что гейт пропустил стенд, ничего не посмотрев, — и такой зелёный
// неотличим от исправного.

// probeObservation — синтетическое наблюдение, которое заглушка `curl` отдаёт
// скрипту. Пустой код + ненулевой exit = транспортного ответа не было вовсе.
type probeObservation struct {
	// controlCode — ответ на пробу С заведомо негодным удостоверением. Она
	// доказывает лишь одно: адрес пробы доходит до гейта аутентификации. Про
	// посадку она не говорит ничего (негодное удостоверение отвергается в
	// ЛЮБОЙ посадке) — и потому годится как положительный контроль.
	controlCode string
	// decidingCode — ответ на пробу БЕЗ удостоверения. Это и есть решающее
	// наблюдение.
	decidingCode string
	// curlExit — код возврата заглушки. 7 = транспорт не ответил (реальный curl
	// на недоступном адресе выходит 7 и печатает 000).
	curlExit int
}

// stubbedPATHWithProbe собирает каталог заглушек и возвращает PATH, в котором
// они перекрывают настоящие программы. Заглушка пишет `<имя> <аргументы>` в файл
// из $SEED_STUB_LOG — это единственный наблюдаемый выход пробы.
//
// `curl` отличает контрольную пробу от решающей по наличию заголовка
// Authorization в аргументах, а не по порядку вызова: порядок — деталь
// реализации, а предмет пробы — какое наблюдение к какому исходу приводит.
func stubbedPATHWithProbe(t *testing.T, dir, logPath string) string {
	t.Helper()
	for _, name := range []string{"python3", "curl", "grpcurl", "kubectl"} {
		var body string
		switch name {
		case "curl":
			// Печатает код ответа в stdout — ровно то, что делает настоящий
			// curl с `-o /dev/null -w '%{http_code}'`.
			body = "#!/usr/bin/env bash\n" +
				"printf 'curl %s\\n' \"$*\" >> \"$SEED_STUB_LOG\"\n" +
				"if [[ \"$*\" == *Authorization* ]]; then\n" +
				"  printf '%s' \"$SEED_STUB_CTL_CODE\"\n" +
				"else\n" +
				"  printf '%s' \"$SEED_STUB_DEC_CODE\"\n" +
				"fi\n" +
				"exit \"${SEED_STUB_CURL_EXIT:-0}\"\n"
		case "kubectl":
			// kubectl обязан ПРОВАЛИТЬСЯ: иначе блок самообеспечения
			// сертификатом выше гейта запишет мусорные файлы и уведёт пробу от
			// предмета. Его провал заодно доказывает, что новая классификация
			// доступа к кластеру НЕ требует.
			body = "#!/usr/bin/env bash\n" +
				"printf 'kubectl %s\\n' \"$*\" >> \"$SEED_STUB_LOG\"\n" +
				"exit 1\n"
		default:
			body = "#!/usr/bin/env bash\n" +
				"printf '" + name + " %s\\n' \"$*\" >> \"$SEED_STUB_LOG\"\n" +
				"exit 0\n"
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

// seedRun — исход прогона скрипта на синтетическом наблюдении.
type seedRun struct {
	delegated bool // дошло до prodseed_all.py
	probes    int  // сколько раз гейт реально спросил край
	stderr    string
	code      int
}

// runSeedOnObservation исполняет setup.sh, подсунув ему заданное наблюдение
// края. extraEnv позволяет проверить, что посадку нельзя продавить окружением.
func runSeedOnObservation(t *testing.T, obs probeObservation, extraEnv ...string) seedRun {
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
	path := stubbedPATHWithProbe(t, binDir, logPath)

	// Срок жизни пробы. Оба ожидаемых исхода — делегирование и отказ —
	// наступают за доли секунды: они лежат ДО первого обращения к стенду.
	// Скрипт, который за это время не закончил, прошёл гейт и ушёл в поллинг
	// посева. Это ТРЕТИЙ исход — «не выполнилось», и он назван отдельно, а не
	// приравнен ни к отказу, ни к успеху.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", script)
	cmd.Dir = tmp
	cmd.Env = append(os.Environ(),
		"PATH="+path,
		"SEED_STUB_LOG="+logPath,
		"SEED_STUB_CTL_CODE="+obs.controlCode,
		"SEED_STUB_DEC_CODE="+obs.decidingCode,
		fmt.Sprintf("SEED_STUB_CURL_EXIT=%d", obs.curlExit),
		"BASE_URL=http://localhost:18080",
		// Повторы на транспортном отказе не должны растягивать пробу: край в
		// пробе либо отвечает сразу, либо не отвечает вовсе.
		"POSTURE_PROBE_RETRIES=2",
		"POSTURE_PROBE_RETRY_DELAY=0",
		"OUT_DIR="+filepath.Join(tmp, "out"),
		"PATCH_ENV=false",
		"IAM_MTLS_CERT_DIR="+filepath.Join(tmp, "mtls"),
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	cmd.Stdout = &strings.Builder{}
	runErr := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		raw, _ := os.ReadFile(logPath)
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: setup.sh не завершился за 30с. Оба законных исхода "+
			"(делегирование выдающему / отказ) наступают до первого обращения к стенду, "+
			"поэтому такой прогон означает, что скрипт прошёл посадочный гейт и ушёл "+
			"в посев.\nзапуски внешних программ:\n%s\nstderr:\n%s",
			strings.TrimSpace(string(raw)), errBuf.String())
	}
	out := seedRun{stderr: errBuf.String()}
	if runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			out.code = ee.ExitCode()
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
		switch {
		case line == "":
		case strings.HasPrefix(line, "curl "):
			out.probes++
		case strings.Contains(line, "prodseed_all.py"):
			out.delegated = true
		}
	}
	return out
}

// Коды возврата гейта. Три исхода — три кода, и «не выполнилось» не сливается
// ни с отказом, ни с успехом.
const (
	seedProceeds        = 0 // посадка доказана — посев отдан выдающему
	seedRefusesPosture  = 1 // посадка доказана и она НЕ та
	seedEvidenceMissing = 3 // посадку доказать НЕЧЕМ — это не вердикт о посадке
)

// TestSeedPostureFromEdgeRefusal — таблица наблюдений. Положительный контроль
// (край отвергает запрос без удостоверения) держит гейт честным: без него
// «всё отказывает» читалось бы как исправность.
func TestSeedPostureFromEdgeRefusal(t *testing.T) {
	cases := []struct {
		name string
		obs  probeObservation
		want int
		// mustSay — подстроки, которые обязан содержать отказ: оператор должен
		// понять, ЧТО именно наблюдалось.
		mustSay []string
		why     string
	}{
		{
			name:    "край требует удостоверение",
			obs:     probeObservation{controlCode: "401", decidingCode: "401"},
			want:    seedProceeds,
			why:     "положительный контроль: доказанная боевая посадка обязана отдавать посев выдающему, иначе проба доказывала бы лишь «всё сломано»",
		},
		{
			name:    "край обслужил запрос без удостоверения",
			obs:     probeObservation{controlCode: "401", decidingCode: "200"},
			want:    seedRefusesPosture,
			mustSay: []string{"200"},
			why:     "запрос без удостоверения получил ответ по существу — расслабленный край, посев здесь не сеется",
		},
		{
			name:    "край пропустил анонима к авторизации",
			obs:     probeObservation{controlCode: "401", decidingCode: "403"},
			want:    seedRefusesPosture,
			mustSay: []string{"403"},
			why:     "403 без удостоверения означает, что личность была подставлена ДО авторизации — сквозной проход расслабленной посадки",
		},
		{
			name:    "адрес пробы не доходит до гейта аутентификации",
			obs:     probeObservation{controlCode: "403", decidingCode: "403"},
			want:    seedEvidenceMissing,
			mustSay: []string{"403"},
			why:     "контроль не подтвердился ⇒ про посадку НИЧЕГО не известно; объявить это расслабленной посадкой значило бы солгать на переименованном маршруте",
		},
		{
			name:    "край не ответил вовсе",
			obs:     probeObservation{controlCode: "000", decidingCode: "000", curlExit: 7},
			want:    seedEvidenceMissing,
			mustSay: []string{"http://localhost:18080"},
			why:     "нет ответа — нет свидетельства; это третья категория, а не вердикт о посадке",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runSeedOnObservation(t, tc.obs)

			if got.probes == 0 {
				t.Fatalf("гейт не спросил край НИ РАЗУ — значит вердикт вынесен без свидетельства.\n%s\nкод: %d\nstderr:\n%s",
					tc.why, got.code, got.stderr)
			}
			if got.code != tc.want {
				t.Fatalf("наблюдение control=%q deciding=%q exit=%d обязано дать код %d, а дало %d.\n%s\nstderr:\n%s",
					tc.obs.controlCode, tc.obs.decidingCode, tc.obs.curlExit, tc.want, got.code, tc.why, got.stderr)
			}
			if tc.want == seedProceeds {
				if !got.delegated {
					t.Fatalf("доказанная боевая посадка обязана делегировать prodseed_all.py.\n%s\nstderr:\n%s", tc.why, got.stderr)
				}
				return
			}
			if got.delegated {
				t.Fatalf("гейт пропустил стенд к посеву, хотя посадка не доказана.\n%s\nstderr:\n%s", tc.why, got.stderr)
			}
			for _, want := range tc.mustSay {
				if !strings.Contains(got.stderr, want) {
					t.Fatalf("отказ обязан НАЗВАТЬ наблюдённое (%q), иначе оператор не поймёт, что именно отвергнуто.\n%s\nstderr:\n%s",
						want, tc.why, got.stderr)
				}
			}
		})
	}
}

// TestSeedPostureRefusalNamesTheRealCause — отказ не вправе приписывать
// отсутствие свидетельства доступу к кластеру.
//
// Ровно этим прежний гейт и был вреден: свидетельство (строка в журнале)
// исчезало по ротации, а текст отказа советовал «проверь доступ к ns/…» и
// предлагал продавить классификацию переменной окружения. Диагноз был ложным —
// доступ был, — и оператор шёл чинить не то. Новая классификация к кластеру не
// обращается вовсе, поэтому упоминание kubectl в её отказе может означать
// только возврат прежнего дефекта.
func TestSeedPostureRefusalNamesTheRealCause(t *testing.T) {
	got := runSeedOnObservation(t, probeObservation{controlCode: "000", decidingCode: "000", curlExit: 7})
	if got.code != seedEvidenceMissing {
		t.Fatalf("недостижимый край обязан дать код %d («не выполнилось»), а дал %d.\nstderr:\n%s",
			seedEvidenceMissing, got.code, got.stderr)
	}
	for _, forbidden := range []string{"kubectl", "SEED_POSTURE"} {
		if strings.Contains(got.stderr, forbidden) {
			t.Fatalf("отказ на недостижимом крае назвал %q — это ложный диагноз (доступ к кластеру "+
				"классификации больше не нужен) либо возврат молчаливого обхода.\nstderr:\n%s",
				forbidden, got.stderr)
		}
	}
}

// TestSeedPostureCannotBeForcedByEnvironment — переменная окружения больше не
// назначает посадку.
//
// Прежний обход существовал ради «CI без доступа на чтение кластера». У новой
// классификации такого предмета нет: она спрашивает тот же адрес края, до
// которого посеву всё равно надо дозвониться. Обход обязан быть не просто
// снят, но и НЕ ПРИМЕНИМ МОЛЧА: переменная, которая тихо игнорируется, читается
// как «я продавил», и оператор уверен в том, чего не происходило.
func TestSeedPostureCannotBeForcedByEnvironment(t *testing.T) {
	t.Run("продавить расслабленный край не выходит", func(t *testing.T) {
		got := runSeedOnObservation(t,
			probeObservation{controlCode: "401", decidingCode: "200"},
			"SEED_POSTURE=production")
		if got.code == seedProceeds || got.delegated {
			t.Fatalf("окружение продавило посадку, которой край не подтвердил — обход вернулся.\nstderr:\n%s", got.stderr)
		}
	})
	t.Run("переменная объявлена снятой, а не проигнорирована", func(t *testing.T) {
		got := runSeedOnObservation(t,
			probeObservation{controlCode: "401", decidingCode: "401"},
			"SEED_POSTURE=production")
		if got.code == seedProceeds {
			t.Fatalf("SEED_POSTURE тихо проигнорирована: прогон прошёл, и оператор остался в уверенности, "+
				"что он что-то продавил.\nstderr:\n%s", got.stderr)
		}
		if !strings.Contains(got.stderr, "SEED_POSTURE") {
			t.Fatalf("отказ обязан НАЗВАТЬ снятую переменную, иначе непонятно, что именно отвергнуто.\nstderr:\n%s", got.stderr)
		}
	})
}
