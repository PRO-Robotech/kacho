// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// TestHumanTextMetadataKey_Injection — доказательство, что проверка выше
// СПОСОБНА упасть и способна смолчать. Инъекция зовёт ту же функцию, что и
// проверка дерева, а не свою копию.
func TestHumanTextMetadataKey_Injection(t *testing.T) {
	cases := []struct {
		name    string
		decl    string
		wantHit string // подстрока в находке; пусто = молчит
		wantErr bool
	}{
		{
			name:    "человеческий текст без двоичной формы — краснеет с ИМЕНЕМ ключа",
			decl:    `const K = "x-kacho-principal-display-name"`,
			wantHit: "x-kacho-principal-display-name",
		},
		{
			name: "тот же ключ с двоичным близнецом рядом — молчит (окно выкатки)",
			decl: `const (
	K    = "x-kacho-principal-display-name"
	KBin = "x-kacho-principal-display-name-bin"
)`,
		},
		{
			name: "только двоичная форма — молчит (цель достигнута)",
			decl: `const KBin = "x-kacho-principal-display-name-bin"`,
		},
		{
			name: "идентификатор — не человеческий текст, кодировать нечего",
			decl: `const K = "x-kacho-principal-id"`,
		},
		{
			name: "имя ресурса — DNS label по контракту, латиница by construction",
			decl: `const K = "x-kacho-namespace-name"`,
		},
		{
			name:    "HTTP-заголовок в канонической форме — НЕ метаданные, не предмет",
			decl:    `const H = "X-Kacho-Principal-Display-Name"`,
			wantErr: true, // ключей метаданных ноль ⇒ предпосылка отказывает
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "pkg", "x")
			if err := os.MkdirAll(dir, 0o750); err != nil {
				t.Fatalf("подготовка: %v", err)
			}
			body := "package x\n\n" + c.decl + "\n"
			if err := os.WriteFile(filepath.Join(dir, "keys.go"), []byte(body), 0o600); err != nil {
				t.Fatalf("запись: %v", err)
			}
			// Фикстура обязана быть видна обходчику дерева: он читает индекс git,
			// а не диск, — поэтому синтетический корень становится репозиторием.
			initTinyRepo(t, root)

			census, findings, err := auditMetadataHumanText(t, root)
			if c.wantErr {
				if err == nil {
					t.Fatalf("предпосылка обязана была отказать; перепись: %s", census)
				}
				return
			}
			if err != nil {
				t.Fatalf("неожиданный отказ предпосылки: %v (перепись: %s)", err, census)
			}
			if c.wantHit == "" {
				if len(findings) != 0 {
					t.Fatalf("законный случай признан находкой: %v", findings)
				}
				return
			}
			if len(findings) == 0 {
				t.Fatalf("дефект не найден — проверка неспособна упасть; перепись: %s", census)
			}
			var named bool
			for _, f := range findings {
				if strings.Contains(f, c.wantHit) {
					named = true
				}
			}
			if !named {
				t.Fatalf("находка есть, виновник не назван (%q): %v", c.wantHit, findings)
			}
		})
	}
}

// initTinyRepo превращает синтетический каталог в настоящий репозиторий:
// обходчик берёт состав у версионного контроля, поэтому подменить его нечем —
// фикстура обязана быть отслеживаемой.
//
// Окружение git задаётся ЯВНО (`gitenv.Command`): проба, зовущая git с
// унаследованным окружением, писала бы в репозиторий, из которого запущена,
// а испорченный чужой индекс делает лживыми ВСЕ проверки, читающие дерево.
func initTinyRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := gitenv.Command(dir, args...)
		cmd.Env = append(cmd.Env,
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q")
	run("add", ".")
	run("commit", "-qm", "фикстура инъекции")
}
