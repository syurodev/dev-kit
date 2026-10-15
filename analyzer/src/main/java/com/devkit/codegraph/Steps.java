package com.devkit.codegraph;

import com.github.javaparser.ast.Node;
import com.github.javaparser.ast.expr.BinaryExpr;
import com.github.javaparser.ast.expr.ConditionalExpr;
import com.github.javaparser.ast.expr.Expression;
import com.github.javaparser.ast.expr.LambdaExpr;
import com.github.javaparser.ast.expr.MethodCallExpr;
import com.github.javaparser.ast.stmt.CatchClause;
import com.github.javaparser.ast.stmt.DoStmt;
import com.github.javaparser.ast.stmt.ForEachStmt;
import com.github.javaparser.ast.stmt.ForStmt;
import com.github.javaparser.ast.stmt.IfStmt;
import com.github.javaparser.ast.stmt.Statement;
import com.github.javaparser.ast.stmt.SwitchEntry;
import com.github.javaparser.ast.stmt.TryStmt;
import com.github.javaparser.ast.stmt.WhileStmt;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Optional;

/**
 * What the flow outline needs to know about one call, read off the source
 * around it: where it is, how it is written, and the control structures it
 * sits in. Syntactic like {@link Guards} — nothing here is evaluated.
 *
 * Frames differ from a guard in one deliberate way: a guard clause
 * ({@code if (skip) { …; return; }}) is a frame for the calls INSIDE it and
 * nothing for the calls after it. A guard has to say that everything after
 * runs only when {@code !skip}; an outline says it better by showing the early
 * exit and leaving the rest flat.
 */
final class Steps {

    /** A call's text is a label, not a listing. */
    static final int MAX_TEXT = 120;

    private Steps() {}

    static int col(Node node) {
        return node.getBegin().map(p -> p.column).orElse(0);
    }

    /**
     * The call as written, on one line. A lambda argument is shortened to its
     * head — its body is the outline's next rows, not part of this one.
     */
    static String text(Node node) {
        if (node instanceof MethodCallExpr m && m.getArguments().stream().anyMatch(a -> a instanceof LambdaExpr)) {
            StringBuilder b = new StringBuilder();
            m.getScope().ifPresent(s -> b.append(Guards.flatten(s.toString())).append('.'));
            b.append(m.getNameAsString()).append('(');
            List<String> args = new ArrayList<>();
            for (Expression a : m.getArguments()) {
                args.add(a instanceof LambdaExpr l ? lambdaHead(l) + " -> …" : Guards.flatten(a.toString()));
            }
            b.append(String.join(", ", args)).append(')');
            return clip(b.toString());
        }
        return clip(Guards.flatten(node.toString()));
    }

    static String clip(String s) {
        return s.length() <= MAX_TEXT ? s : s.substring(0, MAX_TEXT - 1) + "…";
    }

    /** The control structures around {@code node} inside {@code scope}, outermost first. */
    static List<StepFrame> frames(Node node, Node scope) {
        List<StepFrame> inner = new ArrayList<>();
        Node child = node;
        Optional<Node> parent = node.getParentNode();
        while (parent.isPresent() && child != scope) {
            Node p = parent.get();
            StepFrame frame = frameOf(p, child);
            if (frame != null) {
                inner.add(frame);
            }
            child = p;
            parent = p.getParentNode();
        }
        Collections.reverse(inner);
        return inner;
    }

