// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// rendered_key_reader_test.go — КАЖДЫЙ ключ, который чарт кладёт в файл
// настроек, обязан иметь поле в описателе настроек процесса.
//
// # Предмет и почему его не закрывает соседний гейт
//
// Значение идёт от профиля к процессу двумя прыжками: `values.yaml` → шаблон
// чарта → файл настроек → `Config`. Прыжок ПЕРВЫЙ уже стережёт гейт дерева
// `internal/repohygiene/profileknobreader.go`: ключ профиля обязан иметь
// читателя в шаблоне. Его собственная шапка объявляет это условие «необходимым
// и достаточным» — «шаблон подставляет значение, процесс читает получившееся».
// Необходимое оно, а достаточным не является: вторая половина фразы — ДОПУЩЕНИЕ
// о процессе, и ровно оно здесь и не выполнялось.
//
// Наблюдалось (kacho#1796): чарт рендерил `fga.endpoint`, `fga.store-id`,
// `fga.model-id` и `fga.tuple-write.*` — адрес внешнего движка отношений, его
// стор и модель. Движок снят целиком (#747), полей под эти ключи в `FGAConfig`
// нет, и viper молча выбрасывает неизвестное. Оператор задавал адрес, продукт
// его не читал. Первый гейт при этом был зелён и прав: читатель в шаблоне у
// ключа профиля есть.
//
// Это «принято-и-проигнорировано» (`api-conventions.md`) на поверхности
// развёртывания: исходов у него три — реализовать · снять · отвергать явно, —
// и молчаливый приём в их число не входит.
//
// # Предикат
//
// Дерево ключей снимается ИЗ ШАБЛОНА (действия шаблона выбрасываются, helm не
// нужен — см. `nlbRenderedConfigTree`), множество читаемых путей выводится
// РАЗБОРОМ ТИПА `Config` по тегам mapstructure. Оба множества — пути вида
// `fga.require-iam`; находка — путь, отрендеренный чартом и не читаемый ничем.
//
// Читаемым считается и всякий путь ПОД полем-картой: у карты ключи произвольны
// by construction, и требовать от них поля значило бы краснеть на верном коде.
//
// # Направление, в котором гейт НЕ судит, названо прямо
//
// Обратное («у каждого поля есть ключ в чарте») здесь не утверждается: поле с
// умолчанием законно не рендерится, и такое требование краснело бы на верном
// профиле. Предмет — только ключ без читателя.
//
// # Перепись
//
// Печатается: путей отрендерено, читаемых полей выведено, находок. Ноль
// отрендеренных путей — ОТКАЗ, а не чистота: гейт, чей обход сломан, молчит
// так же, как гейт над чистым деревом.

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// renderedKeyPaths — все пути дерева настроек: и узлы, и листья.
func renderedKeyPaths(tree map[string]any, prefix string, out map[string]struct{}) {
	for k, v := range tree {
		p := strings.ToLower(k)
		if prefix != "" {
			p = prefix + "." + p
		}
		out[p] = struct{}{}
		if child, ok := v.(map[string]any); ok {
			renderedKeyPaths(child, p, out)
		}
	}
}

// readableConfigPaths — пути, которые читает описатель настроек. Второе
// множество — префиксы полей-карт: под ними читаемо ЛЮБОЕ поддерево.
func readableConfigPaths(t reflect.Type, prefix string, paths, mapRoots map[string]struct{}) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || t == reflect.TypeOf(time.Time{}) {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" { // неэкспортируемое поле mapstructure не заполняет
			continue
		}
		name := strings.ToLower(f.Name)
		if tag, ok := f.Tag.Lookup("mapstructure"); ok {
			if head := strings.Split(tag, ",")[0]; head != "" {
				if head == "-" {
					continue
				}
				name = strings.ToLower(head)
			}
		}
		p := name
		if prefix != "" {
			p = prefix + "." + name
		}
		paths[p] = struct{}{}

		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Map {
			mapRoots[p] = struct{}{}
			continue
		}
		readableConfigPaths(ft, p, paths, mapRoots)
	}
}

func TestEveryRenderedConfigKeyHasAFieldThatReadsIt(t *testing.T) {
	tree := nlbRenderedConfigTree(t, nlbConfigMapTemplate, "config.yaml: |")

	rendered := map[string]struct{}{}
	renderedKeyPaths(tree, "", rendered)

	readable, mapRoots := map[string]struct{}{}, map[string]struct{}{}
	readableConfigPaths(reflect.TypeOf(Config{}), "", readable, mapRoots)

	var findings []string
	for p := range rendered {
		if _, ok := readable[p]; ok {
			continue
		}
		var underMap bool
		for root := range mapRoots {
			if strings.HasPrefix(p, root+".") {
				underMap = true
				break
			}
		}
		if underMap {
			continue
		}
		findings = append(findings, p)
	}
	sort.Strings(findings)

	t.Logf("осмотрено: путей отрендерено %d, читаемых полей %d (из них полей-карт %d); находок %d",
		len(rendered), len(readable), len(mapRoots), len(findings))

	if len(rendered) == 0 {
		t.Fatal("чарт не отрендерил ни одного ключа настроек — снятие блока сломано. " +
			"Это отказ, а не чистота")
	}
	if len(readable) == 0 {
		t.Fatal("разбор Config не дал ни одного читаемого пути — предикат разошёлся с типом. " +
			"Это отказ, а не чистота")
	}

	if len(findings) > 0 {
		t.Errorf("чарт рендерит %d ключ(ей), которых описатель настроек не читает:\n  %s\n"+
			"viper выбрасывает неизвестный ключ молча: оператор задаёт значение, продукт его не "+
			"применяет. Исходов три — реализовать, снять, отвергать явно",
			len(findings), strings.Join(findings, "\n  "))
	}
	_ = fmt.Sprint()
}
