import type { FC } from "react";
import { PageHead } from "@shared/components/organisms/DetailShell/PageHead";

export const HomePage: FC = () => {
  return (
    <section className="workbench">
      <div className="panel-heading">
        {/* Шапка ОБЩАЯ с прочими страницами консоли: у стартовой не может быть
            своего кегля — с неё начинают, и по ней судят об остальном.
            
            Подзаголовок снят: «оболочка хоста для федеративных модулей» — фраза
            о нашем устройстве, а не о том, что человек здесь получит. */}
        <PageHead title="Сервисы облака" />
      </div>
    </section>
  );
};
