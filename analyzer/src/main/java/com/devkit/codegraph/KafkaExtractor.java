package com.devkit.codegraph;

import com.devkit.codegraph.ConfigExtractor.Member;
import com.devkit.codegraph.ConfigExtractor.Owner;
import com.devkit.codegraph.ConfigExtractor.Scope;
import com.github.javaparser.ast.expr.AnnotationExpr;
import com.github.javaparser.ast.expr.ArrayInitializerExpr;
import com.github.javaparser.ast.expr.Expression;
import com.github.javaparser.ast.expr.FieldAccessExpr;
import com.github.javaparser.ast.expr.MethodCallExpr;
import com.github.javaparser.ast.expr.NameExpr;
import com.github.javaparser.ast.expr.NormalAnnotationExpr;
import com.github.javaparser.ast.expr.ObjectCreationExpr;
import com.github.javaparser.ast.expr.SingleMemberAnnotationExpr;
import com.github.javaparser.ast.expr.ThisExpr;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;

/**
 * The Kafka topics the code produces to and consumes from: Spring Kafka
 * ({@code @KafkaListener}, {@code KafkaTemplate.send}), the plain client
 * ({@code new ProducerRecord<>(topic, …)}, {@code consumer.subscribe}) and Kafka
 * Streams ({@code builder.stream("in")…to("out")}).
 *
 * Recognised by what the code IS — an annotation, a constructor, a receiver
 * declared as a Kafka type — never by a method name: {@code send} and
 * {@code subscribe} are on plenty of things that are not Kafka. A topic is the
 * argument folded the way a URL is: literals and constants joined, an injected
 * value as its {@code ${placeholder}}, a parameter as a {@code {hole}}.
 */
final class KafkaExtractor {

    // Kafka's own Producer<K,V> and Consumer<K,V> interfaces are left out on purpose: the names
    // are a project's own types as often as Kafka's, and matching by simple name would turn
    // every domain Producer.send(String) into a topic.
    private static final Set<String> PRODUCER_TYPES = Set.of(
            "KafkaTemplate", "KafkaOperations", "KafkaOperations2", "ReactiveKafkaProducerTemplate",
            "KafkaProducer");
    private static final Set<String> CONSUMER_TYPES = Set.of(
            "KafkaConsumer", "ReactiveKafkaConsumerTemplate");
    private static final Set<String> STREAM_TYPES = Set.of(
            "StreamsBuilder", "KStream", "KTable", "GlobalKTable", "KGroupedStream", "KGroupedTable");
    private static final Set<String> STREAM_SOURCES = Set.of("stream", "table", "globalTable");
    private static final Set<String> LIST_FACTORIES = Set.of("of", "asList", "singletonList", "singleton", "copyOf");

    private final ConstantResolver constants;
    private final Annotations annotations;
    private final Set<String> nodeIds;
    private final List<KafkaAccess> out = new ArrayList<>();

    private KafkaExtractor(ConstantResolver constants, Annotations annotations, Set<String> nodeIds) {
        this.constants = constants;
        this.annotations = annotations;
        this.nodeIds = nodeIds;
    }

    static List<KafkaAccess> extract(List<Owner> owners, List<Scope> scopes, Annotations annotations,
                                     ConstantResolver constants, Set<String> nodeIds) {
        KafkaExtractor x = new KafkaExtractor(constants, annotations, nodeIds);
        for (Owner owner : owners) {
            x.readListeners(owner);
        }
        for (Scope scope : scopes) {
            x.readCalls(scope);
        }
        return x.deduped();
    }

    private List<KafkaAccess> deduped() {
        Map<String, KafkaAccess> byKey = new LinkedHashMap<>();
        for (KafkaAccess a : out) {
            byKey.putIfAbsent(String.join("\n", a.from, a.topic, a.role, a.client, String.valueOf(a.line)), a);
        }
        return new ArrayList<>(byKey.values());
    }

    // --- @KafkaListener -----------------------------------------------------------------------

    private void readListeners(Owner owner) {
        annotations.find(owner.decl(), "KafkaListener").ifPresent(a -> listener(a, owner.id(), owner.file()));
        for (Member method : owner.methods()) {
            annotations.find(method.decl(), "KafkaListener").ifPresent(a -> listener(a, method.id(), owner.file()));
        }
    }

    private void listener(AnnotationExpr a, String from, String file) {
        if (!nodeIds.contains(from)) {
            return;
        }
        String group = strings(a, "groupId").stream().findFirst().orElse("");
        int line = a.getBegin().map(p -> p.line).orElse(0);
        List<String> topics = new ArrayList<>(strings(a, "topics"));
        // topicPartitions = @TopicPartition(topic = "t", partitions = …)
        for (AnnotationExpr partition : nestedAnnotations(a, "topicPartitions")) {
            topics.addAll(strings(partition, "topic"));
        }
        for (String topic : topics) {
            add(from, topic, "consume", "kafka-listener", group, false, file, line);
        }
        for (String regex : strings(a, "topicPattern")) {
            add(from, regex, "consume", "kafka-listener", group, true, file, line);
        }
    }

    // --- calls --------------------------------------------------------------------------------

