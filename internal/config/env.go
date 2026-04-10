package config

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

func ApplyEnvOverrides(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	return applyEnv(reflect.ValueOf(cfg).Elem(), "MONSOON")
}

func applyEnv(v reflect.Value, prefix string) error {
	t := v.Type()
	for i := range v.NumField() {
		field := v.Field(i)
		fieldType := t.Field(i)

		yamlName := strings.Split(fieldType.Tag.Get("yaml"), ",")[0]
		if yamlName == "" || yamlName == "-" {
			yamlName = toSnake(fieldType.Name)
		}

		name := prefix + "_" + strings.ToUpper(strings.ReplaceAll(yamlName, "-", "_"))

		if field.Kind() == reflect.Struct && fieldType.Type != reflect.TypeOf(time.Time{}) && fieldType.Type != reflect.TypeOf(Duration{}) {
			if err := applyEnv(field, name); err != nil {
				return err
			}
			continue
		}

		val, ok := os.LookupEnv(name)
		if !ok {
			continue
		}
		if err := setValue(field, val); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func setValue(field reflect.Value, raw string) error {
	switch field.Interface().(type) {
	case Duration:
		d, err := time.ParseDuration(strings.TrimSpace(raw))
		if err != nil {
			return err
		}
		field.Set(reflect.ValueOf(Duration{Duration: d}))
		return nil
	}

	switch field.Kind() {
	case reflect.String:
		field.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		field.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		iv, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return err
		}
		field.SetInt(iv)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uv, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return err
		}
		field.SetUint(uv)
	case reflect.Slice:
		if field.Type().Elem().Kind() == reflect.String {
			parts := strings.Split(raw, ",")
			result := make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					result = append(result, p)
				}
			}
			field.Set(reflect.ValueOf(result))
			return nil
		}
		return fmt.Errorf("unsupported slice type: %s", field.Type().String())
	default:
		return fmt.Errorf("unsupported kind: %s", field.Kind().String())
	}
	return nil
}

func toSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}
