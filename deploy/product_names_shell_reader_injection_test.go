// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// product_names_shell_reader_injection_test.go — ДОКАЗАТЕЛЬСТВО, что пробы
// читателя имён способны упасть, и что каждая падает на СВОЁМ предмете.
//
// # Зачем это отдельной пробой, а не скриптом рядом
//
// Утверждения читателя имён (`TestShellReaderDoesNotCheckThatTheDirectoryExists`
// и соседи) зелены на исправном дереве by design: они закрепляют ПОВЕДЕНИЕ, а не
// чинят дефект. Такое утверждение неотличимо от мёртвого до тех пор, пока ему не
// подали вход, на котором оно обязано покраснеть. Доказательство, которое надо
// помнить позвать, — то же обещание, поэтому оно гоняется вместе с прогоном.
//
// # Три прогона, а не два
//
// Контроль (всё цело — молчат все) · снятие НОВОГО свойства (краснеет только
// новая проба) · снятие СТАРОГО (краснеет только существующая). Без третьего
// молчание существующей пробы неотличимо от молчания мёртвой.
//
// # Одно-фактность и «ронять только проверяемое»
//
// Каждый мир отличается от контрольного РОВНО ОДНИМ фактом, и ожидание —
// ИМЕНОВАННОЕ: не «что-то покраснело», а «покраснела вот эта проба и только
// она». Первая редакция инъекции (а) роняла ДВЕ пробы сразу, потому что
// проверяла существование `services/<каталог>` и отвергала `api-gateway`, чей
// каталог — `gateway/`. Это и есть третий довод против исхода «дать коду
// обещанное поведение»: чтобы проверять существование, читателю имён пришлось
// бы знать РАСКЛАДКУ каталогов, то есть завести второе место о ней. Инъекция
// здесь этой раскладке обучена НАМЕРЕННО — чтобы доказательство было чистым.
//
// # Почему вариант библиотеки кладётся РЯДОМ с настоящей
//
// Читатель выводит корень дерева из места своего файла (`../../..`), поэтому
// копия во временном каталоге ответила бы по чужому корню и инъекция доказывала
// бы не то. Имя файла на вывод корня не влияет — значит вариант обязан лежать в
// том же каталоге. Файл эфемерный и убирается `t.Cleanup`.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// libVariant — вариант читателя рядом с настоящим: одна названная подстановка.
func libVariant(t *testing.T, tag, old, new string) string {
	t.Helper()
	src := filepath.Join("scripts", "lib", "product-names.sh")
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("читателя имён нет (%v) — инъекции неоткуда взять предмет", err)
	}
	body := string(b)
	if n := strings.Count(body, old); n != 1 {
		t.Fatalf("подстановка инъекции встречается %d раз, ждали 1 — мир отличался бы "+
			"не одним фактом:\n%s", n, old)
	}
	// Метка ЛАТИНСКАЯ и без разделителей пути: имя подтеста несёт `/`, а имя
	// файла — координата на диске, не заголовок пробы.
	dst := filepath.Join("scripts", "lib", "product-names.injected-"+tag+".sh")
	if err := os.WriteFile(dst, []byte(strings.Replace(body, old, new, 1)), 0o644); err != nil {
		t.Fatalf("вариант читателя не записан: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(dst) })
	return dst
}

// askVariant — тот же сценарий, что у проб, но к названному варианту читателя.
func askVariant(t *testing.T, lib string, direct bool, args ...string) (string, int) {
	t.Helper()
	script := ". ./" + filepath.ToSlash(lib) + "\n"
	if direct {
		script += `product_image_name "$1"`
	} else {
		script += `product_names_load "$@" || exit $?
for s in "$@"; do printf '%s\t%s\n' "$s" "$(product_image_name "$s")"; done`
	}
	cmd := exec.Command("bash", append([]string{"-c", script, "bash"}, args...)...)
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("оболочку не запустить (%v) — это «не выполнилось»", err)
	}
	return out.String() + errb.String(), code
}

