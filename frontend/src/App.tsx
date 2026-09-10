import { lazy, Suspense } from "react";
import { QueryClientProvider } from "@tanstack/react-query";

import "./app.css";
import { TooltipProvider } from "./components/ui/tooltip";
import { queryClient } from "./lib/query";
import { VaultGate } from "./workbench/VaultGate";
import { Workbench } from "./workbench/Workbench";
import { ThemeProvider } from "./theme/ThemeProvider";

// Lazy so the gallery never reaches the initial bundle the performance budget
// measures. See ComponentGallery for why a browser-reachable route exists at
// all: VaultGate needs the Wails bridge, so without this every shared component
// is unreachable outside the desktop app.
const ComponentGallery = lazy(() => import("./dev/ComponentGallery"));

// Lazy for the same reason: sonner is 36 KiB of the shell's initial JS for a
// surface nothing can raise in the first frames. Mounting it behind Suspense
// lets it arrive a tick after the workbench, which is still long before the
// first toast.
const Toaster = lazy(() =>
  import("./components/ui/sonner").then((m) => ({ default: m.Toaster })),
);

function isGalleryRequested(): boolean {
  if (typeof window === "undefined") {
    return false;
  }
  return new URLSearchParams(window.location.search).has("gallery");
}

function App() {
  if (isGalleryRequested()) {
    return (
      <QueryClientProvider client={queryClient}>
        <ThemeProvider>
          <TooltipProvider>
            <Suspense fallback={null}>
              <ComponentGallery />
            </Suspense>
          </TooltipProvider>
        </ThemeProvider>
      </QueryClientProvider>
    );
  }

  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <TooltipProvider>
          <VaultGate>
            <Workbench />
          </VaultGate>
          {/* sonner renders nothing without a Toaster in the tree, and there
              was none: every toast.error in Notes (save failure, tag limits,
              oversized image) has been going nowhere. Outside VaultGate so a
              toast raised while the vault is locked still has somewhere to
              land. Motion is sonner's own — the catalogue asks only that the
              numbers agree, not for a second implementation. */}
          <Suspense fallback={null}>
            <Toaster />
          </Suspense>
        </TooltipProvider>
      </ThemeProvider>
    </QueryClientProvider>
  );
}

export default App;
