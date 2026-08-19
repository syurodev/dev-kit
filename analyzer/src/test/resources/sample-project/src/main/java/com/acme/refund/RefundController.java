package com.acme.refund;

public class RefundController {
    private RefundService refundService;

    public RefundController(RefundService refundService) {
        this.refundService = refundService;
    }

    public String create(String requestId) {
        return refundService.process(requestId);
    }
}