    private static StepFrame frameOf(Node parent, Node child) {
        if (parent instanceof IfStmt s) {
            if (child == s.getThenStmt()) {
                boolean elseIf = s.getParentNode()
                        .filter(g -> g instanceof IfStmt outer && outer.getElseStmt().filter(e -> e == s).isPresent())
                        .isPresent();
                return new StepFrame(at(s.getThenStmt()), "if",
                        (elseIf ? "else if " : "if ") + Guards.text(s.getCondition()), Guards.leaves(s.getThenStmt()));
            }
            Optional<Statement> otherwise = s.getElseStmt();
            // `else if` is drawn by the nested if's own frame, so the else adds nothing.
            if (otherwise.isPresent() && otherwise.get() == child && !(child instanceof IfStmt)) {
                return new StepFrame(at(child), "else", "else", Guards.leaves(otherwise.get()));
            }
            return null;
        }
        if (parent instanceof ConditionalExpr t) {
            if (child == t.getThenExpr()) {
                return new StepFrame(at(child), "ternary", Guards.text(t.getCondition()), false);
            }
            if (child == t.getElseExpr()) {
                return new StepFrame(at(child), "ternary", Guards.negate(t.getCondition()), false);
            }
            return null;
        }
        if (parent instanceof BinaryExpr b && child == b.getRight()) {
            if (b.getOperator() == BinaryExpr.Operator.AND) {
                return new StepFrame(at(child), "and", Guards.text(b.getLeft()), false);
            }
            if (b.getOperator() == BinaryExpr.Operator.OR) {
                return new StepFrame(at(child), "or", Guards.negate(b.getLeft()), false);
            }
            return null;
        }
        if (parent instanceof SwitchEntry entry && isStatementOf(entry, child)) {
            List<String> labels = new ArrayList<>();
            entry.getLabels().forEach(l -> labels.add(Guards.flatten(l.toString())));
            String text = labels.isEmpty() ? "default" : "case " + String.join(", ", labels);
            var statements = entry.getStatements();
            boolean exits = !statements.isEmpty() && Guards.leaves(statements.get(statements.size() - 1));
            return new StepFrame(at(entry), "case", text, exits);
        }
        if (parent instanceof CatchClause c && child == c.getBody()) {
            return new StepFrame(at(c), "catch", "catch " + Guards.flatten(c.getParameter().getType().toString()),
                    Guards.leaves(c.getBody()));
        }
        if (parent instanceof TryStmt t) {
            if (child == t.getTryBlock()) {
                return new StepFrame(at(t), "try", "try", false);
            }
            if (t.getFinallyBlock().filter(f -> f == child).isPresent()) {
                return new StepFrame(at(child), "finally", "finally", false);
            }
            return null;
        }
        if (parent instanceof ForEachStmt f && child == f.getBody()) {
            String name = f.getVariable().getVariables().get(0).getNameAsString();
            return new StepFrame(at(f), "loop", "for " + name + " : " + Guards.flatten(f.getIterable().toString()), false);
        }
        if (parent instanceof ForStmt f && child == f.getBody()) {
            return new StepFrame(at(f), "loop", "for " + f.getCompare().map(Guards::text).orElse("ever"), false);
        }
        if (parent instanceof WhileStmt w && child == w.getBody()) {
            return new StepFrame(at(w), "loop", "while " + Guards.text(w.getCondition()), false);
        }
        if (parent instanceof DoStmt d && child == d.getBody()) {
            return new StepFrame(at(d), "loop", "do … while " + Guards.text(d.getCondition()), false);
        }
        if (parent instanceof LambdaExpr l && child == l.getBody()) {
            Optional<Node> owner = l.getParentNode();
            if (owner.isPresent() && owner.get() instanceof MethodCallExpr m
                    && m.getArguments().stream().anyMatch(a -> a == l)) {
                // Framed at the call it is passed to, so a reader (and rule 3
                // on the Go side) can tell which call runs it.
                return new StepFrame(at(m), "closure", text(m), false);
            }
            return new StepFrame(at(l), "closure", lambdaHead(l) + " -> …", false);
        }
        return null;
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

    private static String lambdaHead(LambdaExpr l) {
        List<String> names = new ArrayList<>();
        l.getParameters().forEach(p -> names.add(p.getNameAsString()));
        return names.size() == 1 && !l.isEnclosingParameters() ? names.get(0) : "(" + String.join(", ", names) + ")";
    }

    private static String at(Node n) {
        return n.getBegin().map(p -> p.line + ":" + p.column).orElse("0:0");
    }
}
