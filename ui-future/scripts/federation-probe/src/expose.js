// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/*
 * Модуль, который федерация ОТДАЁТ наружу. Он намеренно устроен так же, как страница
 * настоящего remote, но без разметки: берёт общую зависимость из shared-области и
 * тянет за собой стиль. Ровно эти две вещи и ломались, когда федерация переставала
 * работать, — а JSX для проверки контракта не нужен и потребовал бы второго
 * преобразователя в пробе.
 */

import { useState } from "react";

import "./expose.css";

// Маркер доказывает, что чанк ВЫПОЛНИЛСЯ, а не просто зарезолвился именем.
export const marker = "kacho-federation-probe";

// Общая зависимость обязана приехать из shared-области рабочей: имя, которое не
// вызывается, ничего не доказывает — federation отдаёт заглушку так же охотно.
export function sharedResolved() {
  return typeof useState === "function";
}

export default { marker, sharedResolved };
