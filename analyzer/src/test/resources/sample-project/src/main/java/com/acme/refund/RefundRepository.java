package com.acme.refund;

public class RefundRepository {
    public String save(String requestId) {
        return "saved:" + requestId;
    }
}
