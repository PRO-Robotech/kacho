// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// productimagename_injection_test.go — доказательство, что соседний гейт
// СПОСОБЕН упасть, называет координату и МОЛЧИТ на законном близнеце.
//
// Инъекция кормит ТУ ЖЕ функцию `scanImageDerivations`, которую на настоящем
// дереве зовёт гейт, — поэтому доказанное здесь верно там. Копия логики
// осталась бы зелёной ровно тогда, когда гейт перестал бы работать.
//
// Одно-фактность: мир каждого отрицательного случая отличается от
// положительного близнеца РОВНО ОДНИМ фактом — формой одного слова. Дельта
// вычисляется, а не объявляется: близнецы собраны из одной заготовки.
//
// Осей у близнецов четыре, и все четыре — законные формы, встречающиеся в
// дереве. Односторонняя проверка (только дефект) зеленела бы на
// распознавателе, объявляющем находкой всё подряд.

import (
	"strings"
	"testing"
)

// recipe — заготовка рецепта. Подставляется ОДНО слово: аргумент `-t`.
func recipe(imageWord string) string {
	return "" +
		"build-services:\n" +
		"\t@for svc in $(IMAGES); do \\\n" +
		"\t  ( cd .. && docker build -f $$d/Dockerfile -t " + imageWord + " \"$$bctx\" ) || exit 1; \\\n" +
		"\tdone\n"
}

func TestImageDerivationGateCanFail(t *testing.T) {
	// ── ДЕФЕКТ: имя выведено приставкой из переменной ────────────────────────
	for _, defect := range []string{
		"kacho-$$svc:dev",   // форма make
		"kacho-$(SVC):dev",  // форма make, скобочная переменная
		"kacho-${svc}:dev",  // форма оболочки, фигурные скобки
		"kacho-$svc:latest", // иной тег — предмет не в слове «dev»
		"kacho-$1:dev",      // позиционный параметр функции оболочки
	} {
		found, words := scanImageDerivations("deploy/Makefile", recipe(defect))
		if len(found) != 1 {
			t.Errorf("дефект %q: находок %d, ожидалась одна — гейт слеп к этой форме записи",
				defect, len(found))
			continue
		}
		if found[0].Line != 3 {
			t.Errorf("дефект %q: координата строки %d, ожидалась 3 — читатель пойдёт не туда",
				defect, found[0].Line)
		}
		if !strings.Contains(found[0].Text, defect) {
			t.Errorf("дефект %q: находка не называет само слово (%q) — координата без предмета",
				defect, found[0].Text)
		}
		if !strings.Contains(found[0].File, "Makefile") {
			t.Errorf("дефект %q: находка не называет файл (%q)", defect, found[0].File)
		}
		if words == 0 {
			t.Errorf("дефект %q: слов с приставкой сосчитано ноль — перепись не работает", defect)
		}
	}

	// ── ЗАКОННЫЕ БЛИЗНЕЦЫ: та же форма, предмет другой — гейт МОЛЧИТ ─────────
	for _, twin := range []struct{ word, why string }{
		{"$$img:dev", "имя спрошено у объявленного источника — ради этого всё и заведено"},
		{"kacho-vpc:dev", "литерал: это ВЕРНОЕ имя своей части, а не вывод из переменной"},
		{"kacho-ui-future-$$p:dev", "семейство модулей консоли: приставка склеена со своим сегментом, не с переменной"},
		{"kaname:dev", "собственное имя продукта литералом"},
	} {
		found, _ := scanImageDerivations("deploy/Makefile", recipe(twin.word))
		if len(found) != 0 {
			t.Errorf("законный близнец %q (%s) объявлен находкой %v — гейт краснеет на исправном",
				twin.word, twin.why, found)
		}
	}

	// ── ЗАКОННЫЕ БЛИЗНЕЦЫ ВНЕ ЗАГОТОВКИ ─────────────────────────────────────
	// Селектор метки: приставка склеена с переменной, но ТЕГА нет — это не
	// ссылка на образ. В дереве такая форма осознанна (перебор форм меток,
	// задача #1007), и гейт обязан её пропускать by construction, а не по
	// списку прощённых.
	if found, _ := scanImageDerivations("deploy/scripts/x.sh",
		"printf '%s\\n' \"app.kubernetes.io/name=kacho-$1\" \"app=kacho-$1\"\n"); len(found) != 0 {
		t.Errorf("селектор метки объявлен находкой %v — гейт судит не свой предмет", found)
	}

	// БЕЗ ТЕГА, НО В ПОЗИЦИИ ССЫЛКИ НА ОБРАЗ — находка (замечание З4).
	// `docker build -t kacho-$svc` соберёт `:latest`, `kind load` загрузит.
	for _, cmd := range []string{
		"\t  ( cd .. && docker build -f x/Dockerfile -t kacho-$$svc \"$$bctx\" ) || exit 1\n",
		"\t  kind load docker-image kacho-$$svc --name x\n",
	} {
		found, _ := scanImageDerivations("deploy/Makefile", cmd)
		if len(found) != 1 {
			t.Errorf("бестеговая ссылка в позиции образа (%q): находок %d, ожидалась одна — "+
				"форма уходит из-под наблюдения молча", strings.TrimSpace(cmd), len(found))
		}
	}

	// Законный близнец той же бестеговой формы: НЕ в позиции ссылки на образ.
	if found, _ := scanImageDerivations("deploy/scripts/x.sh",
		"echo \"каталог kacho-$svc собран\"\n"); len(found) != 0 {
		t.Errorf("бестеговое имя вне команды работы с образом объявлено находкой %v — "+
			"гейт судит не свой предмет", found)
	}

	// Комментарий, ОБЪЯСНЯЮЩИЙ запрет, находкой не является: иначе гейт
	// краснел бы на собственной шапке.
	if found, _ := scanImageDerivations("deploy/Makefile",
		"# прежде здесь собирался kacho-$$svc:dev — так часть со своим именем не называется\n"); len(found) != 0 {
		t.Errorf("комментарий объявлен находкой %v — гейт читает текст, а не исполняемую часть", found)
	}

	// ── ПЕРЕПИСЬ ОТЛИЧАЕТ «НОЛЬ НАХОДОК» ОТ «НОЛЬ ПРОЧИТАННОГО» ─────────────
	if _, words := scanImageDerivations("deploy/Makefile", "echo нет предмета\n"); words != 0 {
		t.Errorf("на тексте без приставки сосчитано %d слов — перепись завышает объём", words)
	}
	if _, words := scanImageDerivations("deploy/Makefile", recipe("kacho-vpc:dev")); words == 0 {
		t.Error("на законном литерале перепись дала ноль слов — тогда «ноль находок» " +
			"неотличимо от «ноль прочитанного»")
	}
}
