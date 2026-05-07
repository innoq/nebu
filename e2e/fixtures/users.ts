/**
 * Nebu test users — pre-configured in Dex.
 *
 * Story 9-26 — Phase 1, AC2.
 * Story 9-26a — m-1 fix: extract DEX_TEST_PASSWORD constant.
 * RED PHASE: this file compiles but playwright-bdd is not yet installed,
 * so `npx bddgen` will fail when nebu-fixtures.ts imports createBdd.
 */

export type NebUser = {
  name: string;
  email: string;
  matrixId: string;
};

/** Shared Dex test password for all pre-configured test users. */
export const DEX_TEST_PASSWORD = 'changeme';

export const NEBU_USERS = {
  alex:  { name: 'alex',  email: 'alex@example.com',  matrixId: '@alex:localhost'  },
  marie: { name: 'marie', email: 'marie@example.com', matrixId: '@marie:localhost' },
  /** Reserved — not used in current stories but declared for future multi-user scenarios. */
  tom:   { name: 'tom',   email: 'tom@example.com',   matrixId: '@tom:localhost'   },
  kai:   { name: 'kai',   email: 'kai@example.com',   matrixId: '@kai:localhost'   },
} as const satisfies Record<string, NebUser>;

export type NebuUserName = keyof typeof NEBU_USERS;
