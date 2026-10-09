#!/usr/bin/env bun
// Medium-size Java and TypeScript services for timing the real analyzers.
//
//   bun scripts/codegraph-index-fixture.mjs <dir> [javaClasses=400] [tsFiles=600]
//
// <dir>/java-a and <dir>/java-b: Spring-style layers (controller → service → repository) with calls between
// classes. <dir>/ts-svc: Express-style routes over a chain of modules.
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";

const out = resolve(process.argv[2] ?? "./codegraph-index");
const javaClasses = Number(process.argv[3] ?? 400);
const tsFiles = Number(process.argv[4] ?? 600);
const put = (rel, content) => {
  const p = join(out, rel);
  mkdirSync(dirname(p), { recursive: true });
  writeFileSync(p, content);
};

function java(name) {
  put(`${name}/pom.xml`, `<?xml version="1.0"?>\n<project xmlns="http://maven.apache.org/POM/4.0.0"><modelVersion>4.0.0</modelVersion><groupId>com.acme</groupId><artifactId>${name}</artifactId><version>1.0.0</version></project>\n`);
  const per = Math.max(1, Math.floor(javaClasses / 4));
  for (let i = 0; i < per; i++) {
    const m = (kind) => `com.acme.${name.replace("-", "")}.${kind}`;
    const pkg = `com.acme.${name.replace("-", "")}`;
    const base = `${name}/src/main/java/${pkg.replaceAll(".", "/")}`;
    put(`${base}/Repo${i}.java`, `package ${pkg};\n\nimport org.springframework.stereotype.Repository;\n\n@Repository\npublic class Repo${i} {\n${Array.from({ length: 6 }, (_, k) => `    public String load${k}(String id) { return "r${i}-${k}-" + id; }\n`).join("")}}\n`);
    put(`${base}/Service${i}.java`, `package ${pkg};\n\nimport org.springframework.stereotype.Service;\n\n@Service\npublic class Service${i} {\n    private final Repo${i} repo;\n${i > 0 ? `    private final Service${i - 1} previous;\n` : ""}    public Service${i}(Repo${i} repo${i > 0 ? `, Service${i - 1} previous` : ""}) { this.repo = repo;${i > 0 ? " this.previous = previous;" : ""} }\n${Array.from({ length: 6 }, (_, k) => `    public String find${k}(String id) { return repo.load${k}(id)${i > 0 ? ` + previous.find${k}(id)` : ""}; }\n`).join("")}}\n`);
    put(`${base}/Dto${i}.java`, `package ${pkg};\n\npublic record Dto${i}(String id, int count, String name) {}\n`);
    put(`${base}/Controller${i}.java`, `package ${pkg};\n\nimport org.springframework.web.bind.annotation.*;\n\n@RestController\n@RequestMapping("/c${i}")\npublic class Controller${i} {\n    private final Service${i} service;\n    public Controller${i}(Service${i} service) { this.service = service; }\n${Array.from({ length: 4 }, (_, k) => `    @GetMapping("/g${k}/{id}")\n    public String get${k}(@PathVariable String id) { return service.find${k}(id); }\n`).join("")}}\n`);
  }
}
java("java-a");
java("java-b");

put("ts-svc/package.json", JSON.stringify({ name: "ts-svc", version: "1.0.0" }));
put("ts-svc/tsconfig.json", JSON.stringify({ compilerOptions: { target: "es2022", module: "esnext", moduleResolution: "bundler", strict: true, skipLibCheck: true }, include: ["src"] }));
for (let i = 0; i < tsFiles; i++) {
  put(`ts-svc/src/m${i}.ts`, `${i > 0 ? `import { f${i - 1} } from "./m${i - 1}";\n` : ""}export interface T${i} { id: string; n: number }\nexport function f${i}(x: number): number {\n  ${i > 0 ? `return f${i - 1}(x) + ${i};` : "return x;"}\n}\nexport class C${i} {\n  run(v: number): number { return f${i}(v); }\n}\n`);
}
put("ts-svc/src/routes.ts", `import { f${tsFiles - 1} } from "./m${tsFiles - 1}";\ninterface App { get(p: string, h: (req: unknown, res: unknown) => void): void }\nexport function register(app: App) {\n${Array.from({ length: 20 }, (_, k) => `  app.get("/api/r${k}", () => { f${tsFiles - 1}(${k}); });\n`).join("")}}\n`);
console.log(JSON.stringify({ root: out, java: javaClasses, ts: tsFiles }));
