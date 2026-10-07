package codegraph

import "testing"

func outboundByLine(t *testing.T, result AnalysisResult) map[int]OutboundCall {
	t.Helper()
	byLine := map[int]OutboundCall{}
	for _, o := range result.OutboundCalls {
		byLine[o.Line] = o
	}
	return byLine
}

// The Kong plugin in the feedback: it calls the access service for the
// organization and API of the request.
func TestLuaRestyHTTPRequestsBecomeOutboundCalls(t *testing.T) {
	result := analyzeLua(t, map[string]string{
		"kong/plugins/fsap-api-access/handler.lua": `local http = require "resty.http"

local plugin = { PRIORITY = 900 }

function plugin:access(conf)
  local org = kong.request.get_header("x-org")
  local api = kong.router.get_service().name
  local httpc = http.new()
  local res, err = httpc:request_uri(conf.access_url .. "/v1/internal/api-access/" .. org .. "/api/" .. api, {
    method = "GET",
  })
  local other = httpc:request_uri(string.format("%s/v1/internal/api-access/%s/enable", conf.access_url, tostring(org)), {
    method = "POST",
  })
end

return plugin
`,
	})
	calls := outboundByLine(t, result)
	if len(calls) != 2 {
		t.Fatalf("outbound calls = %+v", result.OutboundCalls)
	}
	get := calls[9]
	if get.Client != OutboundRestyHTTP || get.HTTPMethod != "GET" || get.URL != "{conf.access_url}/v1/internal/api-access/{org}/api/{api}" {
		t.Fatalf("get = %+v", get)
	}
	if get.File != "kong/plugins/fsap-api-access/handler.lua" {
		t.Fatalf("file = %q", get.File)
	}
	post := calls[12]
	if post.HTTPMethod != "POST" || post.URL != "{conf.access_url}/v1/internal/api-access/{org}/enable" {
		t.Fatalf("post = %+v", post)
	}
	// Attributed to the function that makes it.
	node := nodeByLabel(t, result, "kong.plugins.fsap-api-access.handler.access")
	if get.From != node.ID {
		t.Fatalf("from = %q, want the access function %q", get.From, node.ID)
	}
}

func TestLuaRestyHTTPRequestWithAPathTable(t *testing.T) {
	result := analyzeLua(t, map[string]string{
		"lib/client.lua": `local http = require("resty.http")
local M = {}
local BASE = "/v2/ledger"
function M.entries(id)
  local c = http.new()
  c:connect("ledger", 8080)
  return c:request({ path = BASE .. "/entries/" .. id, method = ngx.HTTP_DELETE })
end
return M
`,
	})
	if len(result.OutboundCalls) != 1 {
		t.Fatalf("outbound calls = %+v", result.OutboundCalls)
	}
	call := result.OutboundCalls[0]
	if call.HTTPMethod != "DELETE" || call.URL != "/v2/ledger/entries/{id}" {
		t.Fatalf("call = %+v: a constant local and ngx.HTTP_DELETE are both seen through", call)
	}
}

func TestLuaOnlyAnHTTPClientsRequestsAreOutboundCalls(t *testing.T) {
	result := analyzeLua(t, map[string]string{
		"lib/other.lua": `local M = {}
local queue = { request_uri = function() end }
function M.a(q) return q:request_uri("/not/http") end
function M.b() return queue:request_uri("/also/not") end
function M.c(httpc) return httpc:request_uri("/unknown/origin") end
return M
`,
	})
	if len(result.OutboundCalls) != 0 {
		t.Fatalf("a method called request_uri on something that is not resty.http is not an HTTP call: %+v", result.OutboundCalls)
	}
}

func TestLuaReassignedLocalIsNotAConstant(t *testing.T) {
	result := analyzeLua(t, map[string]string{
		"lib/reassign.lua": `local http = require "resty.http"
local M = {}
function M.go(flag)
  local base = "/v1/a"
  if flag then base = "/v1/b" end
  return http.new():request_uri(base .. "/x")
end
return M
`,
	})
	if len(result.OutboundCalls) != 1 || result.OutboundCalls[0].URL != "{base}/x" {
		t.Fatalf("outbound calls = %+v: a reassigned local is a hole, not its first value", result.OutboundCalls)
	}
}

