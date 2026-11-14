import assert from "node:assert/strict";
import { existsSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";

import { loadModel } from "../src/model.js";
import { buildPlan } from "../src/plan.js";
import { emitPackage } from "../src/source.js";
import { verifyPackage } from "../src/verification.js";
import { conformanceSource } from "../src/conformance.js";
import { TOOLING, developmentTarget, runTsc, temporaryDirectory } from "./support.js";

const COMMON = join(
  TOOLING,
  "../../bindings/common-integrations/go/runtimeconditions.binding-model.yaml",
);
const ENV = join(
  TOOLING,
  "../../bindings/env-configuration/go/runtimeconditions.binding-model.yaml",
);
test(
  "the committed first-party models emit packages that compile, including recursive maps",
  { skip: !existsSync(COMMON) || !existsSync(ENV) },
  async () => {
    const workspace = temporaryDirectory();
    try {
      const common = loadModel(COMMON);
      const commonPlan = buildPlan(common, {
        ...developmentTarget(),
        packageKey: "common-integrations",
        sourceDirectory: "bindings/common-integrations/typescript",
        packageName: "@runtimeconditions/common-integrations",
        rootExtension: common["rootExtension"].id,
      });
      const commonDirectory = join(
        workspace,
        "node_modules/@runtimeconditions/common-integrations",
      );
      mkdirSync(commonDirectory, { recursive: true });
      rmSync(commonDirectory, { recursive: true });
      await emitPackage(commonPlan, common, commonDirectory);
      verifyPackage(commonPlan, commonDirectory, common);
      writeFileSync(
        join(commonDirectory, "src/_conformance.ts"),
        conformanceSource(commonPlan, common),
      );
      const compiled = runTsc(commonDirectory, false);
      assert.equal(compiled.status, 0, compiled.output);

      const env = loadModel(ENV);
      const envPlan = buildPlan(env, {
        ...developmentTarget(),
        packageKey: "env-configuration",
        sourceDirectory: "bindings/env-configuration/typescript",
        packageName: "@runtimeconditions/env-configuration",
        rootExtension: env["rootExtension"].id,
        dependencies: [
          {
            extension: common["rootExtension"].id,
            packageName: "@runtimeconditions/common-integrations",
            version: "0.1.0",
          },
        ],
      });
      const envDirectory = join(workspace, "node_modules/@runtimeconditions/env-configuration");
      await emitPackage(envPlan, env, envDirectory);
      verifyPackage(envPlan, envDirectory, env);
      writeFileSync(join(envDirectory, "src/_conformance.ts"), conformanceSource(envPlan, env));
      const envCompiled = runTsc(envDirectory, true);
      assert.equal(envCompiled.status, 0, envCompiled.output);
    } finally {
      rmSync(workspace, { recursive: true, force: true });
    }
  },
);
