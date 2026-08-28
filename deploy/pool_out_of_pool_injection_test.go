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
			got, where, err := subscriptionStreamCeiling(t, "services/probe", s.read(t))
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
		if _, _, err := subscriptionStreamCeiling(t, "services/probe", s.read(t)); err == nil {
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
		_, _, err := subscriptionStreamCeiling(t, "services/probe", s.read(t))
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
		if _, _, err := subscriptionStreamCeiling(t, "services/probe", s.read(t)); err == nil {
			t.Fatal("потолок 0 принят: неограниченное число потоков объявлено сходящимся")
		}
	})
}

// TestOutOfPoolCeilingIsNotSettableFromTheChart — ПРЕДПОСЫЛКА резолвера.
//
// Величина читается из кода ПОТОМУ, что оттуда её читает процесс: ни в значения
// чарта, ни в переменные окружения шаблона она не выведена. Стань она объявляемой
// — умолчание перестанет быть побеждающим значением, а гейт продолжит читать его,
// то есть начнёт судить о посадке, которой нет.
//
// Предпосылка САМОИСТЕКАЕТ: появится ручка — покраснеет эта проба, а не вердикт.
func TestOutOfPoolCeilingIsNotSettableFromTheChart(t *testing.T) {
	dirs := subchartDirs(t)
	tree := readOutOfPoolTree(t)
	looked := 0
	for alias, chartDir := range dirs {
		svcDir, ok := serviceSourceDir("..", alias)
		if !ok {
			continue
		}
		if len(tree.importers(svcDir+"/", subscriptionPkg)) == 0 {
			continue
		}
		looked++
		err := filepath.WalkDir(chartDir, func(path string, d os.DirEntry, werr error) error {
			if werr != nil || d.IsDir() {
				return werr
			}
			raw, rerr := os.ReadFile(path) // #nosec G304 -- обход собственного дерева
			if rerr != nil {
				return rerr
			}
			if subscriptionCeilingKey(string(raw)) {
				t.Errorf("%s: потолок потоков подписки стал объявляемым в чарте (%s) — "+
					"умолчание кода больше не является побеждающим значением, и слагаемое "+
					"обязано читаться из значений, а не из исходника", alias, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("обход чарта %s: %v", alias, err)
		}
	}
	// Перепись: «ноль ручек» обязано быть отличимо от «ни одной службы не смотрели».
	t.Logf("перепись: служб, поднимающих сервер подписки, осмотрено %d", looked)
	if looked == 0 {
		t.Fatal("ни одной службы с сервером подписки не осмотрено — предпосылка беспредметна, " +
			"а не подтверждена")
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
