// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Объявления для общего разбора подписей (`label-resolver.mjs`).
 *
 * Файл существует ради ОДНОГО потребителя — пробы на TypeScript, которая читает
 * тот же разбор, что и гейты на .mjs. Без объявлений её пришлось бы завести с
 * подавлением проверки типов, а подавление переживает свой предмет молча.
 */

import type ts from "typescript";

export declare const HOLE: string;
export declare const MAX_RESOLVE_DEPTH: number;
export declare const VERBATIM_CONTENT_TAGS: Set<string>;

export declare function collectBindings(sf: ts.SourceFile): Map<string, ts.Expression>;
export declare function isDirectLiteral(node: ts.Node | undefined): boolean;
export declare function literalsOf(
  node: ts.Node | undefined,
  bound: Map<string, ts.Expression>,
  depth?: number,
  seen?: Set<string>,
): string[];
export declare function isVerbatimContent(el: ts.Node): boolean;
export declare function isSoleChild(expr: ts.Node): boolean;

/** Одна прочитанная надпись: где стоит, чем является и как собрана. */
export interface ResolvedLabel {
  line: number;
  /** «свойство» · «атрибут JSX» · «текст JSX» · «аргумент <помощник>». */
  kind: string;
  key: string;
  value: string;
  /** «разметка» — литерал на месте; «вычислено» — собрано резолвером. */
  origin: string;
}

export interface CollectLabelsOptions {
  labelKeys: Set<string>;
  labelAttrsOnly?: Set<string>;
  valueKeys?: Set<string>;
  labelHelpers?: Set<string>;
  jsxText?: boolean;
}

export interface CollectedLabels {
  labels: ResolvedLabel[];
  /** Позиции ПРИМЕРА значения: не надпись, считаются отдельно. */
  valueSites: number;
  /** Граница разбора: позиция есть, текста в ней нет — значение из данных. */
  dataSites: number;
}

export declare function collectLabels(
  rel: string,
  source: string,
  options: CollectLabelsOptions,
): CollectedLabels;
