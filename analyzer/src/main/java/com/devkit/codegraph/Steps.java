package com.devkit.codegraph;

import com.github.javaparser.ast.Node;
import com.github.javaparser.ast.body.BodyDeclaration;
import com.github.javaparser.ast.body.CallableDeclaration;
import com.github.javaparser.ast.body.FieldDeclaration;
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
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
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

    /**
     * The column a step is placed at. For a method call that is its NAME:
     * {@code getBegin()} of {@code a().b(...)} is where {@code a()} starts, so
     * every call of a chain would share one column.
     */
    static int col(Node node) {
        Node at = node instanceof MethodCallExpr m ? m.getName() : node;
        return at.getBegin().or(node::getBegin).map(p -> p.column).orElse(0);
    }

    /**
     * {@code line:col} of a step: the line it begins on (the line a call site
     * records) and {@link #col}. A closure frame's id is the position of the
     * call it is passed to, so the Go side can tell which step runs it.
     */
    static String position(Node node) {
        return node.getBegin().map(p -> p.line + ":" + col(node)).orElse("0:0");
    }

    /**
     * The call as written, on one line. Every lambda in it — an argument, or
     * one further up the receiver chain — is shortened to its head, and an
     * anonymous class to {@code new T() {…}}: their bodies are the outline's
     * next rows, not part of this one. Printed from a copy; the tree the
     * analyzer walks is left alone.
     */
    static String text(Node node) {
        boolean shortens = node.findFirst(LambdaExpr.class).isPresent()
                || node.findFirst(ObjectCreationExpr.class, o -> o.getAnonymousClassBody().isPresent()).isPresent();
        if (!shortens) {
            return clip(Guards.flatten(node.toString()));
        }
        return clip(Guards.flatten(shortened(node.clone())));
    }

    private static String shortened(Node copy) {
        // Lambdas first: one in an anonymous class's arguments is printed with it.
        for (LambdaExpr l : copy.findAll(LambdaExpr.class)) {
            if (insideCollapsed(l)) {
                continue;
            }
            String head = lambdaHead(l) + " -> …";
            if (l == copy) {
                return head;
            }
            l.replace(new NameExpr(head));
        }
        // Innermost first, so a class passed to another class's constructor is already short.
        List<ObjectCreationExpr> anonymous = copy.findAll(ObjectCreationExpr.class,
                o -> o.getAnonymousClassBody().isPresent());
        Collections.reverse(anonymous);
        for (ObjectCreationExpr o : anonymous) {
            if (insideCollapsed(o)) {
                continue;
            }
            o.setAnonymousClassBody(null);
            String head = Guards.flatten(o.toString()) + " {…}";
            if (o == copy) {
                return head;
            }
            o.replace(new NameExpr(head));
        }
        return copy.toString();
    }

    /** Inside a lambda body or an anonymous class body: printed as part of that, or not at all. */
    private static boolean insideCollapsed(Node n) {
        for (Node p = n.getParentNode().orElse(null); p != null; p = p.getParentNode().orElse(null)) {
            if (p instanceof LambdaExpr || (p instanceof BodyDeclaration<?> && p.getParentNode()
                    .filter(o -> o instanceof ObjectCreationExpr).isPresent())) {
                return true;
            }
        }
        return false;
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
                        condition(elseIf ? "else if" : "if", s.getCondition()), Guards.leaves(s.getThenStmt()));
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
        if (parent instanceof SwitchEntry entry && Guards.isStatementOf(entry, child)) {
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
            String compare = f.getCompare().map(Guards::text).orElse("");
            return new StepFrame(at(f), "loop", "for " + (compare.isEmpty() ? "ever" : compare), false);
        }
        if (parent instanceof WhileStmt w && child == w.getBody()) {
            return new StepFrame(at(w), "loop", condition("while", w.getCondition()), false);
        }
        if (parent instanceof DoStmt d && child == d.getBody()) {
            return new StepFrame(at(d), "loop", condition("do … while", d.getCondition()), false);
        }
        if (parent instanceof LambdaExpr l && child == l.getBody()) {
            Optional<Node> owner = l.getParentNode();
            if (owner.isPresent() && owner.get() instanceof MethodCallExpr m
                    && m.getArguments().stream().anyMatch(a -> a == l)) {
                // Framed at the call it is passed to, so a reader (and rule 3
                // on the Go side) can tell which call runs it.
                return new StepFrame(position(m), "closure", text(m), false);
            }
            return new StepFrame(at(l), "closure", lambdaHead(l) + " -> …", false);
        }
        if (parent instanceof CallableDeclaration<?> && parent.getParentNode().orElse(null) instanceof ObjectCreationExpr o) {
            // A method of an anonymous class is a closure written the long way.
            Optional<Node> owner = o.getParentNode();
            if (owner.isPresent() && owner.get() instanceof MethodCallExpr m
                    && m.getArguments().stream().anyMatch(a -> a == o)) {
                return new StepFrame(position(m), "closure", text(m), false);
            }
            return new StepFrame(position(o), "closure", text(o), false);
        }
        return null;
    }

    /** {@code if flag}; {@code if true} for a literal Guards reads as no condition at all. */
    private static String condition(String keyword, Expression e) {
        String text = Guards.text(e);
        return keyword + " " + (text.isEmpty() ? "true" : text);
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
    record Info(int col, String text, String receiver, String resultUse, List<StepFrame> frames) {
        Info {
            frames = List.copyOf(frames);
        }

        /** What a call says when reading it failed: where it is, and nothing else. */
        static Info empty(Node call) {
            int col;
            try {
                col = Steps.col(call);
            } catch (RuntimeException e) {
                col = 0;
            }
            return new Info(col, "", "unknown", "unknown", List.of());
        }
    }

    /**
     * What every call of one scope asks about the same names, read once:
     * its parameters, the first local declared under each name, and what each
     * name was found to be. A scope with hundreds of calls would otherwise walk
     * its whole body, and ask the solver, once per call.
     */
    static final class Context {
        final Node scope;
        private final Map<String, Parameter> params = new LinkedHashMap<>();
        private final Map<String, VariableDeclarator> locals = new HashMap<>();
        private final Map<String, String> kinds = new HashMap<>();
        private final Map<String, Optional<String>> fieldTypes = new HashMap<>();
        private Optional<TypeDeclaration<?>> owner;

        private Context(Node scope) {
            this.scope = scope;
            if (scope instanceof CallableDeclaration<?> c) {
                for (Parameter p : c.getParameters()) {
                    params.putIfAbsent(p.getNameAsString(), p);
                }
            }
            for (VariableDeclarator v : scope.findAll(VariableDeclarator.class)) {
                if (v.getParentNode().filter(p -> p instanceof FieldDeclaration).isEmpty()) {
                    locals.putIfAbsent(v.getNameAsString(), v);
                }
            }
        }

        @SuppressWarnings("unchecked")
        Optional<TypeDeclaration<?>> owner() {
            if (owner == null) {
                owner = scope.findAncestor(TypeDeclaration.class).map(t -> (TypeDeclaration<?>) t);
            }
            return owner;
        }
    }

    static Context context(Node scope) {
        return new Context(scope);
    }

    /** For a single call; an analyzer reading many calls of one scope shares a {@link Context}. */
    static Info of(Node call, Node scope) {
        return of(call, context(scope));
    }

    static Info of(Node call, Context ctx) {
        Node scope = ctx.scope;
        String receiver = call instanceof MethodCallExpr m ? receiver(m, ctx) : "unknown";
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
        return receiver(call, context(scope));
    }

    static String receiver(MethodCallExpr call, Context ctx) {
        return call.getScope().map(s -> origin(s, call, ctx, false)).orElse("this");
    }

    private static String origin(Expression e, Node at, Context ctx, boolean derived) {
        Expression x = unwrap(e);
        if (x instanceof ThisExpr || x instanceof SuperExpr) {
            return "this";
        }
        if (x instanceof FieldAccessExpr f) {
            return f.getScope() instanceof ThisExpr ? "field" : origin(f.getScope(), at, ctx, true);
        }
        if (x instanceof MethodCallExpr m) {
            return m.getScope().map(s -> origin(s, at, ctx, true)).orElse("local");
        }
        if (x instanceof NameExpr n) {
            String kind = nameKind(n, at, ctx);
            return derived && kind.equals("param") ? "local-from-param" : kind;
        }
        return "unknown";
    }

    private static String nameKind(NameExpr ref, Node at, Context ctx) {
        String name = ref.getNameAsString();
        for (Node n = at; n != null && n != ctx.scope; n = n.getParentNode().orElse(null)) {
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
            // A method of an anonymous (or local) class inside the scope: its parameters are its locals.
            if (n instanceof CallableDeclaration<?> c
                    && c.getParameters().stream().anyMatch(p -> p.getNameAsString().equals(name))) {
                return "local";
            }
        }
        String known = ctx.kinds.get(name);
        if (known != null) {
            return known;
        }
        ctx.kinds.put(name, "local"); // `Foo f = f.next()` ends here instead of going round
        String kind = scopeKind(ref, ctx);
        ctx.kinds.put(name, kind);
        return kind;
    }

    private static String scopeKind(NameExpr ref, Context ctx) {
        String name = ref.getNameAsString();
        VariableDeclarator local = ctx.locals.get(name);
        if (local != null) {
            // From a parameter only when its VALUE is reached from one: `exchange.getIn()`
            // is, `new Resp(req.id())` and `foo(req)` merely read one.
            boolean fromParam = local.getInitializer().map(init -> origin(init, local, ctx, true))
                    .filter(o -> o.equals("param") || o.equals("local-from-param"))
                    .isPresent();
            return fromParam ? "local-from-param" : "local";
        }
        if (ctx.params.containsKey(name)) {
            return "param";
        }
        if (fieldType(name, ctx) != null) {
            return "field";
        }
        if (!name.isEmpty() && Character.isUpperCase(name.charAt(0))) {
            return "static";
        }
        // `log` is Lombok's, which the solver cannot see; asking costs a failed resolution per scope.
        if (!name.equals("log")) {
            try {
                if (ref.resolve().isField()) {
                    return "field"; // inherited: declared on a superclass
                }
            } catch (RuntimeException | StackOverflowError ignored) {
                // stays unknown
            }
        }
        return "unknown";
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
        if (p instanceof ExpressionStmt stmt) {
            // JavaParser wraps an expression lambda's body in a statement: `o -> o.getId()` returns it.
            boolean lambdaBody = stmt.getParentNode().filter(l -> l instanceof LambdaExpr le && le.getBody() == stmt)
                    .isPresent();
            return lambdaBody ? "returned" : "discarded";
        }
        if (p instanceof VariableDeclarator || p instanceof AssignExpr) {
            return "assigned";
        }
        if (p instanceof ReturnStmt) {
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
        return declaredType(receiver, context(scope));
    }

    static String declaredType(Expression receiver, Context ctx) {
        Expression x = unwrap(receiver);
        if (x instanceof MethodCallExpr m && m.getScope().isEmpty()) {
            return ctx.owner()
                    .flatMap(td -> td.getMethodsByName(m.getNameAsString()).stream().findFirst())
                    .map(md -> simple(md.getType().toString()))
                    .orElse(null);
        }
        if (!(x instanceof NameExpr n)) {
            return null;
        }
        String name = n.getNameAsString();
        Parameter param = ctx.params.get(name);
        if (param != null) {
            return simple(param.getType().toString());
        }
        VariableDeclarator local = ctx.locals.get(name);
        if (local != null && !local.getType().isVarType()) {
            return simple(local.getType().toString());
        }
        return fieldType(name, ctx);
    }

    /** The scope's class, by qualified name where it has one; "" outside any type. */
    static String enclosingType(Context ctx) {
        return ctx.owner()
                .map(td -> td.getFullyQualifiedName().orElse(td.getNameAsString()))
                .orElse("");
    }

    private static String fieldType(String name, Context ctx) {
        return ctx.fieldTypes.computeIfAbsent(name, k -> Optional.ofNullable(readFieldType(k, ctx))).orElse(null);
    }

    private static String readFieldType(String name, Context ctx) {
        Optional<TypeDeclaration<?>> owner = ctx.owner();
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

    static Expression unwrap(Expression e) {
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
