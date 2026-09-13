// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
	"github.com/PRO-Robotech/corelib/modulemanifest"

	"github.com/PRO-Robotech/kacho/internal/contractsource"
)

// TestModelCanonAgreesWithPlatformManifests — блоки канона модели прав
// побайтово совпадают с порождёнными из манифестов доменов этого дерева.
//
// Предмет, устройство и довод «почему это не вторая реализация разбора и не
// копия чужого файла» — в шапке modelcanonpin.go. Здесь — прогон.
//
// # Что этот гейт судит, а что НЕТ
//
// Судит СОГЛАСИЕ двух операндов, и только его. Форму манифеста он не судит
// (её судит один исполнитель, `module-manifest-check` службы, и второго
// заводить запрещено решением #1778), правила ролей не судит (у них свой
// прогон против живой базы в дереве службы), а согласие манифеста с каталогом
// прав судит соседний TestManifestIsNotASecondDeclarationOfARight — ось там
// другая: якорь области, а не текст блока.
//
// # Почему манифесты берутся из ИНДЕКСА, а не обходом диска
//
// Обход диска прочитал бы игнорируемые каталоги — рабочие копии полос,
// распаковки чартов, отчёты прогонов, — и вердикт стал бы свойством рабочего
// каталога, а не коммита. Тот же довод и у contractsource.Files.
func TestModelCanonAgreesWithPlatformManifests(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	manifests := platformDomainManifests(t, root)
	if len(manifests) == 0 {
		t.Fatal("манифестов домена в индексе НЕ НАЙДЕНО НИ ОДНОГО (services/*/manifest.yaml) — " +
			"«ноль находок» здесь означало бы «ноль прочитанного»: сверять было бы нечего, " +
			"а вердикт выглядел бы зелёным")
	}

	// Канон резолвит contractsource: корень, лежащий в этом дереве, он берёт
	// оттуда, отсутствующий — из каталога опубликованного модуля. Какой из двух
	// случаев наступил, видно в переписи: там печатается фактический путь.
	canonSrc, err := contractsource.Path(root, ModelCanonRelToProto)
	if err != nil {
		t.Fatalf("канон не резолвится (%s): %v\n  Это ОТКАЗ ПРЕДПОСЫЛКИ, а не находка о "+
			"продукте: второй операнд сверки объявлен пином github.com/PRO-Robotech/kaname "+
			"в go.mod этого дерева. Пропуском он не является — пропуск был бы неотличим от "+
			"успеха, а прогон идёт без -v.", ModelCanonRelToProto, err)
	}

	dst := t.TempDir()
	census, err := ComposeCanonJudgeRoot(dst, manifests, canonSrc, kanameModuleManifest(t, root))
	if err != nil {
		t.Fatalf("корень сверки не собран: %v", err)
	}

	out, code := runCanonJudge(t, buildCanonJudge(t, root), dst)

	blocks, owned, bytesCompared, perr := ParseCanonJudgeCensus(out)
	if perr != nil {
		t.Fatalf("%v", perr)
	}
	census.BlocksCompared, census.BlocksOwned, census.BytesCompared = blocks, owned, bytesCompared
	t.Logf("перепись: %s", census)

	if name, meaning, green := CanonJudgeOutcome(code); !green {
		t.Fatalf("%s (код %d): %s\n%s\n  перепись: %s", name, code, meaning,
			strings.TrimSpace(out), census)
	}

	// ВТОРОЕ, независимое утверждение об объёме. Исполнитель отдаёт «без
	// предмета» на пустом обходе сам, но его код возврата отвечает на вопрос
	// «нашёл ли», а не «прочитал ли». Зелёное над нулём сверенных байт есть
	// вакуумное зелёное, и отличить его от настоящего можно только здесь.
	if blocks == 0 || owned == 0 || bytesCompared == 0 {
		t.Fatalf("исполнитель вышел зелёным, НИЧЕГО НЕ СВЕРИВ: блоков %d из %d, байт %d — "+
			"это «ноль прочитанного», поданное как «ноль находок»\n  перепись: %s\n%s",
			blocks, owned, bytesCompared, census, strings.TrimSpace(out))
	}
}

// platformDomainManifests — манифесты доменов ЭТОГО дерева, абсолютными путями,
// из индекса git.
func platformDomainManifests(t *testing.T, root string) []string {
	t.Helper()
	out, err := gitenv.Command(root, "ls-files", "-z", "--", "services/*/"+modulemanifest.FileName).Output()
	if err != nil {
		t.Fatalf("обход индекса за манифестами домена: %v", err)
	}
	var paths []string
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel = strings.TrimSpace(rel); rel != "" {
			paths = append(paths, filepath.Join(root, filepath.FromSlash(rel)))
		}
	}
	return paths
}

// kanameModuleManifest — манифест САМОЙ службы, приехавший пином, либо пустая
// строка, если модуль его не везёт.
//
// Пустая строка отказом НЕ является и молчанием тоже: модуль своего манифеста
// не привёз — исполнитель объявит его модуль непокрытым и выйдет находкой. То
// есть отсутствие названо тем, кто о наборе модулей знает, а не угадано здесь.
func kanameModuleManifest(t *testing.T, root string) string {
	t.Helper()
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/PRO-Robotech/kaname")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("каталог модуля службы не резолвится: %v — пин объявлен в go.mod, и его "+
			"отказ есть отказ предпосылки этого дерева", err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		t.Fatal("каталог модуля службы пуст: модуль объявлен пином и не распакован")
	}
	p := filepath.Join(dir, modulemanifest.FileName)
	if st, serr := os.Stat(p); serr != nil || !st.Mode().IsRegular() {
		return ""
	}
	return p
}

// buildCanonJudge — собирает исполнителя сверки ИЗ ПИНА и возвращает путь к
// двоичному.
//
// Собирается, а не зовётся через `go run`: тот схлопывает код возврата
// программы в единицу, и четыре исхода исполнителя стали бы двумя.
func buildCanonJudge(t *testing.T, root string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "modelcanoncheck")
	cmd := exec.Command("go", "build", "-o", bin, CanonJudgePackage)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("исполнитель сверки не собирается из пина (%s): %v\n%s\n  Он лежит вне "+
			"`internal/` своего модуля и потому собирается отсюда законно; отказ сборки "+
			"означает, что пин перестал его везти.", CanonJudgePackage, err, out)
	}
	return bin
}

// runCanonJudge — прогон исполнителя над собранным корнем: вывод и КОД ВОЗВРАТА.
//
// Код снимается явно и дословно: у исполнителя исходов четыре, и схлопывание их
// в «ненулевой» вернуло бы ровно тот дефект, ради которого гейт заведён.
func runCanonJudge(t *testing.T, bin, treeRoot string) (string, int) {
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
	t.Fatalf("исполнитель сверки не запустился: %v\n%s", err, out)
	return "", -1
}
