// Дублёр навигации администрирования для проб каркаса.
//
// Дублёр НЕ снисходительнее настоящего: он отдаёт ту же форму (раздел с
// сегментом и перечнем пунктов), что и `system/src/navigation.ts`, — иначе
// проба «раздел вне области проекта получает колонку» зеленела бы на пустом
// перечне, то есть утверждала бы о продукте меньше, чем обещает её заголовок.
//
// Пункты сокращены до двух: проба спрашивает про наличие колонки и подсветку
// открытого раздела, а не про полноту перечня — её держит сам модуль.
import type { RemoteNavSection } from "dashboard/navigation";

export const SYSTEM_NAVIGATION: RemoteNavSection[] = [
  {
    key: "system",
    segment: "system",
    icon: "globe",
    label: "Администрирование",
    landingPath: "/system/regions",
    items: [
      { key: "system-regions", icon: "globe", label: "Регионы", path: "/system/regions" },
      { key: "system-zones", icon: "route", label: "Зоны", path: "/system/zones" },
    ],
  },
];

export default SYSTEM_NAVIGATION;
