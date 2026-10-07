package com.devkit.codegraph;

/**
 * One place that reads or writes a Redis key, as far as the source says.
 *
 * A key is a pattern: what the code wrote, with the parts only the runtime knows
 * as {@code {expr}} holes — {@code fsap:access:{org}:{api}}. Two services share
 * a key when their patterns can name the same string, which is what the
 * workspace join asks (see crossservice).
 */
public class RedisAccess {
    /** The method that makes the call. */
    public String from;
    /** The key pattern. */
    public String key;
    /** "read" | "write" (a delete is a write). */
    public String access;
    /** The Redis command or the template method: get, set, hset, delete, opsForValue().set… */
    public String command = "";
    public String file;
    public int line;

    public RedisAccess() {}
}
