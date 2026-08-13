import "./app.css";
import { TooltipProvider } from "./components/ui/tooltip";
import { VaultGate } from "./workbench/VaultGate";
import { Workbench } from "./workbench/Workbench";

function App() {
  return (
    <TooltipProvider>
      <VaultGate>
        <Workbench />
      </VaultGate>
    </TooltipProvider>
  );
}

export default App;
