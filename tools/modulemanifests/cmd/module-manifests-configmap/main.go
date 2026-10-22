// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Команда module-manifests-configmap — ПРОИЗВОДИТЕЛЬ ConfigMap с манифестами
// модулей (задача #1901).
//
// Использование:
//
//	module-manifests-configmap [-root КОРЕНЬ] ПРОФИЛЬ [ПРОФИЛЬ…] > configmap.yaml
//
// ПРОФИЛЬ — файлы значений в том же порядке, в каком их получает helm: имя
// ConfigMap читается ОТТУДА ЖЕ, откуда его берёт чарт, поэтому объявление одно, а
// не два. Объект печатается в стандартный вывод, перепись — в стандартный поток
// ошибок; применяет его тот, кто поднимает стенд.
//
// # Исходов ЧЕТЫРЕ, и каждый отдельный код
//
//	0  объект собран и напечатан;
//	1  находка — профиль или манифест не прочитан, объект не помещается в предел;
//	2  доставка объявлена, а манифеста в дереве нет НИ ОДНОГО — обход беспредметен;
//	3  доставка этой цепочкой профилей НЕ ОБЪЯВЛЕНА — законный исход, не отказ.
//
// # Исключение из единого объекта (kacho#3356, SD-3356-2)
//
// Цепочка вправе назвать каталоги служб, чьи манифесты объект НЕ несёт
// (`kaname.manifests.unifiedExclude`): их модуль на этой посадке поставляет
// другой чарт подобранным по метке источником, и второй манифест того же модуля
// служба отвергла бы на старте. Исключение называется переписью; исключение без
// предмета (каталога нет либо у него нет манифеста), повтор, пустое имя и
// значение не перечнем — код 1, исключение всех — код 2: объект без единого
// манифеста применять нельзя по той же причине, что и при пустом дереве.
//
// Третий и четвёртый разведены намеренно. «Стенд не опирается на манифесты» —
// решение посадки, и вызывающий обязан пройти дальше молча; «манифестов нет» —
// беспредметный обход, и пройти дальше значило бы применить пустой ConfigMap, от
// которого служба откажется стартовать, назвав сорванную доставку. Схлопни их в
// один код — и подъём стенда либо ломается там, где всё верно, либо продолжается
// там, где сломано.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	manifestproducer "github.com/PRO-Robotech/corelib/modulemanifest/producer"
	"gopkg.in/yaml.v3"
)

// unifiedExcludePath — объявление исключения в профиле умбреллы: тот же узел
// `kaname.manifests`, что и имя объекта, и читает его ещё рендер чарта службы
// доступа (`_helpers.tpl`, `kaname.manifests.selected`).
var unifiedExcludePath = []string{"kaname", "manifests", "unifiedExclude"}

// declaredUnifiedExclude — перечень исключённых каталогов, объявленный цепочкой.
//
// Побеждает последнее ОБЪЯВЛЕНИЕ, ровно как у helm (перечень заменяется, а не
// сливается); пустое объявление — решение «ничего не исключать», а не молчание.
func declaredUnifiedExclude(profiles []string) ([]string, error) {
	var out []string
	for _, p := range profiles {
		// #nosec G304 -- путь приходит от вызывающего (цепочка стенда из
		// stacks.txt); его же уже прочитал производитель.
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("профиль %s не прочитан: %w", p, err)
		}
		var tree map[string]any
		if err := yaml.Unmarshal(raw, &tree); err != nil {
			return nil, fmt.Errorf("профиль %s не разобран: %w", p, err)
		}
		cur := any(tree)
		declared := true
		for _, seg := range unifiedExcludePath {
			m, ok := cur.(map[string]any)
			if !ok {
				declared = false
				break
			}
			if cur, ok = m[seg]; !ok {
				declared = false
				break
			}
		}
		if !declared {
			continue
		}
		if cur == nil {
			out = nil
			continue
		}
		list, ok := cur.([]any)
		if !ok {
			return nil, fmt.Errorf("профиль %s: %s — не перечень каталогов (%T)",
				p, strings.Join(unifiedExcludePath, "."), cur)
		}
		out = out[:0:0]
		for _, v := range list {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("профиль %s: %s несёт не строку (%v)",
					p, strings.Join(unifiedExcludePath, "."), v)
			}
			out = append(out, s)
		}
	}
	return out, nil
}

