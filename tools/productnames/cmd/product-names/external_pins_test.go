// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// external_pins_test.go — доказательство ЧИТАТЕЛЯ пинов инъекцией.
//
// Читатель отвечает на вопрос «какой ССЫЛКОЙ объявлена часть, чьи исходники
// вынесены». Отказ у него МОЛЧАЛИВЫЙ по устройству: форму объявления, о которой
// он не знает, он не отвергает — он её НЕ ВИДИТ, и вызывающий получает «пин не
// объявлен» на верно объявленном пине. Поэтому здесь доказывается не то, что
// читатель что-то печатает, а то, что он печатает РАЗНОЕ на входах, различающихся
// РОВНО ОДНИМ фактом.
//
// Настоящее дерево тоже читается — но отдельной пробой и без ожидания величины:
// тег пина правит чужой конвейер, и выписанное здесь число устарело бы молча.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
)

// externalImageName — имя образа вынесенной части, взятое у ЕДИНСТВЕННОГО
// владельца. Выписать `kaname` литералом значило бы завести второе место об одном
// предмете — ровно тот класс, против которого заведён productnaming.
func externalImageName(t *testing.T) string {
	t.Helper()
	external := productnaming.ExternallySourcedServices()
	if len(external) == 0 {
		t.Fatal("ведомость вынесенных частей пуста — у пробы нет предмета, " +
			"и это «не выполнилось», а не «пинов нет»")
	}
	for dir := range external {
		return productnaming.ChartName(dir)
	}
	return ""
}

// runPins — читатель на названных ЦЕПОЧКАХ. Возвращает stdout, stderr и код.
//
// Потоки подаются АРГУМЕНТАМИ, а не подменой `os.Stdout`: подмена сделала бы
// пробу зависимой от того, кто ещё пишет в тот же дескриптор, а под `-race` —
// ещё и гонкой. Тот же довод записан у самого режима.
func runPins(t *testing.T, chains ...string) (string, string, int) {
	t.Helper()
	var out, errOut strings.Builder
	rc := externalPinsMode(&out, &errOut, chains)
	return out.String(), errOut.String(), rc
}

// writeValues — слой значений в каталоге пробы.
func writeValues(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("слой значений не записан: %v", err)
	}
	return p
}

// writeChain — цепочка слоёв в ОДНОМ каталоге, в форме аргумента режима.
func writeChain(t *testing.T, name string, bodies ...string) string {
	t.Helper()
	dir := t.TempDir()
	layers := make([]string, 0, len(bodies))
	for i, body := range bodies {
		p := filepath.Join(dir, fmt.Sprintf("values.%d.yaml", i))
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("слой значений не записан: %v", err)
		}
		layers = append(layers, p)
	}
	return name + "=" + strings.Join(layers, ",")
}

// TestExternalPinsReaderSeesBothDeclarationForms — ФОРМ ОБЪЯВЛЕНИЯ ТРИ, и все три
// законны в этом дереве: карта (подчарты службы доступа и nlb), плоская строка
// (вендоренный geo) и НАКЛАДКА ТОЛЬКО С ТЕГОМ (values.fe3455-prod.yaml). Читатель,
// знающий не все, на остальных не краснеет — он их НЕ ВИДИТ.
func TestExternalPinsReaderSeesBothDeclarationForms(t *testing.T) {
	img := externalImageName(t)

	t.Run("карта", func(t *testing.T) {
		f := writeValues(t, "values.yaml", img+":\n  image:\n    repository: docker.io/prorobotech/"+img+"\n    tag: main-01234567\n")
		out, _, rc := runPins(t, f)
		want := "PIN\t" + img + "\tdocker.io/prorobotech/" + img + ":main-01234567\t"
		if rc != 0 || !strings.Contains(out, want) {
			t.Errorf("код %d, вывод %q — ожидалась строка %q", rc, out, want)
		}
	})

	t.Run("плоская строка", func(t *testing.T) {
		f := writeValues(t, "values.yaml", img+":\n  image: docker.io/prorobotech/"+img+":main-89abcdef\n")
		out, _, rc := runPins(t, f)
		want := "PIN\t" + img + "\tdocker.io/prorobotech/" + img + ":main-89abcdef\t"
		if rc != 0 || !strings.Contains(out, want) {
			t.Errorf("код %d, вывод %q — ожидалась строка %q", rc, out, want)
		}
	})

	t.Run("отпечаток сильнее тега", func(t *testing.T) {
		f := writeValues(t, "values.yaml", img+":\n  image:\n    repository: docker.io/prorobotech/"+img+
			"\n    tag: main-01234567\n    digest: sha256:abc\n")
		out, _, rc := runPins(t, f)
		want := "PIN\t" + img + "\tdocker.io/prorobotech/" + img + "@sha256:abc\t"
		if rc != 0 || !strings.Contains(out, want) {
			t.Errorf("код %d, вывод %q — ожидалась строка %q", rc, out, want)
		}
	})
}

