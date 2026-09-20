// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// Прогон одной командой:
//
//	go test ./internal/repohygiene/ -run TestRetiredIdentityVendorBindingsStayUnderTheirCeiling -count=1 -v
//
// Печатает перепись по каждому дереву (файлов · строк прочитано · архивов ·
// привязок с разбивкой по осям · потолок) и итог с ЕДИНИЦЕЙ СЧЁТА.

// vendorArchiveTextCap — сколько текста берётся из архива. Архив судится по
// признаку «принадлежит издателю», а не по числу совпадений; читать его целиком
// незачем, а предел держит прогон конечным на чужом вендоренном чарте.
const vendorArchiveTextCap = 4 << 20

// vendorReadFile — тело файла, если оно ТЕКСТ. Двоичное молча пропускается: по
// нему нельзя считать строки, и «прочитано» о нём было бы ложью.
func vendorReadFile(abs string) (string, bool) {
	b, err := os.ReadFile(abs) // #nosec G304 -- путь получен обходом дерева под его корнем
	if err != nil {
		return "", false
	}
	if len(b) == 0 {
		return "", false
	}
	if strings.IndexByte(string(b), 0) >= 0 || !utf8.Valid(b) {
		return "", false
	}
	return string(b), true
}

// vendorReadArchive — текст из архива: распаковка gzip+tar, до предела.
//
// Нечитаемый архив возвращает пустой текст, и его судьбу решает ОДНО имя — это
// и есть подпорка: содержимое может стать нечитаемым или быть переименованным у
// издателя, имя остаётся.
func vendorReadArchive(abs string) string {
	f, err := os.Open(abs) // #nosec G304 -- путь получен обходом дерева под его корнем
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return ""
	}
	defer func() { _ = gz.Close() }()
	var b strings.Builder
	tr := tar.NewReader(gz)
	for b.Len() < vendorArchiveTextCap {
		h, err := tr.Next()
		if err != nil {
			break
		}
		b.WriteString(h.Name)
		b.WriteString("\n")
		if h.Typeflag != tar.TypeReg {
			continue
		}
		chunk, err := io.ReadAll(io.LimitReader(tr, int64(vendorArchiveTextCap-b.Len())))
		if err != nil {
			break
		}
		b.Write(chunk)
	}
	return b.String()
}

// vendorCorpusFromPaths — корпус дерева из перечня относительных путей.
func vendorCorpusFromPaths(root string, rels []string) vendorTreeCorpus {
	corpus := vendorTreeCorpus{Bodies: map[string]string{}}
	for _, rel := range rels {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		switch {
		case vendorArchiveFile(rel):
			corpus.Archives = append(corpus.Archives, vendorArchive{Name: rel, Text: vendorReadArchive(abs)})
		case vendorProseFile(rel):
			corpus.ProseSkipped++
			continue
		default:
			body, ok := vendorReadFile(abs)
			if !ok {
				corpus.ProseSkipped++
				continue
			}
			corpus.Bodies[rel] = body
			corpus.LinesRead += strings.Count(body, "\n") + 1
		}
	}
	return corpus
}

// vendorWalkModuleDir — пути модуля из кэша: пин go.mod читается целиком.
func vendorWalkModuleDir(t *testing.T, root string) []string {
	t.Helper()
	var rels []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rels = append(rels, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("обход %s: %v — проверка НЕ ИСПОЛНЯЛАСЬ", root, err)
	}
	return rels
}

// vendorModuleDir — каталог модуля по ПИНУ из go.mod текущего дерева.
//
// Именно пин, а не соседний клон: клон стоит на случайной ревизии, а собирается
// платформа против пина. Модуль недоступен — третья категория, не зелёное.
func vendorModuleDir(t *testing.T, repo, module string) string {
	t.Helper()
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", module)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: пин модуля %s не разрешён: %v\n%s\n"+
			"дерево этого модуля — часть предмета, и без него число неизвестно", module, err, out)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: пин модуля %s разрешён в пустой путь", module)
	}
	return dir
}

