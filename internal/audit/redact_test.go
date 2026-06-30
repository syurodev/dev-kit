package audit

import "testing"

type namedSecretMap map[string]string
type namedSecretSlice []namedSecretMap
type namedSecretArray [2]namedSecretMap
type boolSecretMap map[string]bool
type errorSlice []error

func TestRedactedValue(t *testing.T) {
	if RedactedValue != "[REDACTED]" {
		t.Fatalf("RedactedValue = %q, want %q", RedactedValue, "[REDACTED]")
	}
}

func TestRedactSensitiveKeys(t *testing.T) {
	input := map[string]any{
		"username": "alice",
		"password": "secret",
		"token":    "abc",
		"nested": map[string]any{
			"private_key": "ssh-key",
			"host":        "localhost",
		},
	}

	output := RedactMap(input)

	if output["username"] != "alice" {
		t.Fatalf("username = %v", output["username"])
	}
	if output["password"] != RedactedValue {
		t.Fatalf("password was not redacted")
	}
	if output["token"] != RedactedValue {
		t.Fatalf("token was not redacted")
	}

	nested, ok := output["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested map type = %T", output["nested"])
	}
	if nested["private_key"] != RedactedValue {
		t.Fatalf("private_key was not redacted")
	}
	if nested["host"] != "localhost" {
		t.Fatalf("host = %v", nested["host"])
	}
}

func TestRedactMapPreservesNilMapSemantics(t *testing.T) {
	var input map[string]any

	output := RedactMap(input)

	if output != nil {
		t.Fatalf("output = %#v, want nil map", output)
	}
}

func TestRedactSensitiveKeysInSlicesOfMaps(t *testing.T) {
	input := map[string]any{
		"items": []any{
			map[string]any{
				"private-key": "ssh-key",
				"name":        "primary",
			},
			map[string]any{
				"encryptionKey": "value",
				"enabled":       true,
			},
		},
	}

	output := RedactMap(input)

	items, ok := output["items"].([]any)
	if !ok {
		t.Fatalf("items type = %T", output["items"])
	}

	first, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("items[0] type = %T", items[0])
	}
	if first["private-key"] != RedactedValue {
		t.Fatalf("private-key was not redacted")
	}
	if first["name"] != "primary" {
		t.Fatalf("name = %v", first["name"])
	}

	second, ok := items[1].(map[string]any)
	if !ok {
		t.Fatalf("items[1] type = %T", items[1])
	}
	if second["encryptionKey"] != RedactedValue {
		t.Fatalf("encryptionKey was not redacted")
	}
	if second["enabled"] != true {
		t.Fatalf("enabled = %v", second["enabled"])
	}
}

func TestRedactPreservesNilSliceSemantics(t *testing.T) {
	var inputItems []map[string]any

	output := RedactMap(map[string]any{
		"items": inputItems,
	})

	items, ok := output["items"].([]map[string]any)
	if !ok {
		t.Fatalf("items type = %T", output["items"])
	}
	if items != nil {
		t.Fatalf("items = %#v, want nil slice", items)
	}
}

func TestRedactTraversesArrays(t *testing.T) {
	input := map[string]any{
		"items": [2]map[string]any{
			{
				"private-key": "ssh-key",
				"name":        "primary",
			},
			{
				"token":  "abc",
				"active": true,
			},
		},
	}

	output := RedactMap(input)

	items, ok := output["items"].([2]map[string]any)
	if !ok {
		t.Fatalf("items type = %T", output["items"])
	}
	if items[0]["private-key"] != RedactedValue {
		t.Fatalf("items[0][private-key] was not redacted")
	}
	if items[0]["name"] != "primary" {
		t.Fatalf("items[0][name] = %v", items[0]["name"])
	}
	if items[1]["token"] != RedactedValue {
		t.Fatalf("items[1][token] was not redacted")
	}
	if items[1]["active"] != true {
		t.Fatalf("items[1][active] = %v", items[1]["active"])
	}
}

