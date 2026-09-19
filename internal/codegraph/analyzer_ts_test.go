// internal/codegraph/analyzer_ts_test.go
package codegraph

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tsgoPath finds the language server the TypeScript analyzer drives, or skips.
//
// Built rather than committed (it is 28 MB), so a checkout that has not run
// the build has no way to run these — which is the same deal the Java
// analyzer's end-to-end tests have with its jar.
func tsgoPath(t *testing.T) string {
	t.Helper()
	if override := os.Getenv(AnalyzerOverrideEnv("typescript")); override != "" {
		return override
	}
	candidate, err := filepath.Abs(filepath.Join("..", "..", "analyzer-ts", "bin", tsgoBinary))
	if err != nil {
		t.Skip("cannot resolve the analyzer path")
	}
	if _, err := os.Stat(candidate); err != nil {
		t.Skipf("no TypeScript analyzer at %s — build it with: cd analyzer-ts && go build -o bin/%s github.com/microsoft/typescript-go/cmd/tsgo", candidate, tsgoBinary)
	}
	return candidate
}

func writeTSProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
}

func analyzeTS(t *testing.T, files map[string]string) AnalysisResult {
	t.Helper()
	server := tsgoPath(t)
	root := writeTSProject(t, files)

	analyzer := NewTypeScriptAnalyzer(func() ([]string, []string, error) {
		return []string{server, "--lsp", "-stdio"}, os.Environ(), nil
	})
	t.Cleanup(func() {
		if closer, ok := analyzer.(analyzerCloser); ok {
			_ = closer.CloseAnalyzer()
		}
	})

	result, err := analyzer.Analyze(context.Background(), root)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("the analyzer produced an inconsistent index: %v", err)
	}
	return result
}

func tsconfig() string {
	config, _ := json.MarshalIndent(map[string]any{
		"compilerOptions": map[string]any{
			"module": "commonjs", "target": "ES2021", "strict": true,
			"experimentalDecorators": true, "emitDecoratorMetadata": true,
			"esModuleInterop": true, "skipLibCheck": true,
		},
		"include": []string{"src/**/*"},
	}, "", "  ")
	return string(config)
}

// nestFixture is the shape the work was asked for: a controller, the service
// it injects, the repository that service injects, and a module wiring them.
var nestFixture = map[string]string{
	"tsconfig.json": tsconfig(),
	"package.json": `{
  "name": "api",
  "dependencies": { "@nestjs/common": "^10.0.0", "@nestjs/core": "^10.0.0", "typeorm": "^0.3.0" }
}`,
	"src/users/user.entity.ts": `
export class UserEntity {
  id!: number;
  name!: string;
}
`,
	"src/users/dto/create-user.dto.ts": `
export class CreateUserDto {
  name!: string;
}
`,
	"src/users/users.repository.ts": `
import { Injectable } from '@nestjs/common';
import { Repository } from 'typeorm';
import { UserEntity } from './user.entity';

/** Stores users. */
@Injectable()
export class UsersRepository {
  private cached = 0;

  constructor(private readonly repo: Repository<UserEntity>) {}

  async findAll(): Promise<UserEntity[]> {
    this.cached = this.cached + 1;
    return this.repo.find();
  }

  async findById(id: number): Promise<UserEntity | null> {
    return this.repo.findOne({ where: { id } });
  }
}
`,
	"src/users/users.service.ts": `
import { Injectable, NotFoundException } from '@nestjs/common';
import { UsersRepository } from './users.repository';
import { UserEntity } from './user.entity';
import { CreateUserDto } from './dto/create-user.dto';

/** Business rules for users. */
@Injectable()
export class UsersService {
  constructor(private readonly repository: UsersRepository) {}

  async list(): Promise<UserEntity[]> {
    return this.repository.findAll();
  }

  async get(id: number): Promise<UserEntity> {
    const found = await this.repository.findById(id);
    if (!found) {
      throw new NotFoundException('no such user');
    }
    return found;
  }

  async create(dto: CreateUserDto): Promise<UserEntity> {
    const entity = new UserEntity();
    entity.name = dto.name;
    return entity;
  }
}
`,
	"src/users/users.controller.ts": `
import { Controller, Get, Post, Param, Body } from '@nestjs/common';
import { UsersService } from './users.service';
import { CreateUserDto } from './dto/create-user.dto';

/** HTTP surface for users. */
@Controller('users')
export class UsersController {
  constructor(private readonly service: UsersService) {}

  @Get()
  async list() {
    return this.service.list();
  }

  @Get(':id')
  async get(@Param('id') id: string) {
    return this.service.get(Number(id));
  }

  @Post()
  async create(@Body() dto: CreateUserDto) {
    return this.service.create(dto);
  }
}
`,
	"src/users/users.module.ts": `
import { Module } from '@nestjs/common';
import { UsersController } from './users.controller';
import { UsersService } from './users.service';
import { UsersRepository } from './users.repository';

@Module({
  controllers: [UsersController],
  providers: [UsersService, UsersRepository],
  exports: [UsersService],
})
export class UsersModule {}
`,
}

