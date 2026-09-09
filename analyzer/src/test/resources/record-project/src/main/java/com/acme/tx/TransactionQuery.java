package com.acme.tx;

import java.util.List;

/** A record, as a Spring @RequestBody usually is. */
public record TransactionQuery(List<String> filters, int limit) {}
