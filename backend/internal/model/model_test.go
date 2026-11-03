package model

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestEnumColumnsFitAllowedValues(t *testing.T) {
	cases := []struct {
		value  any
		field  string
		values []string
	}{
		{Material{}, "Part", MaterialParts},
		{Material{}, "Stage", MaterialStages},
		{Pack{}, "DocType", DocTypes},
		{Pack{}, "Grade", Grades},
		{Pack{}, "Term", Terms},
	}
	for _, tc := range cases {
		longest := 0
		for _, v := range tc.values {
			if len(v) > longest {
				longest = len(v)
			}
		}
		field, ok := reflect.TypeOf(tc.value).FieldByName(tc.field)
		if !ok {
			t.Fatalf("%T.%s not found", tc.value, tc.field)
		}
		size, err := gormSize(field.Tag.Get("gorm"))
		if err != nil {
			t.Fatalf("%T.%s: %v", tc.value, tc.field, err)
		}
		if size < longest {
			t.Errorf("%T.%s column size is %d; longest allowed value requires %d", tc.value, tc.field, size, longest)
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