func tsNode(t *testing.T, result AnalysisResult, label string) Node {
	t.Helper()
	for _, node := range result.Nodes {
		if node.Label == label {
			return node
		}
	}
	var labels []string
	for _, node := range result.Nodes {
		labels = append(labels, node.Label)
	}
	t.Fatalf("no node labelled %q; have %v", label, labels)
	return Node{}
}

func TestTypeScriptAnalyzerReadsANestProject(t *testing.T) {
	result := analyzeTS(t, nestFixture)

	controller := tsNode(t, result, "src/users/users.controller.UsersController")
	if controller.Kind != "class" {
		t.Errorf("controller kind = %q, want class", controller.Kind)
	}
	if controller.Layer != "controller" {
		t.Errorf("controller layer = %q", controller.Layer)
	}
	if controller.Summary != "HTTP surface for users." {
		t.Errorf("controller summary = %q", controller.Summary)
	}

	// The types TypeScript has and Lua did not: a parameter reads as
	// "dto: CreateUserDto" rather than as a bare name.
	create := tsNode(t, result, "src/users/users.controller.UsersController.create")
	if len(create.Params) != 1 || create.Params[0].Name != "dto" || create.Params[0].Type != "CreateUserDto" {
		t.Errorf("create params = %+v, want dto: CreateUserDto", create.Params)
	}

	service := tsNode(t, result, "src/users/users.service.UsersService")
	if service.Layer != "service" {
		t.Errorf("service layer = %q", service.Layer)
	}
	// @Injectable marks a repository the same as a service, so the decorator
	// alone cannot tell them apart — what it injects can.
	repository := tsNode(t, result, "src/users/users.repository.UsersRepository")
	if repository.Layer != "repository" {
		t.Errorf("repository layer = %q, want repository — it injects a Repository", repository.Layer)
	}
}

func TestTypeScriptAnalyzerFindsNestRoutes(t *testing.T) {
	result := analyzeTS(t, nestFixture)

	byID := map[string]Node{}
	for _, node := range result.Nodes {
		byID[node.ID] = node
	}
	routes := map[string]string{}
	for _, endpoint := range result.Endpoints {
		routes[endpoint.HTTPMethod+" "+endpoint.Path] = byID[endpoint.EntryNode].Label
	}

	for route, want := range map[string]string{
		"GET /users":     "src/users/users.controller.UsersController.list",
		"GET /users/:id": "src/users/users.controller.UsersController.get",
		"POST /users":    "src/users/users.controller.UsersController.create",
	} {
		if routes[route] != want {
			t.Errorf("%s -> %q, want %q; all: %v", route, routes[route], want, routes)
		}
	}
	if len(result.Endpoints) != 3 {
		t.Errorf("endpoints = %d, want 3; %v", len(result.Endpoints), routes)
	}
}

func tsHasEdge(result AnalysisResult, from, to, kind string) bool {
	byID := map[string]string{}
	for _, node := range result.Nodes {
		byID[node.ID] = node.Label
	}
	for _, edge := range result.Edges {
		if byID[edge.From] == from && byID[edge.To] == to && edge.Kind == kind {
			return true
		}
	}
	return false
}

