package model

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestMaterialRoleColumnsFitAllowedValues(t *testing.T) {
	longest := 0
	for _, role := range MaterialRoles {
		if len(role) > longest {
			longest = len(role)
		}
	}

	for name, value := range map[string]any{
		"Material":       Material{},
		"CollectionItem": CollectionItem{},
	} {
		field, ok := reflect.TypeOf(value).FieldByName("Role")
		if !ok {
			t.Fatalf("%s.Role not found", name)
		}
		size, err := gormSize(field.Tag.Get("gorm"))
		if err != nil {
			t.Fatalf("%s.Role: %v", name, err)
		}
		if size < longest {
			t.Errorf("%s.Role column size is %d; longest allowed role requires %d", name, size, longest)
		}
	}
}

func gormSize(tag string) (int, error) {
	for _, part := range strings.Split(tag, ";") {
		value, ok := strings.CutPrefix(part, "size:")
		if !ok {
			continue
		}
		return strconv.Atoi(value)
	}
	return 0, fmt.Errorf("missing gorm size tag")
}
