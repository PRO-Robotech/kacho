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

	manifestproducer "github.com/PRO-Robotech/kacho/pkg/modulemanifest/producer"
)

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
