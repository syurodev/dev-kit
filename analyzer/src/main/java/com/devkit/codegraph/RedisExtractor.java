package com.devkit.codegraph;

import com.github.javaparser.ast.expr.Expression;
import com.github.javaparser.ast.expr.FieldAccessExpr;
import com.github.javaparser.ast.expr.MethodCallExpr;
import com.github.javaparser.ast.expr.NameExpr;
import com.github.javaparser.ast.expr.ThisExpr;

import java.util.Optional;
import java.util.Set;

/**
 * The Redis keys a call reads or writes — Spring Data Redis, Jedis, Lettuce.
 *
 * Like the HTTP clients, recognised by what the receiver is, not by a method
 * name: {@code get} is on every map. A receiver is Redis when it is
 * {@code x.opsForValue()} and its siblings (a strong signal on its own), a name
 * declared as a template, an operations interface or a client, or a command
 * object — read from the declaration in the source, so no solver is needed.
 *
 * The key is the first argument, folded: literals and constants joined, a
 * parameter or a call a {@code {hole}}. {@code "fsap:access:" + org} is
 * {@code fsap:access:{org}}.
 */
final class RedisExtractor {

    /** What a Spring Data operations accessor hands back, so a call chained on it is on that Redis structure. */
    private static final Set<String> OPS_ACCESSORS = Set.of(
            "opsForValue", "opsForHash", "opsForList", "opsForSet", "opsForZSet", "opsForStream",
            "opsForGeo", "opsForHyperLogLog", "opsForCluster", "boundValueOps", "boundHashOps",
            "boundListOps", "boundSetOps", "boundZSetOps", "boundStreamOps", "boundGeoOps");

    /** Declared types whose methods are Redis commands. */
    private static final Set<String> REDIS_TYPES = Set.of(
            "RedisTemplate", "StringRedisTemplate", "ReactiveRedisTemplate", "ReactiveStringRedisTemplate",
            "RedisOperations", "ReactiveRedisOperations",
            "ValueOperations", "HashOperations", "ListOperations", "SetOperations", "ZSetOperations",
            "StreamOperations", "GeoOperations", "HyperLogLogOperations", "BoundValueOperations",
            "ReactiveValueOperations", "ReactiveHashOperations", "ReactiveListOperations",
            "ReactiveSetOperations", "ReactiveZSetOperations",
            "Jedis", "UnifiedJedis", "JedisCluster", "Pipeline",
            "RedisCommands", "RedisAsyncCommands", "RedisReactiveCommands", "RedisClusterCommands",
            "RedisClusterAsyncCommands", "RedisClusterReactiveCommands", "RedisStringCommands");

    private static final Set<String> READS = Set.of(
            "get", "mget", "multiget", "getAll", "getRange", "getAndExpire", "strlen", "size", "length",
            "hget", "hmget", "hgetall", "hgetAll", "hkeys", "hvals", "hexists", "hlen", "hasKey", "entries", "keys", "values",
            "exists", "ttl", "pttl", "getExpire", "type", "scan", "randomKey",
            "lrange", "lindex", "llen", "range", "index",
            "smembers", "members", "sismember", "isMember", "scard", "sdiff", "sunion", "sinter", "randomMember",
            "zrange", "zrangeByScore", "zrangeWithScores", "zscore", "score", "zcard", "zcount", "zrank", "rank",
            "rangeByScore", "reverseRange", "reverseRangeByScore", "count", "bitcount", "getbit", "getBit");
    private static final Set<String> WRITES = Set.of(
            "set", "setex", "setnx", "psetex", "setIfAbsent", "setIfPresent", "multiSet", "mset", "msetnx", "append",
            "getset", "getSet", "getAndSet", "getdel", "getAndDelete", "incr", "incrBy", "incrby", "increment", "decr", "decrBy", "decrby",
            "decrement", "setrange", "setbit", "setBit",
            "hset", "hmset", "hsetnx", "hincrby", "hincrBy", "hincrbyfloat", "put", "putAll", "putIfAbsent",
            "lpush", "rpush", "lpushx", "rpushx", "leftPush", "rightPush", "leftPushAll", "rightPushAll", "lset", "linsert", "ltrim", "trim",
            "sadd", "add", "addAll", "smove", "move", "sdiffstore", "sunionstore", "sinterstore",
            "zadd", "zincrby", "incrementScore", "zremrangebyscore", "removeRangeByScore",
            "expire", "pexpire", "expireAt", "pexpireAt", "persist", "rename", "renamenx", "renameIfAbsent", "restore", "unlink", "touch",
            // Deletes and removals are writes: they change what the next reader sees.
            "del", "delete", "hdel", "srem", "zrem", "remove", "lpop", "rpop", "leftPop", "rightPop", "pop", "lrem", "flushdb", "flushall");

    private RedisExtractor() {}

    /** A Redis access this call makes, if it makes one. */
    static Optional<RedisAccess> access(MethodCallExpr call, ConstantResolver constants) {
        String command = call.getNameAsString();
        boolean read = READS.contains(command);
        boolean write = WRITES.contains(command);
        if ((!read && !write) || call.getArguments().isEmpty() || call.getScope().isEmpty() || !onRedis(call.getScope().get())) {
            return Optional.empty();
        }
        String key = constants.resolve(call.getArgument(0));
        // A key that is nothing but a hole — a variable the resolver could not follow — names
        // nothing two services could share, and listing it would only add a "{key}" that
        // overlaps every other key.
        if (key.replaceAll("\\{[^{}]*}", "").isEmpty()) {
            return Optional.empty();
        }
        RedisAccess access = new RedisAccess();
        access.key = key;
        access.access = write ? "write" : "read";
        access.command = command;
        access.line = call.getBegin().map(p -> p.line).orElse(0);
        return Optional.of(access);
    }

    /** True when the receiver is a Redis template, operations object, client or command object. */
    private static boolean onRedis(Expression receiver) {
        // redisTemplate.opsForValue().get(k): the accessor in the chain says it.
        Expression e = receiver;
        while (e instanceof MethodCallExpr m) {
            if (OPS_ACCESSORS.contains(m.getNameAsString())) {
                return true;
            }
            if (m.getScope().isEmpty()) {
                break;
            }
            e = m.getScope().get();
        }
        // A name declared as one of the Redis types.
        if (receiver instanceof NameExpr || (receiver instanceof FieldAccessExpr access && access.getScope() instanceof ThisExpr)) {
            String name = receiver instanceof NameExpr n ? n.getNameAsString() : ((FieldAccessExpr) receiver).getNameAsString();
            Optional<OutboundCallExtractor.Declared> declared =
                    OutboundCallExtractor.declaration(receiver, name, !(receiver instanceof NameExpr));
            return declared.isPresent() && REDIS_TYPES.contains(OutboundCallExtractor.simpleName(declared.get().type()));
        }
        return false;
    }
}
