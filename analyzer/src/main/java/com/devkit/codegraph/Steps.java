package com.devkit.codegraph;

import com.github.javaparser.ast.Node;
import com.github.javaparser.ast.body.CallableDeclaration;
import com.github.javaparser.ast.body.FieldDeclaration;
import com.github.javaparser.ast.body.MethodDeclaration;
import com.github.javaparser.ast.body.Parameter;
import com.github.javaparser.ast.body.RecordDeclaration;
import com.github.javaparser.ast.body.TypeDeclaration;
import com.github.javaparser.ast.body.VariableDeclarator;
import com.github.javaparser.ast.expr.AssignExpr;
import com.github.javaparser.ast.expr.BinaryExpr;
import com.github.javaparser.ast.expr.CastExpr;
import com.github.javaparser.ast.expr.ConditionalExpr;
import com.github.javaparser.ast.expr.EnclosedExpr;
import com.github.javaparser.ast.expr.Expression;
import com.github.javaparser.ast.expr.FieldAccessExpr;
import com.github.javaparser.ast.expr.InstanceOfExpr;
import com.github.javaparser.ast.expr.LambdaExpr;
import com.github.javaparser.ast.expr.MethodCallExpr;
import com.github.javaparser.ast.expr.NameExpr;
import com.github.javaparser.ast.expr.ObjectCreationExpr;
import com.github.javaparser.ast.expr.SuperExpr;
import com.github.javaparser.ast.expr.SwitchExpr;
import com.github.javaparser.ast.expr.ThisExpr;
import com.github.javaparser.ast.expr.UnaryExpr;
import com.github.javaparser.ast.stmt.CatchClause;
import com.github.javaparser.ast.stmt.DoStmt;
import com.github.javaparser.ast.stmt.ExplicitConstructorInvocationStmt;
import com.github.javaparser.ast.stmt.ExpressionStmt;
import com.github.javaparser.ast.stmt.ForEachStmt;
import com.github.javaparser.ast.stmt.ForStmt;
import com.github.javaparser.ast.stmt.IfStmt;
import com.github.javaparser.ast.stmt.ReturnStmt;
import com.github.javaparser.ast.stmt.Statement;
import com.github.javaparser.ast.stmt.SwitchEntry;
import com.github.javaparser.ast.stmt.SwitchStmt;
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

    /** Everything {@link CallSite#describe} records about one call. */
    record Info(int col, String text, String receiver, String resultUse, List<StepFrame> frames) {}

    static Info of(Node call, Node scope) {
        String receiver = call instanceof MethodCallExpr m ? receiver(m, scope) : "unknown";
        return new Info(col(call), text(call), receiver, resultUse(call), frames(call, scope));
    }

    /**
     * Where the receiver of {@code call} comes from: param, field,
     * local-from-param (a local or a chain built from a parameter), local,
     * static (a type name), this, or unknown. A call that changes something
     * the method was handed — `in.setHeader(...)` with `in = exchange.getIn()` —
     * is a step a reader wants to see; the same call on a temporary is not.
     */
    static String receiver(MethodCallExpr call, Node scope) {
        return call.getScope().map(s -> origin(s, call, scope, false)).orElse("this");
    }

    private static String origin(Expression e, Node at, Node scope, boolean derived) {
        Expression x = unwrap(e);
        if (x instanceof ThisExpr || x instanceof SuperExpr) {
            return "this";
        }
        if (x instanceof FieldAccessExpr f) {
            return f.getScope() instanceof ThisExpr ? "field" : origin(f.getScope(), at, scope, true);
        }
        if (x instanceof MethodCallExpr m) {
            return m.getScope().map(s -> origin(s, at, scope, true)).orElse("local");
        }
        if (x instanceof NameExpr n) {
            String kind = nameKind(n.getNameAsString(), at, scope);
            if (kind.equals("unknown")) {
                try {
                    if (n.resolve().isField()) {
                        kind = "field"; // inherited: declared on a superclass
                    }
                } catch (RuntimeException | StackOverflowError ignored) {
                    // stays unknown
                }
            }
            return derived && kind.equals("param") ? "local-from-param" : kind;
        }
        return "unknown";
    }

    private static String nameKind(String name, Node at, Node scope) {
        for (Node n = at; n != null && n != scope; n = n.getParentNode().orElse(null)) {
            if (n instanceof LambdaExpr l && l.getParameters().stream().anyMatch(p -> p.getNameAsString().equals(name))) {
                return "local";
            }
            if (n instanceof CatchClause c && c.getParameter().getNameAsString().equals(name)) {
                return "local";
            }
            if (n instanceof ForEachStmt f
                    && f.getVariable().getVariables().stream().anyMatch(v -> v.getNameAsString().equals(name))) {
                return "local";
            }
        }
        List<String> params = parameterNames(scope);
        for (VariableDeclarator v : scope.findAll(VariableDeclarator.class)) {
            if (!v.getNameAsString().equals(name) || v.getParentNode().filter(p -> p instanceof FieldDeclaration).isPresent()) {
                continue;
            }
            boolean fromParam = v.getInitializer()
                    .filter(init -> init.findAll(NameExpr.class).stream().anyMatch(ref -> params.contains(ref.getNameAsString())))
                    .isPresent();
            return fromParam ? "local-from-param" : "local";
        }
        if (params.contains(name)) {
            return "param";
        }
        if (fieldType(name, scope) != null) {
            return "field";
        }
        if (!name.isEmpty() && Character.isUpperCase(name.charAt(0))) {
            return "static";
        }
        return "unknown";
    }

    private static List<String> parameterNames(Node scope) {
        List<String> names = new ArrayList<>();
        if (scope instanceof CallableDeclaration<?> c) {
            for (Parameter p : c.getParameters()) {
                names.add(p.getNameAsString());
            }
        }
        return names;
    }

    /**
     * Where the value of {@code call} goes: discarded (a statement of its
     * own), assigned, returned, argument (into another call, or as the
     * receiver of the next call in a chain), condition (decides a branch),
     * or unknown.
     */
    static String resultUse(Node call) {
        Node child = call;
        Node p = call.getParentNode().orElse(null);
        while (p instanceof EnclosedExpr || p instanceof CastExpr) {
            child = p;
            p = p.getParentNode().orElse(null);
        }
        if (p == null) {
            return "unknown";
        }
        if (p instanceof ExpressionStmt) {
            return "discarded";
        }
        if (p instanceof VariableDeclarator || p instanceof AssignExpr) {
            return "assigned";
        }
        if (p instanceof ReturnStmt || p instanceof LambdaExpr) {
            return "returned";
        }
        if (p instanceof MethodCallExpr || p instanceof ObjectCreationExpr || p instanceof ExplicitConstructorInvocationStmt) {
            return "argument";
        }
        if (inCondition(child, p)) {
            return "condition";
        }
        if (p instanceof BinaryExpr || p instanceof UnaryExpr || p instanceof InstanceOfExpr || p instanceof ConditionalExpr) {
            Node c = p;
            Node q = p.getParentNode().orElse(null);
            while (q instanceof BinaryExpr || q instanceof UnaryExpr || q instanceof EnclosedExpr || q instanceof InstanceOfExpr) {
                c = q;
                q = q.getParentNode().orElse(null);
            }
            return q != null && inCondition(c, q) ? "condition" : "argument";
        }
        return "unknown";
    }

    private static boolean inCondition(Node child, Node p) {
        return (p instanceof IfStmt s && s.getCondition() == child)
                || (p instanceof WhileStmt w && w.getCondition() == child)
                || (p instanceof DoStmt d && d.getCondition() == child)
                || (p instanceof ForStmt f && f.getCompare().filter(c -> c == child).isPresent())
                || (p instanceof ConditionalExpr t && t.getCondition() == child)
                || (p instanceof SwitchStmt s2 && s2.getSelector() == child)
                || (p instanceof SwitchExpr s3 && s3.getSelector() == child);
    }

    /**
     * The receiver's declared type as written (generics dropped), for a callee
     * key when the solver could not resolve the call: a parameter, a local, a
     * field, or the return type of a method of the same class.
     */
    static String declaredType(Expression receiver, Node scope) {
        Expression x = unwrap(receiver);
        if (x instanceof MethodCallExpr m && m.getScope().isEmpty()) {
            return scope.findAncestor(TypeDeclaration.class)
                    .flatMap(td -> ((TypeDeclaration<?>) td).getMethodsByName(m.getNameAsString()).stream().findFirst())
                    .map(md -> simple(((MethodDeclaration) md).getType().toString()))
                    .orElse(null);
        }
        if (!(x instanceof NameExpr n)) {
            return null;
        }
        String name = n.getNameAsString();
        if (scope instanceof CallableDeclaration<?> c) {
            for (Parameter p : c.getParameters()) {
                if (p.getNameAsString().equals(name)) {
                    return simple(p.getType().toString());
                }
            }
        }
        for (VariableDeclarator v : scope.findAll(VariableDeclarator.class)) {
            if (v.getNameAsString().equals(name) && !v.getType().isVarType()) {
                return simple(v.getType().toString());
            }
        }
        return fieldType(name, scope);
    }

    private static String fieldType(String name, Node scope) {
        Optional<TypeDeclaration> owner = scope.findAncestor(TypeDeclaration.class);
        if (owner.isEmpty()) {
            return null;
        }
        TypeDeclaration<?> td = owner.get();
        Optional<FieldDeclaration> field = td.getFieldByName(name);
        if (field.isPresent()) {
            return simple(field.get().getVariable(0).getType().toString());
        }
        if (td instanceof RecordDeclaration r) {
            for (Parameter p : r.getParameters()) {
                if (p.getNameAsString().equals(name)) {
                    return simple(p.getType().toString());
                }
            }
        }
        return null;
    }

    private static String simple(String type) {
        int lt = type.indexOf('<');
        return lt >= 0 ? type.substring(0, lt) : type;
    }

    private static Expression unwrap(Expression e) {
        Expression x = e;
        while (true) {
            if (x instanceof EnclosedExpr en) {
                x = en.getInner();
            } else if (x instanceof CastExpr c) {
                x = c.getExpression();
            } else {
                return x;
            }
        }
    }
}
