//// [tests/cases/compiler/checkedExceptionsSoundModules.ts] ////

//// [dependency.ts]
export const value = 1;

//// [main.ts]
// Static module evaluation is an effect boundary. Until modules can publish an
// initialization effect, checked exceptions must not silently call an import safe.
import { value } from "./dependency";
value;


//// [dependency.js]
export const value = 1;
//// [main.js]
// Static module evaluation is an effect boundary. Until modules can publish an
// initialization effect, checked exceptions must not silently call an import safe.
import { value } from "./dependency";
value;
