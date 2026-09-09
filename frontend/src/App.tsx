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
        </TooltipProvider>
      </ThemeProvider>
    </QueryClientProvider>
  );
}

export default App;
