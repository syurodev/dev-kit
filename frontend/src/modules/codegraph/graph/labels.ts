/**
 * The analyzer emits fully-qualified labels — `resolved.getQualifiedName()`
 * for methods, the qualified type name for classes — so a real node's label
 * looks like `com.synx.devkit.identity.adapter.web.DeviceEnrollmentController.enroll`.
 *
 * A card is a fixed 268px wide, so rendering that raw truncates from the right
 * and leaves only the package prefix on screen: every node in a project reads
 * as `com.synx.devkit.identity.ada…`, which is the half that identifies
 * nothing. Show the tail instead — the class and method are what tell two
 * nodes apart — and keep the full name for the tooltip.
 */
export function shortLabel(label: string, kind: string): string {
  const segments = label.split(".");
  // A method's identity is Class.method; a class's is just its own name.
  // Nested classes still read correctly: com.a.Outer.Inner.run -> Inner.run.
  const keep = kind === "method" ? 2 : 1;
  if (segments.length <= keep) {
    return label;
  }
  return segments.slice(-keep).join(".");
}
