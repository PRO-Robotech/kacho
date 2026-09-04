// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_chart_premise_reachability_test.go — раскол по тегу сборки
// `helmcharts` держится проверками, а не памятью автора.
//
// # Предмет
//
// identity_chart_default_premise_test.go выведен из общего прогона тегом
// сборки: он читает архивы чартов, а их материализует не всякое задание. Это
// законный исход («своё задание, создающее условие»), но у него два способа
// стать ложью, и оба невидимы изнутри самого тега:
//
//  1. тег никто не зовёт — файл перестаёт компилироваться где бы то ни было, и
//     проверка исчезает из конвейера БЕЗ единого красного;
//  2. архивы стали отслеживаться git — раскол потерял предмет и превратился в
//     необъяснимое исключение, которое следующий читатель снимет наугад.
//
// Обе проверки живут ЗДЕСЬ, вне тега: проверка под собственным тегом о
// собственном невызове молчит by construction.
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// ciWorkflow — конвейер, читаемый от каталога deploy.
const ciWorkflow = "../.github/workflows/ci.yaml"

// TestChartPremiseIsActuallyInvoked — тег `helmcharts` зовётся из конвейера.
//
// Без этой проверки вывод файла из общего прогона неотличим от его удаления:
// `go test ./...` тегированный файл не компилирует, поэтому ни ошибка сборки,
// ни красный тест о пропаже не сообщат.
func TestChartPremiseIsActuallyInvoked(t *testing.T) {
	raw, err := os.ReadFile(ciWorkflow)
	if err != nil {
		t.Fatalf("конвейер %s не читается (%v) — предпосылка проверки исчезла, "+
			"а не тег стал достижим", ciWorkflow, err)
	}
	body := string(raw)

	var invoking []string
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, "-tags helmcharts") {
			continue
		}
		if !strings.Contains(line, "./deploy") {
			continue
		}
		invoking = append(invoking, strings.TrimSpace(line))
	}

	t.Logf("осмотрено: строк конвейера %d, зовущих тег на пакете deploy %d",
		strings.Count(body, "\n")+1, len(invoking))

	if len(invoking) == 0 {
		t.Fatalf("в %s нет шага, зовущего `go test -tags helmcharts ./deploy/...`: проверка "+
			"умолчаний чужого чарта выведена из общего прогона и НЕ добрана отдельным заданием — "+
			"то есть снята целиком, оставаясь на вид написанной. Либо верните шаг, либо снимите "+
			"тег вместе с файлом", ciWorkflow)
	}
	for _, l := range invoking {
		t.Logf("зов тега: %s", l)
	}
}

// TestChartArchivesAreStillUntracked — у раскола есть предмет.
//
// Самоистечение: станут архивы отслеживаемыми — условие проверки создаётся
// любым заданием, тег больше ничего не объясняет, и эта проверка становится
// красной РАНЬШЕ, чем кто-нибудь успеет прочитать раскол как традицию.
func TestChartArchivesAreStillUntracked(t *testing.T) {
	dir := filepath.Join(umbrellaDir, "charts")
	tracked, err := treecorpus.Under(dir)
	if err != nil {
		t.Fatalf("состав %s: %v", dir, err)
	}

	var archives []string
	for _, p := range tracked {
		if strings.HasSuffix(p, ".tgz") {
			archives = append(archives, p)
		}
	}

	t.Logf("осмотрено: отслеживаемых элементов в %s = %d, из них архивов чартов = %d",
		dir, len(tracked), len(archives))

	if len(archives) > 0 {
		t.Errorf("архивы чартов стали отслеживаться git (%v) — предмет тега `helmcharts` исчез: "+
			"условие проверки умолчаний теперь создаёт любое задание. Верните проверку в общий "+
			"прогон и снимите тег, иначе исключение переживёт своё основание", archives)
	}
}
