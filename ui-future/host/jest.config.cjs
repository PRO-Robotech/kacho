module.exports = {
  preset: "ts-jest",
  testEnvironment: "jsdom",
  extensionsToTreatAsEsm: [".ts", ".tsx"],
  setupFilesAfterEnv: ["<rootDir>/src/test/setup.ts"],
  testMatch: ["<rootDir>/src/**/*.test.{ts,tsx}"],
  moduleNameMapper: {
    "^(\\.{1,2}/.*)\\.js$": "$1",
    "^dashboard/DashboardPage$": "<rootDir>/src/test/dashboard-remote.tsx",
    "^dashboard/navigation$": "<rootDir>/src/test/dashboard-navigation.ts",
    "^vpc/VpcPage$": "<rootDir>/src/test/vpc-remote.tsx",
    "^vpc/navigation$": "<rootDir>/src/test/vpc-navigation.ts",
    "^nlb/NlbPage$": "<rootDir>/src/test/nlb-remote.tsx",
    "^nlb/navigation$": "<rootDir>/src/test/nlb-navigation.ts",
    "^iam/IamPage$": "<rootDir>/src/test/iam-remote.tsx",
    "^iam/navigation$": "<rootDir>/src/test/iam-navigation.ts",
  },
  transform: {
    "^.+\\.(ts|tsx)$": [
      "ts-jest",
      {
        tsconfig: "tsconfig.app.json",
        useESM: true,
      },
    ],
  },
};
