package audit

import (
	"reflect"
	"strings"
	"unicode"
)

const RedactedValue = "[REDACTED]"

var sensitiveKeyParts = []string{
	"password",
	"passphrase",
	"privatekey",
	"secret",
	"token",
	"apikey",
	"recovery",
	"encryptionkey",
}

func RedactMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}

	output := make(map[string]any, len(input))
	for key, value := range input {
		if IsSensitiveKey(key) {
			output[key] = RedactedValue
			continue
		}

		output[key] = redactValue(value)
	}

	return output
}

func IsSensitiveKey(key string) bool {
	normalized := normalizeKey(key)
	for _, part := range sensitiveKeyParts {
		if strings.Contains(normalized, part) {
			return true
		}
	}

	return false
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return RedactMap(typed)
	case []any:
		if typed == nil {
			return typed
		}
		output := make([]any, len(typed))
		for i, item := range typed {
			output[i] = redactValue(item)
		}
		return output
	}

	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() {
		return value
	}

	switch reflected.Kind() {
	case reflect.Map:
		if reflected.Type().Key().Kind() != reflect.String {
			return value
		}
		if reflected.IsNil() {
			return value
		}
		return redactStringKeyedMap(reflected)
	case reflect.Slice:
		if reflected.IsNil() {
			return value
		}
		return redactSequence(reflected)
	case reflect.Array:
		return redactSequence(reflected)
	default:
		return value
	}
}

func redactStringKeyedMap(input reflect.Value) any {
	output := reflect.MakeMapWithSize(input.Type(), input.Len())
	generic := make(map[string]any, input.Len())
	preserveType := true

	for _, key := range input.MapKeys() {
		value := input.MapIndex(key)
		if !value.IsValid() {
			continue
		}

		var redacted any
		if IsSensitiveKey(key.String()) {
			redacted = RedactedValue
		} else {
			redacted = redactValue(value.Interface())
		}

		generic[key.String()] = redacted
		if !preserveType {
			continue
		}

		converted, ok := convertAnyToType(redacted, input.Type().Elem())
		if !ok {
			preserveType = false
			continue
		}
		output.SetMapIndex(key, converted)
	}

	if preserveType {
		return output.Interface()
	}

	return generic
}

func redactSequence(input reflect.Value) any {
	items := make([]any, input.Len())
	preserveType := true

	var output reflect.Value
	if input.Kind() == reflect.Slice {
		output = reflect.MakeSlice(input.Type(), input.Len(), input.Len())
	} else {
		output = reflect.New(input.Type()).Elem()
	}

	for i := 0; i < input.Len(); i++ {
		redacted := redactValue(input.Index(i).Interface())
		items[i] = redacted

		if !preserveType {
			continue
		}

		converted, ok := convertAnyToType(redacted, input.Type().Elem())
		if !ok {
			preserveType = false
			continue
		}
		output.Index(i).Set(converted)
	}

	if preserveType {
		return output.Interface()
	}

	return items
}

func convertAnyToType(value any, targetType reflect.Type) (reflect.Value, bool) {
	redactedValue := reflect.ValueOf(value)
	if !redactedValue.IsValid() {
		return reflect.Zero(targetType), true
	}
	if redactedValue.Type().AssignableTo(targetType) {
		return redactedValue, true
	}
	if redactedValue.Type().ConvertibleTo(targetType) {
		return redactedValue.Convert(targetType), true
	}
	return reflect.Value{}, false
}

func normalizeKey(key string) string {
	trimmed := strings.TrimSpace(key)
	var builder strings.Builder
	builder.Grow(len(trimmed))
	for _, r := range trimmed {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(unicode.ToLower(r))
		}
	}
	return builder.String()
}
