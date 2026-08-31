// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_mail_lane_single_declaration_injection_test.go — доказательство того,
// что MAIL-54 СПОСОБЕН упасть, и падает ТОЛЬКО на своём предмете.
//
// Инъекция идёт по КОПИИ дерева в t.TempDir(): состояние, которого проверка не
// заводила, она не трогает — ни рабочую копию, ни индекс, ни настройки.
//
// По КАЖДОЙ из трёх осей сценария — три прогона (`testing.md` §«Гейт на класс»,
// п. 2в):
//
//	контроль          — копия как есть: гейт молчит;
//	инъекция          — возвращён дефект: гейт краснеет И НАЗЫВАЕТ КООРДИНАТУ;
//	законный близнец  — та же ФОРМА без дефекта: гейт молчит.
//
// Законный близнец обязателен: без него гейт ловил бы форму, а не существо, и
// первый же ложный срабат его отключил бы.
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyTree — копия дерева в каталог пробы. Возвращает корень копии.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o644)
	})
	if err != nil {
		t.Fatalf("копия дерева %s → %s: %v", src, dst, err)
	}
}

// mailLaneFixture — копия зонтичного чарта плюс координаты внутри неё.
type mailLaneFixture struct {
	root, tpl, script string
}

func newMailLaneFixture(t *testing.T) mailLaneFixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), "umbrella")
	copyTree(t, umbrellaDir, root)
	return mailLaneFixture{
		root:   root,
		tpl:    filepath.Join(root, "charts", "kacho-iam", "templates", "_kratos-identity.tpl"),
		script: filepath.Join(root, filepath.Base(cutoverScript)),
	}
}

// run — находки гейта по копии. Утверждения возвращают находки СПИСКОМ, а не
// роняют прогон: предмет инъекции — сам факт находки, поэтому её появление не
// должно красить пробу-доказательство.
func (f mailLaneFixture) run(t *testing.T) []string {
	t.Helper()
	return mailLaneAssertions(t, f.root, f.tpl, f.script)
}

