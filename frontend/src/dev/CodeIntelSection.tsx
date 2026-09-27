import { useMemo, useState } from "react";
import { java } from "@codemirror/lang-java";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import {
  codeIntel,
  type CodeIntelCapability,
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

const REFERENCE_FILES = [
  PATH,
  "src/main/java/DeviceController.java",
  "src/main/java/DeviceRepository.java",
  "src/test/java/DeviceServiceTest.java",
];

// Realistic-looking call sites, cycled across a handful of shapes so the
// preview column in a 25-row picker doesn't read as the same line repeated —
// each still contains the identifier as a whole word, which is what
// splitPreviewSegments needs to emphasise it.
const REFERENCE_SHAPES: Array<(identifier: string) => string> = [
  (id) => `        return repository.${id}(id);`,
  (id) => `    List<Device> devices = service.${id}(id);`,
  (id) => `        assertThat(service.${id}(id)).isNotEmpty();`,
  (id) => `        for (Device d : service.${id}(id)) {`,
  (id) => `        Optional.ofNullable(service.${id}(id)).ifPresent(this::publish);`,
];

function fileLabel(path: string): string {
  return path.slice(path.lastIndexOf("/") + 1).replace(/\.java$/, "");
}

function makeReferences(count: number, identifier: string): CodeLocation[] {
  return Array.from({ length: count }, (_, index) => {
    const path = REFERENCE_FILES[index % REFERENCE_FILES.length];
    const shape = REFERENCE_SHAPES[index % REFERENCE_SHAPES.length];
    return {
      path,
      // Small and cycling on purpose: a couple of these land on `path ===
      // PATH` (the sample's own file, shown as "this file"), and that file
      // is only ~16 lines — a line number past its end would just clamp to
      // the last line (currentLineAt's job for a genuinely stale index), not
      // show something realistic.
      line: 3 + (index % 6),
      label: fileLabel(path),
      preview: shape(identifier),
    };
  });
}

/** A fake, in-memory stand-in for a real index — the gallery has no backend to ask. */
function createFakeProvider(onRequest: (request: CodeIntelRequest, capability: CodeIntelCapability) => void): CodeIntelProvider {
  return {
    async definition(request) {
      onRequest(request, "definition");
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
    async implementation(request) {
      onRequest(request, "implementation");
      switch (request.identifier) {
        case "single":
          return [{ path: "src/main/java/InMemoryDeviceRepository.java", line: 9, label: "find", kind: "method" }];
        case "multiple":
          return [
            { path: "src/main/java/PostgresDeviceRepository.java", line: 31, label: "findAll", kind: "method" },
            { path: "src/main/java/MySqlDeviceRepository.java", line: 28, label: "findAll", kind: "method" },
            { path: "src/main/java/InMemoryDeviceRepository.java", line: 14, label: "findAll", kind: "method" },
          ];
        case "broken":
          throw new Error("backend unavailable");
        default:
          return [];
      }
    },
    async references(request) {
      onRequest(request, "references");
      switch (request.identifier) {
        case "single":
          return [
            {
              path: "src/main/java/DeviceController.java",
              line: 22,
              label: "DeviceController",
              preview: "        Device d = service.single(id);",
            },
          ];
        case "multiple":
          return makeReferences(25, "multiple");
        case "broken":
          throw new Error("backend unavailable");
        default:
          return [];
      }
    },
  };
}

/** References-only, no Cut (a read-only buffer): the right-click menu's other end of the spectrum. */
function createReferencesOnlyProvider(onRequest: (request: CodeIntelRequest, capability: CodeIntelCapability) => void): CodeIntelProvider {
  return {
    async references(request) {
      onRequest(request, "references");
      return [
        {
          path: "src/main/java/DeviceService.java",
          line: 3,
          label: "DeviceService",
          preview: "        return repository.find(id);",
        },
        {
          path: "src/main/java/DeviceController.java",
          line: 22,
          label: "DeviceController",
          preview: "        Device d = repository.find(id);",
        },
      ];
    },
  };
}

const READONLY_PATH = "src/main/java/DeviceRepository.java";
const READONLY_SAMPLE = `public interface DeviceRepository {
    Device find(String id);
}
`;

/**
 * codeIntel.ts against a fake provider — the only kind the gallery can run
 * without a real backend. Exercises every outcome the shared lookup pipeline
 * has to handle (navigate / picker / message / failure, across all three
 * capabilities), the baseline/display line split, and the right-click menu.
 */
export function CodeIntelSection() {
  const [source, setSource] = useState(SAMPLE);
  const [lastRequest, setLastRequest] = useState<{ request: CodeIntelRequest; capability: CodeIntelCapability } | null>(
    null,
  );
  const [lastNavigate, setLastNavigate] = useState<{ location: CodeLocation; beside: boolean } | null>(null);

  const provider = useMemo(
    () => createFakeProvider((request, capability) => setLastRequest({ request, capability })),
    [],
  );
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

  const [readOnlySource] = useState(READONLY_SAMPLE);
  const readOnlyProvider = useMemo(
    () => createReferencesOnlyProvider((request, capability) => setLastRequest({ request, capability })),
    [],
  );
  const readOnlyExtensions = useMemo(
    () => [
      java(),
      codeIntel({
        path: READONLY_PATH,
        provider: readOnlyProvider,
        onNavigate: (location, options) => setLastNavigate({ location, beside: options.beside }),
      }),
    ],
    [readOnlyProvider],
  );

  return (
    <section id="code-intel" className="flex flex-col gap-3 px-6 py-5">
      <h2 className="text-title-16 text-text-primary">Code editor — go to definition, implementations, references</h2>
      <p className="text-body-13 text-text-tertiary">
        Place the caret on an identifier: <code>F12</code> definition, <code>Mod-F12</code> implementations,{" "}
        <code>Shift-F12</code> references, <code>Mod-Alt-F12</code> definition beside. <code>single</code> navigates
        directly for definition/implementations (references always opens a list), <code>multiple</code> opens a
        picker — try it on <code>multiple</code> for a 25-row references list — <code>missing</code> reports nothing
        found, and <code>broken</code> reports a lookup failure. Cmd/Ctrl-click (or Cmd/Ctrl-Alt-click for beside)
        does the same for definition. Right-click an identifier for a context menu with the same actions, plus Cut
        and Copy.
      </p>
      <div className="h-72 w-140 overflow-hidden rounded-md border border-border-subtle bg-surface-well">
        <CodeEditor value={source} onChange={setSource} extensions={extensions} />
      </div>
      <div className="flex flex-col gap-1 text-code-12 text-text-secondary">
        <span>
          last request:{" "}
          {lastRequest
            ? `${lastRequest.capability} "${lastRequest.request.identifier}" — line ${lastRequest.request.line} (baseline), displayLine ${lastRequest.request.displayLine} (on screen)`
            : "none yet"}
        </span>
        <span>
          last onNavigate:{" "}
          {lastNavigate
            ? `${lastNavigate.location.path}:${lastNavigate.location.line} (beside: ${String(lastNavigate.beside)})`
            : "none yet"}
        </span>
      </div>

      <h3 className="text-title-14 text-text-primary">Read-only, references-only provider</h3>
      <p className="text-body-13 text-text-tertiary">
        This buffer is read-only and its provider only implements <code>references</code>: right-click{" "}
        <code>find</code> and the menu has just Find References and Copy — no definition rows, no Cut.
      </p>
      <div className="h-32 w-140 overflow-hidden rounded-md border border-border-subtle bg-surface-well">
        <CodeEditor value={readOnlySource} onChange={() => {}} extensions={readOnlyExtensions} readOnly />
      </div>
    </section>
  );
}
