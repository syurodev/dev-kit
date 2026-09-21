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
  // A method FQN carries its parameter types after the name
  // (Foo.bar(java.lang.String, some.pkg.Baz)), each dotted themselves — split
  // on "." first and the tail of that list is parameter-type segments, not
  // the method's own package or class. Everything after the first "(" is the
  // signature, never part of the name being classified.
  const withoutParams = fqn.split("(")[0];
  // Compare on the simple class name: a package called `.service.` should not
  // make every class inside it a Service, since the annotation is what counts
  // and it lives on the type.
  // Last capitalised segment: for a method FQN (com.a.OrderService.process)
  // that is the class, and for a nested class it is the inner one. A loop
  // rather than findLast, which needs a newer lib target than this project
  // compiles against.
  const parts = withoutParams.split(".");
  let simpleName = "";
  for (let i = parts.length - 1; i >= 0; i--) {
    if (/^[A-Z]/.test(parts[i])) {
      simpleName = parts[i];
      break;
    }
  }
  // OrderServiceImpl implements OrderService — an intended implementation
  // class carries the same suffix as its interface, one "Impl" removed.
  const base = simpleName.replace(/Impl$/, "");

  if (/(RestController|Controller|Resource|Endpoint)$/.test(base)) return "controller";
  if (/(Service|UseCase|Handler|Manager)$/.test(base)) return "service";
  if (/(Repository|Dao|Store|Mapper)$/.test(base)) return "repository";
  return "unknown";
}
