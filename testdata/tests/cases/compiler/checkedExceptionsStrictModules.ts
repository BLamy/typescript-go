// @checkedExceptions: strict
// @strict: true
// @module: esnext

// @filename: dependency.ts
export const value = 1;

// @filename: main.ts
// Static module evaluation is an effect boundary. Until modules can publish an
// initialization effect, strict mode must not silently call an import safe.
import { value } from "./dependency";
value;