// TestExternalPinsReaderResolvesATagOnlyOverlay — ТРЕТЬЯ ФОРМА, и она была слепым
// местом: накладка объявляет ОДИН ТЕГ, а репозиторий приходит из слоя ниже. Читатель,
// судивший слои по одному, ссылки такой накладки не видел вовсе — то есть для стенда,
// поднятого такой цепочкой, отвечал «пин не объявлен» либо, при ином теге накладки,
// давал находку на верной ссылке.
//
// Два утверждения в одной оси намеренно: что тег накладки ПОБЕДИЛ и что тег базы
// пином этого стека НЕ остался. Без второго «сложил» неотличимо от «взял оба».
func TestExternalPinsReaderResolvesATagOnlyOverlay(t *testing.T) {
	img := externalImageName(t)
	chain := writeChain(t, "стенд",
		img+":\n  image:\n    repository: docker.io/prorobotech/"+img+"\n    tag: main-01234567\n",
		img+":\n  image:\n    tag: main-89abcdef\n")
	out, census, rc := runPins(t, chain)
	want := "PIN\t" + img + "\tdocker.io/prorobotech/" + img + ":main-89abcdef\tстенд"
	if rc != 0 || !strings.Contains(out, want) {
		t.Errorf("код %d, вывод %q — ожидалась строка %q (перепись: %s)", rc, out, want, census)
	}
	if strings.Contains(out, "main-01234567") {
		t.Errorf("тег базового слоя остался пином стека — слои не сложены, а собраны: %q", out)
	}
}

// TestExternalPinsReaderStaysSilentOnAnotherImage — ЗАКОННЫЙ БЛИЗНЕЦ: то же
// объявление, отличающееся РОВНО ИМЕНЕМ образа, пином вынесенной части не
// является. Без этой оси «видит всё» было бы неотличимо от «видит нужное».
func TestExternalPinsReaderStaysSilentOnAnotherImage(t *testing.T) {
	img := externalImageName(t)
	f := writeValues(t, "values.yaml", "neighbour:\n  image:\n    repository: docker.io/prorobotech/"+img+
		"-extras\n    tag: main-01234567\n")
	out, _, rc := runPins(t, f)
	if rc != 0 {
		t.Fatalf("код %d, вывод %q", rc, out)
	}
	if strings.Contains(out, "PIN\t") {
		t.Errorf("посторонний образ принят за пин вынесенной части: %q", out)
	}
	if !strings.Contains(out, "PART\t") {
		t.Errorf("строка о вынесенной части не напечатана — «пина нет» стало бы "+
			"неотличимо от «вынесенных частей нет»: %q", out)
	}
}

// TestExternalPinsReaderRefusesInsteadOfAnsweringEmpty — ПУСТОЙ ВХОД НЕ ЕСТЬ
// ВСЕРАЗРЕШЕНИЕ. Вызывающий сверяет ссылку контейнера с прочитанным перечнем;
// перечень, молча оказавшийся пустым, дал бы «пин не объявлен» на каждом стенде.
func TestExternalPinsReaderRefusesInsteadOfAnsweringEmpty(t *testing.T) {
	t.Run("цепочек не названо", func(t *testing.T) {
		_, errOut, rc := runPins(t)
		if rc != 2 || !strings.Contains(errOut, "не названо ни одной цепочки") {
			t.Errorf("код %d, диагностика %q — ожидался код 2 с причиной", rc, errOut)
		}
	})

	t.Run("файл не читается", func(t *testing.T) {
		_, errOut, rc := runPins(t, filepath.Join(t.TempDir(), "нет-такого.yaml"))
		if rc != 2 || !strings.Contains(errOut, "не читается") {
			t.Errorf("код %d, диагностика %q — ожидался код 2 с причиной", rc, errOut)
		}
	})

	t.Run("файл не разбирается", func(t *testing.T) {
		f := writeValues(t, "values.yaml", "kaname:\n  image:\n   - это не карта\n  \tтабуляция\n")
		_, errOut, rc := runPins(t, f)
		if rc != 2 || !strings.Contains(errOut, "не разбирается") {
			t.Errorf("код %d, диагностика %q — ожидался код 2 с причиной", rc, errOut)
		}
	})
}

// TestExternalPinsReaderReadsTheRealTree — настоящее дерево отвечает НЕПУСТЫМ
// перечнем. Величина тега здесь НЕ ожидается: её правит чужой конвейер, и
// выписанная сюда, она устарела бы молча (ровно тем, чем устарело «17 тегов» в
// прозе профиля). Проверяется ровно то, что у пробы есть производитель: базовый
// профиль умбреллы объявляет ссылку для каждой вынесенной части.
func TestExternalPinsReaderReadsTheRealTree(t *testing.T) {
	root := repoRootForPins(t)
	umbrella := filepath.Join(root, "deploy", "helm", "umbrella")
	chain := "dev-prod=" + strings.Join([]string{
		filepath.Join(umbrella, "values.yaml"),
		filepath.Join(umbrella, "values.dev.yaml"),
		filepath.Join(umbrella, "values.dev-prod.yaml"),
	}, ",")
	out, census, rc := runPins(t, chain)
	if rc != 0 {
		t.Fatalf("код %d на цепочке стенда разработки — перепись: %s", rc, census)
	}
	img := externalImageName(t)
	if !strings.Contains(out, "PIN\t"+img+"\t") {
		t.Errorf("цепочка стенда разработки не объявила ссылку для %q.\n"+
			"Либо объявление переехало, либо читатель перестал его узнавать — "+
			"второе МОЛЧАЛИВО, поэтому ось падает, а не пропускается.\nвывод: %s\nперепись: %s",
			img, out, census)
	}
	if !strings.Contains(census, "объявлений образа") {
		t.Errorf("перепись не назвала объём осмотренного: %q", census)
	}
}

// repoRootForPins — корень дерева от каталога пробы.
func repoRootForPins(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("текущий каталог не разрешается: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("корень дерева не найден — go.mod выше не встретился")
		}
		dir = parent
	}
}
