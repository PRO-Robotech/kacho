import type { FC } from "react";
import { useNavigate, useParams } from "react-router";
import { ModuleUnavailablePanel } from "@shared/components/organisms/ModuleErrorBoundary";
// Заголовок раздела — из того же зеркала канона, что и крошка хоста: своя
// карта здесь была третьей подписью одного раздела в одном продукте.
import { SERVICES } from "../../lib/entity-names";

/**
 * Раздел, до страницы которого пользователь дошёл, а показать нечего.
 *
 * Вид — ТОТ ЖЕ экран, что у отказавшего модуля (`ModuleUnavailablePanel`), а не
 * свой. Для того, кто это читает, оба состояния — одно и то же: раздел сейчас
 * не открывается. Разница («страница не выставлена наружу» против «модуль не
 * приехал по сети») интересна разработчику, и её место в журнале браузера, а не
 * на экране: два разных вида одного предмета читаются как два разных предмета.
 *
 * Кнопки «Повторить» здесь НЕТ намеренно: повторять нечего — страницы раздела
 * не существует, и повтор вернул бы ровно этот же экран. Кнопка, которая
 * заведомо ничего не меняет, хуже её отсутствия.
 */
export const ModulePlaceholderPage: FC = () => {
  const navigate = useNavigate();
  const params = useParams();
  const moduleKey = params.moduleKey ?? params.iamSection ?? params.systemSection ?? "";
  // Имя раздела — то, что пользователь видит в меню. Ключ модуля («vpc»,
  // «registry») ему не адресован: раздел опознают по имени, а не по сегменту
  // адреса. Канон ключа не знает — панель обойдётся без имени вовсе.
  const label = SERVICES[moduleKey]?.menuTitle ?? "";

  return (
    <section className="workbench" data-testid="module-placeholder-page">
      {/* Уход к списку сервисов — переходом внутри приложения: здесь, в отличие
          от корневой границы каркаса, маршрутизатор есть, и перезагружать всю
          консоль ради смены страницы незачем. */}
      <ModuleUnavailablePanel moduleLabel={label} onGoHome={() => void navigate("/dashboard")} />
    </section>
  );
};
