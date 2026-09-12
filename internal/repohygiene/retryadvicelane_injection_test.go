// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// ПРОБА СПОСОБНОСТИ ГЕЙТА ПАДАТЬ — и молчать там, где молчать обязан.
//
// Гейт, который не падал ни разу, зелёного не доказывает: он неотличим от
// проверки, читающей пустой корпус. Поэтому каждая ось предиката и каждая форма
// записи совета подаются анализатору настоящим входом, и обе стороны
// утверждаются отдельно: дефект в синтетике ОБЯЗАН находиться, законный близнец
// ОБЯЗАН молчать (`testing.md` §«Гейт на класс», п.1 и п.2).
//
// Корпус подаётся В ПАМЯТИ: ни репозитория, ни временного каталога проба не
// заводит, поэтому чужое состояние она не трогает и не может «не выполниться»
// из-за отсутствия git.
//
// Предпосылка самой пробы утверждается первой: синтетический вход обязан
// РАЗБИРАТЬСЯ. Файл с ошибкой синтаксиса дал бы ноль находок по причине, никак не
// связанной с предметом, — и проба зеленела бы, ничего не проверив.

// retryAdviceProbe — один случай: имя, тело файла, ожидание.
type retryAdviceProbe struct {
	name string
	body string
	// wantAxis — ось, по которой находка обязана быть; пусто означает «молчание».
	wantAxis string
	wantForm string
	// why — ПРИЧИНА молчания законного близнеца. Молчание само по себе ничего не
	// доказывает: оно одинаково выглядит и когда ось отрицания сработала, и когда
	// распознаватель совета вообще ослеп. Поэтому у каждого близнеца утверждается
	// та величина переписи, которая обязана вырасти именно от его ветки.
	why func(t *testing.T, c retryAdviceCensus)
}

// retryAdviceWrap — тело файла Go вокруг одного выражения отказа.
func retryAdviceWrap(stmts string) string {
	return "package probe\n\nimport (\n\t\"fmt\"\n\n\t\"google.golang.org/grpc/codes\"\n" +
		"\t\"google.golang.org/grpc/status\"\n)\n\nvar ErrFailedPrecondition = fmt.Errorf(\"failed precondition\")\n" +
		"var ErrAborted = fmt.Errorf(\"aborted\")\nvar ErrPermanent = fmt.Errorf(\"permanent\")\n\n" +
		"func probe(id string) error {\n" + stmts + "\n}\n"
}