    private void readCalls(Scope scope) {
        for (MethodCallExpr call : scope.node().findAll(MethodCallExpr.class)) {
            String name = call.getNameAsString();
            if (call.getScope().isEmpty() || call.getArguments().isEmpty()) {
                continue;
            }
            Optional<String> root = rootType(call.getScope().get());
            if (root.isEmpty()) {
                continue;
            }
            int line = call.getBegin().map(p -> p.line).orElse(0);
            if (name.equals("send") && PRODUCER_TYPES.contains(root.get()) && !(call.getArgument(0) instanceof ObjectCreationExpr)) {
                // send(ProducerRecord) is read where the record is built.
                for (String owner : scope.ownerIds()) {
                    add(owner, constants.resolve(call.getArgument(0)), "produce",
                            root.get().equals("KafkaTemplate") || root.get().startsWith("Reactive") || root.get().startsWith("KafkaOperations") ? "kafka-template" : "kafka-producer",
                            "", false, scope.file(), line);
                }
            } else if (name.equals("subscribe") && CONSUMER_TYPES.contains(root.get())) {
                for (String topic : topicArguments(call.getArgument(0))) {
                    for (String owner : scope.ownerIds()) {
                        add(owner, topic, "consume", "kafka-consumer", "", false, scope.file(), line);
                    }
                }
            } else if (STREAM_SOURCES.contains(name) && STREAM_TYPES.contains(root.get())) {
                for (String topic : topicArguments(call.getArgument(0))) {
                    for (String owner : scope.ownerIds()) {
                        add(owner, topic, "consume", "kafka-streams", "", false, scope.file(), line);
                    }
                }
            } else if (name.equals("to") && STREAM_TYPES.contains(root.get())) {
                for (String owner : scope.ownerIds()) {
                    add(owner, constants.resolve(call.getArgument(0)), "produce", "kafka-streams", "", false, scope.file(), line);
                }
            }
        }
        // new ProducerRecord<>("topic", key, value): the topic a producer writes to, however the record is sent.
        for (ObjectCreationExpr creation : scope.node().findAll(ObjectCreationExpr.class)) {
            if (creation.getType().getNameAsString().equals("ProducerRecord") && !creation.getArguments().isEmpty()) {
                int line = creation.getBegin().map(p -> p.line).orElse(0);
                for (String owner : scope.ownerIds()) {
                    add(owner, constants.resolve(creation.getArgument(0)), "produce", "kafka-producer", "", false, scope.file(), line);
                }
            }
        }
    }

    /** The declared type, by simple name, of the object a chain of calls starts from. */
    private static Optional<String> rootType(Expression receiver) {
        Expression e = receiver;
        while (e instanceof MethodCallExpr m && m.getScope().isPresent()) {
            e = m.getScope().get();
        }
        if (e instanceof NameExpr || (e instanceof FieldAccessExpr access && access.getScope() instanceof ThisExpr)) {
            String name = e instanceof NameExpr n ? n.getNameAsString() : ((FieldAccessExpr) e).getNameAsString();
            return OutboundCallExtractor.declaration(e, name, !(e instanceof NameExpr))
                    .map(d -> OutboundCallExtractor.simpleName(d.type()));
        }
        return Optional.empty();
    }

    /** The topic names an argument gives: List.of("a", "b"), a single name, or a Pattern (a regex). */
    private List<String> topicArguments(Expression arg) {
        List<String> topics = new ArrayList<>();
        if (arg instanceof MethodCallExpr m && LIST_FACTORIES.contains(m.getNameAsString())) {
            for (Expression e : m.getArguments()) {
                topics.add(constants.resolve(e));
            }
        } else if (arg instanceof MethodCallExpr compile && compile.getNameAsString().equals("compile")) {
            return List.of(); // a Pattern is a regex over topics, not a topic
        } else {
            topics.add(constants.resolve(arg));
        }
        return topics;
    }

    // --- helpers ------------------------------------------------------------------------------

    private void add(String from, String topic, String role, String client, String group, boolean pattern, String file, int line) {
        if (!nodeIds.contains(from) || topic == null) {
            return;
        }
        // A topic that is only a hole — a variable the resolver could not follow — names nothing to match.
        if (topic.replaceAll("\\{[^{}]*}", "").isBlank()) {
            return;
        }
        KafkaAccess a = new KafkaAccess();
        a.from = from;
        a.topic = topic;
        a.role = role;
        a.client = client;
        a.groupId = group;
        a.pattern = pattern;
        a.file = file;
        a.line = line;
        out.add(a);
    }

    /** The strings an attribute holds — one, or an array — constants folded. */
    private List<String> strings(AnnotationExpr a, String name) {
        List<String> values = new ArrayList<>();
        attribute(a, name).ifPresent(e -> {
            if (e instanceof ArrayInitializerExpr array) {
                array.getValues().forEach(v -> values.add(constants.resolve(v)));
            } else {
                values.add(constants.resolve(e));
            }
        });
        values.removeIf(String::isEmpty);
        return values;
    }

    private static List<AnnotationExpr> nestedAnnotations(AnnotationExpr a, String name) {
        List<AnnotationExpr> out = new ArrayList<>();
        attribute(a, name).ifPresent(e -> {
            if (e instanceof ArrayInitializerExpr array) {
                array.getValues().forEach(v -> {
                    if (v instanceof AnnotationExpr inner) {
                        out.add(inner);
                    }
                });
            } else if (e instanceof AnnotationExpr inner) {
                out.add(inner);
            }
        });
        return out;
    }

    private static Optional<Expression> attribute(AnnotationExpr a, String name) {
        if (a instanceof NormalAnnotationExpr normal) {
            return normal.getPairs().stream().filter(p -> p.getNameAsString().equals(name)).map(p -> p.getValue()).findFirst();
        }
        if (a instanceof SingleMemberAnnotationExpr single && name.equals("value")) {
            return Optional.of(single.getMemberValue());
        }
        return Optional.empty();
    }
}
