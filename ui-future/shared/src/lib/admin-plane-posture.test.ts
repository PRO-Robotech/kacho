// Посадка admin-плоскости читается из ОТВЕТА края на пробный запрос, а не
// объявляется профилем (#2692). Предмет пробы — классификация: 404 на чтении
// КОЛЛЕКЦИИ admin-плоскости означает ровно одно — на этом слушателе такого
// маршрута нет (край отвечает на скрытую поверхность как на несуществующую,
// by construction); настоящий отказ в правах и всё остальное так читаться не
// вправе, иначе «нет прав» превратилось бы в «раздела нет».

import { ApiError } from "@shared/api/client";
import { ADMIN_PLANE_PROBE_PATH, classifyAdminPlaneProbe } from "./admin-plane-posture";

const deny403 = new ApiError(403, 7, [{ "@type": "type.googleapis.com/google.rpc.ErrorInfo", reason: "AUTHZ_DENIED" }], "permission denied");

describe("classifyAdminPlaneProbe", () => {
  it("проба идёт на чтение коллекции admin-плоскости, у которой нет публичного близнеца", () => {
    // `/vpc/v1/addressPools` обслуживает только InternalAddressPoolService —
    // публичного GET по этому пути нет, поэтому 404 на нём не может означать
    // «ресурса нет».
    expect(ADMIN_PLANE_PROBE_PATH).toBe("/vpc/v1/addressPools");
  });

  it("успешный ответ — плоскость обслуживается здесь", () => {
    expect(classifyAdminPlaneProbe(null)).toBe("present");
  });

  it("404 на чтении коллекции — плоскости на этой посадке нет", () => {
    expect(classifyAdminPlaneProbe(new ApiError(404, 5, [], "Not Found"))).toBe("absent");
  });

  it("настоящий отказ в правах остаётся отказом в правах, а не отсутствием раздела", () => {
    expect(classifyAdminPlaneProbe(deny403)).toBe("denied");
  });

  it("сбой, о котором нечего сказать, не выдаётся ни за отсутствие, ни за наличие", () => {
    expect(classifyAdminPlaneProbe(new ApiError(503, 14, [], "unavailable"))).toBe("unknown");
    expect(classifyAdminPlaneProbe(new TypeError("Failed to fetch"))).toBe("unknown");
  });
});
