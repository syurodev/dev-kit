package com.acme.tx;

public class TransactionController {
    private TransactionQueryService transactionQueryService;

    // Every argument is a record accessor, which this JavaParser cannot
    // resolve — the case that used to take the whole call down with it.
    public Page<String> getTransactions(TransactionQuery request) {
        return transactionQueryService
                .queryPaged(request.filters(), request.limit())
                .map(s -> s);
    }
}