// excludeSources — источники без исключённых каталогов; каждое исключение
// обязано снять ровно один найденный манифест.
func excludeSources(sources []manifestproducer.Source, exclude []string) ([]manifestproducer.Source, error) {
	drop := make(map[string]bool, len(exclude))
	for _, dir := range exclude {
		switch {
		case strings.TrimSpace(dir) == "":
			return nil, errors.New("unifiedExclude несёт пустое имя каталога")
		case drop[dir]:
			return nil, fmt.Errorf("unifiedExclude: каталог %q назван повторно", dir)
		}
		drop[dir] = true
	}
	kept := make([]manifestproducer.Source, 0, len(sources))
	for _, s := range sources {
		if drop[s.Dir] {
			delete(drop, s.Dir)
			continue
		}
		kept = append(kept, s)
	}
	for _, dir := range exclude {
		if drop[dir] {
			return nil, fmt.Errorf("unifiedExclude исключает %q, а манифеста services/%s/manifest.yaml в дереве нет — "+
				"исключение без предмета: опечатка либо снятый модуль, и молча пропустить его значило бы "+
				"не заметить, что исключение ничего не делает", dir, dir)
		}
	}
	return kept, nil
}

func main() {
	root := flag.String("root", ".", "корень дерева: под ним ищется services/*/manifest.yaml")
	flag.Parse()

	profiles := flag.Args()
	if len(profiles) == 0 {
		fmt.Fprintln(os.Stderr,
			"ОТКАЗ: цепочка профилей не названа — имя ConfigMap читать неоткуда.\n"+
				"       Умолчания здесь нет намеренно: подставленная цепочка означала бы\n"+
				"       решение о стенде, принятое за оператора.\n"+
				"       Пример: module-manifests-configmap -root .. helm/umbrella/values.dev.yaml")
		os.Exit(1)
	}

	delivery, err := manifestproducer.Collect(*root, profiles)
	// Перепись печатается ВСЕГДА, до ветвления по исходу: без неё «имя не
	// объявлено» неотличимо от «профили не прочитаны».
	fmt.Fprintf(os.Stderr, "module-manifests-configmap: %s\n", delivery.Census.Summary())

	switch {
	case errors.Is(err, manifestproducer.ErrNotDeclared):
		fmt.Fprintf(os.Stderr,
			"доставка манифестов этой цепочкой не объявлена (kaname.manifests.configMapName "+
				"пусто) — ConfigMap не заводится, и это решение посадки, а не отказ\n")
		os.Exit(3)
	case errors.Is(err, manifestproducer.ErrNoManifests):
		fmt.Fprintf(os.Stderr,
			"ОТКАЗ: доставка объявлена, а манифеста в дереве нет ни одного — применить "+
				"пустой ConfigMap нельзя: пустой каталог доставки служба читает как сорванную "+
				"доставку и отказывается стартовать\n")
		os.Exit(2)
	case err != nil:
		fmt.Fprintf(os.Stderr, "ОТКАЗ: %v\n", err)
		os.Exit(1)
	}

	exclude, err := declaredUnifiedExclude(profiles)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ОТКАЗ: %v\n", err)
		os.Exit(1)
	}
	if len(exclude) > 0 {
		kept, err := excludeSources(delivery.Sources, exclude)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ОТКАЗ: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "module-manifests-configmap: исключено из единого: %s\n", strings.Join(exclude, ", "))
		if len(kept) == 0 {
			fmt.Fprintf(os.Stderr,
				"ОТКАЗ: unifiedExclude исключил все манифесты — не осталось ни одного: пустой "+
					"ConfigMap служба читает как сорванную доставку и отказывается стартовать\n")
			os.Exit(2)
		}
		delivery.Sources = kept
	}

	out, err := manifestproducer.Render(delivery)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ОТКАЗ: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(out); err != nil {
		fmt.Fprintf(os.Stderr, "ОТКАЗ: вывод не записан: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "module-manifests-configmap: ConfigMap %q, ключей %d\n",
		delivery.Name, len(delivery.Sources))
}
