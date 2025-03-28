package plugins

import (
	"reflect"
	"strconv"
)

type Config struct {
	Instance string `config:"instance"`
}

// ParseConfig takes a map with string keys and string values, and populates
// the fields of the provided struct with the corresponding values from the map.
// It automatically converts the string values to the correct types (e.g., string, int, bool)
// based on the struct's field types. The function supports only primitive types.
func ParseConfig(m map[string]string, s any) error {
	val := reflect.ValueOf(s).Elem()
	typ := val.Type()

	for i := range val.NumField() {
		field := val.Field(i)
		fieldTyp := typ.Field(i)

		// to lookup into the map we first check into the tag config
		// if not present, we fallback to the field name
		fieldName, ok := fieldTyp.Tag.Lookup("config")
		if !ok {
			fieldName = typ.Field(i).Name
		}

		v, ok := m[fieldName]
		if !ok {
			continue
		}

		switch field.Kind() {
		case reflect.String:
			field.SetString(v)
		case reflect.Bool:
			b, err := strconv.ParseBool(v)
			if err != nil {
				return err
			}
			field.SetBool(b)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			i, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return err
			}
			field.SetInt(i)
		}
	}

	return nil
}