func TestLuaStringFormatPercentEscapesAndShortArgs(t *testing.T) {
	result := analyzeLua(t, map[string]string{
		"lib/format.lua": `local http = require "resty.http"
local M = {}
function M.go(id)
  return http.new():request_uri(string.format("/v1/100%%/x/%s/%d", id))
end
return M
`,
	})
	if len(result.OutboundCalls) != 1 || result.OutboundCalls[0].URL != "/v1/100%/x/{id}/{?}" {
		t.Fatalf("outbound calls = %+v", result.OutboundCalls)
	}
}

// The other half of the feedback's shared resource: the plugin READS the keys a
// Java service writes.
func TestLuaRestyRedisCommandsBecomeRedisAccesses(t *testing.T) {
	result := analyzeLua(t, map[string]string{
		"kong/plugins/fsap-api-access/handler.lua": `local redis = require "resty.redis"
local plugin = { PRIORITY = 900 }
function plugin:access(conf)
  local org = kong.request.get_header("x-org")
  local api = kong.router.get_service().name
  local red = redis:new()
  red:connect(conf.redis_host, 6379)
  local enabled = red:get("fsap:access:" .. org .. ":" .. api)
  red:setex(string.format("fsap:seen:%s", org), 60, "1")
  red:del("fsap:access:" .. org)
  local dynamic = red:get(conf.key)
  local notRedis = org:get("fsap:not:redis")
end
return plugin
`,
	})
	type use struct{ access, key, command string }
	var got []use
	for _, a := range result.RedisAccesses {
		got = append(got, use{a.Access, a.Key, a.Command})
	}
	want := []use{
		{"read", "fsap:access:{org}:{api}", "get"},
		{"write", "fsap:seen:{org}", "setex"},
		{"write", "fsap:access:{org}", "del"},
	}
	if len(got) != len(want) {
		t.Fatalf("redis accesses = %+v", result.RedisAccesses)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("access %d = %+v, want %+v (all: %+v)", i, got[i], want[i], got)
		}
	}
	node := nodeByLabel(t, result, "kong.plugins.fsap-api-access.handler.access")
	if result.RedisAccesses[0].From != node.ID || result.RedisAccesses[0].File != "kong/plugins/fsap-api-access/handler.lua" {
		t.Fatalf("access = %+v", result.RedisAccesses[0])
	}
}

func TestLuaOnlyRestyRedisCommandsAreRedis(t *testing.T) {
	result := analyzeLua(t, map[string]string{
		"lib/cache.lua": `local M = {}
local store = {}
function M.a(t) return t:get("fsap:a:b") end
function M.b() return store:set("fsap:c:d", 1) end
return M
`,
	})
	if len(result.RedisAccesses) != 0 {
		t.Fatalf("get/set on something that is not resty.redis is not Redis: %+v", result.RedisAccesses)
	}
}

// A Kong plugin that publishes an event for the Java service that consumes it.
func TestLuaRestyKafkaProducerSendIsAProduce(t *testing.T) {
	result := analyzeLua(t, map[string]string{
		"kong/plugins/fsap-api-access/handler.lua": `local producer = require "resty.kafka.producer"
local plugin = { PRIORITY = 900 }
local TOPIC = "fsap.access.denied"
function plugin:log(conf)
  local p = producer:new(conf.brokers, { producer_type = "async" })
  p:send(TOPIC, nil, "denied")
  p:send("fsap.audit." .. kong.request.get_header("x-org"), nil, "x")
  p:send(conf.topic, nil, "dynamic")
  local other = {}
  other:send("fsap.not.kafka", nil, "x")
end
return plugin
`,
	})
	var got []string
	for _, a := range result.KafkaAccesses {
		got = append(got, a.Role+" "+a.Topic+" ["+a.Client+"]")
	}
	want := []string{"produce fsap.access.denied [resty-kafka]", "produce fsap.audit.{kong.request.get_header()} [resty-kafka]"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("kafka accesses = %v, want %v", got, want)
	}
}
