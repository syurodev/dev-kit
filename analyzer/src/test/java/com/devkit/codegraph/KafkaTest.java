package com.devkit.codegraph;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.stream.Collectors;

import static org.junit.jupiter.api.Assertions.assertEquals;

class KafkaTest {

    private static List<String> accesses(Path root, String source) throws IOException {
        Path file = root.resolve("src/main/java/acme/Events.java");
        Files.createDirectories(file.getParent());
        Files.writeString(file, source);
        AnalysisResult r = new ProjectAnalyzer(root, 1).analyze();
        return r.kafkaAccesses.stream()
                .map(a -> a.from.replaceAll("^acme\\.Events\\.", "").replaceAll("\\(.*", "") + " " + a.role + " " + a.topic
                        + (a.groupId.isEmpty() ? "" : " group=" + a.groupId) + (a.pattern ? " pattern" : "") + " [" + a.client + "]")
                .sorted().collect(Collectors.toList());
    }

    @Test
    void springKafkaListenersAndTemplates(@TempDir Path root) throws IOException {
        List<String> got = accesses(root, """
                package acme;
                import org.springframework.kafka.annotation.*;
                import org.springframework.kafka.core.KafkaTemplate;
                import org.springframework.beans.factory.annotation.Value;
                class Events {
                    static final String ACCESS = "fsap.access.changed";
                    private final KafkaTemplate<String, String> kafka = null;
                    @Value("${app.kafka.topic.audit}") private String auditTopic;

                    @KafkaListener(topics = {ACCESS, "fsap.org.created"}, groupId = "access-service")
                    void onChange(String message) {}

                    @KafkaListener(topics = "${app.kafka.topic.in:orders.in}", groupId = "${app.group}")
                    void onOrder(String message) {}

                    @KafkaListener(topicPattern = "billing\\\\..*", groupId = "billing-readers")
                    void onBilling(String message) {}

                    @KafkaListener(topicPartitions = @TopicPartition(topic = "fsap.partitioned", partitions = {"0"}))
                    void onPartition(String message) {}

                    void publish(String org) { kafka.send(ACCESS, org); }
                    void audit(String msg) { kafka.send(auditTopic, msg); }
                    void dynamic(String topic, String msg) { kafka.send(topic, msg); }
                }
                """);
        assertEquals(List.of(
                "audit produce ${app.kafka.topic.audit} [kafka-template]",
                "onBilling consume billing\\..* group=billing-readers pattern [kafka-listener]",
                "onChange consume fsap.access.changed group=access-service [kafka-listener]",
                "onChange consume fsap.org.created group=access-service [kafka-listener]",
                "onOrder consume ${app.kafka.topic.in:orders.in} group=${app.group} [kafka-listener]",
                "onPartition consume fsap.partitioned [kafka-listener]",
                "publish produce fsap.access.changed [kafka-template]"), got);
    }

    @Test
    void theClassLevelListenerAndTheProducerRecord(@TempDir Path root) throws IOException {
        List<String> got = accesses(root, """
                package acme;
                import org.springframework.kafka.annotation.*;
                import org.apache.kafka.clients.producer.*;
                import org.apache.kafka.clients.consumer.*;
                import java.util.List;
                @KafkaListener(topics = "fsap.commands")
                class Events {
                    private final KafkaProducer<String, String> producer = null;
                    private final KafkaConsumer<String, String> consumer = null;
                    @KafkaHandler void handle(String m) {}
                    void send(String org) { producer.send(new ProducerRecord<>("fsap.events." + org, "k", "v")); }
                    void poll() { consumer.subscribe(List.of("fsap.a", "fsap.b")); }
                }
                """);
        assertEquals(List.of(
                "acme.Events consume fsap.commands [kafka-listener]", // a class-level listener is the class's
                "poll consume fsap.a [kafka-consumer]",
                "poll consume fsap.b [kafka-consumer]",
                "send produce fsap.events.{org} [kafka-producer]"), got);
    }

    @Test
    void kafkaStreams(@TempDir Path root) throws IOException {
        List<String> got = accesses(root, """
                package acme;
                import org.apache.kafka.streams.StreamsBuilder;
                import org.apache.kafka.streams.kstream.KStream;
                class Events {
                    void topology(StreamsBuilder builder) {
                        KStream<String, String> in = builder.stream("fsap.raw");
                        in.mapValues(v -> v).to("fsap.clean");
                        builder.table("fsap.reference");
                    }
                }
                """);
        assertEquals(List.of(
                "topology consume fsap.raw [kafka-streams]",
                "topology consume fsap.reference [kafka-streams]",
                "topology produce fsap.clean [kafka-streams]"), got);
    }

    @Test
    void thingsThatLookLikeKafkaButAreNot(@TempDir Path root) throws IOException {
        List<String> got = accesses(root, """
                package acme;
                import java.util.*;
                class Events {
                    interface Mailer { void send(String to, String body); }
                    interface Producer { void send(String what); }
                    private final Mailer mailer = null;
                    private final Producer producer = null;
                    private final List<String> list = new ArrayList<>();
                    void a() { mailer.send("ops@example.com", "hi"); producer.send("domain-thing"); list.add("x"); }
                }
                """);
        assertEquals(List.of(), got);
    }
}
