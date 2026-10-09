import { ArrowLeft, ArrowRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { goBack, goForward } from "../fileActions";
import { useCanNavigate } from "../navigation";

/**
 * Back and Forward for the focused editor group. The same pair sits in a file's
 * toolbar and a flow's header: a group's history crosses both kinds of document.
 */
export function NavButtons() {
  const can = useCanNavigate();
  return (
    <>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button size="icon-sm" variant="ghost" aria-label="Go back" disabled={!can.back} onClick={() => void goBack()}>
            <ArrowLeft className="size-3.5" aria-hidden />
          </Button>
        </TooltipTrigger>
        <TooltipContent>Go back</TooltipContent>
      </Tooltip>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button size="icon-sm" variant="ghost" aria-label="Go forward" disabled={!can.forward} onClick={() => void goForward()}>
            <ArrowRight className="size-3.5" aria-hidden />
          </Button>
        </TooltipTrigger>
        <TooltipContent>Go forward</TooltipContent>
      </Tooltip>
    </>
  );
}
