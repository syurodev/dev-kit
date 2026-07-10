import "./app.css";
import { VaultGate } from "./workbench/VaultGate";
import { Workbench } from "./workbench/Workbench";

function App() {
  return (
    <VaultGate>
      <Workbench />
    </VaultGate>
  );
}

export default App;