func TestTypeScriptAnalyzerResolvesCallsThroughTheChecker(t *testing.T) {
	result := analyzeTS(t, nestFixture)

	for _, edge := range [][2]string{
		{"src/users/users.controller.UsersController.list", "src/users/users.service.UsersService.list"},
		{"src/users/users.controller.UsersController.get", "src/users/users.service.UsersService.get"},
		{"src/users/users.service.UsersService.list", "src/users/users.repository.UsersRepository.findAll"},
		{"src/users/users.service.UsersService.get", "src/users/users.repository.UsersRepository.findById"},
	} {
		if !tsHasEdge(result, edge[0], edge[1], "call") {
			t.Errorf("expected a call edge %s -> %s", edge[0], edge[1])
		}
	}
}

// Constructor injection is the relationship no call expression shows, and the
// one that makes a Nest project readable.
func TestTypeScriptAnalyzerRecordsConstructorInjection(t *testing.T) {
	result := analyzeTS(t, nestFixture)

	if !tsHasEdge(result, "src/users/users.controller.UsersController", "src/users/users.service.UsersService", "di") {
		t.Error("expected a di edge from the controller to the service it injects")
	}
	if !tsHasEdge(result, "src/users/users.service.UsersService", "src/users/users.repository.UsersRepository", "di") {
		t.Error("expected a di edge from the service to the repository it injects")
	}
	// A type that resolves outside the project is nobody's node.
	if tsHasEdge(result, "src/users/users.repository.UsersRepository", "", "di") {
		t.Error("a library type must not produce an edge")
	}
}

func TestTypeScriptAnalyzerFollowsImportsForBlastRadius(t *testing.T) {
	result := analyzeTS(t, nestFixture)

	want := map[string]string{
		"src/users/users.controller.ts": "src/users/users.service.ts",
		"src/users/users.service.ts":    "src/users/users.repository.ts",
	}
	found := map[string]bool{}
	for _, dep := range result.FileDependencies {
		if want[dep.FilePath] == dep.DependsOnFilePath {
			found[dep.FilePath] = true
		}
	}
	for file := range want {
		if !found[file] {
			t.Errorf("expected %s to depend on %s; got %+v", file, want[file], result.FileDependencies)
		}
	}
}

func TestTypeScriptAnalyzerRecordsFieldAccess(t *testing.T) {
	result := analyzeTS(t, nestFixture)

	class := tsNode(t, result, "src/users/users.repository.UsersRepository")
	var reads, writes int
	for _, access := range result.FieldAccesses {
		if access.ClassID != class.ID || access.FieldName != "cached" {
			continue
		}
		switch access.Kind {
		case "read":
			reads++
		case "write":
			writes++
		}
	}
	if reads != 1 || writes != 1 {
		t.Errorf("cached: %d reads, %d writes, want one of each; %+v", reads, writes, result.FieldAccesses)
	}
}

// The seam: a framework that shares nothing with NestJS — no decorators, no
// classes, no injection — expressed through the same interface.
func TestTypeScriptAnalyzerReadsNextRouteHandlers(t *testing.T) {
	result := analyzeTS(t, map[string]string{
		"tsconfig.json": tsconfig(),
		"package.json":  `{"name": "web", "dependencies": {"next": "^14.0.0"}}`,
		"src/app/api/users/route.ts": `
export async function GET() {
  return { users: [] };
}

export async function POST(request: Request) {
  return { created: true };
}
`,
		"src/app/(marketing)/api/health/route.ts": `
export async function GET() {
  return { ok: true };
}
`,
	})

	routes := map[string]bool{}
	for _, endpoint := range result.Endpoints {
		routes[endpoint.HTTPMethod+" "+endpoint.Path] = true
	}
	for _, want := range []string{"GET /api/users", "POST /api/users", "GET /api/health"} {
		if !routes[want] {
			t.Errorf("missing %s; got %v", want, routes)
		}
	}
	// A route group organises files, not URLs.
	for route := range routes {
		if strings.Contains(route, "(marketing)") {
			t.Errorf("a route group leaked into the path: %s", route)
		}
	}
}
