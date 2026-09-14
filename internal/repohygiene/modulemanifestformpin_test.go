// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestModuleManifestFormIsJudgedByTheOneExecutor — форму манифестов доменов
// этого дерева судит тот самый единственный исполнитель, собранный из пина.
//
// Предмет, устройство и довод «почему это не второй судья формы и почему корень
// не собирается» — в шапке modulemanifestformpin.go. Здесь — прогон.
//
// # Что этот гейт судит, а что НЕТ
//
// Судит ФОРМУ документов этого дерева, и только её. Побайтовое согласие блоков
// канона с манифестами судит соседний TestModelCanonAgreesWithPlatformManifests
// (ось там другая: текст блока модели, а не схема документа). Согласие раздела
// `roles` и посева с живой базой судится в дереве службы против её базы, а
// платформенная половина — производитель доставки (deploy,
// TestModuleManifestConfigMapHasAProducer).
//
// # Почему сюда добавлено ВТОРОЕ утверждение об объёме
//
// Код возврата судьи отвечает на вопрос «нашёл ли», а не «прочитал ли».
// Зелёное над нулём прочитанных документов есть вакуумное зелёное, и отличить
// его от настоящего можно только здесь — сверив число прочитанного с числом
// документов, лежащих в индексе.
func TestModuleManifestFormIsJudgedByTheOneExecutor(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	manifests := platformDomainManifests(t, root)
	if len(manifests) == 0 {
		t.Fatal("манифестов домена в индексе НЕ НАЙДЕНО НИ ОДНОГО (services/*/manifest.yaml) — " +
			"«ноль находок» здесь означало бы «ноль прочитанного»: судить было бы нечего, " +
			"а вердикт выглядел бы зелёным")
	}

	out, code := runManifestFormJudge(t, buildManifestFormJudge(t, root), root)

	census, err := ParseManifestFormCensus(out)
	if err != nil {
		t.Fatalf("%v", err)
	}
	t.Logf("перепись: %s (манифестов в индексе %d)", census, len(manifests))

	if name, meaning, green := ManifestFormJudgeOutcome(code); !green {
		t.Fatalf("%s (код %d): %s\n%s\n  перепись: %s", name, code, meaning,
			strings.TrimSpace(out), census)
	}

	// ВТОРОЕ, независимое утверждение об объёме: судья обязан прочитать РОВНО
	// столько документов, сколько их лежит в индексе. Меньше — часть дерева ушла
	// из-под суда молча; больше — он прочитал то, чего в коммите нет.
	if census.ManifestsRead != len(manifests) {
		t.Fatalf("судья прочитал %d манифестов, а в индексе их %d — числа обязаны совпадать: "+
			"меньше означает, что часть документов ушла из-под суда МОЛЧА, больше — что "+
			"вердикт вынесен о том, чего в коммите нет\n  перепись: %s\n  индекс: %s",
			census.ManifestsRead, len(manifests), census, strings.Join(manifestFormRelTo(root, manifests), ", "))
	}
	if census.FilesWalked == 0 {
		t.Fatalf("судья вышел зелёным, НЕ ОСМОТРЕВ НИ ОДНОГО пути: это «ноль прочитанного», "+
			"поданное как «ноль находок»\n  перепись: %s", census)
	}
}

// manifestFormRelTo — пути относительно корня, для читаемого текста находки.
func manifestFormRelTo(root string, paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if rel, err := filepath.Rel(root, p); err == nil {
			out = append(out, filepath.ToSlash(rel))
			continue
		}
		out = append(out, p)
	}
	return out
}

// buildManifestFormJudge — собирает судью формы ИЗ ПИНА и возвращает путь к
// двоичному.
//
// Собирается, а не зовётся через `go run`: тот схлопывает код возврата программы
// в единицу, и три исхода судьи стали бы двумя.
func buildManifestFormJudge(t *testing.T, root string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "modulemanifestcheck")
	cmd := exec.Command("go", "build", "-o", bin, ManifestFormJudgePackage)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("судья формы не собирается из пина (%s): %v\n%s\n  Он лежит вне `internal/` "+
			"своего модуля и потому собирается отсюда законно; отказ сборки означает, что "+
			"пин перестал его везти.", ManifestFormJudgePackage, err, out)
	}
	return bin
}

// runManifestFormJudge — прогон судьи над названным корнем: вывод и КОД ВОЗВРАТА.
//
// Код снимается явно и дословно: у судьи исходов три, и схлопывание их в
// «ненулевой» вернуло бы ровно тот дефект, ради которого гейт заведён.
func runManifestFormJudge(t *testing.T, bin, treeRoot string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, "-root="+treeRoot)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return string(out), ee.ExitCode()
	}
	t.Fatalf("судья формы не запустился: %v\n%s", err, out)
	return "", -1
}