func TestShellReaderProbesCanFail(t *testing.T) {
	const absent = "нет-такого-каталога-в-дереве"

	// ── КОНТРОЛЬ: настоящий читатель. Все три свойства на месте.
	real := filepath.Join("scripts", "lib", "product-names.sh")
	if _, code := askVariant(t, real, true, absent); code != 0 {
		t.Errorf("контроль: отсутствующий каталог дал код %d — предмет инъекции (а) "+
			"уже отсутствует, и она доказывала бы пустоту", code)
	}
	if _, code := askVariant(t, real, false, standServices(t)...); code != 0 {
		t.Errorf("контроль: перенос имён дал код %d — предмет инъекции (б) отсутствует", code)
	}
	if txt, _ := askVariant(t, real, true, ""); strings.Contains(txt, "_PRODUCT_IMAGE_NAME") {
		t.Errorf("контроль: отказ уже несёт внутренность — предмет инъекции (в) отсутствует:\n%s", txt)
	}

	// ── (а) СНЯТО НОВОЕ: читатель начинает проверять существование каталога.
	// Форма обучена раскладке (`api-gateway` → `gateway/`), поэтому перенос имён
	// остаётся исправным и красное приходит ТОЛЬКО от нового свойства.
	t.Run("а_проверка_существования", func(t *testing.T) {
		lib := libVariant(t, "a",
			`  if [ -z "${_PRODUCT_IMAGE_NAME[$dir]+set}" ]; then
    product_names_load "$dir" || return $?
  fi`,
			`  _r="$(_product_names_root)"
  _d="services/$dir"; [ "$dir" = api-gateway ] && _d=gateway
  [ -d "$_r/$_d" ] || { echo "product-names: части $dir в дереве нет" >&2; return 1; }
  if [ -z "${_PRODUCT_IMAGE_NAME[$dir]+set}" ]; then
    product_names_load "$dir" || return $?
  fi`)
		if txt, code := askVariant(t, lib, true, absent); code == 0 {
			t.Errorf("читатель проверяет существование, а проба этого не заметила "+
				"(код %d):\n%s", code, txt)
		}
		// ЗАКОННЫЙ БЛИЗНЕЦ: перенос имён при этом ЦЕЛ — красное не от соседа.
		if txt, code := askVariant(t, lib, false, standServices(t)...); code != 0 {
			t.Errorf("инъекция (а) уронила и перенос имён (код %d) — она роняет больше "+
				"проверяемого, и красное могло прийти от соседа:\n%s", code, txt)
		}
	})

	// ── (б) СНЯТО СТАРОЕ: перенос подменён выводом по приставке.
	t.Run("б_перенос_подменён_приставкой", func(t *testing.T) {
		lib := libVariant(t, "b",
			`    _PRODUCT_IMAGE_NAME["$dir"]="$name"`,
			`    _PRODUCT_IMAGE_NAME["$dir"]="kacho-$dir"`)
		out, code := askVariant(t, lib, false, standServices(t)...)
		if code != 0 {
			t.Fatalf("инъекция (б) сорвала прогон (код %d) вместо подмены имени:\n%s", code, out)
		}
		if strings.Contains(out, "\tkaname") {
			t.Error("подмена не состоялась — переименованная часть всё ещё названа верно, " +
				"и инъекция (б) доказывала бы пустоту")
		}
		// ЗАКОННЫЙ БЛИЗНЕЦ: отсутствующий каталог по-прежнему даёт имя.
		if _, c := askVariant(t, lib, true, absent); c != 0 {
			t.Errorf("инъекция (б) уронила и проверку существования (код %d) — "+
				"мир отличается больше чем одним фактом", c)
		}
	})

	// ── (в) СНЯТО НОВОЕ (вторая ось): отказ снова несёт внутренность.
	t.Run("в_отказ_несёт_внутренность", func(t *testing.T) {
		lib := libVariant(t, "c",
			`    echo "product-names: имя части пусто — печатать нечего." >&2`,
			`    echo "product-names: имя части пусто (_PRODUCT_IMAGE_NAME) — печатать нечего." >&2`)
		txt, code := askVariant(t, lib, true, "")
		if code == 0 {
			t.Fatal("инъекция (в) сняла сам отказ — мир отличается не одним фактом")
		}
		if !strings.Contains(txt, "_PRODUCT_IMAGE_NAME") {
			t.Errorf("внутренность в отказ не попала — доказывать нечего:\n%s", txt)
		}
	})

	t.Logf("перепись: миров 4 (контроль + 3 инъекции), у каждого ожидание именованное")
}