func TestRetryAdviceGateCanFail(t *testing.T) {
	t.Parallel()

	probes := []retryAdviceProbe{
		// ── A1: код запрещает повтор. По одной форме записи на случай ──────────
		{
			name:     "A1/F1-en-retry/status.Error",
			body:     retryAdviceWrap("\treturn status.Error(codes.FailedPrecondition, \"group \"+id+\" was modified concurrently, please retry\")"),
			wantAxis: "A1-код-запрещает-повтор",
			wantForm: "F1-en-retry",
		},
		{
			name:     "A1/F1-en-retry/сентинел-под-%w",
			body:     retryAdviceWrap("\treturn fmt.Errorf(\"%w: group %s was modified concurrently, please retry\", ErrFailedPrecondition, id)"),
			wantAxis: "A1-код-запрещает-повтор",
			wantForm: "F1-en-retry",
		},
		{
			name:     "A1/F1-en-retry/свой-конструктор-с-codes-аргументом",
			body:     retryAdviceWrap("\treturn refuse(codes.NotFound, fmt.Sprintf(\"group %s vanished, retry\", id))"),
			wantAxis: "A1-код-запрещает-повтор",
			wantForm: "F1-en-retry",
		},
		{
			name:     "A1/F2-en-tryagain",
			body:     retryAdviceWrap("\treturn status.Errorf(codes.AlreadyExists, \"group %s exists, try again\", id)"),
			wantAxis: "A1-код-запрещает-повтор",
			wantForm: "F2-en-tryagain",
		},
		{
			name:     "A1/F3-en-resend",
			body:     retryAdviceWrap("\treturn status.Errorf(codes.InvalidArgument, \"group %s: resubmit the request\", id)"),
			wantAxis: "A1-код-запрещает-повтор",
			wantForm: "F3-en-resend",
		},
		{
			name:     "A1/F4-ru-povtor",
			body:     retryAdviceWrap("\treturn status.Errorf(codes.PermissionDenied, \"группа %s: повторите запрос\", id)"),
			wantAxis: "A1-код-запрещает-повтор",
			wantForm: "F4-ru-povtor",
		},
		{
			name:     "A1/F5-ru-poprobuyte",
			body:     retryAdviceWrap("\treturn status.Errorf(codes.Unauthenticated, \"группа %s: попробуйте снова\", id)"),
			wantAxis: "A1-код-запрещает-повтор",
			wantForm: "F5-ru-poprobuyte",
		},
		// ── A2: асинхронное тело мутации ──────────────────────────────────────
		{
			name: "A2/совет-внутри-асинхронного-тела-на-РАЗРЕШАЮЩЕМ-коде",
			body: "package probe\n\nimport (\n\t\"context\"\n\n\t\"google.golang.org/grpc/codes\"\n" +
				"\t\"google.golang.org/grpc/status\"\n)\n\nfunc probe(ctx context.Context) {\n" +
				"\toperations.Run(ctx, nil, \"op-1\", func(ctx context.Context) error {\n" +
				"\t\treturn status.Error(codes.Aborted, \"conflicting concurrent change, retry the request\")\n" +
				"\t})\n}\n",
			wantAxis: "A2-асинхронное-тело",
			wantForm: "F1-en-retry",
		},
		// ── ЗАКОННЫЕ БЛИЗНЕЦЫ: каждый обязан молчать ──────────────────────────
		{
			name: "близнец/совет-на-РАЗРЕШАЮЩЕМ-коде-вне-асинхронного-тела",
			body: retryAdviceWrap("\treturn status.Errorf(codes.Aborted, \"subnet %s: could not claim a free address, retry\", id)"),
			why: func(t *testing.T, c retryAdviceCensus) {
				if c.Advices != 1 || c.OnAllowedCode != 1 {
					t.Fatalf("совет распознан %d раз, отнесён к разрешающему коду %d раз — "+
						"молчание получено НЕ веткой разрешающего кода", c.Advices, c.OnAllowedCode)
				}
			},
		},
		{
			name: "близнец/отрицание-повтора",
			body: retryAdviceWrap("\treturn fmt.Errorf(\"%w: register rejected (no retry): %s\", ErrFailedPrecondition, id)"),
			why: func(t *testing.T, c retryAdviceCensus) {
				if c.Advices != 1 || c.Negated != 1 {
					t.Fatalf("совет распознан %d раз, снят отрицанием %d раз — молчание "+
						"получено НЕ осью отрицания", c.Advices, c.Negated)
				}
			},
		},
		{
			name: "близнец/отрицание-по-русски",
			body: retryAdviceWrap("\treturn status.Errorf(codes.FailedPrecondition, \"подсеть %s исчерпана — повтор бессмыслен\", id)"),
			why: func(t *testing.T, c retryAdviceCensus) {
				if c.Advices != 1 || c.Negated != 1 {
					t.Fatalf("совет распознан %d раз, снят отрицанием %d раз — молчание "+
						"получено НЕ осью отрицания", c.Advices, c.Negated)
				}
			},
		},
		{
			name: "близнец/строка-журнала-внутри-асинхронного-тела",
			body: "package probe\n\nimport (\n\t\"context\"\n\t\"log/slog\"\n)\n\nfunc probe(ctx context.Context) {\n" +
				"\toperations.Run(ctx, nil, \"op-1\", func(ctx context.Context) error {\n" +
				"\t\tslog.WarnContext(ctx, \"sweep interrupted; rows will be picked up on retry\", \"code\", codes.Aborted)\n" +
				"\t\treturn nil\n" +
				"\t})\n}\n",
			why: func(t *testing.T, c retryAdviceCensus) {
				// Число здесь НЕ единица, и это не оговорка: вызов журнала несёт и
				// сообщение, и ключи атрибутов, поэтому снимается больше одного
				// литерала. Утверждается непустота ветки, а не её размер.
				if c.LogSkipped == 0 {
					t.Fatal("ни один литерал не снят как строка журнала — молчание получено " +
						"НЕ веткой журнала, а значит строка оператора и текст вызывающему " +
						"здесь не различены")
				}
				if c.Advices != 0 {
					t.Fatalf("совет распознан %d раз в строке журнала — снятие произошло "+
						"ПОСЛЕ распознавания, а порядок здесь несущий", c.Advices)
				}
			},
		},
		{
			name: "близнец/сентинел-не-называет-кода",
			body: retryAdviceWrap("\treturn fmt.Errorf(\"%w: register rejected, retry: %s\", ErrPermanent, id)"),
			why: func(t *testing.T, c retryAdviceCensus) {
				if c.Refusals != 0 {
					t.Fatalf("выражений отказа найдено %d — сентинел без кода в имени "+
						"судился, то есть граница 3 шапки не исполняется", c.Refusals)
				}
			},
		},
		{
			name: "близнец/текст-без-совета-на-запрещающем-коде",
			body: retryAdviceWrap("\treturn fmt.Errorf(\"%w: group %s was modified concurrently\", ErrFailedPrecondition, id)"),
			why: func(t *testing.T, c retryAdviceCensus) {
				if c.Literals != 1 {
					t.Fatalf("текстов отказа осмотрено %d, ожидался 1 — молчание получено "+
						"обрывом обхода, а не отсутствием совета", c.Literals)
				}
				if c.Advices != 0 {
					t.Fatalf("в тексте без совета совет распознан %d раз", c.Advices)
				}
			},
		},
		{
			name: "близнец/имя-пакета-отступа-не-текст-отказа",
			body: "package probe\n\nimport (\n\t\"github.com/PRO-Robotech/corelib/retry\"\n)\n\nvar _ = retry.Do\n",
			why: func(t *testing.T, c retryAdviceCensus) {
				if c.Refusals != 0 || c.Literals != 0 {
					t.Fatalf("имя пакета прочитано как выражение отказа (выражений %d, "+
						"текстов %d)", c.Refusals, c.Literals)
				}
			},
		},
	}

	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			c := auditRetryAdvice([]retryAdviceSource{{Rel: "probe/probe.go", Body: p.body}})
			// ПРЕДПОСЫЛКА пробы: вход разобран. Ноль находок при неразобранном
			// входе доказывал бы только то, что синтетика не компилируется.
			if len(c.ParseFails) > 0 {
				t.Fatalf("синтетический вход НЕ разобран (%v) — ноль находок здесь "+
					"не про предмет, а про сломанную фикстуру", c.ParseFails)
			}
			if c.Parsed != 1 {
				t.Fatalf("разобрано %d файлов, ожидался 1 — фикстура не доехала", c.Parsed)
			}
			if p.wantAxis == "" {
				if len(c.Findings) != 0 {
					t.Fatalf("законный близнец назван находкой — гейт красит правду: %s",
						c.Findings[0].Describe())
				}
				if p.why == nil {
					t.Fatal("у законного близнеца не назначена причина молчания — " +
						"такое зелёное неотличимо от ослепшего распознавателя")
				}
				p.why(t, c)
				return
			}
			if len(c.Findings) != 1 {
				t.Fatalf("дефект в синтетике НЕ найден (находок %d) — по этой оси гейт "+
					"не падает, то есть её зелёное ничего не доказывает", len(c.Findings))
			}
			f := c.Findings[0]
			if !strings.Contains(strings.Join(f.Axis, "+"), p.wantAxis) {
				t.Fatalf("ось находки %v, ожидалась %s — предикат сработал, но на другом "+
					"основании, и диагноз повёл бы читателя не туда", f.Axis, p.wantAxis)
			}
			if f.Form != p.wantForm {
				t.Fatalf("форма совета %s, ожидалась %s — распознаватель сработал другой "+
					"веткой, и перепись по формам врала бы", f.Form, p.wantForm)
			}
		})
	}
}

