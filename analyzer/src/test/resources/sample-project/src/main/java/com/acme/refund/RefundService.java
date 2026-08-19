package com.acme.refund;

public class RefundService {
    private RefundRepository refundRepository;

    public RefundService(RefundRepository refundRepository) {
        this.refundRepository = refundRepository;
    }

    public String process(String requestId) {
        return refundRepository.save(requestId);
    }
}
