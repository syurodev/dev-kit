package com.devkit.codegraph;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.stream.Collectors;

import static org.junit.jupiter.api.Assertions.*;

class RedisTest {

    private static List<String> accesses(Path root, String source) throws IOException {
        Path file = root.resolve("src/main/java/acme/AccessCache.java");
        Files.createDirectories(file.getParent());
        Files.writeString(file, source);
        AnalysisResult r = new ProjectAnalyzer(root, 1).analyze();
        return r.redisAccesses.stream()
                .map(a -> a.from.replaceAll("^acme\\.AccessCache\\.", "").replaceAll("\\(.*", "") + " " + a.access + " " + a.key + " [" + a.command + "]")
                .sorted().collect(Collectors.toList());
    }

    @Test
    void springDataRedisThroughTheOperationsAccessors(@TempDir Path root) throws IOException {
        List<String> got = accesses(root, """
                package acme;
                import org.springframework.data.redis.core.*;
                class AccessCache {
                    static final String PREFIX = "fsap:access:";
                    private final StringRedisTemplate redis = null;
                    private final RedisTemplate<String, String> template = null;
                    void put(String org, String api) { redis.opsForValue().set(PREFIX + org + ":" + api, "1"); }
                    String find(String org) { return redis.opsForValue().get(PREFIX + org); }
                    void drop(String org) { redis.delete(PREFIX + org); }
                    boolean has(String org) { return template.hasKey("fsap:enabled:" + org); }
                    void fields(String org) { redis.opsForHash().put("fsap:profile:" + org, "name", "x"); redis.opsForHash().get("fsap:profile:" + org, "name"); }
                    void viaLocal(String org) {
                        ValueOperations<String, String> ops = redis.opsForValue();
                        ops.set("fsap:local:" + org, "v");
                    }
                }
                """);
        assertEquals(List.of(
                "drop write fsap:access:{org} [delete]",
                "fields read fsap:profile:{org} [get]",
                "fields write fsap:profile:{org} [put]",
                "find read fsap:access:{org} [get]",
                "has read fsap:enabled:{org} [hasKey]",
                "put write fsap:access:{org}:{api} [set]",
                "viaLocal write fsap:local:{org} [set]"), got);
    }

    @Test
    void jedisAndLettuceCommandObjects(@TempDir Path root) throws IOException {
        List<String> got = accesses(root, """
                package acme;
                class AccessCache {
                    private final Jedis jedis = null;
                    private final RedisCommands<String, String> commands = null;
                    void a(String id) { jedis.setex("session:" + id, 60, "x"); jedis.get("session:" + id); jedis.del("session:" + id); }
                    void b(String id) { commands.hset("user:" + id, "n", "v"); commands.hgetall("user:" + id); }
                    interface Jedis { String get(String k); String setex(String k, int s, String v); long del(String k); }
                    interface RedisCommands<K, V> { boolean hset(K k, K f, V v); Object hgetall(K k); }
                }
                """);
        assertEquals(List.of(
                "a read session:{id} [get]",
                "a write session:{id} [del]",
                "a write session:{id} [setex]",
                "b read user:{id} [hgetall]",
                "b write user:{id} [hset]"), got);
    }

    @Test
    void aMapOrAnythingElseWithTheSameMethodNamesIsNotRedis(@TempDir Path root) throws IOException {
        List<String> got = accesses(root, """
                package acme;
                import java.util.*;
                class AccessCache {
                    private final Map<String, String> map = new HashMap<>();
                    private final List<String> list = new ArrayList<>();
                    private final Cache cache = null;
                    interface Cache { String get(String k); void put(String k, String v); void delete(String k); }
                    void a(String id) { map.get("fsap:access:" + id); map.put("x:y", "z"); list.remove("a:b"); cache.get("fsap:access:" + id); cache.delete("k:" + id); }
                }
                """);
        assertEquals(List.of(), got);
    }

    @Test
    void aKeyThatIsOnlyAHoleNamesNothingToShare(@TempDir Path root) throws IOException {
        List<String> got = accesses(root, """
                package acme;
                import org.springframework.data.redis.core.*;
                class AccessCache {
                    private final StringRedisTemplate redis = null;
                    String a(String key) { return redis.opsForValue().get(key); }
                    String b(String org) { return redis.opsForValue().get("fsap:" + org); }
                }
                """);
        assertEquals(List.of("b read fsap:{org} [get]"), got);
    }
}
