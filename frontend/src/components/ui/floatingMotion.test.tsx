import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { Dialog, DialogContent } from "./dialog";
import { AlertDialog, AlertDialogContent } from "./alert-dialog";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "./tooltip";
import { Popover, PopoverContent, PopoverTrigger } from "./popover";

describe("floatingMotion", () => {
  it("DialogContent carries animate-in and motion token classes", () => {
    const html = renderToString(
      <Dialog open>
        <DialogContent portal={false}>
          <div>Dialog Content</div>
        </DialogContent>
      </Dialog>,
    );
    expect(html).toContain("data-[state=open]:animate-in");
    expect(html).toContain("duration-(--motion-base)");
    expect(html).toContain("data-[state=closed]:duration-(--motion-fast)");
  });

  it("AlertDialogContent carries animate-in and motion token classes", () => {
    const html = renderToString(
      <AlertDialog open>
        <AlertDialogContent portal={false}>
          <div>Alert Dialog Content</div>
        </AlertDialogContent>
      </AlertDialog>,
    );
    expect(html).toContain("data-[state=open]:animate-in");
    expect(html).toContain("duration-(--motion-base)");
    expect(html).toContain("data-[state=closed]:duration-(--motion-fast)");
  });

  it("TooltipContent carries animate-in and motion token classes", () => {
    const html = renderToString(
      <TooltipProvider>
        <Tooltip open>
          <TooltipTrigger>Hover me</TooltipTrigger>
          <TooltipContent portal={false}>Tooltip text</TooltipContent>
        </Tooltip>
      </TooltipProvider>,
    );
    expect(html).toContain("data-[state=instant-open]:animate-in");
    expect(html).toContain("data-[state=delayed-open]:animate-in");
    expect(html).toContain("duration-(--motion-base)");
    expect(html).toContain("data-[state=closed]:duration-(--motion-fast)");
  });

  it("PopoverContent carries animate-in and motion token classes", () => {
    const html = renderToString(
      <Popover open>
        <PopoverTrigger>Open popover</PopoverTrigger>
        <PopoverContent portal={false}>Popover text</PopoverContent>
      </Popover>,
    );
    expect(html).toContain("data-[state=open]:animate-in");
    expect(html).toContain("duration-(--motion-base)");
    expect(html).toContain("data-[state=closed]:duration-(--motion-fast)");
  });
});
