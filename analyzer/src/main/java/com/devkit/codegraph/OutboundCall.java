package com.devkit.codegraph;

/**
 * One outbound HTTP call to another service, as far as the source says:
 * the edge the workspace join (docs/dev-kit/workspace.mdx § Outbound edge)
 * matches against the other side's endpoints.
 *
 * Nothing here is resolved against config or against other services — this
 * analyzer reads one project and no config. {@code url} keeps what the code
 * wrote: literals and project constants folded, a Spring-injected value as
 * its placeholder ("${billing.url}"), and "{expr}" for any part that comes
 * from runtime (a parameter, a computed value). Whoever holds the caller's
 * config and the list of services decides where it goes.
 */
public class OutboundCall {
    /** The method node the call is made from; for a Feign client, the client interface's method. */
    public String from;
    public String file;
    public int line;
    /** "feign" | "rest-template" | "web-client" | "rest-client" */
    public String client;
    /** GET, POST, ... or "ANY" when the source does not say. */
    public String httpMethod;
    /** @FeignClient's name/value — a logical service name, may hold ${...}. "" for the others. */
    public String serviceName;
    /** The URL as folded; for Feign, its url attribute joined with path and the method's route. */
    public String url;

    public OutboundCall() {}

    OutboundCall(String client, String httpMethod, String serviceName, String url, int line) {
        this.client = client;
        this.httpMethod = httpMethod;
        this.serviceName = serviceName;
        this.url = url;
        this.line = line;
    }
}
