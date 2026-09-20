// internal/codegraph/analyzer_ts_read_test.go
package codegraph

import (
	"reflect"
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
		{Name: "dto", Type: "CreateUserDto", Source: ParamSourceBody},
		{Name: "id", Type: "string", Source: ParamSourcePath},
		// No decorator at all: the source stays empty, which says this
		// analyzer cannot tell rather than "not part of the request".
		{Name: "flag"},
	}
	if len(params) != len(want) {
		t.Fatalf("params = %+v, want %+v", params, want)
	}
	for i := range want {
		if !reflect.DeepEqual(params[i], want[i]) {
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

// Where a handler parameter's value comes from, which is what a drafted
// request is built out of. NestJS's decorators are the same shapes Spring's
// annotations are, and they are read the same way.
func TestReadsWhereEachParameterComesFromOnTheWire(t *testing.T) {
	lines := linesOf("  create(@Body() dto: CreateUserDto, @Param('id') id: string, " +
		"@Query('page_size') pageSize: number, @Headers('x-tenant') tenant: string, " +
		"@Req() request: Request, @CurrentUser() user: User) {")
	params := tsReadParams(lines, lsp.Position{Line: 0, Character: 2})
	want := []Param{
		{Name: "dto", Type: "CreateUserDto", Source: ParamSourceBody},
		{Name: "id", Type: "string", Source: ParamSourcePath},
		// The decorator renames it, so the wire name is not the parameter's.
		{Name: "pageSize", Type: "number", Source: ParamSourceQuery, WireName: "page_size"},
		{Name: "tenant", Type: "string", Source: ParamSourceHeader, WireName: "x-tenant"},
		// The framework handing the handler something that is not a value on
		// the wire.
		{Name: "request", Type: "Request", Source: ParamSourceOther},
		// A project's own decorator is not in the table, so the answer is "I
		// cannot tell" rather than "not part of the request".
		{Name: "user", Type: "User"},
	}
	if len(params) != len(want) {
		t.Fatalf("params = %+v, want %+v", params, want)
	}
	for i := range want {
		if !reflect.DeepEqual(params[i], want[i]) {
			t.Errorf("param %d = %+v, want %+v", i, params[i], want[i])
		}
	}
}

// A decorator whose argument names the same thing the parameter is called
// carries no wire name: it would be noise on every draft.
func TestAWireNameThatMatchesTheParameterIsNotRecorded(t *testing.T) {
	lines := linesOf("  get(@Param('id') id: string) {")
	params := tsReadParams(lines, lsp.Position{Line: 0, Character: 2})
	if len(params) != 1 || params[0].WireName != "" {
		t.Fatalf("params = %+v", params)
	}
}

// The fields a body skeleton is built from. A DTO's methods are not fields,
// and a skeleton made of them is worse than an empty one.
func TestReadsAClassesOwnFieldsAndNotItsMethods(t *testing.T) {
	lines := linesOf(`export class CreateUserDto {
  @IsString()
  readonly email: string;
  age?: number;
  tags: string[] = [];
  private secret!: string;
  static KIND = 'user';
  greet(name: string): string {
    return name;
  }
}`)
	fields := tsReadProperties(lines, 1, 11)
	want := []Field{
		{Name: "email", Type: "string"},
		{Name: "age", Type: "number"},
		// The default value is not part of the shape.
		{Name: "tags", Type: "string[]"},
		{Name: "secret", Type: "string"},
	}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("fields = %+v, want %+v", fields, want)
	}
}

func TestBareTypeNameStripsWhatALookupCannotUse(t *testing.T) {
	for written, want := range map[string]string{
		"CreateUserDto":             "CreateUserDto",
		"CreateUserDto[]":           "CreateUserDto",
		"Array<CreateUserDto>":      "Array",
		"dto.CreateUserDto":         "CreateUserDto",
		"CreateUserDto | undefined": "CreateUserDto",
	} {
		if got := tsBareTypeName(written); got != want {
			t.Errorf("tsBareTypeName(%q) = %q, want %q", written, got, want)
		}
	}
}