func (f mailLaneFixture) edit(t *testing.T, path, old, new string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := string(raw)
	switch n := strings.Count(body, old); {
	case n == 0:
		t.Fatalf("инъекция не нашла своего входа в %s: подстроки %q там нет. "+
			"Это НЕ «дефекта не осталось» — это «фикстура перестала описывать дерево», "+
			"и доказательство способности гейта упасть исчезло вместе с ней", path, old)
	case n > 1:
		// Неоднозначный якорь — та же беда, только тише: правка сядет в ПЕРВОЕ
		// вхождение, условия инъекции не создаст, и зелёное будет означать
		// «дефект не воспроизведён», а не «гейт его не нашёл». Именно так и
		// вышло при первом заходе: якорь `kratos:` совпал внутри `pg-kratos:`.
		t.Fatalf("якорь инъекции %q встречается в %s %d раз — правка сядет в первое "+
			"вхождение и условия может не создать. Зелёный прогон тогда означал бы "+
			"«дефект не воспроизведён», а не «гейт исправен»", old, path, n)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(body, old, new, 1)), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestMailLaneGateFailsOnAReturnedDefect — по три прогона на каждую из трёх осей.
func TestMailLaneGateFailsOnAReturnedDefect(t *testing.T) {
	// ── КОНТРОЛЬ: копия как есть ──────────────────────────────────────────
	//
	// Без него молчание на «законном близнеце» ниже ничего не значило бы: гейт,
	// молчащий всегда, неотличим от исправного.
	t.Run("контроль: сведённое дерево — молчание", func(t *testing.T) {
		f := newMailLaneFixture(t)
		if found := f.run(t); len(found) > 0 {
			t.Errorf("гейт краснеет на НЕТРОНУТОЙ копии — значит его находки не про "+
				"инъекцию, и все прогоны ниже недействительны:\n%s", strings.Join(found, "\n"))
		}
	})

	// ── ОСЬ 1: второе объявление раздела `courier` ────────────────────────
	t.Run("ось1 инъекция: возвращён встроенный блок courier", func(t *testing.T) {
		f := newMailLaneFixture(t)
		f.edit(t, filepath.Join(f.root, "values.dev.yaml"),
			"      # ─── ВСТРОЕННОГО БЛОКА `courier` ЗДЕСЬ НЕТ",
			"      courier:\n"+
				"        smtp:\n"+
				"          connection_uri: \"smtp://elsewhere.invalid:1025/\"\n"+
				"      # ─── ВСТРОЕННОГО БЛОКА `courier` ЗДЕСЬ НЕТ")
		if found := f.run(t); len(found) == 0 {
			t.Errorf("возвращённый встроенный блок `courier` в values.dev.yaml гейт НЕ " +
				"нашёл — он не способен упасть на своём предмете, то есть удостоверяет " +
				"единственность объявления, ничего о ней не зная")
		}
	})
	t.Run("ось1 близнец: раздел, который мы НЕ объявляем, — молчание", func(t *testing.T) {
		// Законный близнец той же ФОРМЫ: профиль объявляет значением раздел,
		// которого наша конфигурация не рендерит. Второго объявления почтовой
		// полосы это не заводит, и гейт обязан молчать — иначе он ловил бы
		// «блок в значениях», а не «второе объявление ПОЧТОВОЙ полосы».
		f := newMailLaneFixture(t)
		f.edit(t, filepath.Join(f.root, "values.dev.yaml"),
			"      # ─── ВСТРОЕННОГО БЛОКА `courier` ЗДЕСЬ НЕТ",
			"      oauth2:\n"+
				"        expose_internal_errors: false\n"+
				"      # ─── ВСТРОЕННОГО БЛОКА `courier` ЗДЕСЬ НЕТ")
		if found := f.run(t); len(found) > 0 {
			t.Errorf("гейт покраснел на разделе, которого наша конфигурация НЕ "+
				"объявляет, — значит он ловит форму («блок в значениях»), а не "+
				"существо («второе объявление почтовой полосы»):\n%s", strings.Join(found, "\n"))
		}
	})

	// ── ОСЬ 2: координата оснастки ────────────────────────────────────────
	t.Run("ось2 инъекция: координата раскатки переставлена на чужую", func(t *testing.T) {
		f := newMailLaneFixture(t)
		f.edit(t, f.script,
			"global.kacho.identity.smtp.connectionURI",
			"kratos.kratos.config.courier.smtp.connection_uri")
		if found := f.run(t); len(found) == 0 {
			t.Errorf("координата, переставленная на координату ПОСТАВЩИКА, гейтом не " +
				"найдена — то есть #1679 воспроизводится, а гейт остаётся зелёным: " +
				"оператор кладёт узел не туда, и сигнала снова нет ни одного")
		}
	})
	t.Run("ось2 близнец: НЕпочтовая координата раскатки — молчание", func(t *testing.T) {
		// Перечень разрешённых координат несёт и непочтовые (DSN, секреты).
		// Гейт обязан судить ТОЛЬКО почтовую: покраснев на соседней, он стал бы
		// красным на исправном дереве, и его сняли бы первым.
		f := newMailLaneFixture(t)
		f.edit(t, f.script, "kratos.kratos.config.dsn", "kratos.kratos.config.dsn_replica")
		if found := f.run(t); len(found) > 0 {
			t.Errorf("гейт покраснел на НЕпочтовой координате перечня — он судит не свой "+
				"предмет и на исправном дереве будет красным:\n%s", strings.Join(found, "\n"))
		}
	})

	// ── ОСЬ 3: рукописный перечень разделов ───────────────────────────────
	t.Run("ось3 инъекция: второй перечень разделов в прозе", func(t *testing.T) {
		f := newMailLaneFixture(t)
		f.edit(t, filepath.Join(f.root, "values.dev.yaml"),
			"\nkratos:\n  # KAC-127: Phase 2",
			"\n# За подчартом поставщика остаются `courier` и `serve`.\nkratos:\n  # KAC-127: Phase 2")
		if found := f.run(t); len(found) == 0 {
			t.Errorf("рукописный перечень разделов гейтом не найден — второе место об " +
				"одном предмете переживает правку шаблона молча, а гейт это удостоверяет")
		}
	})
	t.Run("ось3 близнец: блок, называющий ОДИН раздел, — молчание", func(t *testing.T) {
		// Одно имя раздела в прозе перечнем не является: перечень — это два и
		// более. Покраснев на одном, гейт запретил бы называть раздел вообще,
		// в том числе там, где это единственный способ объяснить решение.
		f := newMailLaneFixture(t)
		f.edit(t, filepath.Join(f.root, "values.dev.yaml"),
			"\nkratos:\n  # KAC-127: Phase 2",
			"\n# Раздел `courier` объявлен нашей конфигурацией личности.\nkratos:\n  # KAC-127: Phase 2")
		if found := f.run(t); len(found) > 0 {
			t.Errorf("гейт покраснел на блоке, называющем ОДИН раздел, — он запрещает "+
				"называть раздел вовсе, тогда как его предмет — рукописный ПЕРЕЧЕНЬ:\n%s", strings.Join(found, "\n"))
		}
	})
}
