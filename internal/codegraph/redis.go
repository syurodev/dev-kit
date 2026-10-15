package codegraph

// RedisAccess is one place that reads or writes a Redis key, as far as the
// source says. Key is a pattern: what the code wrote, with each part only the
// runtime knows as an {expr} hole — fsap:access:{org}. Two services share a key
// when their patterns can name the same string (see crossservice).
type RedisAccess struct {
	// From is the method that makes the call.
	From string `json:"from"`
	Key  string `json:"key"`
	// Access is "read" or "write"; a delete is a write.
	Access string `json:"access"`
	// Command is the Redis command or the template method: get, hset, delete…
	Command string `json:"command,omitempty"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	// Col is the column of the call it was seen at (the call's name); 0 when
	// it was read off a declaration, or by an analyzer that does not say.
	Col int `json:"col,omitempty"`
}

// redisReadCommands and redisWriteCommands are the Redis commands a Lua client
// (resty.redis) calls as methods, lower-case as Redis names them. A delete is
// a write: it changes what the next reader sees.
var redisReadCommands = map[string]bool{
	"get": true, "mget": true, "getrange": true, "strlen": true,
	"hget": true, "hmget": true, "hgetall": true, "hkeys": true, "hvals": true, "hexists": true, "hlen": true,
	"exists": true, "ttl": true, "pttl": true, "type": true, "keys": true, "scan": true,
	"lrange": true, "lindex": true, "llen": true,
	"smembers": true, "sismember": true, "scard": true, "srandmember": true,
	"zrange": true, "zrangebyscore": true, "zrevrange": true, "zscore": true, "zcard": true, "zcount": true, "zrank": true,
	"bitcount": true, "getbit": true,
}

var redisWriteCommands = map[string]bool{
	"set": true, "setex": true, "setnx": true, "psetex": true, "mset": true, "msetnx": true, "append": true,
	"getset": true, "incr": true, "incrby": true, "decr": true, "decrby": true, "setrange": true, "setbit": true,
	"hset": true, "hmset": true, "hsetnx": true, "hincrby": true,
	"lpush": true, "rpush": true, "lpushx": true, "rpushx": true, "lset": true, "ltrim": true, "linsert": true,
	"sadd": true, "smove": true, "zadd": true, "zincrby": true,
	"expire": true, "pexpire": true, "expireat": true, "persist": true, "rename": true, "unlink": true, "touch": true,
	"del": true, "hdel": true, "srem": true, "zrem": true, "lpop": true, "rpop": true, "lrem": true, "flushdb": true,
}