func TestRedactTypedStringKeyedMaps(t *testing.T) {
	input := map[string]any{
		"plain": map[string]string{
			"password": "secret",
			"name":     "alice",
		},
		"named": namedSecretMap{
			"private-key": "ssh-key",
			"label":       "primary",
		},
	}

	output := RedactMap(input)

	plain, ok := output["plain"].(map[string]string)
	if !ok {
		t.Fatalf("plain type = %T", output["plain"])
	}
	if plain["password"] != RedactedValue {
		t.Fatalf("plain password = %q", plain["password"])
	}
	if plain["name"] != "alice" {
		t.Fatalf("plain name = %q", plain["name"])
	}

	named, ok := output["named"].(namedSecretMap)
	if !ok {
		t.Fatalf("named type = %T", output["named"])
	}
	if named["private-key"] != RedactedValue {
		t.Fatalf("named private-key = %q", named["private-key"])
	}
	if named["label"] != "primary" {
		t.Fatalf("named label = %q", named["label"])
	}
}

func TestRedactTypedStringKeyedMapsInTypedSliceAndArray(t *testing.T) {
	input := map[string]any{
		"slice": namedSecretSlice{
			{
				"token": "abc",
				"name":  "first",
			},
		},
		"array": namedSecretArray{
			{
				"encryptionKey": "value",
				"name":          "left",
			},
			{
				"password": "secret",
				"name":     "right",
			},
		},
	}

	output := RedactMap(input)

	slice, ok := output["slice"].(namedSecretSlice)
	if !ok {
		t.Fatalf("slice type = %T", output["slice"])
	}
	if slice[0]["token"] != RedactedValue {
		t.Fatalf("slice token = %q", slice[0]["token"])
	}
	if slice[0]["name"] != "first" {
		t.Fatalf("slice name = %q", slice[0]["name"])
	}

	array, ok := output["array"].(namedSecretArray)
	if !ok {
		t.Fatalf("array type = %T", output["array"])
	}
	if array[0]["encryptionKey"] != RedactedValue {
		t.Fatalf("array[0] encryptionKey = %q", array[0]["encryptionKey"])
	}
	if array[0]["name"] != "left" {
		t.Fatalf("array[0] name = %q", array[0]["name"])
	}
	if array[1]["password"] != RedactedValue {
		t.Fatalf("array[1] password = %q", array[1]["password"])
	}
	if array[1]["name"] != "right" {
		t.Fatalf("array[1] name = %q", array[1]["name"])
	}
}

func TestRedactTypedNonStringKeyedMapWidensWithoutLeakingSecret(t *testing.T) {
	input := map[string]any{
		"flags": boolSecretMap{
			"token":  true,
			"active": false,
		},
	}

	output := RedactMap(input)

	flags, ok := output["flags"].(map[string]any)
	if !ok {
		t.Fatalf("flags type = %T", output["flags"])
	}
	if flags["token"] != RedactedValue {
		t.Fatalf("flags token = %#v", flags["token"])
	}
	if flags["token"] == true {
		t.Fatalf("flags token leaked original secret")
	}
	if flags["active"] != false {
		t.Fatalf("flags active = %#v", flags["active"])
	}
}

func TestRedactPreservesNilInterfaceElementsInReflectedContainer(t *testing.T) {
	input := map[string]any{
		"errors": errorSlice{nil},
	}

	output := RedactMap(input)

	errors, ok := output["errors"].(errorSlice)
	if !ok {
		t.Fatalf("errors type = %T", output["errors"])
	}
	if len(errors) != 1 {
		t.Fatalf("len(errors) = %d", len(errors))
	}
	if errors[0] != nil {
		t.Fatalf("errors[0] = %#v, want nil", errors[0])
	}
}

func TestIsSensitiveKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  string
		want bool
	}{
		{key: "password", want: true},
		{key: " API_KEY ", want: true},
		{key: "userPrivateKeyPath", want: true},
		{key: "private-key", want: true},
		{key: "encryptionKey", want: true},
		{key: "username", want: false},
		{key: "host", want: false},
	}

	for _, tc := range tests {
		if got := IsSensitiveKey(tc.key); got != tc.want {
			t.Fatalf("IsSensitiveKey(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}
