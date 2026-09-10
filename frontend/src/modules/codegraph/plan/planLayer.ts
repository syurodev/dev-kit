import { type Layer } from "../graph/tokens";

/**
 * Guesses a planned node's layer from the name the agent committed to.
 *
 * The real classifier reads Spring annotations (LayerClassifier.java), which a
 * class that does not exist yet cannot have, so at plan time the intended name
 * is the only signal. A guess about intent, not a claim about code.
 *
 * Falls back to "unknown", which is also what the analyzer returns for a class
 * without a layer annotation — so an unrecognised name looks like the real
 * thing rather than like a failure.
 */
export function inferPlanLayer(fqn: string): Layer {
  // Compare on the simple class name: a package called `.service.` should not
  // make every class inside it a Service, since the annotation is what counts
  // and it lives on the type.
  // Last capitalised segment: for a method FQN (com.a.OrderService.process)
  // that is the class, and for a nested class it is the inner one. A loop
  // rather than findLast, which needs a newer lib target than this project
  // compiles against.
  const parts = fqn.split(".");
  let simpleName = "";
  for (let i = parts.length - 1; i >= 0; i--) {
    if (/^[A-Z]/.test(parts[i])) {
      simpleName = parts[i];
      break;
    }
  }

  if (/(RestController|Controller|Resource|Endpoint)$/.test(simpleName)) return "controller";
  if (/(Service|UseCase|Handler|Manager)$/.test(simpleName)) return "service";
  if (/(Repository|Dao|Store|Mapper)$/.test(simpleName)) return "repository";
  return "unknown";
}
