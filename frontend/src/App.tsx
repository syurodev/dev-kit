import "./app.css";
import { TooltipProvider } from "./components/ui/tooltip";
import { VaultGate } from "./workbench/VaultGate";
import { Workbench } from "./workbench/Workbench";
import { ThemeProvider } from "./theme/ThemeProvider";

function App() {
  return (
    <ThemeProvider>
      <TooltipProvider>
        <VaultGate>
          <Workbench />
        </VaultGate>
      </TooltipProvider>
    </ThemeProvider>
  );
}

export default App;
