# Code Styles

## Source Layout

- Components use atomic folders under `src/components/{atoms,molecules,organisms}`.
- Each component lives in its own folder with the implementation next to a barrel:
  - `ComponentName/ComponentName.tsx`
  - `ComponentName/index.ts`
- Keep level barrels:
  - `src/components/atoms/index.ts`
  - `src/components/molecules/index.ts`
  - `src/components/organisms/index.ts`
  - `src/components/index.ts`
- Pages follow the same pattern:
  - `src/pages/PageName/PageName.tsx`
  - `src/pages/PageName/index.ts`
  - `src/pages/index.ts`
- Shared non-React logic belongs in `src/utils`, exported through `src/utils/index.ts`.

## React Style

- Use arrow functions for React components.
- Type React components with `FC`.
- Prefer named exports from component files.
- Keep `index.ts` files as re-export barrels only.
- Avoid adding UI that is not present in the original `kacho-ui` flow unless explicitly requested.

## Mirroring `kacho-ui`

- Mirror original behavior component-by-component when porting from `D:\Repos\job\kacho\kacho-ui`.
- Header right side should match original `Layout.tsx`: page slot area plus theme toggle. Do not add search, activity, notification, or dev icons to the header.
- Root paths `/` and `/accounts` must clear `kacho.context.v2` account/project state before first render.
- Breadcrumb account/project logic should use real IAM endpoints:
  - `GET /iam/v1/accounts?pageSize=1000`
  - `GET /iam/v1/projects?account_id=...&pageSize=1000`
- No mocked account/project lists in app code.

## Testing

- Use Jest with React Testing Library.
- Tests are colocated next to the component/page implementation:
  - `ComponentName/ComponentName.test.tsx`
- Add tests for every component and page.
- Tests should protect important mirrored behavior, especially:
  - no extra header icons
  - unauthenticated rail surface
  - unselected root breadcrumb state
  - real IAM request paths
- `npm run test` should stay quiet; avoid console noise during successful runs.

## Verification

- Before handoff, run:
  - `npm run test`
  - `npm run lint`
  - `npm run build`
- Do not start the dev server unless explicitly requested.
