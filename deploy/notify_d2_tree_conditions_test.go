// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// notify_d2_tree_conditions_test.go — условия полосы D2 NTF-1 на дерево,
// судимые по тексту файлов (замысел З28; N10, N18, N35-2, R37-1, CX1-85).
//
//   - N10 / CX1-85: перечень координат учётных данных `CRED_PATHS` и текст
//     отказа проверки слоя живут в `cutover-creds-layer.sh`; скрипт раскатки
//     зовёт его функцию; почтовых координат в перечне нет; фраза «in this
//     script» в скриптах раскатки — 0.
//   - N18 / CX1-93: в `prod-profile-fail-closed-test.sh` каждая строка рендера
//     профиля `prod` идёт через обёртку цепочки, прямого `-f` профиля нет.
//   - N35-2: одиночный рендер подчарта `charts/kaname` идёт только через
//     обёртку — `render_kaname_alone` (шелл) и `renderKanameAlone` (Go).
//   - R37-1: копия осмотра notify (`deploy/scripts/render-notify-inspect.sh`,
//     `deploy/testdata/notify-inspect/**`) снята тем же изменением, что вносит
//     строки таблицы модулей.

package deploy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestNTFD2_CredPathsLiveInTheCredsLayerScript — N10 / CX1-85.
func TestNTFD2_CredPathsLiveInTheCredsLayerScript(t *testing.T) {
	scripts, _ := filepath.Glob(filepath.Join(umbrellaDir, "cutover-*.sh"))
	if len(scripts) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: скриптов раскатки %s/cutover-*.sh ноль — обход пуст", umbrellaDir)
	}
	layer := filepath.Join(umbrellaDir, "cutover-creds-layer.sh")
	body, err := os.ReadFile(layer)
	if err != nil {
		t.Errorf("КРАСНЫЙ: %s нет — перечень CRED_PATHS и проверка слоя не вынесены (CX1-85, N10): %v", layer, err)
	} else {
		text := string(body)
		blk := regexp.MustCompile(`(?s)CRED_PATHS=\((.*?)\)`).FindAllStringSubmatch(text, -1)
		if len(blk) != 1 {
			t.Errorf("КРАСНЫЙ: в %s блоков CRED_PATHS=( … ) %d, ожидался ровно один", layer, len(blk))
		} else if strings.Contains(blk[0][1], "smtp") {
			t.Errorf("КРАСНЫЙ: CRED_PATHS в %s несёт почтовую координату — почта не учётные данные слоя (CX1-85)", layer)
		}
	}
	cut := filepath.Join(umbrellaDir, "cutover-fe3455.sh")
	if b, rerr := os.ReadFile(cut); rerr == nil && !strings.Contains(string(b), "cutover-creds-layer.sh") {
		t.Errorf("КРАСНЫЙ: %s не подключает cutover-creds-layer.sh — проверка слоя не одна функция (CX1-85)", cut)
	}
	for _, s := range scripts {
		b, rerr := os.ReadFile(s)
		if rerr != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s не прочитан: %v", s, rerr)
		}
		if n := strings.Count(string(b), "in this script"); n != 0 {
			t.Errorf("КРАСНЫЙ: %s — «in this script» %d раз: комментарий и текст отказа не перенесены к перечню (N10)", s, n)
		}
	}
	t.Logf("осмотрено скриптов раскатки %d", len(scripts))
}

// TestNTFD2_ProdProfileRendersGoThroughTheChainWrapper — N18 / CX1-93.
func TestNTFD2_ProdProfileRendersGoThroughTheChainWrapper(t *testing.T) {
	p := filepath.Join("tests", "helm", "prod-profile-fail-closed-test.sh")
	body, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s не прочитан: %v", p, err)
	}
	direct := regexp.MustCompile(`(-f\s+"?\$PROD"?|render_only\s+"?\$PROD"?|-f\s+\S*values\.prod\.yaml)`)
	var hits []string
	renders := 0
	for i, line := range strings.Split(string(body), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "#") {
			continue
		}
		if strings.Contains(line, "helm_try") || strings.Contains(line, "render_only") {
			renders++
		}
		if direct.MatchString(line) {
			hits = append(hits, p+":"+strconv.Itoa(i+1)+": "+trim)
		}
	}
	if !strings.Contains(string(body), "render-chain.sh") {
		t.Errorf("КРАСНЫЙ: %s не подключает обёртку lib/render-chain.sh (N18)", p)
	}
	if len(hits) > 0 {
		t.Errorf("КРАСНЫЙ: рендер профиля prod мимо обёртки цепочки — %d строк (N18, CX1-93):\n  %s", len(hits), strings.Join(hits, "\n  "))
	}
	t.Logf("%s: строк рендера %d, прямых -f профиля prod %d", p, renders, len(hits))
}

// TestNTFD2_KanameAloneRenderWrappersExist — N35-2: обёртки одиночного рендера
// kaname объявлены (шелл и Go).
func TestNTFD2_KanameAloneRenderWrappersExist(t *testing.T) {
	lib := filepath.Join("tests", "helm", "lib", "render-chain.sh")
	b, err := os.ReadFile(lib)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s не прочитан: %v", lib, err)
	}
	if !regexp.MustCompile(`(?m)^render_kaname_alone\s*\(\)`).Match(b) {
		t.Errorf("КРАСНЫЙ: в %s нет функции render_kaname_alone — одиночный рендер charts/kaname не видит помощников notify (З28, N35-2)", lib)
	}
	goFiles, _ := filepath.Glob("*.go")
	if len(goFiles) == 0 {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: файлов Go в deploy/ ноль")
	}
	found := 0
	for _, f := range goFiles {
		body, rerr := os.ReadFile(f) // #nosec G304 -- путь из обхода каталога
		if rerr != nil {
			t.Fatal(rerr)
		}
		if regexp.MustCompile(`(?m)^func renderKanameAlone\(`).Match(body) {
			found++
		}
	}
	if found != 1 {
		t.Errorf("КРАСНЫЙ: func renderKanameAlone в deploy/*.go — %d объявлений, ожидалось 1 (З28, N35-2)", found)
	}
	t.Logf("осмотрено файлов Go %d; объявлений renderKanameAlone %d", len(goFiles), found)
}

// TestNTFD2_NotifyInspectCopyIsRetired — R37-1: копия осмотра снята.
func TestNTFD2_NotifyInspectCopyIsRetired(t *testing.T) {
	var left []string
	for _, p := range []string{
		filepath.Join("scripts", "render-notify-inspect.sh"),
		filepath.Join("testdata", "notify-inspect"),
	} {
		if _, err := os.Stat(p); err == nil {
			left = append(left, "deploy/"+p)
		}
	}
	if len(left) > 0 {
		t.Errorf("КРАСНЫЙ: копия осмотра notify не снята: %s — строки таблицы модулей внесены, самоистечение не исполнено (R37-1)", strings.Join(left, ", "))
	}
}
