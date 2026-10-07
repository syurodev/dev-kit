package com.devkit.codegraph;

/**
 * One place the code reads a property, or switches on one.
 *
 * {@code kind} says how: {@code value} (@Value), {@code properties}
 * (@ConfigurationProperties binding), {@code condition} (@ConditionalOnProperty
 * and friends that name a key), {@code environment} (Environment.getProperty),
 * {@code env-var} (System.getenv), {@code profile} (@Profile), and
 * {@code bean-condition} (@ConditionalOnMissingBean and the like — a switch with
 * no key, which decides whether a bean exists at all).
 */
public class ConfigUsage {
    /** The property as the code names it; "" for a profile or a bean-condition. */
    public String key = "";
    public String kind;
    /** The node that holds the use: the class (a field, a binding, a gated bean) or the method. */
    public String from;
    /** The field or parameter the value lands in; "" when none. */
    public String field = "";
    public String file;
    public int line;
    /** The default written beside the key — {@code ${a.b:false}} gives "false". */
    public String defaultValue = "";
    /** What else the source says: havingValue/matchIfMissing, profile names, the condition's target. */
    public String detail = "";

    public ConfigUsage() {}

    ConfigUsage(String kind, String key, String from, String field, String file, int line) {
        this.kind = kind;
        this.key = key;
        this.from = from;
        this.field = field;
        this.file = file;
        this.line = line;
    }
}
