import { describe, expect, it } from "bun:test";
import { inferPlanLayer } from "./planLayer";

describe("inferPlanLayer", () => {
  it("reads the class name, not the package", () => {
    // A class in a .service. package is not a Service unless it says so —
    // the real classifier reads the annotation on the type.
    expect(inferPlanLayer("com.synx.devkit.service.OrderRepository")).toBe("repository");
    expect(inferPlanLayer("com.synx.devkit.repository.OrderService")).toBe("service");
  });

  it("classifies the common suffixes", () => {
    expect(inferPlanLayer("com.a.DeviceController")).toBe("controller");
    expect(inferPlanLayer("com.a.ListDevicesUseCase")).toBe("service");
    expect(inferPlanLayer("com.a.DeviceDao")).toBe("repository");
  });

  it("falls back to unknown, the same value the analyzer gives an unannotated class", () => {
    expect(inferPlanLayer("com.a.SyncIdentifier")).toBe("unknown");
    expect(inferPlanLayer("")).toBe("unknown");
  });

  it("uses the last capitalised segment, so a method FQN classifies by its class", () => {
    // Method FQNs look like com.a.OrderService.process — the trailing segment
    // is lowercase, so the class name is what must be matched.
    expect(inferPlanLayer("com.a.OrderService.process")).toBe("service");
    expect(inferPlanLayer("com.a.DeviceController.list")).toBe("controller");
  });

  it("handles a nested class without tripping on the outer name", () => {
    expect(inferPlanLayer("com.a.Outer.InnerRepository")).toBe("repository");
  });
});
