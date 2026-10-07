package com.devkit.codegraph;

/**
 * One place that produces to or consumes from a Kafka topic, as far as the
 * source says.
 *
 * {@code topic} is what the code wrote: a literal, a project constant, or a
 * property placeholder ({@code ${app.kafka.topic.access}}) that only the
 * service's own config can fold — so it stays a placeholder here, as outbound
 * URLs do, and the join resolves it against the config of the service that
 * wrote it.
 */
public class KafkaAccess {
    /** The method that does it; for a class-level @KafkaListener, the class. */
    public String from;
    /** The topic, a topic pattern ({@link #pattern}), or a name with {hole}s. */
    public String topic;
    /** "produce" | "consume". */
    public String role;
    /** "kafka-template" | "kafka-producer" | "kafka-listener" | "kafka-consumer" | "kafka-streams". */
    public String client;
    /** A consumer's group id, as written; "" when none is visible. */
    public String groupId = "";
    /** True when {@link #topic} is a regular expression, a {@code topicPattern}. */
    public boolean pattern;
    public String file;
    public int line;

    public KafkaAccess() {}
}
