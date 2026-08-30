// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_deploy_dropguard_wired_injection_test.go — доказательство того, что
// соседний гейт СПОСОБЕН упасть и способен смолчать.
//
// Гейт, который сегодня зелен, от гейта, потерявшего способность краснеть,
// неотличим ничем: оба печатают ноль находок. Поэтому распознаватель вызова
// проверяется здесь на синтетике — по одной пробе на каждую форму записи, в
// которой предмет встречается законно, и по одной на каждую форму, которой он
// НЕ является.
//
// Синтетика, а не правка настоящего дерева: вход подаётся целиком отсюда, поэтому
// проба детерминирована, ничего за собой не оставляет и не зависит от того, что
// сегодня лежит в services/*. Границу этого способа надо назвать честно — он
// доказывает, что МЕХАНИЗМ жив, и НЕ доказывает, что предмет ещё производится
// деревом; вторую половину держит перепись в самом гейте (обход пуст → отказ).
package repohygiene

import (
	"os"
	"path/filepath"
	"testing"
)

// writeGoFile кладёт в каталог файл с исходником.
func writeGoFile(t *testing.T, dir, name, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// TestDropguardCallDetectorSeesCodeAndIgnoresProse — распознаватель вызова.
//
// Ось у проверки одна — «это УЗЕЛ-ВЫЗОВ или текст, похожий на него», — и по ней
// перечислены обе стороны. Законные близнецы (проза, строка, имя из другого
// пакета, вызов другой функции того же пакета) обязаны молчать: гейт, краснеющий
// на объяснении стража, сняли бы первым, а объяснений этих в миграторах много.
func TestDropguardCallDetectorSeesCodeAndIgnoresProse(t *testing.T) {
	const head = "package migrator\n\n"

	cases := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "вызов на пути наката — находка",
			src: head + `import (
	"context"
	"database/sql"
	"os"

	"github.com/PRO-Robotech/kacho/internal/dropguard"
)

func (r *Runner) preflight(ctx context.Context, db *sql.DB) error {
	return dropguard.Gate(ctx, db, "svc", r.fs, os.Stderr, dropguard.WholeChain())
}`,
			want: true,
		},
		{
			name: "вызов внутри условия — тоже находка (обход идёт по всему дереву)",
			src: head + `func up(cond bool) error {
	if cond {
		return dropguard.Gate(ctx, db, "svc", fs, out, scope)
	}
	return nil
}`,
			want: true,
		},
		{
			name: "ПРОЗА про страж — молчание (иначе гейт зеленел бы на объяснении)",
			src: head + `// СЧЁТ ПЕРЕД СНОСОМ. Здесь когда-то стоял dropguard.Gate(ctx, db, "svc", ...),
// и объяснение, почему его нельзя обойти целью, осталось. Вызова НЕТ.
func up() error { return goose.Up(db, ".") }`,
			want: false,
		},
		{
			name: "имя в СТРОКЕ — молчание",
			src: head + `func up() error {
	return fmt.Errorf("накат без dropguard.Gate(...) отвергнут")
}`,
			want: false,
		},
		{
			name: "другая функция того же пакета — молчание (Gate, а не Inventory)",
			src: head + `func up() error {
	inv, _ := dropguard.Inventory("svc", fs)
	_ = inv
	return nil
}`,
			want: false,
		},
		{
			name: "Gate из ЧУЖОГО пакета — молчание",
			src: head + `func up() error {
	return bootgate.Gate(ctx, db, "svc", fs, out, scope)
}`,
			want: false,
		},
		{
			name: "мигратор без стража вовсе — молчание",
			src:  head + `func up() error { return goose.UpContext(ctx, db, ".") }`,
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeGoFile(t, dir, "runner.go", tc.src)

			got, filesRead := callsDropguardGate(t, dir)
			if filesRead != 1 {
				t.Fatalf("прочитано файлов %d, ожидался 1 — проба судила бы по пустоте", filesRead)
			}
			if got != tc.want {
				t.Errorf("распознаватель ответил %v, ожидалось %v", got, tc.want)
			}
		})
	}
}

// TestDropguardCallDetectorSkipsTestsAndReportsEmptyScan — две границы обхода.
//
// (а) Вызов, стоящий ТОЛЬКО в пробе, провязкой не является: проба зовёт страж
// сама и о накате не говорит ничего. (б) Каталог без исходников обязан вернуть
// ноль прочитанных — иначе «не нашли» слилось бы с «не читали».
func TestDropguardCallDetectorSkipsTestsAndReportsEmptyScan(t *testing.T) {
	t.Run("вызов только в _test.go — не провязка", func(t *testing.T) {
		dir := t.TempDir()
		writeGoFile(t, dir, "runner_test.go", "package migrator\n\nfunc TestX() {\n\t_ = dropguard.Gate(ctx, db, \"svc\", fs, out, scope)\n}")

		got, filesRead := callsDropguardGate(t, dir)
		if got {
			t.Error("вызов из пробы принят за провязку наката")
		}
		if filesRead != 0 {
			t.Errorf("прочитано %d файлов, ожидалось 0: _test.go не входит в обход", filesRead)
		}
	})

	t.Run("пустой каталог — ноль прочитанных", func(t *testing.T) {
		got, filesRead := callsDropguardGate(t, t.TempDir())
		if got || filesRead != 0 {
			t.Errorf("пустой каталог дал found=%v filesRead=%d, ожидалось false/0", got, filesRead)
		}
	})
}
