package com.devkit.codegraph;

import com.github.javaparser.ast.Node;
import com.github.javaparser.ast.NodeList;
import com.github.javaparser.ast.expr.BinaryExpr;
import com.github.javaparser.ast.expr.BooleanLiteralExpr;
import com.github.javaparser.ast.expr.ConditionalExpr;
import com.github.javaparser.ast.expr.EnclosedExpr;
import com.github.javaparser.ast.expr.Expression;
import com.github.javaparser.ast.expr.FieldAccessExpr;
import com.github.javaparser.ast.expr.MethodCallExpr;
import com.github.javaparser.ast.expr.NameExpr;
import com.github.javaparser.ast.expr.SwitchExpr;
import com.github.javaparser.ast.expr.UnaryExpr;
import com.github.javaparser.ast.stmt.BlockStmt;
import com.github.javaparser.ast.stmt.BreakStmt;
import com.github.javaparser.ast.stmt.CatchClause;
import com.github.javaparser.ast.stmt.ContinueStmt;
import com.github.javaparser.ast.stmt.IfStmt;
import com.github.javaparser.ast.stmt.ReturnStmt;
import com.github.javaparser.ast.stmt.Statement;
import com.github.javaparser.ast.stmt.SwitchEntry;
import com.github.javaparser.ast.stmt.SwitchStmt;
import com.github.javaparser.ast.stmt.ThrowStmt;

import java.util.ArrayList;
import java.util.Collection;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Optional;
import java.util.Set;

/**
 * The condition under which a call runs, read off the source around it.
 *
 * An edge says "A calls B", which reads as always. {@code if (flag) return
 * isAvailableByModule(x); return isAvailableByRole(x);} gives A two callees
 * and the graph lets neither of them be the one that depends on {@code flag}.
 * The guard is what separates them: the conditions, outermost first, that
 * must hold for control to reach the call — {@code "flag"} for the first,
 * {@code "!flag"} for the second.
 *
 * Read syntactically, as the condition is WRITTEN: no evaluation, no
 * resolution, so a guard is a claim about the source and never about a value.
 * Five shapes make a condition: the branches of an {@code if}, of a ternary
 * and of a {@code switch} entry; the right side of {@code &&} and {@code ||},
 * which only runs when the left side let it; and the guard clause, an
 * {@code if} before the call whose branch leaves the block
 * ({@code if (!enabled) return false;}), which is how most of this code is
 * written. Loops and the {@code try} block itself are not conditions here; a
 * {@code catch} block is — it runs only when its exception was thrown. A loop
 * repeats a call, it does not choose it.
 */
final class Guards {

    /** A guard longer than this is cut: it names a condition, it is not meant to carry a paragraph. */
    static final int MAX_LENGTH = 240;

    private Guards() {}

    /**
     * The guard of {@code node}, an expression or statement inside {@code scope}
     * (the method, constructor or initializer it belongs to), or "" when it runs
     * whenever its scope does.
     */
    static String of(Node node, Node scope) {
        // One list of conditions per ancestor level, innermost first; each
        // level's conditions are already in source order.
        List<List<String>> levels = new ArrayList<>();
        Node child = node;
        Optional<Node> parent = node.getParentNode();
        while (parent.isPresent() && child != scope) {
            Node p = parent.get();
            List<String> level = new ArrayList<>();
            collect(p, child, level);
            if (!level.isEmpty()) {
                levels.add(level);
            }
            child = p;
            parent = p.getParentNode();
        }

        // Outermost first, and no condition twice: `if (a) { if (a) ... }` is one fact.
        Set<String> ordered = new LinkedHashSet<>();
        for (int i = levels.size() - 1; i >= 0; i--) {
            ordered.addAll(levels.get(i));
        }
        return clip(String.join(" && ", ordered));
    }

    /** What reaching {@code child} through {@code parent} tells about the condition. */
    private static void collect(Node parent, Node child, List<String> out) {
        if (parent instanceof IfStmt ifStmt) {
            if (child == ifStmt.getThenStmt()) {
                add(out, text(ifStmt.getCondition()));
            } else if (ifStmt.getElseStmt().filter(e -> e == child).isPresent()) {
                add(out, negate(ifStmt.getCondition()));
            }
        } else if (parent instanceof ConditionalExpr ternary) {
            if (child == ternary.getThenExpr()) {
                add(out, text(ternary.getCondition()));
            } else if (child == ternary.getElseExpr()) {
                add(out, negate(ternary.getCondition()));
            }
        } else if (parent instanceof BinaryExpr binary && child == binary.getRight()) {
            if (binary.getOperator() == BinaryExpr.Operator.AND) {
                add(out, text(binary.getLeft()));
            } else if (binary.getOperator() == BinaryExpr.Operator.OR) {
                add(out, negate(binary.getLeft()));
            }
        } else if (parent instanceof SwitchEntry entry && isStatementOf(entry, child)) {
            add(out, switchCondition(entry));
        } else if (parent instanceof CatchClause clause && child == clause.getBody()) {
            add(out, "catch " + flatten(clause.getParameter().getType().toString()));
        } else if (parent instanceof BlockStmt block) {
            guardClauses(block, child, out);
        }
    }

    /** Identity, not equals: two identical statements in different entries are different places. */
    private static boolean isStatementOf(SwitchEntry entry, Node child) {
        for (Statement s : entry.getStatements()) {
            if (s == child) {
                return true;
            }
        }
        return false;
    }