// TestRetiredIdentityVendorBindingsStayUnderTheirCeiling — гейт убывающего
// потолка: привязок к снимаемому издателю личности в трёх деревьях продукта не
// больше записанного, а меньше — повод переписать запись.
func TestRetiredIdentityVendorBindingsStayUnderTheirCeiling(t *testing.T) {
	t.Parallel()

	repo := repoRoot(t)
	tree, err := treecorpus.NewTree(repo)
	if err != nil {
		t.Fatalf("состав дерева не установлен: %v — «ноль находок» означало бы "+
			"«ноль прочитанного»", err)
	}

	corpora := map[string]vendorTreeCorpus{
		vendorTreePlatform: vendorCorpusFromPaths(repo, tree.SortedFiles()),
	}
	for name, module := range map[string]string{
		vendorTreeAccess:     "github.com/PRO-Robotech/kaname",
		vendorTreeFoundation: "github.com/PRO-Robotech/corelib",
	} {
		dir := vendorModuleDir(t, repo, module)
		corpora[name] = vendorCorpusFromPaths(dir, vendorWalkModuleDir(t, dir))
	}

	findings, census, _, err := judgeRetiredVendorCeiling(corpora, retiredVendorCeilings)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}

	total, ceiling := 0, 0
	for _, name := range []string{vendorTreePlatform, vendorTreeAccess, vendorTreeFoundation} {
		c := census[name]
		t.Logf("перепись %s: %s", name, c)
		total += c.Bindings
		ceiling += c.Ceiling
	}
	t.Logf("ИТОГО привязок к снимаемому издателю личности: %d СТРОК при потолке %d СТРОК "+
		"(деревьев обойдено %d; единица счёта — строка исходника, архив — одна строка "+
		"независимо от числа совпадений внутри)", total, ceiling, len(census))

	if total == 0 && ceiling == 0 {
		t.Log("предмет снят целиком: привязок ноль во всех трёх деревьях — снимите этот " +
			"гейт вместе с предметом, он больше не стережёт ничего")
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// vendorSourceArm — одно ИМЕННОЕ плечо предиката-источника: та форма записи, в
// которой отчёт искал привязку.
//
// Перечень здесь стоит не как распознаватель, а как ПРОВЕРЯЕМОЕ УТВЕРЖДЕНИЕ:
// «класс накрывает каждое плечо». Сам гейт ни одного из этих выражений не
// исполняет — он смотрит один признак класса.
type vendorSourceArm struct {
	Name string
	Re   *regexp.Regexp
}

// vendorSourceArms — восемь ИМЕННЫХ плеч источника. Три оставшихся (пути
// `/admin/...`, `/sessions/whoami`, `/self-service/` и порты 4444/4445) сюда не
// входят: первые два — безымянная поверхность, её дом `ProviderSurfaces`, порт
// под ось не заведён (см. шапку гейта, п. 2 границы).
var vendorSourceArms = []vendorSourceArm{
	{"образ издателя", regexp.MustCompile(`(?i)oryd/(hydra|kratos)`)},
	{"переменная окружения", regexp.MustCompile(`[A-Z0-9]+_(HYDRA|KRATOS)_[A-Z0-9_]+`)},
	{"имя службы", regexp.MustCompile(`(?i)(hydra|kratos)-[a-z]+`)},
	{"алиас подчарта базы", regexp.MustCompile(`(?i)pg-(hydra|kratos)`)},
	{"столбец", regexp.MustCompile(`(?i)hydra_client_id`)},
	{"имя печенья", regexp.MustCompile(`(?i)ory_kratos_session`)},
	{"файл конфигурации", regexp.MustCompile(`(?i)(hydra|kratos)\.(yaml|yml|ts|go)`)},
	{"имя чарта", regexp.MustCompile(`(?i)(hydra|kratos)-[0-9]+\.[0-9]+`)},
}

// TestRetiredVendorClassSubsumesEverySourceArm — ПРЕДПОСЫЛКА гейта: признак
// класса накрывает КАЖДУЮ именную форму, которую предикат-источник перечислял
// отдельным плечом.
//
// Перепись форм выводится ОБХОДОМ дерева, а не памятью: каждое плечо гоняется по
// настоящему корпусу платформы, и печатается, сколько строк оно нашло и сколько
// из них класс НЕ поймал. Последнее обязано быть нулём — иначе распознаватель
// слеп ровно на ту форму, ради которой заведено плечо.
//
// ЧЕМ ОБХОД ОГРАНИЧЕН, названо вслух: он видит только дерево платформы (kaname и
// corelib читаются по пину и на состав форм не влияют — там тех же форм не
// больше), только судимые файлы (проза и двоичное исключены тем же правилом, что
// в гейте) и только строки, не начинающиеся маркером комментария. Плечо, ушедшее
// в ноль, здесь не падение, а ЦЕЛЬ: оно печатается как «ноль — форма снята».
func TestRetiredVendorClassSubsumesEverySourceArm(t *testing.T) {
	t.Parallel()

	repo := repoRoot(t)
	tree, err := treecorpus.NewTree(repo)
	if err != nil {
		t.Fatalf("состав дерева не установлен: %v", err)
	}
	corpus := vendorCorpusFromPaths(repo, tree.SortedFiles())
	if len(corpus.Bodies) == 0 {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: обход платформы пуст")
	}

	rels := make([]string, 0, len(corpus.Bodies))
	for rel := range corpus.Bodies {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	// vendorSourceNames — имена издателя В ЗАПИСИ ИСТОЧНИКА. Перечень НЕЗАВИСИМ
	// от `retiredVendorMarks` намеренно: он отбирает строки, которые плечи
	// источника вообще способны поймать, и остаётся прежним, если признак класса
	// в гейте сузят. Сузят — строки дойдут до плеч, класс их не поймает, и
	// побег станет виден.
	vendorSourceNames := []string{"hydra", "kratos", "oryd"}
	carries := func(lower string) bool {
		for _, n := range vendorSourceNames {
			if strings.Contains(lower, n) {
				return true
			}
		}
		return false
	}

	type armStat struct{ found, escaped int }
	stats := make([]armStat, len(vendorSourceArms))
	var escapes []string
	for _, rel := range rels {
		if vendorProseFile(rel) {
			continue
		}
		body := corpus.Bodies[rel]
		lowerBody := strings.ToLower(body)
		if !carries(lowerBody) {
			continue // ни одно плечо источника здесь сработать не может
		}
		lines := strings.Split(body, "\n")
		lowerLines := strings.Split(lowerBody, "\n")
		for i, line := range lines {
			if vendorCommentLine(line) || !carries(lowerLines[i]) {
				continue
			}
			caught := vendorLineAxis(lowerLines[i]) != ""
			for a, arm := range vendorSourceArms {
				if !arm.Re.MatchString(line) {
					continue
				}
				stats[a].found++
				if !caught {
					stats[a].escaped++
					if len(escapes) < 10 {
						escapes = append(escapes, fmt.Sprintf("%s:%d (%s) %s",
							rel, i+1, arm.Name, strings.TrimSpace(line)))
					}
				}
			}
		}
	}

	live := 0
	for a, arm := range vendorSourceArms {
		if stats[a].found == 0 {
			t.Logf("плечо %q: строк 0 — форма в дереве платформы снята", arm.Name)
			continue
		}
		live++
		t.Logf("плечо %q: строк %d · мимо класса %d", arm.Name, stats[a].found, stats[a].escaped)
	}
	t.Logf("перепись форм выведена обходом: плеч объявлено %d · живо сегодня %d · "+
		"файлов судимо %d · строк прочитано %d", len(vendorSourceArms), live,
		len(corpus.Bodies), corpus.LinesRead)

	if live == 0 {
		t.Log("ни одной именной формы в дереве платформы не осталось — предмет снят, " +
			"снимайте гейт вместе с ним")
	}
	if len(escapes) > 0 {
		t.Errorf("признак класса не накрывает %d форм(ы) источника — распознаватель слеп "+
			"ровно там, где источник смотрел отдельным плечом:\n  %s",
			len(escapes), strings.Join(escapes, "\n  "))
	}
}
