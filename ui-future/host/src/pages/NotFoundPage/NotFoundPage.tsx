import type { FC } from "react";
import { Link } from "react-router";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { PageFrame } from "@shared/components/organisms/PageFrame";

/**
 * Неизвестный адрес консоли (#3199).
 *
 * Прежде замыкающее правило маршрута переводило любой незнакомый адрес на
 * панель: опечатка выглядела успехом, а адрес в строке браузера подменялся, и
 * человек не узнавал, что страницы нет. Теперь адрес остаётся тем, что набрано,
 * а экран говорит, что по нему ничего нет, и называет путь дальше.
 *
 * Набранный адрес на экран не выводится: он пришёл извне (ссылкой), и экран о нём
 * ничего не утверждает, кроме «здесь страницы нет». Путь дальше — ссылка на
 * панель сервисов, переходом внутри приложения.
 */
export const NotFoundPage: FC = () => (
  <PageFrame>
    <section className="workbench" data-testid="not-found-page">
      <ErrorResult
        status="404"
        title={<h1 style={{ fontSize: "inherit", margin: 0 }}>Страница не найдена</h1>}
        subTitle="По этому адресу в консоли ничего нет. Проверьте адрес либо перейдите к сервисам облака."
        extra={<Link to="/dashboard">Перейти к сервисам облака</Link>}
      />
    </section>
  </PageFrame>
);