// Перепись обязана расти вместе с осмотренным: гейт, читающий пустой корпус,
// печатает ноль по каждой оси и выглядит ровно как гейт, не нашедший ничего.
func TestRetryAdviceCensusCountsWhatItRead(t *testing.T) {
	t.Parallel()
	c := auditRetryAdvice(nil)
	if c.Files != 0 || c.Refusals != 0 || c.Literals != 0 || c.Advices != 0 {
		t.Fatalf("пустой вход дал непустую перепись: %+v", c)
	}
	if len(c.Findings) != 0 {
		t.Fatalf("пустой вход дал находки — предикат срабатывает без предмета: %v", c.Findings)
	}

	body := retryAdviceWrap("\treturn fmt.Errorf(\"%w: group %s was modified concurrently, please retry\", ErrFailedPrecondition, id)")
	c2 := auditRetryAdvice([]retryAdviceSource{{Rel: "probe/probe.go", Body: body}})
	if c2.Files != 1 || c2.Parsed != 1 {
		t.Fatalf("файл не дошёл до разбора: %+v", c2)
	}
	if c2.Refusals == 0 || c2.Literals == 0 || c2.Advices == 0 {
		t.Fatalf("перепись не выросла на входе с предметом (выражений %d, текстов %d, "+
			"советов %d) — ни одна её величина не связана с прочитанным",
			c2.Refusals, c2.Literals, c2.Advices)
	}
}
