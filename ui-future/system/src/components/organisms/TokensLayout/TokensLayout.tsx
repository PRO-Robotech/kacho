// TokensLayout — оболочка части «Токены и ключи» раздела администрирования
// (`/system/tokens/{service-account-keys,user-tokens}`).
//
// Вид берётся ОБЩИЙ — `DetailShell`, тот же, что на карточке ресурса и у
// соседней части того же раздела (`AdminLayout`): вертикальный рейл слева,
// содержимое справа. Прежде здесь стоял свой горизонтальный ряд вкладок и свой
// заголовок с абзацем — раздел выглядел двумя разными местами продукта
// (правило 8 `ui.md`, решение владельца #447).
//
// Поясняющий абзац снят вместе с рядом: он пересказывал название раздела
// («Выпуск и отзыв креденшалов» под заголовком «Токены и ключи»), а его
// единственный факт — секрет показывается один раз — сказан там, где он нужен:
// в окне выпуска (`OneTimeSecretModal`).

import { useMemo } from "react";
import { Outlet, useLocation, useNavigate } from "react-router";
import { DetailShell, type DetailTab } from "@shared/components/organisms/DetailShell";

interface TokensSection {
  /** Идентификатор пункта и он же — адрес его страницы. */
  path: string;
  label: string;
}

const SECTIONS: TokensSection[] = [
  { path: "/system/tokens/service-account-keys", label: "Ключи сервисных аккаунтов" },
  { path: "/system/tokens/user-tokens", label: "Токены пользователей" },
];

export function TokensLayout() {
  const location = useLocation();
  const navigate = useNavigate();

  const active = SECTIONS.find((s) => location.pathname.startsWith(s.path))?.path ?? SECTIONS[0].path;

  // Содержимое пункта рисует маршрут, а не сам пункт: страницы части остаются
  // самостоятельными адресами (ссылку на пункт можно дать и открыть).
  const tabs: DetailTab[] = useMemo(
    () => SECTIONS.map((s) => ({ id: s.path, label: s.label, render: () => <Outlet />, fill: true })),
    [],
  );

  return (
    <DetailShell
      resourceLabel="Токены и ключи"
      resourceName="Токены и ключи"
      tabs={tabs}
      activeTabId={active}
      onTabSelect={(id) => void navigate(id)}
    />
  );
}

export default TokensLayout;
