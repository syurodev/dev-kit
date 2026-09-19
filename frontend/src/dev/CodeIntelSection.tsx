import { useMemo, useState } from "react";
import { java } from "@codemirror/lang-java";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import {
  codeIntel,
  type CodeIntelProvider,
  type CodeIntelRequest,
  type CodeLocation,
} from "@/components/code-editor/codeIntel";

const PATH = "src/main/java/DeviceService.java";

// Deliberately one identifier per outcome codeIntel.ts has to handle: a
// single hit navigates directly, three hits open the picker, one resolves
// to nothing, and one is wired to a provider that rejects outright.
const SAMPLE = `public class DeviceService {
    public Device single(String id) {
        return repository.find(id);
    }

    public List<Device> multiple(String id) {
        return repository.findAll(id);
    }

    public Device missing(String id) {
        return null;
    }

    public Device broken(String id) {
        return null;
    }
}
`;

/** A fake, in-memory stand-in for a real index — the gallery has no backend to ask. */
function createFakeProvider(onRequest: (request: CodeIntelRequest) => void): CodeIntelProvider {
  return {
    async definition(request) {
      onRequest(request);
      switch (request.identifier) {
        case "single":
          return [{ path: "src/main/java/DeviceRepository.java", line: 12, label: "find", kind: "method" }];
        case "multiple":
          return [
            { path: "src/main/java/DeviceRepository.java", line: 20, label: "findAll (Postgres)", kind: "method" },
            { path: "src/main/java/DeviceRepository.java", line: 45, label: "findAll (MySQL)", kind: "method" },
            { path: "src/test/java/DeviceRepositoryTest.java", line: 8, label: "findAll (stub)", kind: "method" },
          ];
        case "broken":
          throw new Error("backend unavailable");
        default:
          // Covers "missing" and every other identifier (keywords, types…):
          // the provider still answers, it just never finds anything.
          return [];
      }
    },
  };
}

/**
 * codeIntel.ts against a fake provider — the only kind the gallery can run
 * without a real backend. Exercises all four outcomes go-to-definition has
 * to handle, plus the baseline/display line split: type a few lines above
 * the sample and Cmd/Ctrl-click `single` again to see `line` stay put while
 * `displayLine` moves.
 */
export function CodeIntelSection() {
  const [source, setSource] = useState(SAMPLE);
  const [lastRequest, setLastRequest] = useState<CodeIntelRequest | null>(null);
  const [lastNavigate, setLastNavigate] = useState<{ location: CodeLocation; beside: boolean } | null>(null);

  const provider = useMemo(() => createFakeProvider(setLastRequest), []);
  const extensions = useMemo(
    () => [
      java(),
      codeIntel({
        path: PATH,
        provider,
        onNavigate: (location, options) => setLastNavigate({ location, beside: options.beside }),
      }),
    ],
    [provider],
  );

  return (
    <section id="code-intel" className="flex flex-col gap-3 px-6 py-5">
      <h2 className="text-title-16 text-text-primary">Code editor — go to definition</h2>
      <p className="text-body-13 text-text-tertiary">
        Cmd/Ctrl-click (or place the caret and press F12): <code>single</code> navigates directly,{" "}
        <code>multiple</code> opens a picker, <code>missing</code> reports nothing found, and{" "}
        <code>broken</code> reports a lookup failure. Cmd/Alt-click (or Mod-F12) for beside.
      </p>
      <div className="h-72 w-[560px] overflow-hidden rounded-md border border-border-subtle bg-surface-well">
        <CodeEditor value={source} onChange={setSource} extensions={extensions} />
      </div>
      <div className="flex flex-col gap-1 text-code-12 text-text-secondary">
        <span>
          last request:{" "}
          {lastRequest
            ? `"${lastRequest.identifier}" — line ${lastRequest.line} (baseline), displayLine ${lastRequest.displayLine} (on screen)`
            : "none yet"}
        </span>
        <span>
          last onNavigate:{" "}
          {lastNavigate
            ? `${lastNavigate.location.path}:${lastNavigate.location.line} (beside: ${String(lastNavigate.beside)})`
            : "none yet"}
        </span>
      </div>
    </section>
  );
}