    /**
     * The conditions an earlier statement of {@code block} leaves behind by
     * ending the block when they fail. {@code if (!enabled) return;} then
     * everything after it runs only if enabled.
     */
    private static void guardClauses(BlockStmt block, Node child, List<String> out) {
        for (Statement earlier : block.getStatements()) {
            if (earlier == child) {
                return;
            }
            if (!(earlier instanceof IfStmt ifStmt)) {
                continue;
            }
            boolean thenLeaves = leaves(ifStmt.getThenStmt());
            boolean elseLeaves = ifStmt.getElseStmt().map(Guards::leaves).orElse(false);
            if (thenLeaves && !elseLeaves) {
                add(out, negate(ifStmt.getCondition()));
            } else if (elseLeaves && !thenLeaves) {
                add(out, text(ifStmt.getCondition()));
            }
        }
    }

    /** True when control never falls out of the end of {@code s}: it returns, throws, breaks or continues. */
    static boolean leaves(Statement s) {
        if (s instanceof ReturnStmt || s instanceof ThrowStmt || s instanceof BreakStmt || s instanceof ContinueStmt) {
            return true;
        }
        if (s instanceof BlockStmt block) {
            NodeList<Statement> statements = block.getStatements();
            return !statements.isEmpty() && leaves(statements.get(statements.size() - 1));
        }
        if (s instanceof IfStmt ifStmt) {
            return ifStmt.getElseStmt().isPresent() && leaves(ifStmt.getThenStmt()) && leaves(ifStmt.getElseStmt().get());
        }
        return false;
    }

    private static String switchCondition(SwitchEntry entry) {
        Node owner = entry.getParentNode().orElse(null);
        Expression selector = owner instanceof SwitchStmt s ? s.getSelector()
                : owner instanceof SwitchExpr e ? e.getSelector() : null;
        String subject = selector == null ? "switch" : flatten(selector.toString());
        if (entry.getLabels().isEmpty()) {
            return subject + " == default";
        }
        List<String> labels = new ArrayList<>();
        entry.getLabels().forEach(l -> labels.add(flatten(l.toString())));
        return labels.size() == 1 ? subject + " == " + labels.get(0) : subject + " in (" + String.join(", ", labels) + ")";
    }

    private static void add(List<String> out, String condition) {
        if (condition != null && !condition.isEmpty()) {
            out.add(condition);
        }
    }

    /** The condition as written, parentheses and line breaks aside; "" for one that is always true. */
    static String text(Expression e) {
        Expression inner = unwrap(e);
        if (inner instanceof BooleanLiteralExpr literal && literal.getValue()) {
            return "";
        }
        return flatten(inner.toString());
    }

    /** The opposite of a condition, written the way a person would have: {@code !(a == null)} is {@code a != null}. */
    static String negate(Expression e) {
        Expression inner = unwrap(e);
        if (inner instanceof BooleanLiteralExpr literal) {
            return literal.getValue() ? "false" : "";
        }
        if (inner instanceof UnaryExpr unary && unary.getOperator() == UnaryExpr.Operator.LOGICAL_COMPLEMENT) {
            return text(unary.getExpression());
        }
        if (inner instanceof BinaryExpr binary) {
            BinaryExpr.Operator flipped = switch (binary.getOperator()) {
                case EQUALS -> BinaryExpr.Operator.NOT_EQUALS;
                case NOT_EQUALS -> BinaryExpr.Operator.EQUALS;
                case LESS -> BinaryExpr.Operator.GREATER_EQUALS;
                case LESS_EQUALS -> BinaryExpr.Operator.GREATER;
                case GREATER -> BinaryExpr.Operator.LESS_EQUALS;
                case GREATER_EQUALS -> BinaryExpr.Operator.LESS;
                default -> null;
            };
            if (flipped != null) {
                return flatten(new BinaryExpr(binary.getLeft().clone(), binary.getRight().clone(), flipped).toString());
            }
            return "!(" + flatten(inner.toString()) + ")";
        }
        boolean simple = inner instanceof NameExpr || inner instanceof FieldAccessExpr || inner instanceof MethodCallExpr;
        String body = flatten(inner.toString());
        return simple ? "!" + body : "!(" + body + ")";
    }

    private static Expression unwrap(Expression e) {
        while (e instanceof EnclosedExpr enclosed) {
            e = enclosed.getInner();
        }
        return e;
    }

    static String flatten(String s) {
        return s.replaceAll("\\s+", " ").trim();
    }

    private static String clip(String s) {
        return s.length() <= MAX_LENGTH ? s : s.substring(0, MAX_LENGTH - 1) + "…";
    }

    /**
     * One guard for an edge that several calls make. If any of them is
     * unconditional the edge is too: the callee may run whatever the flags say.
     * Otherwise it runs under one of the guards — their alternatives, joined by
     * {@code ||}.
     */
    static String merge(Collection<String> guards) {
        Set<String> distinct = new LinkedHashSet<>();
        for (String guard : guards) {
            if (guard.isEmpty()) {
                return "";
            }
            distinct.add(guard);
        }
        if (distinct.size() == 1) {
            return distinct.iterator().next();
        }
        return clip(String.join(" || ", distinct.stream().map(Guards::grouped).toList()));
    }

    private static String grouped(String guard) {
        return guard.contains(" && ") || guard.contains(" || ") ? "(" + guard + ")" : guard;
    }
}
