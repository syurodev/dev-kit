package codegraph

// The clients an OutboundCall is made with.
const (
	OutboundFeign         = "feign"
	OutboundHTTPInterface = "http-interface"
	OutboundRestTemplate  = "rest-template"
	OutboundWebClient     = "web-client"
	OutboundRestClient    = "rest-client"
	OutboundRestyHTTP     = "resty-http" // OpenResty's resty.http, as a Kong plugin uses it
)

// OutboundCall is one HTTP request a service makes to another, as far as its
// source says. It is a fact about the CALLER: where the request lands is
// worked out later, against the other services (see crossservice).
//
// URL is what the code wrote with the parts the analyzer could fold joined:
// literals and constants, an injected value as "${key}", and "{expr}" for
// what only the runtime knows. It may be a full URL, or only a path when the
// client was bound to its host elsewhere.
type OutboundCall struct {
	// From is the node the call is made from.
	From string `json:"from"`
	File string `json:"file"`
	Line int    `json:"line"`
	// Client is one of the Outbound* constants.
	Client string `json:"client"`
	// HTTPMethod is GET, POST… or "ANY" when the source does not say.
	HTTPMethod string `json:"httpMethod"`
	// ServiceName is @FeignClient's logical service name; empty for the others.
	ServiceName string `json:"serviceName,omitempty"`
	URL         string `json:"url"`
}
