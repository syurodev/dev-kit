package com.acme.refund;

@RestController
@RequestMapping("/refunds")
public class RefundController {
    private RefundService refundService;

    public RefundController(RefundService refundService) {
        this.refundService = refundService;
    }

    @PostMapping
    public String create(String requestId) {
        return refundService.process(requestId);
    }
}
