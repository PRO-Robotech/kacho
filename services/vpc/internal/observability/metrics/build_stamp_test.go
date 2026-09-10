// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/observability"
)

// build_stamp_test.go — ряд `kacho_vpc_build_info` РАЗЛИЧАЕТ версию и её отсутствие.
//
// Предмет: до #2527 образ штампа не ставил вовсе, и ряд отвечал умолчаниями
// объявления — `version="dev"` `commit="unknown"`. Это ответ, который ничего не
// различает: дежурный, спросивший «какой код исполняется», получает правдоподобное
// имя ветки. Подстановка заведена, но одной её мало — сборка БЕЗ аргументов
// (`go run`, `go test`, сборка руками) по-прежнему существует и обязана называть
// себя ОТДЕЛЬНЫМ словом, а не пустой меткой и не `dev`.
//
// Проба парная по каждой оси: без второй половины годным выглядел бы адаптер,
// затирающий словом ЛЮБУЮ величину, — и витрина отвечала бы о себе, а не о сборке.

// scrapeStamp — текст витрины приватного реестра адаптера.
func scrapeStamp(t *testing.T, m *Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics code=%d, want 200", rec.Code)
	}
	return rec.Body.String()
}

func TestBuildInfo_StampedValuesReachTheSeriesUntouched(t *testing.T) {
	out := scrapeStamp(t, New("release/kaname-tail", "1eedc9e7ff"))
	want := `kacho_vpc_build_info{commit="1eedc9e7ff",version="release/kaname-tail"} 1`
	if !strings.Contains(out, want) {
		t.Errorf("законный близнец: проставленная величина обязана дойти до ряда ДОСЛОВНО,"+
			"\nждали %q\nвитрина:\n%s", want, out)
	}
}

func TestBuildInfo_UnstampedBuildSaysSoInAWord(t *testing.T) {
	// Мир отличается от предыдущего ОДНИМ фактом: сборка величину не проставила.
	out := scrapeStamp(t, New("", ""))
	want := `kacho_vpc_build_info{commit="` + observability.BuildStampUnstamped +
		`",version="` + observability.BuildStampUnstamped + `"} 1`
	if !strings.Contains(out, want) {
		t.Errorf("непроставленный штамп обязан называть себя СЛОВОМ: пустая метка на"+
			" витрине неотличима от «величину не измеряли», и тревоги по ней не написать."+
			"\nждали %q\nвитрина:\n%s", want, out)
	}
}

func TestBuildInfo_GoDefaultsAreNotAnAnswerEither(t *testing.T) {
	// `dev`/`unknown` — умолчания ОБЪЯВЛЕНИЯ композиционного корня. На витрине они
	// такой же не-ответ, как пустая строка: `dev` читается как имя ветки.
	out := scrapeStamp(t, New("dev", "unknown"))
	if strings.Contains(out, `version="dev"`) || strings.Contains(out, `commit="unknown"`) {
		t.Errorf("умолчание объявления доехало до витрины как ответ — именно это и было"+
			" предметом #2527.\nвитрина:\n%s", out)
	}
	if !strings.Contains(out, `version="`+observability.BuildStampUnstamped+`"`) {
		t.Errorf("умолчание обязано читаться тем же словом, что и пустая строка: у одного"+
			" состояния не бывает трёх написаний.\nвитрина:\n%s", out)
	}
}
