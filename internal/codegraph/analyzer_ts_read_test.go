// internal/codegraph/analyzer_ts_read_test.go
package codegraph

import (
	"strings"
	"testing"

	"devkit/internal/lsp"
)

func linesOf(source string) []string { return strings.Split(source, "\n") }

// Decorators are read out of the window the server gives — from where a
// declaration starts to where its name is — so the test feeds exactly that.
func TestReadsDecorators(t *testing.T) {
	source := `@Controller('users')
export class UsersController {`
	got := tsReadDecorators(linesOf(source), 0, 1)
	if len(got) != 1 {
		t.Fatalf("decorators = %+v, want one", got)
	}
	if got[0].Name != "Controller" || got[0].StringArg(0) != "users" {
		t.Errorf("decorator = %+v", got[0])
	}
}

func TestReadsAMultiLineDecorator(t *testing.T) {
	source := `@Module({
  controllers: [UsersController],
  providers: [UsersService, UsersRepository],
})
export class UsersModule {}`
	got := tsReadDecorators(linesOf(source), 0, 4)
	if len(got) != 1 || got[0].Name != "Module" {
		t.Fatalf("decorators = %+v, want one Module", got)
	}
	// The object is one argument however many commas are inside it.
	if len(got[0].Args) != 1 {
		t.Errorf("args = %q, want the object as one", got[0].Args)
	}
}

func TestReadsStackedDecorators(t *testing.T) {
	source := `@Get(':id')
  @UseGuards(AuthGuard)
  @ApiOperation({ summary: 'get, by id' })
  async get(`
	got := tsReadDecorators(linesOf(source), 0, 3)
	var names []string
	for _, decorator := range got {
		names = append(names, decorator.Name)
	}
	if strings.Join(names, ",") != "Get,UseGuards,ApiOperation" {
		t.Errorf("names = %v", names)
	}
	if got[0].StringArg(0) != ":id" {
		t.Errorf("route = %q", got[0].StringArg(0))
	}
}

// A comma inside a route path, a string, or a comment must not look like an
// argument separator.
func TestDecoratorArgumentsSurviveCommasThatAreNotSeparators(t *testing.T) {
	source := `@Get('a,b') // and, a comment
  foo(`
	got := tsReadDecorators(linesOf(source), 0, 1)
	if len(got) != 1 || got[0].StringArg(0) != "a,b" {
		t.Fatalf("decorator = %+v", got)
	}
}

func TestReadsParametersWithTypes(t *testing.T) {
	lines := linesOf(`  async create(@Body() dto: CreateUserDto, @Param('id') id: string, flag = true) {`)
	params := tsReadParams(lines, lsp.Position{Line: 0, Character: 8})
	want := []Param{
		{Name: "dto", Type: "CreateUserDto"},
		{Name: "id", Type: "string"},
		{Name: "flag"},
	}
	if len(params) != len(want) {
		t.Fatalf("params = %+v, want %+v", params, want)
	}
	for i := range want {
		if params[i] != want[i] {
			t.Errorf("param %d = %+v, want %+v", i, params[i], want[i])
		}
	}
}

// A constructor's parameter types are a NestJS class's dependencies, so the
// modifiers that make a parameter a property must not end up in the name.
func TestReadsConstructorInjection(t *testing.T) {
	lines := linesOf(`  constructor(private readonly repo: Repository<User, Id>, public svc: Svc) {}`)
	params := tsReadParams(lines, lsp.Position{Line: 0, Character: 2})
	if len(params) != 2 {
		t.Fatalf("params = %+v, want 2 — a generic's comma is not a separator", params)
	}
	if params[0].Name != "repo" || params[0].Type != "Repository<User, Id>" {
		t.Errorf("first = %+v", params[0])
	}
	if params[1].Name != "svc" || params[1].Type != "Svc" {
		t.Errorf("second = %+v", params[1])
	}
}

func TestReadsJSDocAsASummary(t *testing.T) {
	source := `/**
 * Serves the user routes.
 *
 * Not this paragraph.
 * @see something
 */
@Controller()
export class C {}`
	if got := tsReadDoc(linesOf(source), 6); got != "Serves the user routes." {
		t.Errorf("doc = %q", got)
	}
}

func TestFindsImportsAndTheirLines(t *testing.T) {
	source := `import { Controller, Get } from '@nestjs/common';
import {
  UsersService,
} from './users.service';
export * from './dto';
export class NotAnImport {}
const x = require('legacy');`

	inImport, imports := tsImportLines(linesOf(source))
	var specs []string
	for _, i := range imports {
		specs = append(specs, i.Spec)
	}
	if strings.Join(specs, ",") != "@nestjs/common,./users.service,./dto,legacy" {
		t.Errorf("specifiers = %v", specs)
	}
	// Every line of the multi-line import counts, because tsgo reports each
	// imported name as a symbol of the file and they have to be filtered out.
	for _, line := range []int{0, 1, 2, 3, 4} {
		if !inImport[line] {
			t.Errorf("line %d should be part of an import statement", line)
		}
	}
	if inImport[5] {
		t.Error("an export of a declaration is not an import statement")
	}

	// The specifier's own position is what a definition request is made at.
	line := linesOf(source)[0]
	if got := line[imports[0].SpecChar : imports[0].SpecChar+len("@nestjs/common")]; got != "@nestjs/common" {
		t.Errorf("specifier position points at %q", got)
	}
}
