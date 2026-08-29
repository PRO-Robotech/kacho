// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// pool_out_of_pool_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что слагаемое вне
// пула действительно считается, что его неполнота видна, и что распознаватель
// молчит на законных близнецах.
//
// Настоящее дерево нельзя ни сломать, ни вернуть, а вердикт о нём о способности
// проверки падать не говорит ничего: зелёный получает и та, что не смотрит
// никуда. Поэтому каждый случай стоит ПАРОЙ — внесённый дефект обязан краснеть И
// НАЗЫВАТЬ координату, законный близнец той же формы — молчать.
//
// Близнецы не выдуманы: перехватчик HTTP-соединения на крае и вызов `Connect` у
// пакета, который драйвером не является, взяты из настоящего дерева.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// АРИФМЕТИКА: слагаемое вне пула ВХОДИТ в произведение

// TestPoolGateCountsWhatIsHeldOutsideThePool — инъекция в арифметику.
//
// Обе половины пары стоят на ОДНОМ входе, различающемся ровно слагаемым: пул
// один и тот же, потолок подов один и тот же, предел базы один и тот же. Если бы
// слагаемое не входило в сумму, обе половины молчали бы — и проверка выглядела бы
// исправной.
func TestPoolGateCountsWhatIsHeldOutsideThePool(t *testing.T) {
	// Пул один помещается: 15 × 5 = 75 ≤ 97.
	fits := poolLink{service: "compute", pool: 15, poolPath: "db.maxConns", replicas: 5, replicasPath: "replicas"}

	t.Run("без слагаемого проверка молчит — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ", func(t *testing.T) {
		findings, examined, _ := scanPoolFits([]poolFacts{pfStack("контроль", 100, fits)})
		if len(findings) != 0 {
			t.Fatalf("сходящаяся посадка объявлена негодной: %v", findings)
		}
		if examined != 1 {
			t.Fatalf("осмотрено связок %d, ожидалась 1: молчание на непрочитанном не доказывает ничего", examined)
		}
	})

	t.Run("со слагаемым вне пула — находка с числами", func(t *testing.T) {
		over := fits
		over.outOfPool = 17
		over.outOfPoolWhy = []string{"поток подписки: 16", "LISTEN дренажа очереди: 1"}
		findings, _, _ := scanPoolFits([]poolFacts{pfStack("инъекция", 100, over)})
		if kindsOf(findings)[kindOverPromise] != 1 {
			t.Fatalf("слагаемое вне пула не вошло в произведение: %v", findings)
		}
		// Находка обязана называть ОБА слагаемых: «обещано больше» без разбора
		// посылает править не ту величину.
		if !strings.Contains(findings[0].why, "160") || !strings.Contains(findings[0].why, "вне пула") {
			t.Fatalf("находка не называет разбор суммы: %q", findings[0].why)
		}
	})

	t.Run("невыведенное слагаемое — находка, а не ноль", func(t *testing.T) {
		unknown := fits
		unknown.outOfPoolUnknown = []string{"поток подписки — потолка не объявлено"}
		findings, examined, _ := scanPoolFits([]poolFacts{pfStack("инъекция", 100, unknown)})
		if kindsOf(findings)[kindOutOfPoolUnknown] != 1 {
			t.Fatalf("невыведенное слагаемое принято за ноль: %v", findings)
		}
		// И связка НЕ идёт в осмотренные: считать её осмотренной значило бы
		// зачесть в вердикт сумму с неизвестным слагаемым.
		if examined != 0 {
			t.Fatalf("связка с невыведенным слагаемым зачтена осмотренной (%d)", examined)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// ПОЛНОТА: захват, не приписанный никому, — находка

// outOfPoolStand — синтетическое дерево той же формы, что настоящее.
type outOfPoolStand struct{ root string }

func newOutOfPoolStand(t *testing.T) *outOfPoolStand {
	t.Helper()
	return &outOfPoolStand{root: t.TempDir()}
}

func (s *outOfPoolStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func (s *outOfPoolStand) read(t *testing.T) *outOfPoolTree {
	t.Helper()
	return readOutOfPoolTreeAt(t, s.root)
}

const (
	// Захват своего соединения. Путь драйвера ВЕРСИОНИРОВАН, а пакет зовётся
	// иначе — на этом первая редакция распознавателя и ослепла.
	captureConnect = "package sub\n\nimport (\n\t\"context\"\n\n\t\"github.com/jackc/pgx/v5\"\n)\n\n" +
		"func dial(ctx context.Context, dsn string) error {\n" +
		"\tconn, err := pgx.Connect(ctx, dsn)\n\tif err != nil {\n\t\treturn err\n\t}\n" +
		"\t_, err = conn.Exec(ctx, \"LISTEN journal\")\n\treturn err\n}\n"
	// Изъятие соединения из пула ради подписки.
	captureHijack = "package drn\n\nimport \"context\"\n\n" +
		"type acquired interface{ Hijack() session }\n" +
		"type session interface {\n\tExec(context.Context, string) (int, error)\n}\n\n" +
		"func listen(ctx context.Context, pc acquired) error {\n" +
		"\tconn := pc.Hijack()\n\t_, err := conn.Exec(ctx, \"LISTEN outbox\")\n\treturn err\n}\n"
	// ЗАКОННЫЙ БЛИЗНЕЦ 1: перехват HTTP-соединения. Метод тот же, подписки нет.
	twinHTTPHijack = "package mw\n\nimport (\n\t\"bufio\"\n\t\"net\"\n\t\"net/http\"\n)\n\n" +
		"func take(h http.Hijacker) (net.Conn, *bufio.ReadWriter, error) {\n\treturn h.Hijack()\n}\n"
	// ЗАКОННЫЙ БЛИЗНЕЦ 2: `Connect` у пакета, который драйвером базы не является.
	twinForeignConnect = "package cli\n\nimport (\n\t\"context\"\n\n\t\"google.golang.org/grpc\"\n)\n\n" +
		"var _ = grpc.WithBlock\n\ntype dialer interface {\n\tConnect(context.Context) error\n}\n\n" +
		"func go2(ctx context.Context, d dialer) error { return d.Connect(ctx) }\n"
)

// TestOutOfPoolRecogniserTellsCapturesFromTheirNamesakes — КОНТРОЛЬ роли.
//
// Оба близнеца зовутся так же, как захваты, и оба обязаны молчать. Без этой
// половины распознаватель мог бы «находить» всё подряд, и полнота арифметики
// стала бы шумом, который перестают читать.
func TestOutOfPoolRecogniserTellsCapturesFromTheirNamesakes(t *testing.T) {
	s := newOutOfPoolStand(t)
	s.write(t, "pkg/subscription/server.go", captureConnect)
	s.write(t, "pkg/outbox/drainer/internal.go", captureHijack)
	s.write(t, "gateway/internal/middleware/access_log.go", twinHTTPHijack)
	s.write(t, "services/probe/internal/clients/peer.go", twinForeignConnect)

	tree := s.read(t)
	if tree.files != 4 {
		t.Fatalf("прочитано файлов %d, ожидалось 4: обход не дошёл до близнецов", tree.files)
	}
	got := make([]string, 0, len(tree.captures))
	for _, c := range tree.captures {
		got = append(got, c.File)
	}
	want := []string{"pkg/outbox/drainer/internal.go", "pkg/subscription/server.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("захваты опознаны как %v, ожидались %v: роль подменена именем", got, want)
	}
}

// TestOutOfPoolRecogniserResolvesVersionedImportPaths — ИНЪЕКЦИЯ в разрешение
// имени пакета.
//
// Первая редакция брала последний сегмент пути импорта и потому для
// `github.com/jackc/pgx/v5` считала, что пакет зовётся `v5`. Захватов
// находилось НОЛЬ при трёх живых, и перепись печатала «захватов 0» — то есть
// слепота выглядела чистым деревом. Проба пинит именно разрешение имени.
func TestOutOfPoolRecogniserResolvesVersionedImportPaths(t *testing.T) {
	if got := packageNameOf("github.com/jackc/pgx/v5"); got != "pgx" {
		t.Fatalf("имя пакета версионированного пути = %q, ожидалось \"pgx\"", got)
	}
	if got := packageNameOf("github.com/jackc/pgx/v5/pgxpool"); got != "pgxpool" {
		t.Fatalf("имя пакета = %q, ожидалось \"pgxpool\"", got)
	}
	// Законный близнец: сегмент, начинающийся с `v`, но версией не являющийся.
	if got := packageNameOf("github.com/example/vault"); got != "vault" {
		t.Fatalf("имя пакета = %q, ожидалось \"vault\": «v» принято за версию", got)
	}
}

// TestOutOfPoolArithmeticIsCompleteOrSaysItIsNot — ИНЪЕКЦИЯ в полноту.
//
// Захват, не приписанный ни службе, ни записи каталога, обязан быть НАЗВАН.
// Молчание здесь и есть та самая «честная неполнота, названная полнотой»: сумма
// сходится по отсутствию слагаемого, а не по сходимости.
func TestOutOfPoolArithmeticIsCompleteOrSaysItIsNot(t *testing.T) {
	write := func(t *testing.T, s *outOfPoolStand) {
		t.Helper()
		for _, h := range outOfPoolHolders {
			body := captureConnect
			if strings.Contains(h.Site, "drainer") {
				body = captureHijack
			}
			s.write(t, h.Site, body)
		}
	}

	t.Run("каждый захват приписан — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ", func(t *testing.T) {
		s := newOutOfPoolStand(t)
		write(t, s)
		// Захват в дереве СЛУЖБЫ приписан ей by construction — и это тоже надо
		// проверить, иначе «ноль неучтённых» достижимо тем, что служб не читали.
		s.write(t, "services/probe/internal/repo/notify.go", captureHijack)
		tree := s.read(t)
		if got := unattributedCaptures(t, tree); len(got) != 0 {
			t.Fatalf("законное дерево объявлено неполным: %v", got)
		}
		if len(tree.captures) != len(outOfPoolHolders)+1 {
			t.Fatalf("захватов опознано %d, ожидалось %d", len(tree.captures), len(outOfPoolHolders)+1)
		}
	})

	t.Run("захват вне каталога — находка с координатой", func(t *testing.T) {
		s := newOutOfPoolStand(t)
		write(t, s)
		s.write(t, "pkg/newholder/keepalive.go", captureConnect)
		got := unattributedCaptures(t, s.read(t))
		if len(got) != 1 || got[0].kind != kindOutOfPoolUnattributed {
			t.Fatalf("неучтённый захват принят молча: %v", got)
		}
		if !strings.Contains(got[0].why, "pkg/newholder/keepalive.go") {
			t.Fatalf("находка не называет координату: %q", got[0].why)
		}
	})

	t.Run("запись каталога без предмета — сама находка", func(t *testing.T) {
		s := newOutOfPoolStand(t)
		write(t, s)
		// Снимаем захват у ОДНОГО держателя, оставив файл на месте: запись обязана
		// истечь по отсутствию ПРЕДМЕТА, а не по отсутствию файла.
		s.write(t, outOfPoolHolders[0].Site, "package sub\n\nfunc noop() {}\n")
		got := unattributedCaptures(t, s.read(t))
		if len(got) != 1 {
			t.Fatalf("истёкшая запись каталога пережила свой предмет молча: %v", got)
		}
		if !strings.Contains(got[0].why, outOfPoolHolders[0].Site) {
			t.Fatalf("находка не называет истёкшую запись: %q", got[0].why)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// ПОТОЛОК ПОТОКОВ: читается по КЛЮЧУ НАСТРОЙКИ, а незнание — отказ

// TestOutOfPoolCeilingIsReadInEveryLegalForm — обе формы объявления величины.
//
// Форм в дереве две, обе законны, и та, о которой резолвер не знает, дала бы не
// красное и не зелёное, а МОЛЧАНИЕ — слагаемое просто исчезло бы из суммы.
func TestOutOfPoolCeilingIsReadInEveryLegalForm(t *testing.T) {
	cases := []struct {
		name, body string
		want       int
	}{
		{
			"тег настройки",
			"package config\n\ntype Config struct {\n" +
				"\tSubscriptionMaxStreams int `envconfig:\"KACHO_PROBE_SUBSCRIPTION_MAX_STREAMS\" default:\"16\"`\n}\n",
			16,
		},
		{
			"умолчание раскладчика",
			"package config\n\ntype setter interface{ SetDefault(string, any) }\n\n" +
				"func defaults(v setter) {\n\tv.SetDefault(\"api-server.subscription-max-streams\", 24)\n}\n",
			24,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newOutOfPoolStand(t)
			s.write(t, "services/probe/internal/config/config.go", tc.body)
			got, where, err := subscriptionStreamCeiling(t, outOfPoolCtx{svcDir: "services/probe", tree: s.read(t)})
			if err != nil {
				t.Fatalf("форма не опознана: %v", err)
			}
			if got != tc.want {
				t.Fatalf("потолок %d, ожидался %d", got, tc.want)
			}
			if !strings.Contains(where, "config.go") {
				t.Fatalf("происхождение величины не названо: %q", where)
			}
		})
	}
}

// TestOutOfPoolCeilingRefusesInsteadOfReturningZero — неизвестный вход даёт ЯВНЫЙ
// ОТКАЗ.
//
// Ноль здесь был бы худшим из возможных ответов: он молча вычёркивает слагаемое
// и возвращает ровно тот дефект, ради которого слагаемое заводится.
func TestOutOfPoolCeilingRefusesInsteadOfReturningZero(t *testing.T) {
	t.Run("величины нет ни в одной форме", func(t *testing.T) {
		s := newOutOfPoolStand(t)
		s.write(t, "services/probe/internal/config/config.go", "package config\n\ntype Config struct{}\n")
		if _, _, err := subscriptionStreamCeiling(t, outOfPoolCtx{svcDir: "services/probe", tree: s.read(t)}); err == nil {
			t.Fatal("необъявленный потолок принят за ноль — сумма стала меньше настоящей молча")
		}
	})

	t.Run("величина объявлена дважды — побеждающее неопределимо", func(t *testing.T) {
		s := newOutOfPoolStand(t)
		s.write(t, "services/probe/internal/config/a.go",
			"package config\n\ntype A struct {\n"+
				"\tN int `envconfig:\"KACHO_PROBE_SUBSCRIPTION_MAX_STREAMS\" default:\"16\"`\n}\n")
		s.write(t, "services/probe/internal/config/b.go",
			"package config\n\ntype setter interface{ SetDefault(string, any) }\n\n"+
				"func d(v setter) { v.SetDefault(\"subscription-max-streams\", 64) }\n")
		_, _, err := subscriptionStreamCeiling(t, outOfPoolCtx{svcDir: "services/probe", tree: s.read(t)})
		if err == nil {
			t.Fatal("два объявления одной величины приняты молча: гейт выбрал бы одно из двух наугад")
		}
		if !strings.Contains(err.Error(), "НЕСКОЛЬКО") {
			t.Fatalf("отказ не называет причину: %v", err)
		}
	})

	t.Run("нулевой потолок — величина посадки, а не вкус", func(t *testing.T) {
		s := newOutOfPoolStand(t)
		s.write(t, "services/probe/internal/config/config.go",
			"package config\n\ntype C struct {\n"+
				"\tN int `envconfig:\"KACHO_PROBE_SUBSCRIPTION_MAX_STREAMS\" default:\"0\"`\n}\n")
		if _, _, err := subscriptionStreamCeiling(t, outOfPoolCtx{svcDir: "services/probe", tree: s.read(t)}); err == nil {
			t.Fatal("потолок 0 принят: неограниченное число потоков объявлено сходящимся")
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// ПРОИСХОЖДЕНИЕ ВЕЛИЧИНЫ: ОБЪЯВЛЕНИЕ ЧАРТА ПОБЕЖДАЕТ УМОЛЧАНИЕ КОДА

// probeCompiledDefault — служба с ВКОМПИЛИРОВАННЫМ умолчанием 16.
//
// Числа синтетики выбраны различающимися НАМЕРЕННО: чарт объявляет 32, код —
// 16. Совпади они, резолвер, продолжающий читать исходник, отвечал бы верным
// числом по неверному основанию — и вся эта пара молчала бы. Ровно так дефект
// и жил: в настоящем дереве обе величины равны шестнадцати, поэтому сумма
// сходилась, а читалась она не оттуда, где решается.
const probeCompiledDefault = "package config\n\ntype Config struct {\n" +
	"\tSubscriptionMaxStreams int `envconfig:\"KACHO_PROBE_SUBSCRIPTION_MAX_STREAMS\" default:\"16\"`\n}\n"

// probeChartValues — значения подчарта, какими их получит helm.
func probeChartValues(n int) map[string]any {
	return map[string]any{"subscription": map[string]any{"maxStreams": n}}
}

func probeCtx(t *testing.T, s *outOfPoolStand, values map[string]any) outOfPoolCtx {
	t.Helper()
	return outOfPoolCtx{
		svcDir:   "services/probe",
		chartDir: filepath.Join(s.root, "services", "probe", "deploy"),
		values:   values,
		tree:     s.read(t),
	}
}

// TestOutOfPoolCeilingPrefersTheChartOverTheCompiledDefault — ИНЪЕКЦИЯ в
// происхождение величины.
//
// Каждый случай стоит ПАРОЙ с законным близнецом: объявление чарта обязано
// побеждать, а форма, объявлением НЕ являющаяся (комментарий, соседняя
// величина, отсутствие чарта), обязана оставлять победу умолчанию кода.
// Односторонняя проба зеленела бы на резолвере, который всегда читает чарт, —
// и на том, который всегда читает исходник, тоже.
func TestOutOfPoolCeilingPrefersTheChartOverTheCompiledDefault(t *testing.T) {
	const (
		envKey     = "            - name: KACHO_PROBE_SUBSCRIPTION_MAX_STREAMS\n"
		envValue   = "              value: \"{{ .Values.subscription.maxStreams }}\"\n"
		configKey  = "    api-server:\n      subscription-max-streams: {{ .Values.subscription.maxStreams }}\n"
		perSubject = "            - name: KACHO_PROBE_SUBSCRIPTION_MAX_STREAMS_PER_SUBJECT\n" +
			"              value: \"{{ .Values.subscription.maxStreamsPerSubject }}\"\n"
	)

	t.Run("ключ файла настроек — побеждает значение чарта", func(t *testing.T) {
		s := newOutOfPoolStand(t)
		s.write(t, "services/probe/internal/config/config.go", probeCompiledDefault)
		s.write(t, "services/probe/deploy/templates/configmap.yaml", configKey)

		got, where, err := subscriptionStreamCeiling(t, probeCtx(t, s, probeChartValues(32)))
		if err != nil {
			t.Fatalf("объявление чарта не прочитано: %v", err)
		}
		if got != 32 {
			t.Fatalf("потолок %d, ожидался 32: величина взята не оттуда, где решается (%s)", got, where)
		}
		if !strings.Contains(where, "subscription.maxStreams") || !strings.Contains(where, "configmap.yaml") {
			t.Fatalf("происхождение величины не названо: %q", where)
		}
	})

	t.Run("переменная окружения — выражение строкой ниже ключа", func(t *testing.T) {
		s := newOutOfPoolStand(t)
		s.write(t, "services/probe/internal/config/config.go", probeCompiledDefault)
		s.write(t, "services/probe/deploy/templates/deployment.yaml", envKey+envValue)

		got, where, err := subscriptionStreamCeiling(t, probeCtx(t, s, probeChartValues(32)))
		if err != nil {
			t.Fatalf("вторая форма объявления не прочитана: %v", err)
		}
		if got != 32 {
			t.Fatalf("потолок %d, ожидался 32 (%s): окно распознавателя не достаёт до выражения", got, where)
		}
	})

	t.Run("ЗАКОННЫЙ БЛИЗНЕЦ: чарта нет — побеждает умолчание кода", func(t *testing.T) {
		s := newOutOfPoolStand(t)
		s.write(t, "services/probe/internal/config/config.go", probeCompiledDefault)

		got, where, err := subscriptionStreamCeiling(t, outOfPoolCtx{
			svcDir: "services/probe", tree: s.read(t),
		})
		if err != nil {
			t.Fatalf("умолчание кода не прочитано: %v", err)
		}
		if got != 16 || !strings.Contains(where, ".go") {
			t.Fatalf("потолок %d из %q, ожидалось 16 из исходника", got, where)
		}
	})

	t.Run("ЗАКОННЫЙ БЛИЗНЕЦ: чарт есть, ключа не объявляет", func(t *testing.T) {
		s := newOutOfPoolStand(t)
		s.write(t, "services/probe/internal/config/config.go", probeCompiledDefault)
		s.write(t, "services/probe/deploy/templates/deployment.yaml",
			"            - name: KACHO_PROBE_LOG_LEVEL\n              value: \"{{ .Values.logger.level }}\"\n")

		got, where, err := subscriptionStreamCeiling(t, probeCtx(t, s, probeChartValues(32)))
		if err != nil {
			t.Fatalf("умолчание кода не прочитано: %v", err)
		}
		if got != 16 || !strings.Contains(where, ".go") {
			t.Fatalf("потолок %d из %q: чужая переменная принята за объявление потолка", got, where)
		}
	})

	t.Run("ЗАКОННЫЙ БЛИЗНЕЦ: ключ назван КОММЕНТАРИЕМ, а не объявлен", func(t *testing.T) {
		s := newOutOfPoolStand(t)
		s.write(t, "services/probe/internal/config/config.go", probeCompiledDefault)
		s.write(t, "services/probe/deploy/templates/deployment.yaml",
			"            # KACHO_PROBE_SUBSCRIPTION_MAX_STREAMS выставляется профилем площадки\n")

		got, where, err := subscriptionStreamCeiling(t, probeCtx(t, s, probeChartValues(32)))
		if err != nil {
			t.Fatalf("умолчание кода не прочитано: %v", err)
		}
		if got != 16 {
			t.Fatalf("потолок %d (%s): комментарий принят за объявление — проверка судит текст, "+
				"а не исполняемую часть", got, where)
		}
	})

	t.Run("ЗАКОННЫЙ БЛИЗНЕЦ: соседняя величина — потолок на вызывающего", func(t *testing.T) {
		s := newOutOfPoolStand(t)
		s.write(t, "services/probe/internal/config/config.go", probeCompiledDefault)
		s.write(t, "services/probe/deploy/templates/deployment.yaml", perSubject)

		got, where, err := subscriptionStreamCeiling(t, probeCtx(t, s, map[string]any{
			"subscription": map[string]any{"maxStreamsPerSubject": 8},
		}))
		if err != nil {
			t.Fatalf("умолчание кода не прочитано: %v", err)
		}
		if got != 16 {
			t.Fatalf("потолок %d (%s): потолок НА ВЫЗЫВАЮЩЕГО принят за потолок РЕПЛИКИ — "+
				"слагаемое подменено соседней величиной", got, where)
		}
	})

	t.Run("сосед стоит рядом с предметом — берётся предмет, а не отказ", func(t *testing.T) {
		s := newOutOfPoolStand(t)
		s.write(t, "services/probe/internal/config/config.go", probeCompiledDefault)
		// Раскладка настоящего чарта края: обе величины подряд, из разных путей.
		s.write(t, "services/probe/deploy/templates/deployment.yaml", envKey+envValue+perSubject)

		got, where, err := subscriptionStreamCeiling(t, probeCtx(t, s, map[string]any{
			"subscription": map[string]any{"maxStreams": 32, "maxStreamsPerSubject": 8},
		}))
		if err != nil {
			t.Fatalf("объявление рядом с соседом дало отказ вместо величины: %v", err)
		}
		if got != 32 {
			t.Fatalf("потолок %d, ожидался 32 (%s)", got, where)
		}
	})

	t.Run("ОТКАЗ: чарт объявляет ключ НЕ из значений", func(t *testing.T) {
		s := newOutOfPoolStand(t)
		s.write(t, "services/probe/internal/config/config.go", probeCompiledDefault)
		s.write(t, "services/probe/deploy/templates/configmap.yaml",
			"    api-server:\n      subscription-max-streams: 64\n")

		got, _, err := subscriptionStreamCeiling(t, probeCtx(t, s, probeChartValues(32)))
		if err == nil {
			t.Fatalf("литерал шаблона принят молча (потолок %d): резолвер откатился к умолчанию "+
				"кода, которое этим объявлением уже перебито", got)
		}
		if !strings.Contains(err.Error(), "configmap.yaml") {
			t.Fatalf("отказ не называет координату: %v", err)
		}
	})

	t.Run("ОТКАЗ: значения не несут пути, который называет шаблон", func(t *testing.T) {
		s := newOutOfPoolStand(t)
		s.write(t, "services/probe/internal/config/config.go", probeCompiledDefault)
		s.write(t, "services/probe/deploy/templates/configmap.yaml", configKey)

		got, _, err := subscriptionStreamCeiling(t, probeCtx(t, s, map[string]any{}))
		if err == nil {
			t.Fatalf("пустые значения приняты молча (потолок %d): под получил бы пустую величину, "+
				"а сумма сошлась бы по умолчанию кода", got)
		}
		if !strings.Contains(err.Error(), "subscription.maxStreams") {
			t.Fatalf("отказ не называет путь: %v", err)
		}
	})

	t.Run("ОТКАЗ: значение нулевое — величина посадки, а не вкус", func(t *testing.T) {
		s := newOutOfPoolStand(t)
		s.write(t, "services/probe/internal/config/config.go", probeCompiledDefault)
		s.write(t, "services/probe/deploy/templates/configmap.yaml", configKey)

		if got, _, err := subscriptionStreamCeiling(t, probeCtx(t, s, probeChartValues(0))); err == nil {
			t.Fatalf("нулевой потолок принят (%d): неограниченное число потоков объявлено сходящимся", got)
		}
	})

	t.Run("ОТКАЗ: ключ рендерится из НЕСКОЛЬКИХ путей значений", func(t *testing.T) {
		s := newOutOfPoolStand(t)
		s.write(t, "services/probe/internal/config/config.go", probeCompiledDefault)
		s.write(t, "services/probe/deploy/templates/configmap.yaml", configKey)
		s.write(t, "services/probe/deploy/templates/deployment.yaml",
			"            - name: KACHO_PROBE_SUBSCRIPTION_MAX_STREAMS\n"+
				"              value: \"{{ .Values.legacy.maxStreams }}\"\n")

		_, _, err := subscriptionStreamCeiling(t, probeCtx(t, s, map[string]any{
			"subscription": map[string]any{"maxStreams": 32},
			"legacy":       map[string]any{"maxStreams": 64},
		}))
		if err == nil {
			t.Fatal("два разных пути приняты молча: гейт выбрал бы один из двух наугад")
		}
		if !strings.Contains(err.Error(), "НЕСКОЛЬКИХ") {
			t.Fatalf("отказ не называет причину: %v", err)
		}
	})
}

// TestOutOfPoolCeilingIsReadWhereItIsDecided — ПРЕДПОСЫЛКА резолвера,
// ЗАМЕНИВШАЯ снятую (kacho#1384).
//
// # Что здесь стояло и почему снято
//
// Прежняя проба (`TestOutOfPoolCeilingIsNotSettableFromTheChart`) стерегла
// предпосылку «потолок НЕ объявляем чартом, поэтому умолчание кода и есть
// побеждающее значение». Предпосылка была верна в день записи и перестала быть
// верной: величину объявили все ПЯТЬ служб, поднимающих сервер подписки —
// `subscription.maxStreams` в их значениях, отрендеренная напрямую в ключ
// настроек (vpc, nlb) либо в переменную окружения (compute, storage, registry).
//
// Проба сработала ровно как задумана: предпосылка самоистекла и покраснела. Это
// НЕ повод её ослабить — её предмет исчез, а проба, чей предмет исчез,
// ЗАМЕНЯЕТСЯ той, что стережёт новую предпосылку. Новая такая: величина
// читается ОТТУДА, ГДЕ ОНА РЕШАЕТСЯ, — из значений, когда чарт рендерит ключ из
// значений, и из умолчания кода, когда не рендерит.
//
// # Почему проба не вакуумна при ослепшем распознавателе
//
// Соблазн очевидный: распознаватель, переставший видеть объявление, дал бы
// «чарт ключа не объявляет» — законный вход, на котором эта проба молчала бы.
// Поэтому распознавателей ДВА, разной зернистости, и они сверяются между собой:
// построчный (называет файл, строку и путь значений) и грубый по тексту файла
// целиком — тот самый предикат, которым стерегла снятая проба. Грубый видит, а
// построчный нет — НАХОДКА, и находка называет файл.
//
// # Что проба НЕ утверждает
//
// Она судит ПРОИСХОЖДЕНИЕ величины, а не её размер: «шестнадцать много» —
// вопрос замера, и сходимость суммы считает соседний гейт арифметики по всем
// стекам сразу.
func TestOutOfPoolCeilingIsReadWhereItIsDecided(t *testing.T) {
	dirs := subchartDirs(t)
	tree := readOutOfPoolTree(t)
	stacks := deployStacks(t)

	stackNames := make([]string, 0, len(stacks))
	for n := range stacks {
		stackNames = append(stackNames, n)
	}
	sort.Strings(stackNames)

	aliases := make([]string, 0, len(dirs))
	for alias := range dirs {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)

	looked, coarse, fine, fromValues, fromSource := 0, 0, 0, 0, 0
	wiredBy := map[string][]ceilingWiring{}

	for _, alias := range aliases {
		svcDir, ok := serviceSourceDir("..", alias)
		if !ok {
			continue
		}
		// Предпосылка эта — о службах, КОТОРЫЕ ПОДНИМАЮТ сервер потоков, поэтому
		// отбор идёт по его конструктору, а не по импорту пакета. Пакет несёт и
		// переиспользуемую часть (наблюдатель границы устоявшегося), и импортёр
		// ради неё сервера не поднимает: посчитай его здесь — и от него
		// потребовали бы объявить потолок тому, чего он не заводит (kacho#1374).
		if n, _ := tree.callSites(svcDir+"/", subscriptionPkg+".NewServer"); n == 0 {
			continue
		}
		looked++
		chartDir := dirs[alias]

		wirings, err := subscriptionCeilingWirings(chartDir)
		if err != nil {
			t.Fatalf("%s: %v", alias, err)
		}
		rough, err := chartCarriesCeilingKeyCoarsely(chartDir)
		if err != nil {
			t.Fatalf("%s: %v", alias, err)
		}
		if len(rough) > 0 {
			coarse++
		}
		if len(wirings) > 0 {
			fine++
		}
		// ЗЕРКАЛО: два предиката об одном предмете обязаны сходиться. Расхождение
		// в любую сторону — находка о ПРОВЕРКЕ, а не о дереве.
		if len(rough) > 0 && len(wirings) == 0 {
			t.Errorf("%s: ключ потолка несут файлы чарта %v, а построчный распознаватель не нашёл "+
				"ни одного объявления. Причин ровно две, и различить их обязан человек: либо "+
				"распознаватель ослеп (и «чарт ключа не объявляет» стало неотличимо от «форма "+
				"записи перестала узнаваться»), либо чарт лишь УПОМИНАЕТ величину — в "+
				"комментарии или соседним потолком на вызывающего — и тогда правится эта "+
				"перепись, а не резолвер", alias, rough)
			continue
		}
		if len(wirings) > 0 && len(rough) == 0 {
			t.Errorf("%s: построчный распознаватель нашёл объявление (%v), а грубый предикат по "+
				"тексту файла — нет: зеркало перестало быть зеркалом, и одно из двух чтений неверно",
				alias, wirings)
			continue
		}
		wiredBy[alias] = wirings
	}

	// Происхождение сверяется НА КАЖДОМ стеке, а не на одном выбранном: профиль
	// вправе и снять объявление, и переобъявить его, и тогда побеждающее
	// значение у стеков разное. Один выбранный стек делал бы вердикт о шести
	// вердиктом об одном.
	for _, stack := range stackNames {
		vals := valuesWithSubchartDefaults(t, stacks[stack])
		for _, alias := range aliases {
			wirings, seen := wiredBy[alias]
			if !seen {
				continue
			}
			svcDir, _ := serviceSourceDir("..", alias)
			sub, _ := vals[alias].(map[string]any)

			got, where, err := subscriptionStreamCeiling(t, outOfPoolCtx{
				svcDir: svcDir, chartDir: dirs[alias], values: sub, tree: tree,
			})
			if err != nil {
				t.Errorf("стек %s, %s: потолок не выведен: %v", stack, alias, err)
				continue
			}
			if got <= 0 {
				t.Errorf("стек %s, %s: потолок выведен как %d (%s)", stack, alias, got, where)
				continue
			}

			if len(wirings) == 0 {
				fromSource++
				if !strings.HasSuffix(strings.Fields(where)[0], ".go") {
					t.Errorf("стек %s, %s: чарт ключа не объявляет, значит побеждает умолчание "+
						"кода, а величина взята из %q", stack, alias, where)
				}
				continue
			}
			w := wirings[0]
			if w.Path == "" {
				t.Errorf("стек %s, %s: чарт объявляет ключ в %s (%q), но не из значений — "+
					"побеждающее значение чтением значений не выводится, и резолвер обязан "+
					"ОТКАЗАТЬ, а не откатываться к умолчанию кода", stack, alias, w, w.Text)
				continue
			}
			if !strings.Contains(where, w.Path) {
				t.Errorf("стек %s, %s: величина взята из %q, а решается она в значениях (%s "+
					"рендерит её из %s) — читается не побеждающее значение, и вердикт о посадке "+
					"относится к тому, чего под не увидит", stack, alias, where, w, w.Path)
				continue
			}
			fromValues++
		}
	}

	// ПЕРЕПИСЬ печатает ПЯТЬ чисел, а не одно: «ноль объявлений» обязано быть
	// отличимо и от «служб не смотрели», и от «распознаватель ослеп».
	t.Logf("перепись: стеков %d · служб с сервером подписки осмотрено %d · ключ потолка несёт "+
		"чарт (грубо, по тексту файла) %d · распознано построчно %d · пар стек×служба, где "+
		"величина взята из значений %d, из умолчания кода %d",
		len(stackNames), looked, coarse, fine, fromValues, fromSource)
	if looked == 0 {
		t.Fatal("ни одной службы с сервером подписки не осмотрено — предпосылка беспредметна, " +
			"а не подтверждена")
	}
	if fromValues+fromSource == 0 {
		t.Fatal("происхождение величины не сверено ни у одной пары стек×служба — проверка " +
			"ничего не утверждает, хотя выглядит зелёной")
	}
}

// TestOutOfPoolRecogniserDeclaresItsBorder — то, чего распознаватель НЕ ловит,
// названо здесь и в шапке.
//
// Это не оправдание слепоты, а её ОБНАРУЖИВАЕМОСТЬ: изъятое из пула соединение,
// удерживаемое не ради подписки, сегодня не учитывается. Научит кто-нибудь
// распознаватель — проба покраснеет и заставит поправить шапку.
func TestOutOfPoolRecogniserDeclaresItsBorder(t *testing.T) {
	s := newOutOfPoolStand(t)
	s.write(t, "pkg/copyjob/bulk.go", "package copyjob\n\n"+
		"type acquired interface{ Hijack() any }\n\n"+
		"func take(pc acquired) any { return pc.Hijack() }\n")
	tree := s.read(t)
	if len(tree.captures) != 0 {
		t.Fatalf("изъятие БЕЗ подписки стало опознаваться (захватов %d) — распознаватель "+
			"расширился, поправь шапку и сними эту запись", len(tree.captures))
	}
	if tree.files != 1 {
		t.Fatalf("прочитано файлов %d, ожидался 1: молчание на непрочитанном ничего не значит", tree.files)
	}
}
